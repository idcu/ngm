package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/observability"
)

const whyUsage = `ngm why — why is this dependency here?

USAGE:
  ngm why <dep> [--dir=<dir>] [--json] [--all]
  ngm why --digest=<sha256[:]> [--dir=<dir>] [--json]

ARGS:
  <dep>   dependency slug, e.g. github:org/utils (or github:org/repo#sub for a
          monorepo sub-path); not needed with --digest

FLAGS:
  --dir     project directory containing ngm.json (default: .)
  --json    write a machine-readable report to stdout (CI should use this)
  --all     list every path, lifting the default cap (see below)
  --digest  reverse-lookup lock entries by content digest (read-only graph audit)

OUTPUT:
  The paths from ngm.json to the dependency. A dependency pulled in by several
  parents shows several paths - that is the point of the question.

  Since v0.73 the graph is stored in ngm.lock, so this command reads the lock and
  answers WITHOUT touching git (no sub-processes, works offline). If the lock was
  written by an older ngm (no graph yet), it falls back to resolving upstream
  manifests - and the report's last line says which of the two it used.

  The path count can be EXPONENTIAL in a wide graph (41 nodes produced over a
  million paths in a measurement), and the graph's shape comes from the upstream
  manifests, not from this project. So at most 64 paths are enumerated by
  default, and the report SAYS SO when it stops ("还有更多未列出" in text,
  pathsTruncated in --json) - truncation is never silent. --all lifts the cap.

DIGEST LOOKUP (--digest):
  Read-only audit face (ADR-055): list the ngm.lock entries whose content digest
  (archiveDigest / moduleDigest / dirDigest) matches the given sha256, with each
  entry's subPath and requiredBy. Accepts a full digest or a prefix of at least
  12 hex digits (with or without the "sha256:" prefix). The report states how
  many entries and distinct digests matched and whether the prefix is ambiguous.
  No network, no git subprocesses, no writes. --json emits the same report.

EXIT CODES:
  0  the dependency is in the graph
  3  configuration error, or the dependency is not in the graph
	6  internal failure: the conclusion was reached but could not be delivered (writing stdout, encoding JSON, reading stdin), or ngm panicked
`

// runWhy 处理 `ngm why`。
func runWhy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("why", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	allPaths := fs.Bool("all", false, "list every path (lifts the default cap)")
	digestFlag := fs.String("digest", "", "reverse lookup lock entries by content digest (read-only audit)")

	c := newCmdContext(ctx, args, stdout, stderr, fs)
	c.usage(whyUsage)
	if !c.parse([]flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true}, {Name: "all", Bool: true},
		{Name: "digest"},
	}, jsonOut) {
		return 3
	}
	ctx = c.ctx
	// V216：digest 反查是独立的只读模式，不解析 <dep>、不建图、不碰网络。
	if strings.TrimSpace(*digestFlag) != "" {
		if fs.NArg() != 0 {
			return c.rejectExtraArgs(whyUsage, "`ngm why --digest` takes no dependency slug")
		}
		return runWhyDigest(c, *dirFlag, *digestFlag, *jsonOut)
	}
	if fs.NArg() != 1 {
		// v0.108：`--json` 下也要给信封。
		//
		// 此前这一支只打 usage 就 return，于是 `ngm why --json`（漏给依赖）
		// 的 stdout 是 **0 字节**——用户明明要机器可读输出，却什么也没拿到。
		// v0.54 修的是 `fs.Parse` 失败那一支，位置参数这一支它没有覆盖。
		failEarly(args, stdout, "expected exactly one dependency slug",
			"run `ngm why <dep>`; the slug is the one in ngm.json, e.g. `github:org/repo`")
		fmt.Fprint(stderr, whyUsage)
		return 3
	}
	target, terr := resolve.ParseTarget(fs.Arg(0))
	if terr != nil {
		return runErr(ctx, stdout, stderr, terr)
	}
	targetKey := target.Key()

	env, code := c.env(*dirFlag)
	if code != 0 {
		return code
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// v0.73（ADR-028）：lock 里带着依赖图时**不重新解析**——"它从哪来"的答案
	// 早就写进 lock 了，为此再跑一串 git 子进程（且离线不可用）是纯浪费。
	// 两条路径的代价差一个量级，因此来源必须写进报告（WhyReport.Source），
	// 绝不静默回退：旧 lock 没有图时，用户会看到"重新解析"那一行。
	var g *resolve.Graph
	source := observability.WhySourceResolved
	if lf != nil && lf.HasGraph() {
		g = observability.GraphFromLock(lf)
		source = observability.WhySourceLock
	} else {
		var rerr error
		g, rerr = resolve.ResolveGraph(ctx, patchedSpecs(pf), env.GraphOptions())
		if rerr != nil {
			return runErr(ctx, stdout, stderr, rerr)
		}
	}
	node, terr := observability.ResolveTarget(g, targetKey)
	if terr != nil {
		// 不在图里是"问错了对象"，属用法/配置错误：exit 3 而不是静默成功
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			terr.Error(), "run `ngm tree` to see what is actually in the graph", terr))
	}

	// 路径枚举默认有上界（见 MaxWhyPaths 与 ADR-024）：路径数是**指数**的，
	// 而图的形状来自上游清单。`--all` 是显式解除，不是默认行为；
	// 达到上限时报告会**说出来**（PathsTruncated），绝不静默截断。
	limit := observability.MaxWhyPaths
	if *allPaths {
		limit = 0
	}
	paths, truncated := observability.FindPaths(g, node.Key, limit)

	rep := &observability.WhyReport{
		Name:           node.Name,
		Ref:            node.Ref,
		RefType:        string(node.RefType),
		Commit:         node.Commit,
		SubPath:        node.SubPath,
		RootDeclared:   node.RootDeclared,
		Paths:          paths,
		PathsTruncated: truncated,
		PathsLimit:     limit,
		Locked:         observability.LockedFrom(lf, node.Name, node.SubPath),
		Source:         source,
	}

	if *jsonOut {
		return c.writeReport(rep, 0)
	}

	rep.Render(stdout)
	return 0
}

// ---- V216：按 digest 反查（只读审计面，ADR-055）----

const digestAuditMinHex = 12

// digestQuery 是归一化后的反查输入：Hex 为小写裸 hex，Prefix 表示它是前缀而非完整摘要。
type digestQuery struct {
	Raw    string
	Hex    string
	Prefix bool
}

// parseDigestQuery 接受 `sha256:<hex>` 或裸 hex；至少 12 位、至多 64 位。
func parseDigestQuery(raw string) (digestQuery, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return digestQuery{}, errs.New(errs.CodeConfigInvalid,
			"empty --digest query", "pass a sha256 content digest, e.g. `sha256:9f86d0…`")
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		algo := s[:i]
		if !strings.EqualFold(algo, digest.Algorithm) {
			return digestQuery{}, errs.New(errs.CodeConfigInvalid,
				"unsupported digest algorithm in --digest: "+algo,
				"content digests use `sha256:`; subtreeOid (sha1:) is a Git binding anchor, not a content digest")
		}
		s = s[i+1:]
	}
	if len(s) < digestAuditMinHex || len(s) > 64 {
		return digestQuery{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("--digest needs 12..64 hex digits (got %d)", len(s)),
			"use at least 12 hex digits for a prefix query, or the full 64 for an exact match")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return digestQuery{}, errs.New(errs.CodeConfigInvalid,
			"--digest contains a non-hex character",
			"write only hex digits after the optional `sha256:` prefix")
	}
	return digestQuery{Raw: raw, Hex: strings.ToLower(s), Prefix: len(s) < 64}, nil
}

// runWhyDigest 只读 ngm.lock：不读上游清单、不解析图、不执行 git/网络。
func runWhyDigest(c *cmdContext, dir, raw string, jsonOut bool) int {
	q, err := parseDigestQuery(raw)
	if err != nil {
		return c.fail(err)
	}
	env, code := c.env(dir)
	if code != 0 {
		return code
	}
	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return c.fail(err)
	}
	if lf == nil {
		return c.fail(errs.New(errs.CodeConfigInvalid,
			"no ngm.lock found for the digest audit",
			"run `ngm install` in this directory first; digest lookup reads only ngm.lock"))
	}
	rep := auditDigest(lf, q)
	if jsonOut {
		return c.writeReport(rep, 0)
	}
	rep.Render(c.stdout)
	return 0
}

// digestAuditReport 是 --digest 的机器可读报告；与 --json 契约一致（2 空格缩进 + 末尾换行）。
type digestAuditReport struct {
	Query           string             `json:"query"`
	NormalizedQuery string             `json:"normalizedQuery"`
	Prefix          bool               `json:"prefix"`
	MatchCount      int                `json:"matchCount"`
	DistinctDigests int                `json:"distinctDigests"`
	Ambiguous       bool               `json:"ambiguous"`
	Source          string             `json:"source"`
	Matches         []digestAuditMatch `json:"matches"`
}

type digestAuditMatch struct {
	Name        string   `json:"name"`
	Key         string   `json:"key"`
	SubPath     string   `json:"subPath,omitempty"`
	Digest      string   `json:"digest"`
	DigestField string   `json:"digestField"`
	RefType     string   `json:"refType,omitempty"`
	RequiredBy  []string `json:"requiredBy"`
}

func auditDigest(lf *lock.File, q digestQuery) *digestAuditReport {
	rep := &digestAuditReport{
		Query:           q.Raw,
		NormalizedQuery: digest.Algorithm + ":" + q.Hex,
		Prefix:          q.Prefix,
		Source:          "ngm.lock",
		Matches:         []digestAuditMatch{},
	}
	seen := map[string]bool{}
	for _, d := range lf.Dependencies {
		full := d.ContentDigest()
		if full == "" {
			continue
		}
		hex := strings.ToLower(strings.TrimPrefix(full, digest.Algorithm+":"))
		if !strings.HasPrefix(hex, q.Hex) {
			continue
		}
		seen[hex] = true
		field := "archiveDigest"
		switch {
		case resolve.RefType(d.RefType).IsLocal():
			field = "dirDigest"
		case d.SubPath != "":
			field = "moduleDigest"
		}
		rep.Matches = append(rep.Matches, digestAuditMatch{
			Name:        d.Name,
			Key:         d.Key(),
			SubPath:     d.SubPath,
			Digest:      full,
			DigestField: field,
			RefType:     d.RefType,
			RequiredBy:  append([]string(nil), d.RequiredBy...),
		})
	}
	rep.MatchCount = len(rep.Matches)
	rep.DistinctDigests = len(seen)
	// 歧义只针对前缀查询：多个**不同**摘要共享该前缀时无法唯一定位。
	rep.Ambiguous = q.Prefix && len(seen) > 1
	return rep
}

func (r *digestAuditReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (r *digestAuditReport) Render(w io.Writer) {
	kind := "精确匹配"
	if r.Prefix {
		kind = fmt.Sprintf("前缀匹配 (%d hex)", len(r.NormalizedQuery)-len(digest.Algorithm)-1)
	}
	fmt.Fprintf(w, "按 digest 反查（只读审计，来源 %s）\n", r.Source)
	fmt.Fprintf(w, "查询: %s → %s（%s）\n", r.Query, r.NormalizedQuery, kind)
	fmt.Fprintf(w, "匹配: %d 条 lock 条目 · %d 个不同摘要\n", r.MatchCount, r.DistinctDigests)
	switch {
	case !r.Prefix:
		fmt.Fprintln(w, "前缀歧义: 不适用（精确匹配）")
	case r.Ambiguous:
		fmt.Fprintln(w, "前缀歧义: 是 — 多个摘要共享该前缀，请提供更长的 hex 以唯一定位")
	default:
		fmt.Fprintln(w, "前缀歧义: 否")
	}
	if r.MatchCount == 0 {
		fmt.Fprintln(w, "无匹配：ngm.lock 中没有条目引用该摘要；确认摘要属于当前 lock，或先运行 `ngm install`。")
		return
	}
	for _, m := range r.Matches {
		name := m.Name
		if m.SubPath != "" {
			name += "#" + m.SubPath
		}
		fmt.Fprintf(w, "  %s  %s: %s\n", name, m.DigestField, m.Digest)
		if len(m.RequiredBy) == 0 {
			fmt.Fprintf(w, "    requiredBy: []（来源未知）\n")
		} else {
			fmt.Fprintf(w, "    requiredBy: %s\n", strings.Join(m.RequiredBy, ", "))
		}
	}
}
