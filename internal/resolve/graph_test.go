// 本文件使用外部测试包（resolve_test），因为需要 import vendor 来准备 mirror，
// 而 vendor 依赖 resolve —— 外部测试包可以打破这个环。
package resolve_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// ---------------------------------------------------------------------------
// fixture：一个"世界"，包含若干上游仓库与其 mirror
// ---------------------------------------------------------------------------

type fixtureWorld struct {
	t          *testing.T
	mirrorRoot string
	mirror     *vendor.Mirror
	sources    map[string]string // slug → 上游仓库目录
}

func newFixtureWorld(t *testing.T) *fixtureWorld {
	t.Helper()
	testutils.MustHaveGit(t)
	root := t.TempDir()
	return &fixtureWorld{
		t:          t,
		mirrorRoot: root,
		mirror:     vendor.NewMirror(root, git.Options{}),
		sources:    map[string]string{},
	}
}

// add 把一个仓库登记为某 slug 的"上游"，并预置其 mirror。
func (w *fixtureWorld) add(slug string, repo *testutils.GitRepo) {
	w.t.Helper()
	if _, err := w.mirror.EnsureLocal(context.Background(), resolve.MustNormalize(slug), repo.Dir); err != nil {
		w.t.Fatalf("seed mirror for %s: %v", slug, err)
	}
	w.sources[slug] = repo.Dir
}

func (w *fixtureWorld) opts() resolve.GraphOptions {
	return resolve.GraphOptions{
		EnsureMirror: func(ctx context.Context, repo resolve.Canonical) (string, error) {
			res, err := w.mirror.EnsureLocal(ctx, repo, w.sources[repo.Slug()])
			if err != nil {
				return "", err
			}
			return res.Path, nil
		},
		Concurrency: 4,
	}
}

// repoWithDeps 创建一个声明了给定依赖的仓库。
//
// depLines 形如 `{"name": "github:w/c", "ref": "v1", "refType": "tag"}`。
func repoWithDeps(t *testing.T, depLines ...string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	body := "{\n  \"schemaVersion\": 1,\n  \"name\": \"github.com:fixture/repo\",\n" +
		"  \"version\": \"0.1.0\",\n  \"runtime\": \"node\",\n  \"dependencies\": [\n"
	for i, l := range depLines {
		body += "    " + l
		if i < len(depLines)-1 {
			body += ","
		}
		body += "\n"
	}
	body += "  ]\n}\n"
	r.WriteFile("ngm.json", body)
	r.WriteFile("index.ts", "export const x = 1\n")
	r.Commit("feat: with deps")
	return r
}

// leafRepo 创建一个无依赖的仓库，并打上 tag。
func leafRepo(t *testing.T, tag string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("a.txt", "content for "+tag+"\n")
	r.Commit("feat: " + tag)
	r.Tag(tag, false)
	return r
}

func dep(name, ref string) string {
	return `{"name": "` + name + `", "ref": "` + ref + `", "refType": "tag"}`
}

// ---------------------------------------------------------------------------
// M3.2 传递依赖
// ---------------------------------------------------------------------------

func TestResolveGraph_TransitiveChain(t *testing.T) {
	w := newFixtureWorld(t)

	c := leafRepo(t, "v1")
	b := repoWithDeps(t, dep("github:w/c", "v1"))
	b.Tag("v1", false)
	a := repoWithDeps(t, dep("github:w/b", "v1"))
	a.Tag("v1", false)

	w.add("github:w/c", c)
	w.add("github:w/b", b)
	w.add("github:w/a", a)

	root := repoWithDeps(t, dep("github:w/a", "v1"))
	_ = root

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}

	if g.Len() != 3 {
		t.Fatalf("graph has %d nodes, want 3 (a, b, c):\n%s", g.Len(), dumpGraph(g))
	}

	// 深度：a=0, b=1, c=2
	wantDepth := map[string]int{"github:w/a": 0, "github:w/b": 1, "github:w/c": 2}
	for name, want := range wantDepth {
		n, ok := g.Find(name)
		if !ok {
			t.Errorf("missing node %s", name)
			continue
		}
		if n.Depth != want {
			t.Errorf("%s depth=%d want %d", name, n.Depth, want)
		}
		if !n.RootDeclared && name == "github:w/a" {
			t.Errorf("a should be root-declared")
		}
		if n.RootDeclared && name != "github:w/a" {
			t.Errorf("%s should not be root-declared", name)
		}
		if len(n.Commit) != 40 {
			t.Errorf("%s commit=%q", name, n.Commit)
		}
	}

	// 确定性：节点按 Key 排序
	for i := 1; i < len(g.Nodes); i++ {
		if g.Nodes[i-1].Key > g.Nodes[i].Key {
			t.Errorf("nodes not sorted: %s > %s", g.Nodes[i-1].Key, g.Nodes[i].Key)
		}
	}
}

func TestResolveGraph_CycleIsDeduplicated(t *testing.T) {
	w := newFixtureWorld(t)

	// A ↔ B 互相依赖
	a := testutils.NewGitRepo(t)
	b := testutils.NewGitRepo(t)
	a.WriteFile("ngm.json", `{"dependencies":[{"name":"github:w/b","ref":"main","refType":"branch"}]}`)
	a.WriteFile("index.ts", "a\n")
	a.Commit("a")
	b.WriteFile("ngm.json", `{"dependencies":[{"name":"github:w/a","ref":"main","refType":"branch"}]}`)
	b.WriteFile("index.ts", "b\n")
	b.Commit("b")

	w.add("github:w/a", a)
	w.add("github:w/b", b)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "main", RefType: resolve.RefTypeBranch},
	}, w.opts())
	if err != nil {
		t.Fatalf("cycle must not fail: %v", err)
	}
	if g.Len() != 2 {
		t.Fatalf("graph has %d nodes, want 2 (cycle dedup):\n%s", g.Len(), dumpGraph(g))
	}
	b2, ok := g.Find("github:w/b")
	if !ok {
		t.Fatal("missing b")
	}
	if !contains(b2.RequiredBy, "github:w/a") {
		t.Errorf("b.RequiredBy=%v should include github:w/a", b2.RequiredBy)
	}
}

func TestResolveGraph_DiamondSameRefMerges(t *testing.T) {
	w := newFixtureWorld(t)

	z := leafRepo(t, "v1")
	x := repoWithDeps(t, dep("github:w/z", "v1"))
	x.Tag("v1", false)
	y := repoWithDeps(t, dep("github:w/z", "v1"))
	y.Tag("v1", false)

	w.add("github:w/z", z)
	w.add("github:w/x", x)
	w.add("github:w/y", y)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/x", Ref: "v1", RefType: resolve.RefTypeTag},
		{Name: "github:w/y", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("same-ref diamond must merge: %v", err)
	}
	if g.Len() != 3 {
		t.Fatalf("nodes=%d want 3 (x, y, z merged once):\n%s", g.Len(), dumpGraph(g))
	}
	zn, _ := g.Find("github:w/z")
	if !contains(zn.RequiredBy, "github:w/x") || !contains(zn.RequiredBy, "github:w/y") {
		t.Errorf("z.RequiredBy=%v should list both x and y", zn.RequiredBy)
	}
}

// ---------------------------------------------------------------------------
// M3.3 冲突策略
// ---------------------------------------------------------------------------

// TestResolveGraph_RootWins 锁定「根声明优先」：
// 根显式声明 A@v1，上游 B 要求 A@v2 → 采用 v1，不报错，并记录被覆盖的声明。
func TestResolveGraph_RootWins(t *testing.T) {
	w := newFixtureWorld(t)

	a := testutils.NewGitRepo(t)
	a.WriteFile("a.txt", "v1\n")
	a.Commit("v1")
	a.Tag("v1", false)
	a.WriteFile("a.txt", "v2\n")
	a.Commit("v2")
	a.Tag("v2", false)

	b := repoWithDeps(t, dep("github:w/a", "v2"))
	b.Tag("v1", false)

	w.add("github:w/a", a)
	w.add("github:w/b", b)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "v1", RefType: resolve.RefTypeTag},
		{Name: "github:w/b", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("root wins must not fail: %v", err)
	}

	an, ok := g.Find("github:w/a")
	if !ok {
		t.Fatal("missing a")
	}
	if an.Ref != "v1" {
		t.Errorf("a.Ref=%q want v1 (root wins)", an.Ref)
	}
	if !an.RootDeclared {
		t.Errorf("a should be root-declared")
	}
	// 被覆盖的传递声明应被记录（供用户察觉上游版本被改）
	if len(an.IgnoredRefs) == 0 {
		t.Errorf("overridden transitive declaration should be recorded in IgnoredRefs")
	} else if !strings.Contains(strings.Join(an.IgnoredRefs, "\n"), "v2") {
		t.Errorf("IgnoredRefs should mention v2: %v", an.IgnoredRefs)
	}
	// a 的来源应包含根与 b
	if !contains(an.RequiredBy, "(root)") {
		t.Errorf("a.RequiredBy=%v should include (root)", an.RequiredBy)
	}
}

// TestResolveGraph_TransitiveConflictFails 锁定「仅传递间冲突报错」：
// 根未声明 Z，X 要求 Z@v1、Y 要求 Z@v2 → exit 3，提示在根显式声明。
func TestResolveGraph_TransitiveConflictFails(t *testing.T) {
	w := newFixtureWorld(t)

	z := testutils.NewGitRepo(t)
	z.WriteFile("z.txt", "v1\n")
	z.Commit("v1")
	z.Tag("v1", false)
	z.WriteFile("z.txt", "v2\n")
	z.Commit("v2")
	z.Tag("v2", false)

	x := repoWithDeps(t, dep("github:w/z", "v1"))
	x.Tag("v1", false)
	y := repoWithDeps(t, dep("github:w/z", "v2"))
	y.Tag("v1", false)

	w.add("github:w/z", z)
	w.add("github:w/x", x)
	w.add("github:w/y", y)

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/x", Ref: "v1", RefType: resolve.RefTypeTag},
		{Name: "github:w/y", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err == nil {
		t.Fatalf("transitive-only conflict must fail")
	}

	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error type %T", err)
	}
	if ne.Code != errs.CodeConfigInvalid {
		t.Errorf("exit code=%d want 3", ne.Code.ExitCode())
	}
	// 错误必须包含来源链（哪个上游要求了哪个版本）
	for _, want := range []string{"github:w/z", "v1", "v2", "github:w/x", "github:w/y"} {
		if !strings.Contains(err.Error(), want) && !strings.Contains(ne.Hint, want) {
			t.Errorf("conflict message should mention %q:\n%s\nhint: %s", want, err.Error(), ne.Hint)
		}
	}
	// Hint 必须指向"在根 ngm.json 显式声明"
	if !strings.Contains(ne.Hint, "root ngm.json") {
		t.Errorf("hint should point at the root declaration: %q", ne.Hint)
	}
}

// ---------------------------------------------------------------------------
// 上游声明的健壮性
// ---------------------------------------------------------------------------

func TestResolveGraph_UpstreamMissingRefTypeFails(t *testing.T) {
	w := newFixtureWorld(t)

	// 上游声明缺少 refType
	bad := testutils.NewGitRepo(t)
	bad.WriteFile("ngm.json", `{"dependencies":[{"name":"github:w/c","ref":"v1"}]}`)
	bad.WriteFile("index.ts", "x\n")
	bad.Commit("bad")
	bad.Tag("v1", false)

	w.add("github:w/bad", bad)

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/bad", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err == nil {
		t.Fatalf("missing refType in upstream must fail")
	}
	var ne *errs.NgmError
	if !errors.As(err, &ne) || ne.Code != errs.CodeConfigInvalid {
		t.Fatalf("expected exit 3, got %v", err)
	}
	// 错误必须指向上游仓库
	if !strings.Contains(err.Error(), "github:w/bad") {
		t.Errorf("error should name the upstream repo: %v", err)
	}
	if !strings.Contains(ne.Hint, "refType is mandatory") {
		t.Errorf("hint should explain why refType is mandatory: %q", ne.Hint)
	}
}

func TestResolveGraph_NoNgmJSONMeansNoTransitiveDeps(t *testing.T) {
	w := newFixtureWorld(t)

	plain := testutils.NewGitRepo(t)
	plain.WriteFile("index.ts", "export const x = 1\n")
	plain.Commit("no ngm.json")
	plain.Tag("v1", false)

	w.add("github:w/plain", plain)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/plain", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("missing ngm.json is not an error: %v", err)
	}
	if g.Len() != 1 {
		t.Errorf("nodes=%d want 1", g.Len())
	}
	if len(g.Warnings) != 0 {
		t.Errorf("no package.json → no warnings, got %v", g.Warnings)
	}
}

// TestResolveGraph_PackageJSONGitDepsWarns 锁定「不递归 package.json，但输出提示」。
func TestResolveGraph_PackageJSONGitDepsWarns(t *testing.T) {
	w := newFixtureWorld(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("package.json", `{
  "name": "upstream",
  "dependencies": {
    "left-pad": "^1.0.0",
    "some-git-dep": "github:other/lib#v1"
  },
  "devDependencies": {
    "dev-git-dep": "git+https://github.com/other/dev.git"
  }
}`)
	up.WriteFile("index.ts", "x\n")
	up.Commit("package.json only")
	up.Tag("v1", false)

	w.add("github:w/up", up)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/up", Ref: "v1", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}
	if g.Len() != 1 {
		t.Errorf("registry deps must NOT be traversed: nodes=%d", g.Len())
	}
	if len(g.Warnings) == 0 {
		t.Fatalf("expected a warning about Git deps in package.json")
	}
	joined := strings.Join(g.Warnings, "\n")
	for _, want := range []string{"package.json", "some-git-dep", "dev-git-dep", "NOT installed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warning should mention %q:\n%s", want, joined)
		}
	}
}

func TestResolveGraph_UnknownUpstreamRefFails(t *testing.T) {
	w := newFixtureWorld(t)
	a := leafRepo(t, "v1")
	w.add("github:w/a", a)

	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "v9.9.9", RefType: resolve.RefTypeTag},
	}, w.opts())
	if err == nil {
		t.Fatalf("unknown ref must fail")
	}
	var ne *errs.NgmError
	if !errors.As(err, &ne) || ne.Code != errs.CodeConfigInvalid {
		t.Errorf("expected exit 3, got %v", err)
	}
}

func TestResolveGraph_RequiresEnsureMirror(t *testing.T) {
	_, err := resolve.ResolveGraph(context.Background(), nil, resolve.GraphOptions{})
	if err == nil {
		t.Fatalf("missing EnsureMirror should fail loudly")
	}
}

func TestResolveGraph_EmptyRoots(t *testing.T) {
	w := newFixtureWorld(t)
	g, err := resolve.ResolveGraph(context.Background(), nil, w.opts())
	if err != nil {
		t.Fatalf("empty roots should not fail: %v", err)
	}
	if g.Len() != 0 {
		t.Errorf("nodes=%d want 0", g.Len())
	}
}

// TestResolveGraph_MonorepoSubPathIsDistinctNode 同一仓库的不同子路径是不同节点。
func TestResolveGraph_MonorepoSubPathIsDistinctNode(t *testing.T) {
	w := newFixtureWorld(t)

	mono := testutils.NewGitRepo(t)
	mono.WriteFile("packages/core/index.ts", "core\n")
	mono.WriteFile("packages/web/index.ts", "web\n")
	mono.Commit("monorepo")
	mono.Tag("v1", false)
	w.add("github:w/mono", mono)

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/mono", Ref: "v1", RefType: resolve.RefTypeTag, SubPath: "packages/core"},
		{Name: "github:w/mono", Ref: "v1", RefType: resolve.RefTypeTag, SubPath: "packages/web"},
	}, w.opts())
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}
	if g.Len() != 2 {
		t.Fatalf("nodes=%d want 2 (distinct sub-paths):\n%s", g.Len(), dumpGraph(g))
	}
	if _, ok := g.Find("github:w/mono#packages/core"); !ok {
		t.Errorf("missing core node; got %v", nodeKeys(g))
	}
	if _, ok := g.Find("github:w/mono#packages/web"); !ok {
		t.Errorf("missing web node; got %v", nodeKeys(g))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func nodeKeys(g *resolve.Graph) []string {
	out := make([]string, len(g.Nodes))
	for i, n := range g.Nodes {
		out[i] = n.Key
	}
	return out
}

func dumpGraph(g *resolve.Graph) string {
	var sb strings.Builder
	for _, n := range g.Nodes {
		sb.WriteString("  " + n.Key + " @ " + n.Ref + " (" + string(n.RefType) + ")")
		sb.WriteString(" depth=" + itoa(n.Depth))
		sb.WriteString(" root=" + boolStr(n.RootDeclared))
		sb.WriteString(" requiredBy=" + strings.Join(n.RequiredBy, ","))
		sb.WriteString("\n")
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
