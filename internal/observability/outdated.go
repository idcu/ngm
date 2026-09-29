package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
)

// reSemver 识别形如 `v1.2.3` / `1.2.3` 的版本标签。
var reSemver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)([-+].*)?$`)

// OutdatedEntry 是一个依赖的更新检查结果。
type OutdatedEntry struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	RefType string `json:"refType"`
	Commit  string `json:"commit"`

	// Latest 是发现的最新版本；为空表示无法获知。
	Latest string `json:"latest,omitempty"`
	// LatestRule 说明 Latest 是怎么选出来的：semver / tag-date / branch 为空表示未判定。
	LatestRule string `json:"latestRule,omitempty"`
	// Update 为真表示确实有更新。
	Update bool `json:"update"`
	// Behind 是分支落后的提交数；-1 表示未知。
	Behind int `json:"behind"`
	// Stale 表示**无法判定**（离线或取不到远端），不是"已是最新"。
	Stale bool   `json:"stale"`
	Note  string `json:"note,omitempty"`
}

// OutdatedReport 是 `ngm outdated` 的报告。
type OutdatedReport struct {
	Dependencies int             `json:"dependencies"`
	Updates      int             `json:"updates"`
	Stale        int             `json:"stale"`
	Offline      bool            `json:"offline"`
	Entries      []OutdatedEntry `json:"entries"`
}

// OutdatedOptions 是一次更新检查的输入。
type OutdatedOptions struct {
	GitOpts git.Options
	// Offline 为真时绝不访问网络：branch 依赖一律标 stale。
	Offline bool
	// EnsureMirror 返回某依赖的本地 mirror 路径（由调用方注入，便于测试）。
	EnsureMirror func(ctx context.Context, slug string) (string, error)
}

// CheckOutdated 检查 lock 中每个依赖是否有更新。
//
// 三条 refType 的判定（observability.md §ngm outdated）：
//
//	tag     列举 mirror 中的 tag，取最新（优先按 semver，无 semver 时按 tag 创建时间）
//	branch  需要 fetch 才能知道最新提交；离线时**标 stale**
//	commit  已锁定到具体 commit，不存在"更新"这个概念
//
// 最重要的纪律：**取不到远端时不许说"没有更新"**。Stale 与 Update=false 是
// 两种不同的结论——前者是"不知道"，后者是"查过，确实没有"。
func CheckOutdated(ctx context.Context, lf *lock.File, opts OutdatedOptions) (*OutdatedReport, error) {
	if lf == nil {
		return nil, fmt.Errorf("no ngm.lock to check; run `ngm install` first")
	}
	rep := &OutdatedReport{Dependencies: len(lf.Dependencies), Offline: opts.Offline}

	for _, d := range lf.Dependencies {
		e := OutdatedEntry{
			Name:    d.Name,
			Ref:     d.Ref,
			RefType: d.RefType,
			Commit:  d.Commit,
			Behind:  -1,
		}
		switch d.RefType {
		case "commit":
			e.Note = "pinned to a commit; no update to look for"
		default:
			mirrorPath, err := opts.EnsureMirror(ctx, d.Name)
			if err != nil {
				// 取不到 mirror 不算"没有更新"：标 stale 并记下原因
				e.Stale = true
				e.Note = "mirror unavailable: " + err.Error()
				break
			}
			if d.RefType == "branch" {
				checkBranch(ctx, opts, mirrorPath, &e)
			} else {
				if err := checkTag(ctx, opts, mirrorPath, &e); err != nil {
					return nil, err
				}
			}
		}
		if e.Update {
			rep.Updates++
		}
		if e.Stale {
			rep.Stale++
		}
		rep.Entries = append(rep.Entries, e)
	}
	return rep, nil
}

func checkTag(ctx context.Context, opts OutdatedOptions, mirrorPath string, e *OutdatedEntry) error {
	// 按创建时间倒序列出 tag：semver 不可用时用它做兜底排序
	res, err := git.Run(ctx, opts.GitOpts, "-C", mirrorPath,
		"for-each-ref", "--sort=-creatordate", "--format=%(refname:short)", "refs/tags")
	if err != nil {
		return err
	}
	tags := []string{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			tags = append(tags, t)
		}
	}
	if len(tags) == 0 {
		e.Stale = true
		e.Note = "no tags in the local mirror"
		return nil
	}

	if best, ok := newestSemver(tags); ok {
		e.Latest = best
		e.LatestRule = "semver"
	} else {
		e.Latest = tags[0]
		e.LatestRule = "tag-date"
		e.Note = "no semver tags; newest by tag creation date"
	}
	e.Update = e.Latest != e.Ref && isNewer(e.Ref, e.Latest)
	return nil
}

func checkBranch(ctx context.Context, opts OutdatedOptions, mirrorPath string, e *OutdatedEntry) {
	if opts.Offline {
		e.Stale = true
		e.Note = "offline: the branch tip is unknown, not 'up to date'"
		return
	}
	if err := git.FetchMirror(ctx, opts.GitOpts, mirrorPath); err != nil {
		e.Stale = true
		e.Note = "could not fetch: " + err.Error()
		return
	}
	res, err := git.Run(ctx, opts.GitOpts, "-C", mirrorPath, "rev-parse", "refs/heads/"+e.Ref)
	if err != nil {
		e.Stale = true
		e.Note = "branch not present in the mirror: " + err.Error()
		return
	}
	tip := strings.TrimSpace(string(res.Stdout))
	e.Latest = tip
	e.LatestRule = "branch"
	e.Update = tip != e.Commit
	if e.Update {
		// 落后多少提交是一个具体数字，能给出时尽量给
		if cnt, cerr := git.Run(ctx, opts.GitOpts, "-C", mirrorPath,
			"rev-list", "--count", e.Commit+".."+tip); cerr == nil {
			if n, perr := strconv.Atoi(strings.TrimSpace(string(cnt.Stdout))); perr == nil {
				e.Behind = n
			}
		}
	} else {
		e.Behind = 0
	}
}

// newestSemver 在能解析为 semver 的 tag 中取最大者。
func newestSemver(tags []string) (string, bool) {
	best := ""
	var bk [3]int
	found := false
	for _, t := range tags {
		m := reSemver.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		n := [3]int{atoi(m[1]), atoi(m[2]), atoi(m[3])}
		if !found || gt(n, bk) {
			best, bk, found = t, n, true
		}
	}
	return best, found
}

// isNewer 判定 latest 是否比 current 新；任一不是 semver 时退化为"不相等"。
func isNewer(current, latest string) bool {
	cm, lm := reSemver.FindStringSubmatch(current), reSemver.FindStringSubmatch(latest)
	if cm == nil || lm == nil {
		return current != latest
	}
	cn := [3]int{atoi(cm[1]), atoi(cm[2]), atoi(cm[3])}
	ln := [3]int{atoi(lm[1]), atoi(lm[2]), atoi(lm[3])}
	return gt(ln, cn)
}

func gt(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// Marshal 输出机器可读报告。
func (r *OutdatedReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Render 渲染人类可读报告（结构对齐 observability.md §ngm outdated）。
//
// Stale 行显示为 `unknown`，**绝不**写 `no`——那正是这条命令最容易骗人的地方。
func (r *OutdatedReport) Render(w io.Writer) {
	fmt.Fprintf(w, "%-24s %-12s %-12s %-8s %s\n", "Package", "Current", "Latest", "Type", "Drift")
	for _, e := range r.Entries {
		latest := e.Latest
		if latest == "" {
			latest = "-"
		}
		if len(latest) > 12 {
			latest = latest[:12]
		}
		drift := "no"
		switch {
		case e.Stale:
			drift = "unknown"
		case e.Update && e.Behind > 0:
			drift = fmt.Sprintf("yes (%d commits)", e.Behind)
		case e.Update:
			drift = "yes"
		}
		fmt.Fprintf(w, "%-24s %-12s %-12s %-8s %s\n", e.Name, e.Ref, latest, e.RefType, drift)
	}
	fmt.Fprintf(w, "\n%d dependency(s) checked", r.Dependencies)
	if r.Updates > 0 {
		fmt.Fprintf(w, "; %d with updates", r.Updates)
	}
	if r.Stale > 0 {
		fmt.Fprintf(w, "; %d unknown (could not be determined - not 'up to date')", r.Stale)
	}
	fmt.Fprintln(w)
}
