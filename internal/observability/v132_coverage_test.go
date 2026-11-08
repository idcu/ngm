package observability

// 本文件只补测试，**不改任何行为**（v0.132）。
//
// 目标：把 observability 包（全仓覆盖率最低，53.8%）的真实分支补上，按缺口排序：
// outdated.go → tree.go → why.go。断言对准的是用户可见的结论（stale/unknown、
// 截断可见、来源可见、错误路径），不是行数。
//
// 一个发现：`newestSemver` 全仓库零调用（见任务 2 汇报），它无法用"真实调用"覆盖，
// 也不该为了数字写一条只调它的用例——登记为待删的死代码。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/supplychain"
	"github.com/idcu/ngm/internal/testutils"
)

const covHexCommit = "abc123def4567890abcdef1234567890abcdef12"

// ---- outdated.go -----------------------------------------------------------

func branchLock(ref, commit string) *lock.File {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/app", Ref: ref, RefType: "branch", Commit: commit,
	}}
	return lf
}

func cloneMirror(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "mirror.git")
	if err := git.CloneMirror(context.Background(), git.Options{}, src, dst); err != nil {
		t.Fatalf("CloneMirror: %v", err)
	}
	return dst
}

// branch 的三种"不知道"都要标 stale，并且**不许**说成"没有更新"。
func TestOutdatedBranchStaleShapes(t *testing.T) {
	ctx := context.Background()

	t.Run("offline", func(t *testing.T) {
		rep, err := CheckOutdated(ctx, branchLock("main", "deadbeef"), OutdatedOptions{
			Offline:      true,
			EnsureMirror: func(context.Context, string) (string, error) { return "unused", nil },
		})
		if err != nil {
			t.Fatalf("CheckOutdated: %v", err)
		}
		e := rep.Entries[0]
		if !e.Stale || e.Update || rep.Stale != 1 || rep.Updates != 0 {
			t.Errorf("offline branch must be unknown, not up-to-date: %+v", e)
		}
		if !strings.Contains(e.Note, "offline") {
			t.Errorf("note = %q, want the offline reason", e.Note)
		}
	})

	t.Run("mirror unavailable", func(t *testing.T) {
		rep, err := CheckOutdated(ctx, branchLock("main", "deadbeef"), OutdatedOptions{
			EnsureMirror: func(context.Context, string) (string, error) {
				return "", errors.New("no mirror")
			},
		})
		if err != nil {
			t.Fatalf("CheckOutdated: %v", err)
		}
		e := rep.Entries[0]
		if !e.Stale || !strings.Contains(e.Note, "mirror unavailable") {
			t.Errorf("unavailable mirror must be stale with the reason: %+v", e)
		}
	})

	t.Run("fetch fails", func(t *testing.T) {
		// 不是裸仓库：FetchMirror 在启动 git 之前就拒绝。
		rep, err := CheckOutdated(ctx, branchLock("main", "deadbeef"), OutdatedOptions{
			EnsureMirror: func(context.Context, string) (string, error) { return t.TempDir(), nil },
		})
		if err != nil {
			t.Fatalf("CheckOutdated: %v", err)
		}
		e := rep.Entries[0]
		if !e.Stale || !strings.Contains(e.Note, "could not fetch") {
			t.Errorf("fetch failure must be stale, not 'no update': %+v", e)
		}
	})

	t.Run("branch absent from mirror", func(t *testing.T) {
		testutils.MustHaveGit(t)
		src := testutils.NewGitRepo(t)
		src.WriteFile("a.txt", "a\n").Commit("one")
		mirror := cloneMirror(t, src.Dir)

		rep, err := CheckOutdated(ctx, branchLock("release/1.x", "deadbeef"), OutdatedOptions{
			EnsureMirror: func(context.Context, string) (string, error) { return mirror, nil },
		})
		if err != nil {
			t.Fatalf("CheckOutdated: %v", err)
		}
		e := rep.Entries[0]
		if !e.Stale || !strings.Contains(e.Note, "branch not present") {
			t.Errorf("a missing branch is unknown, not 'up to date': %+v", e)
		}
	})
}

// branch 的成功判定：落后提交数能算出来时给出具体数字，算不出来时留在 -1。
func TestOutdatedBranchBehindCount(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	src := testutils.NewGitRepo(t)
	src.WriteFile("a.txt", "a\n").Commit("one")
	c1 := src.Head()
	src.WriteFile("b.txt", "b\n").Commit("two")
	src.WriteFile("c.txt", "c\n").Commit("three")
	head := src.Head()
	mirror := cloneMirror(t, src.Dir)

	mk := func(commit string) *OutdatedReport {
		rep, err := CheckOutdated(ctx, branchLock("main", commit), OutdatedOptions{
			EnsureMirror: func(context.Context, string) (string, error) { return mirror, nil },
		})
		if err != nil {
			t.Fatalf("CheckOutdated(%s): %v", commit, err)
		}
		return rep
	}

	t.Run("behind", func(t *testing.T) {
		e := mk(c1).Entries[0]
		if !e.Update || e.Latest != head || e.LatestRule != "branch" {
			t.Errorf("behind entry = %+v", e)
		}
		if e.Behind != 2 {
			t.Errorf("Behind = %d, want 2 commits after the locked one", e.Behind)
		}
	})

	t.Run("up to date", func(t *testing.T) {
		e := mk(head).Entries[0]
		if e.Update || e.Stale || e.Behind != 0 {
			t.Errorf("tip == locked commit must be 'no update' with Behind 0: %+v", e)
		}
	})

	t.Run("locked commit unknown to the mirror", func(t *testing.T) {
		e := mk("0123456789abcdef0123456789abcdef01234567").Entries[0]
		if !e.Update || e.Stale {
			t.Errorf("a tip that differs from the locked commit is an update: %+v", e)
		}
		// 算不出落后数时不许编一个：-1 的语义是"未知"。
		if e.Behind != -1 {
			t.Errorf("Behind = %d, want -1 (rev-list cannot count from an unknown commit)", e.Behind)
		}
	})
}

// commit 钉死：不存在"更新"这个概念，也不该去碰 mirror。
func TestOutdatedPinnedCommitNeedsNoMirror(t *testing.T) {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/app", Ref: "main", RefType: "commit", Commit: "abc123",
	}}
	called := false
	rep, err := CheckOutdated(context.Background(), lf, OutdatedOptions{
		EnsureMirror: func(context.Context, string) (string, error) {
			called = true
			return "", errors.New("must not be called")
		},
	})
	if err != nil {
		t.Fatalf("CheckOutdated: %v", err)
	}
	e := rep.Entries[0]
	if called || e.Stale || e.Update || !strings.Contains(e.Note, "pinned") {
		t.Errorf("pinned commit = %+v (mirror called=%v)", e, called)
	}
}

// tag 扫描失败必须向上返回错误，而不是静默当成"没有更新"。
func TestOutdatedTagScanFailureIsAnError(t *testing.T) {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/app", Ref: "v1.0.0", RefType: "tag", Commit: "abc123",
	}}
	absent := filepath.Join(t.TempDir(), "absent.git")
	_, err := CheckOutdated(context.Background(), lf, OutdatedOptions{
		EnsureMirror: func(context.Context, string) (string, error) { return absent, nil },
	})
	if err == nil {
		t.Fatal("a failed tag scan must be an error, not an empty report")
	}
}

func TestOutdatedTagDateFallback(t *testing.T) {
	testutils.MustHaveGit(t)
	src := testutils.NewGitRepo(t)
	// 两个 commit、两个非 semver tag。名字按字母序挑选，使"并列时间戳"时
	// 的兜底顺序（refname 升序）与"时间不同"时的顺序都得到同一个答案：
	// 旧 tag 叫 zeta-old，新 tag 叫 alpha-new。
	src.WriteFile("a.txt", "a\n").Commit("one")
	src.Tag("zeta-old", false)
	src.WriteFile("b.txt", "b\n").Commit("two")
	src.Tag("alpha-new", false)
	mirror := cloneMirror(t, src.Dir)

	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{
		{Name: "github:acme/app", Ref: "zeta-old", RefType: "tag", Commit: "abc123"},
		{Name: "github:acme/app", Ref: "alpha-new", RefType: "tag", Commit: "abc123"},
	}
	rep, err := CheckOutdated(context.Background(), lf, OutdatedOptions{
		EnsureMirror: func(context.Context, string) (string, error) { return mirror, nil },
	})
	if err != nil {
		t.Fatalf("CheckOutdated: %v", err)
	}

	older, latest := rep.Entries[0], rep.Entries[1]
	if older.Latest != "alpha-new" || older.LatestRule != "tag-date" || !older.Update {
		t.Errorf("older tag = %+v, want Latest=alpha-new via tag-date", older)
	}
	if !strings.Contains(older.Note, "no semver tags") {
		t.Errorf("note = %q", older.Note)
	}
	if latest.Latest != "alpha-new" || latest.Update {
		t.Errorf("already on the newest tag = %+v, want no update", latest)
	}
}

// rowOf 取出以 prefix 开头的那一行，便于断言单行的渲染结论（而不是整段包含）。
func rowOf(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return line
		}
	}
	return ""
}

func TestOutdatedRenderAndMarshal(t *testing.T) {
	rep := &OutdatedReport{
		Dependencies: 4,
		Updates:      2,
		Stale:        1,
		Entries: []OutdatedEntry{
			{Name: "github:a/up", Ref: "v1.0.0", RefType: "tag", Latest: "v1.1.0", Update: true, Behind: -1},
			{Name: "github:b/behind", Ref: "main", RefType: "branch", Latest: "0123456789abcdef", Update: true, Behind: 3},
			{Name: "github:c/mono", Ref: "v2.0.0", RefType: "tag", SubPath: "packages/c", Latest: "", Behind: -1, Stale: true, Note: "no tags in the local mirror"},
			{Name: "github:d/pinned", Ref: "abc", RefType: "commit", Latest: "", Behind: -1, Note: "pinned to a commit; no update to look for"},
		},
	}

	var buf bytes.Buffer
	rep.Render(&buf)
	out := buf.String()

	if !strings.Contains(out, "github:a/up") || !strings.Contains(out, "yes") {
		t.Errorf("update row missing:\n%s", out)
	}
	if !strings.Contains(out, "yes (3 commits)") {
		t.Errorf("Behind must be rendered as a concrete count:\n%s", out)
	}
	if !strings.Contains(out, "0123456789ab") || strings.Contains(out, "0123456789abcdef") {
		t.Errorf("Latest must be truncated to 12 chars:\n%s", out)
	}
	if !strings.Contains(out, "github:c/mono#packages/c") || !strings.Contains(out, "unknown") {
		t.Errorf("subPath must be visible and stale must read 'unknown':\n%s", out)
	}
	if !strings.Contains(out, "  → no tags in the local mirror") {
		t.Errorf("unknown must carry its reason:\n%s", out)
	}
	// 钉死的 commit 是最容易骗人的行：它既不是"有更新"，也不是"未知"。
	if line := rowOf(out, "github:d/pinned"); !strings.Contains(line, " no") {
		t.Errorf("a pinned commit must render 'no' for drift, got %q:\n%s", line, out)
	}
	if line := rowOf(out, "github:c/mono#packages/c"); !strings.Contains(line, "unknown") {
		t.Errorf("a stale entry must render 'unknown', never 'no', got %q", line)
	}
	if !strings.Contains(out, "4 dependency(s) checked; 2 with updates; 1 unknown (could not be determined - not 'up to date')") {
		t.Errorf("summary line missing:\n%s", out)
	}

	data, err := rep.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("Marshal output must end with a single newline")
	}
	var decoded OutdatedReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Dependencies != 4 || len(decoded.Entries) != 4 || decoded.Entries[1].Behind != 3 {
		t.Errorf("round-trip lost fields: %+v", decoded)
	}
}

// ---- tree.go ---------------------------------------------------------------

func TestTreeDriftAndVulnCountsIncludeNested(t *testing.T) {
	entries := []*TreeEntry{{
		Name: "a", Drift: true,
		Children: []*TreeEntry{{
			Name: "b", Drift: true, Vulns: []supplychain.Vuln{{ID: "GHSA-1"}},
			Children: []*TreeEntry{{
				Name: "c", Drift: true, Vulns: []supplychain.Vuln{{ID: "GHSA-2"}},
			}},
		}},
	}}

	if got := CountDrifted(entries); got != 3 {
		t.Errorf("CountDrifted = %d, want 3 (nested drift must count)", got)
	}
	if got := VulnCount(entries); got != 2 {
		t.Errorf("VulnCount = %d, want 2 (nested vulns must count)", got)
	}
}

// BuildTree 的 lock 比对与环保护：漂移要标出来，"lock 里有但 commit 为空"
// 不能被读成"没有漂移"；环必须停止展开而不是递归到爆栈。
func TestBuildTreeReadsLockAndStopsCycles(t *testing.T) {
	g := &resolve.Graph{Nodes: []*resolve.Node{
		{Key: "a", Name: "a", Ref: "v1", RefType: resolve.RefTypeTag, Commit: "1111",
			RootDeclared: true, RequiredBy: []string{resolve.RootMarker, "b"}},
		{Key: "b", Name: "b", Ref: "v1", RefType: resolve.RefTypeTag, Commit: "2222",
			// ghost 不在图里：断在半路的边必须被忽略，而不是造出一个空节点。
			RequiredBy: []string{"a", "ghost"}},
		{Key: "c", Name: "c", Ref: "v1", RefType: resolve.RefTypeTag, Commit: "3333",
			RootDeclared: true, RequiredBy: []string{resolve.RootMarker}},
	}}

	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{
		{Name: "a", Ref: "v1", RefType: "tag", Commit: "1111"},
		{Name: "b", Ref: "v1", RefType: "tag", Commit: "9999"},
		{Name: "c", Ref: "v1", RefType: "tag", Commit: ""},
	}

	if got, tr := BuildTree(nil, nil, 0); got != nil || tr {
		t.Errorf("nil graph must yield an empty tree, got %v/%v", got, tr)
	}

	entries, truncated := BuildTree(g, lf, 0)
	if truncated {
		t.Error("unlimited expansion must not be truncated")
	}
	if len(entries) != 2 || entries[0].Name != "a" || entries[1].Name != "c" {
		t.Fatalf("roots = %v", entries)
	}
	if entries[0].Drift || entries[0].LockedCommit != "1111" {
		t.Errorf("same commit ⇒ no drift: %+v", entries[0])
	}
	b := entries[0].Children[0]
	if !b.Drift || b.LockedCommit != "9999" {
		t.Errorf("locked commit differs ⇒ drift: %+v", b)
	}
	if entries[1].Drift || entries[1].LockedCommit != "" {
		t.Errorf("an empty locked commit proves nothing, so it cannot be 'drift': %+v", entries[1])
	}
	cycle := b.Children[0]
	if !cycle.Cycle || cycle.Name != "a" {
		t.Errorf("a back edge must be marked as a cycle and not expanded: %+v", b.Children)
	}
}

func TestTreeRenderShowsOriginStates(t *testing.T) {
	entries := []*TreeEntry{{
		Name: "github:a/app", Ref: "v1", RefType: "tag", Commit: covHexCommit,
		SubPath: "pkg", Drift: true, LockedCommit: "fedcba9876543210fedcba9876543210fedcba98",
		Children: []*TreeEntry{
			{Name: "github:b/cycle", Ref: "v1", RefType: "tag", Commit: covHexCommit, Cycle: true},
			{Name: "github:c/cut", Ref: "v1", RefType: "tag", Commit: covHexCommit, Truncated: true},
			{Name: "github:d/vuln", Ref: "v1", RefType: "tag", Commit: covHexCommit,
				Vulns: []supplychain.Vuln{{ID: "GHSA-1", Severity: "HIGH"}}},
		},
	}}

	rep := &TreeReport{
		Project:          "demo",
		Source:           TreeSourceResolved,
		Notes:            []string{"mirror unreadable"},
		Dependencies:     4,
		Drifted:          1,
		Entries:          entries,
		EntriesTruncated: true,
		EntriesLimit:     MaxTreeEntries,
	}
	var buf bytes.Buffer
	rep.Render(&buf)
	out := buf.String()

	for _, want := range []string{
		"demo\n",
		"github:a/app#pkg@v1 (tag) → abc123d ⚠ 漂移 (locked fedcba9",
		"↺ cycle (not expanded)",
		"此处达到展开上限（还有子节点未列出）",
		"✗ 1 known vuln(s)",
		"source: ngm.json (ngm.lock has no graph; re-resolved from the local mirror)",
		"note: mirror unreadable",
		"（展开达到上限 4096，树不完整；用 --all 展开全部）",
		"1 dependency ref(s) moved away from the locked commit (⚠)",
		"vulnerability data not consulted; run `ngm audit` (or `ngm tree --osv`)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tree output missing %q:\n%s", want, out)
		}
	}

	// 查过 OSV 且有漏洞：必须给出下一步；查过但没查到时也不许说"没有漏洞"。
	checked := &TreeReport{Project: "demo", Source: TreeSourceLock, Entries: entries, OSVChecked: true}
	var cb bytes.Buffer
	checked.Render(&cb)
	checkedOut := cb.String()
	if !strings.Contains(checkedOut, "source: ngm.lock (requiredBy graph; no re-resolution)") {
		t.Errorf("lock source label missing:\n%s", checkedOut)
	}
	if !strings.Contains(checkedOut, "→ 1 known vulnerability(ies); run `ngm audit`") {
		t.Errorf("remediation missing:\n%s", checkedOut)
	}
	if strings.Contains(checkedOut, "not consulted") {
		t.Errorf("OSV was consulted; the report must not say otherwise:\n%s", checkedOut)
	}

	data, err := rep.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("Marshal output must end with a single newline")
	}
	var decoded TreeReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Drifted != 1 || !decoded.EntriesTruncated || len(decoded.Entries) != 1 {
		t.Errorf("round-trip lost fields: %+v", decoded)
	}
}

// MarkOSV 只查询"有 commit 且不是环"的节点；忽略规则命中时不留记录。
func TestMarkOSVQueriesOnlyRealCommits(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[{"id":"GHSA-1","summary":"s","severity":"HIGH",
			"affected":[{"ranges":[{"events":[{"fixed":"1.2.4"}]}]}]}]}`))
	}))
	t.Cleanup(srv.Close)

	cfg := supplychain.OSVConfig{BaseURL: srv.URL}
	entries := []*TreeEntry{{
		Name: "a", Commit: covHexCommit,
		Children: []*TreeEntry{
			{Name: "b", Commit: covHexCommit},
			{Name: "c", Commit: ""},
			{Name: "d", Commit: covHexCommit, Cycle: true},
		},
	}}

	if err := MarkOSV(context.Background(), entries, cfg, nil); err != nil {
		t.Fatalf("MarkOSV: %v", err)
	}
	if calls != 2 {
		t.Errorf("OSV calls = %d, want 2 (empty commit and cycle entry must be skipped)", calls)
	}
	if len(entries[0].Vulns) != 1 || len(entries[0].Children[0].Vulns) != 1 {
		t.Errorf("nested nodes must be marked: %+v", entries[0])
	}
	if entries[0].Children[1].Vulns != nil || entries[0].Children[2].Vulns != nil {
		t.Error("nodes without a commit, or already known to be a cycle, must not be queried")
	}

	// 忽略 HIGH ⇒ 全部被过滤，节点上不留下"有漏洞"的记录。
	ignored := []*TreeEntry{{Name: "a", Commit: covHexCommit}}
	if err := MarkOSV(context.Background(), ignored, cfg, []string{"high"}); err != nil {
		t.Fatalf("MarkOSV(ignore): %v", err)
	}
	if len(ignored[0].Vulns) != 0 {
		t.Errorf("ignored severity was recorded anyway: %+v", ignored[0])
	}
}

// 错误必须冒泡：一个节点查不到，不许被吞成"没有漏洞"。
func TestMarkOSVPropagatesErrorsFromChildren(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vulns":[]}`))
	}))
	t.Cleanup(srv.Close)

	entries := []*TreeEntry{{
		Name: "a", Commit: covHexCommit,
		Children: []*TreeEntry{{Name: "child", Commit: "not-a-commit"}},
	}}
	err := MarkOSV(context.Background(), entries, supplychain.OSVConfig{BaseURL: srv.URL}, nil)
	if err == nil {
		t.Fatal("an invalid commit must be an error, not a silently empty result")
	}
}

// ---- why.go ----------------------------------------------------------------

func TestResolveTarget(t *testing.T) {
	g := &resolve.Graph{Nodes: []*resolve.Node{
		{Key: "github:a/app", Name: "github:a/app"},
		{Key: "github:o/r#pkg", Name: "github:o/r", SubPath: "pkg"},
		{Key: "github:b/lib#pkg/x", Name: "github:b/lib", SubPath: "pkg/x"},
		{Key: "github:b/lib#pkg/y", Name: "github:b/lib", SubPath: "pkg/y"},
	}}

	t.Run("exact key", func(t *testing.T) {
		n, err := ResolveTarget(g, "github:b/lib#pkg/x")
		if err != nil || n.Key != "github:b/lib#pkg/x" {
			t.Errorf("ResolveTarget = %v, %v", n, err)
		}
	})

	t.Run("unique name", func(t *testing.T) {
		n, err := ResolveTarget(g, "github:o/r")
		if err != nil || n.Key != "github:o/r#pkg" {
			t.Errorf("ResolveTarget = %v, %v (a unique name must resolve to its only key)", n, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, err := ResolveTarget(g, "github:nope/x")
		if err == nil || !strings.Contains(err.Error(), "is not in the dependency graph") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("ambiguous", func(t *testing.T) {
		_, err := ResolveTarget(g, "github:b/lib")
		if err == nil {
			t.Fatal("a name matching two subpaths must be ambiguous")
		}
		for _, want := range []string{"ambiguous", "github:b/lib#pkg/x", "github:b/lib#pkg/y"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("ambiguity error missing %q: %v", want, err)
			}
		}
	})
}

func TestWhyRenderTruncationAndSources(t *testing.T) {
	truncated := &WhyReport{
		Name:           "github:b/lib",
		Ref:            "v1",
		RefType:        "tag",
		Commit:         covHexCommit,
		SubPath:        "pkg/x",
		Paths:          [][]string{{"ngm.json", "github:a/app@v1", "github:b/lib#pkg/x@v1"}, {"ngm.json", "github:b/lib#pkg/x@v1"}},
		PathsTruncated: true,
		PathsLimit:     MaxWhyPaths,
		Locked: &LockedInfo{
			Commit:        covHexCommit,
			ResolvedAt:    "2026-01-01T00:00:00Z",
			ArchiveDigest: "sha256:deadbeef",
		},
		Source: "weird",
	}
	var buf bytes.Buffer
	truncated.Render(&buf)
	out := buf.String()

	for _, want := range []string{
		"github:b/lib#pkg/x@v1 (tag) → abc123d",
		"直接依赖：否",
		"传递依赖：已列出 1 条（枚举达到上限 64，还有更多未列出；用 --all 展开全部）",
		"  ngm.json → github:a/app@v1 → github:b/lib#pkg/x@v1",
		"archiveDigest: sha256:deadbeef",
		"来源：未知（weird）",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("why output missing %q:\n%s", want, out)
		}
	}
	// 两个元素的直接路径不是"传递"：它只说明根声明了它。
	if strings.Contains(out, "传递依赖：2 条") || strings.Contains(out, "  ngm.json → github:b/lib#pkg/x@v1\n") {
		t.Errorf("a two-element path is the direct declaration, not a transitive path:\n%s", out)
	}

	simple := &WhyReport{
		Name: "github:a/app", Ref: "v1", RefType: "tag", Commit: covHexCommit,
		RootDeclared: true, Source: WhySourceResolved,
		// 未截断的多条路径：默认分支必须把它们全列出来（而不是只给条数）。
		Paths: [][]string{
			{"ngm.json", "github:x/one@v1", "github:a/app@v1"},
			{"ngm.json", "github:x/one@v1", "github:y/two@v1", "github:a/app@v1"},
		},
		Locked: &LockedInfo{
			Commit: covHexCommit, ResolvedAt: "2026-01-01T00:00:00Z",
			ArchiveDigest: "sha256:aaa", SubPath: "pkg", SubtreeOID: "sha1:bbb", Version: "1.0.0",
		},
	}
	var sb bytes.Buffer
	simple.Render(&sb)
	sout := sb.String()
	for _, want := range []string{
		"直接依赖：ngm.json 声明",
		"传递依赖：2 条路径",
		"  ngm.json → github:x/one@v1 → github:a/app@v1",
		"  ngm.json → github:x/one@v1 → github:y/two@v1 → github:a/app@v1",
		"moduleDigest: sha256:aaa",
		"subtreeOid: sha1:bbb",
		"version: 1.0.0",
		"来源：重新解析上游清单（lock 里没有依赖图）",
	} {
		if !strings.Contains(sout, want) {
			t.Errorf("why output missing %q:\n%s", want, sout)
		}
	}

	if txt := whySourceText(WhySourceLock); !strings.Contains(txt, "0 次 git 子进程") {
		t.Errorf("lock source text = %q, must name what it saves", txt)
	}

	data, err := simple.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("Marshal output must end with a single newline")
	}
	var decoded WhyReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.RootDeclared || decoded.Locked == nil || decoded.Locked.Version != "1.0.0" {
		t.Errorf("round-trip lost fields: %+v", decoded)
	}
}

// 环保护：上游写坏 manifest 造出环时，DFS 必须跳过已访问节点，而不是无限展开。
// 这条同时覆盖带 SubPath 的 label（`name#sub@ref`）。
func TestWhyCycleDoesNotLoop(t *testing.T) {
	g := &resolve.Graph{Nodes: []*resolve.Node{
		{Key: "a", Name: "github:a/app", SubPath: "pkg/a", Ref: "v1",
			RootDeclared: true, RequiredBy: []string{resolve.RootMarker, "b"}},
		{Key: "b", Name: "github:b/lib", Ref: "v1", RequiredBy: []string{"a"}},
		{Key: "c", Name: "github:c/target", Ref: "v1", RequiredBy: []string{"b"}},
	}}

	paths, truncated := FindPaths(g, "c", 0)
	if truncated {
		t.Error("a cycle must be cut, not reported as truncation")
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want exactly the one acyclic path", paths)
	}
	want := "ngm.json → github:a/app#pkg/a@v1 → github:b/lib@v1 → github:c/target@v1"
	if got := strings.Join(paths[0], " → "); got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// LockedFrom 的两条"没有"：没有 lock、lock 里没有这个条目。
func TestLockedFromMissing(t *testing.T) {
	if got := LockedFrom(nil, "github:a/app", ""); got != nil {
		t.Errorf("nil lock must yield no locked info, got %+v", got)
	}
	lf := lock.NewFile()
	if got := LockedFrom(lf, "github:a/app", "pkg"); got != nil {
		t.Errorf("missing entry must yield no locked info, got %+v", got)
	}
}
