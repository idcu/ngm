package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 远端夹具的**生成器**（v0.112）。
//
// 为什么要它：本仓库的纪律是"测试不依赖公网"，所以真实的 Gitee 仓库不能出现在
// `go test` 的默认路径上。但它们仍然需要存在——`git ls-remote` 的 ref 广播形状
// （annotated tag 会同时给出 `refs/tags/x` 与 `refs/tags/x^{}`）、真实 clone、
// 以及 `net:<host>` 门禁真的在出网前生效，这三件事本地 mirror 都假不出来。
//
// 形态：一个**生成器 + 一条离线判据**。默认把三个仓库造到临时目录并断言其形状；
// 设了 `NGM_FIXTURES_OUT=<dir>` 时改为造到那个目录，并打印推送命令——
// 人工只需要跑一次 `git push --mirror`，之后远端就是这批固定 SHA。
//
// 全部用底层命令（hash-object / update-index / write-tree / commit-tree /
// update-ref），因此：
//   - **确定性**：作者、时间、消息全固定，同一份内容在任何机器上产生同一批 SHA；
//   - **跨平台**：`Foo.txt` 与 `foo.txt` 无法共存于 Windows 的文件系统，
//     symlink 与 100755 在 Windows 上也无法直接创建——写进树里就不受这些限制。

// fixtureAuthor 与 fixtureDate 一起把 SHA 钉死。
//
// 时间取在过去：`supplyChain.minimumReleaseAge` 是"晾晒期"门禁，若夹具的提交
// 是"刚刚"产生的，启用该策略的项目会把**自己的夹具**判成太新。
const (
	fixtureAuthorName  = "ngm fixtures"
	fixtureAuthorEmail = "fixtures@ngm.invalid"
	fixtureDate        = "2025-01-01T00:00:00+0000"
)

// fileSpec 是树里的一个条目。
type fileSpec struct {
	Path string
	// Mode 取 "100644" / "100755" / "120000"（后者时 Content 是链接目标）。
	Mode    string
	Content string
}

// fixtureRepo 是一个待推送的裸仓库。
type fixtureRepo struct {
	Name string // 仓库名（gitee 上的名字）
	// Refs 在 build 之后填充：ref → sha（annotated tag 给的是 tag 对象的 sha）。
	Refs map[string]string
}

// gitEnv 返回让 SHA 确定的完整环境。
func fixtureGitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+fixtureAuthorName,
		"GIT_AUTHOR_EMAIL="+fixtureAuthorEmail,
		"GIT_COMMITTER_NAME="+fixtureAuthorName,
		"GIT_COMMITTER_EMAIL="+fixtureAuthorEmail,
		"GIT_AUTHOR_DATE="+fixtureDate,
		"GIT_COMMITTER_DATE="+fixtureDate,
		// 不因本地配置改变行为。
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+fixtureConfigHome(),
	)
}

// mustTempConfigHome 给每条命令一个空的 HOME，避免读用户级 gitconfig。
func fixtureConfigHome() string {
	dir := filepath.Join(os.TempDir(), "ngm-fixture-githome")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func fixtureGit(t *testing.T, dir string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = fixtureGitEnv()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeTree 把一批条目写成一棵树，返回 tree sha。
func fixtureWriteTree(t *testing.T, dir string, files []fileSpec) string {
	t.Helper()
	index := filepath.Join(t.TempDir(), "index")
	env := append(fixtureGitEnv(), "GIT_INDEX_FILE="+index)
	run := func(stdin string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	for _, f := range files {
		blob := run(f.Content, "hash-object", "-w", "--stdin")
		run("", "update-index", "--add", "--cacheinfo", f.Mode+","+blob+","+f.Path)
	}
	return run("", "write-tree")
}

func fixtureCommitTree(t *testing.T, dir, tree, parent, message string) string {
	t.Helper()
	args := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return fixtureGit(t, dir, "", args...)
}

// buildBasic 造 `ngm-fixture-basic`：两种 tag 语义 + 一个分叉分支。
func buildBasic(t *testing.T, dir string) *fixtureRepo {
	t.Helper()
	repo := &fixtureRepo{Name: "ngm-fixture-basic", Refs: map[string]string{}}

	c1 := fixtureCommitTree(t, dir, fixtureWriteTree(t, dir, []fileSpec{
		// CRLF 内容：落地/摘要都必须原样保留（ADR-008 不做换行转换）。
		{Path: "index.js", Mode: "100644", Content: "export const x = 1;\r\nexport const y = 2;\r\n"},
		{Path: "README.md", Mode: "100644", Content: "# fixture basic\n"},
		{Path: "bin/run.sh", Mode: "100755", Content: "#!/bin/sh\necho run\n"},
		{Path: "link-to-index", Mode: "120000", Content: "index.js"},
	}), "", "c1: initial")

	c2 := fixtureCommitTree(t, dir, fixtureWriteTree(t, dir, []fileSpec{
		{Path: "index.js", Mode: "100644", Content: "export const x = 1;\r\nexport const y = 2;\r\n"},
		{Path: "README.md", Mode: "100644", Content: "# fixture basic\n"},
		{Path: "bin/run.sh", Mode: "100755", Content: "#!/bin/sh\necho run\n"},
		{Path: "link-to-index", Mode: "120000", Content: "index.js"},
		{Path: "lib/util.js", Mode: "100644", Content: "export const u = 1;\n"},
	}), c1, "c2: add lib/util.js")

	c3 := fixtureCommitTree(t, dir, fixtureWriteTree(t, dir, []fileSpec{
		{Path: "README.md", Mode: "100644", Content: "# fixture basic\n"},
		{Path: "legacy.txt", Mode: "100644", Content: "legacy branch\n"},
	}), c1, "c3: legacy branch from c1")

	fixtureGit(t, dir, "", "update-ref", "refs/heads/main", c2)
	fixtureGit(t, dir, "", "update-ref", "refs/heads/legacy", c3)
	// annotated tag：`ls-remote` 会给出 `refs/tags/v1.0.0` 与 `refs/tags/v1.0.0^{}` 两行。
	fixtureGit(t, dir, "", "tag", "-a", "v1.0.0", "-m", "fixture annotated tag", c1)
	// lightweight tag：只给一行。两种语义必须都被解析对。
	fixtureGit(t, dir, "", "update-ref", "refs/tags/v1.1.0-light", c2)

	repo.Refs["refs/heads/main"] = c2
	repo.Refs["refs/heads/legacy"] = c3
	repo.Refs["refs/tags/v1.0.0"] = fixtureGit(t, dir, "", "rev-parse", "refs/tags/v1.0.0")
	repo.Refs["refs/tags/v1.0.0^{}"] = c1
	repo.Refs["refs/tags/v1.1.0-light"] = c2
	return repo
}

// buildMonorepo 造 `ngm-fixture-monorepo`：两个模块目录，各打一个 tag。
func buildMonorepo(t *testing.T, dir string) *fixtureRepo {
	t.Helper()
	repo := &fixtureRepo{Name: "ngm-fixture-monorepo", Refs: map[string]string{}}

	modA := []fileSpec{
		{Path: "packages/a/package.json", Mode: "100644", Content: `{"name":"a","version":"1.0.0"}`},
		{Path: "packages/a/ngm.json", Mode: "100644",
			Content: `{"schemaVersion":1,"name":"gitee.com:idcu/a","version":"1.0.0","runtime":"node","dependencies":[]}`},
		{Path: "packages/a/index.js", Mode: "100644", Content: "export const a = 1;\n"},
	}
	c1 := fixtureCommitTree(t, dir, fixtureWriteTree(t, dir, modA), "", "c1: packages/a")

	both := append([]fileSpec{}, modA...)
	both = append(both,
		fileSpec{Path: "packages/b/package.json", Mode: "100644", Content: `{"name":"b","version":"1.0.0"}`},
		fileSpec{Path: "packages/b/ngm.json", Mode: "100644",
			Content: `{"schemaVersion":1,"name":"gitee.com:idcu/b","version":"1.0.0","runtime":"node","dependencies":[]}`},
		fileSpec{Path: "packages/b/index.js", Mode: "100644", Content: "export const b = 1;\n"},
	)
	c2 := fixtureCommitTree(t, dir, fixtureWriteTree(t, dir, both), c1, "c2: packages/b")

	fixtureGit(t, dir, "", "update-ref", "refs/heads/main", c2)
	fixtureGit(t, dir, "", "tag", "-a", "packages-a-v1.0.0", "-m", "module a v1.0.0", c1)
	fixtureGit(t, dir, "", "tag", "-a", "packages-b-v1.0.0", "-m", "module b v1.0.0", c2)

	repo.Refs["refs/heads/main"] = c2
	repo.Refs["refs/tags/packages-a-v1.0.0"] = fixtureGit(t, dir, "", "rev-parse", "refs/tags/packages-a-v1.0.0")
	repo.Refs["refs/tags/packages-a-v1.0.0^{}"] = c1
	repo.Refs["refs/tags/packages-b-v1.0.0"] = fixtureGit(t, dir, "", "rev-parse", "refs/tags/packages-b-v1.0.0")
	repo.Refs["refs/tags/packages-b-v1.0.0^{}"] = c2
	return repo
}

// buildContent 造 `ngm-fixture-content`：一份专门用来打断言的树。
//
// 每条都在某个判据上出现过一次真实缺陷，或者能一眼看出"抽象泄漏"：
// 大小写对（Windows 上必须 fail-closed）、BOM、CRLF、非 ASCII 文件名、
// 可执行位、空文件、深层路径。
func buildContent(t *testing.T, dir string) *fixtureRepo {
	t.Helper()
	repo := &fixtureRepo{Name: "ngm-fixture-content", Refs: map[string]string{}}

	tree := fixtureWriteTree(t, dir, []fileSpec{
		{Path: "Foo.txt", Mode: "100644", Content: "upper\n"},
		{Path: "foo.txt", Mode: "100644", Content: "lower\n"},
		{Path: "bom.txt", Mode: "100644", Content: "\ufeffwith bom\n"},
		{Path: "crlf.txt", Mode: "100644", Content: "a\r\nb\r\n"},
		{Path: "empty.txt", Mode: "100644", Content: ""},
		{Path: "中文名.js", Mode: "100644", Content: "export const n = 1;\n"},
		{Path: "bin/tool", Mode: "100755", Content: "#!/bin/sh\nexit 0\n"},
		{Path: "deep/a/b/c/d/e/f/g.txt", Mode: "100644", Content: "deep\n"},
	})
	c1 := fixtureCommitTree(t, dir, tree, "", "c1: pathological content")
	fixtureGit(t, dir, "", "update-ref", "refs/heads/main", c1)
	fixtureGit(t, dir, "", "tag", "-a", "v1.0.0", "-m", "content v1.0.0", c1)

	repo.Refs["refs/heads/main"] = c1
	repo.Refs["refs/tags/v1.0.0"] = fixtureGit(t, dir, "", "rev-parse", "refs/tags/v1.0.0")
	repo.Refs["refs/tags/v1.0.0^{}"] = c1
	return repo
}

// buildFixtures 把三个仓库造进 root，返回它们的描述。
func buildFixtures(t *testing.T, root string) []*fixtureRepo {
	t.Helper()
	builders := []func(*testing.T, string) *fixtureRepo{buildBasic, buildMonorepo, buildContent}
	out := make([]*fixtureRepo, 0, len(builders))
	for _, build := range builders {
		// 先建裸仓库再把内容写进去：推送到空仓时 `git push --mirror` 一句话就够。
		// 名字要等 build 返回才知道，所以先造在临时目录里再改名。
		tmp := filepath.Join(root, "tmp-build")
		_ = os.RemoveAll(tmp)
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			t.Fatal(err)
		}
		fixtureGit(t, tmp, "", "init", "--bare", "-q", ".")
		r := build(t, tmp)
		dir := filepath.Join(root, r.Name)
		_ = os.RemoveAll(dir)
		if err := os.Rename(tmp, dir); err != nil {
			t.Fatalf("rename fixture: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// TestV112FixtureGeneratorReproducesTheSameSHAs 是这条链路的**地基**：
// 同一份描述必须在任何机器、任何时间产生同一批 SHA——否则"远端夹具"就只是
// "某台机器上曾经发生过的事"，而腐坏守卫（比对远端 SHA）也就失去意义。
func TestV112FixtureGeneratorReproducesTheSameSHAs(t *testing.T) {
	root := t.TempDir()
	first := buildFixtures(t, filepath.Join(root, "a"))
	second := buildFixtures(t, filepath.Join(root, "b"))

	if len(first) != 3 {
		t.Fatalf("built %d fixtures, want 3", len(first))
	}
	for i := range first {
		if first[i].Name != second[i].Name {
			t.Fatalf("fixture %d: %q vs %q", i, first[i].Name, second[i].Name)
		}
		for ref, sha := range first[i].Refs {
			if got := second[i].Refs[ref]; got != sha {
				t.Errorf("%s %s: two builds disagree (%s vs %s) — the fixture is not deterministic, "+
					"so a recorded SHA could never work as a rot guard", first[i].Name, ref, sha, got)
			}
		}
	}
}

// TestV112FixtureContentIsWhatTheDocsClaim 断言每个仓库真的有它名字里承诺的形状。
//
// 为什么需要：远端测试里的每一条断言都建立在"这个仓库里有 annotated tag /
// 有一对只差大小写的文件 / 有 BOM"之上。夹具内容一旦漂移，远端测试会以
// **难以归因**的方式失败（"解析出来的 commit 不对"），而真正的原因在夹具。
func TestV112FixtureContentIsWhatTheDocsClaim(t *testing.T) {
	root := t.TempDir()
	repos := map[string]*fixtureRepo{}
	for _, r := range buildFixtures(t, root) {
		repos[r.Name] = r
	}

	basic := repos["ngm-fixture-basic"]
	if basic == nil {
		t.Fatal("ngm-fixture-basic was not built")
	}
	dir := filepath.Join(root, "ngm-fixture-basic")

	// annotated tag 必须是 **tag 对象**（这才会让 ls-remote 给出 `^{}` 那一行）。
	if kind := fixtureGit(t, dir, "", "cat-file", "-t", "refs/tags/v1.0.0"); kind != "tag" {
		t.Errorf("refs/tags/v1.0.0 is a %s, want a tag object", kind)
	}
	// lightweight tag 必须直接指向 commit。
	if kind := fixtureGit(t, dir, "", "cat-file", "-t", "refs/tags/v1.1.0-light"); kind != "commit" {
		t.Errorf("refs/tags/v1.1.0-light is a %s, want a commit", kind)
	}
	// 剥开之后两者必须是**不同**的 commit（否则"解析到了哪个"这件事测不出来）。
	if basic.Refs["refs/tags/v1.0.0^{}"] == basic.Refs["refs/tags/v1.1.0-light"] {
		t.Error("the two tags must point at different commits, otherwise tag resolution is untested")
	}

	listing := fixtureGit(t, dir, "", "ls-tree", "-r", "refs/tags/v1.0.0^{}")
	for _, want := range []string{
		"100755 blob", "120000 blob", "bin/run.sh", "link-to-index",
	} {
		if !strings.Contains(listing, want) {
			t.Errorf("the fixture tree must contain %q (it is the reason this repo exists):\n%s", want, listing)
		}
	}

	// 内容仓库：每一条都要能在树里看到。
	contentDir := filepath.Join(root, "ngm-fixture-content")
	cl := fixtureGit(t, contentDir, "", "ls-tree", "-r", "refs/tags/v1.0.0^{}")
	for _, want := range []string{"Foo.txt", "foo.txt", "bom.txt", "crlf.txt", "empty.txt", "中文名.js", "bin/tool"} {
		if !strings.Contains(cl, want) {
			t.Errorf("ngm-fixture-content is missing %q:\n%s", want, cl)
		}
	}

	// 大小写对：**恰好两个**条目在折叠大小写后落到同一个名字，且内容不同。
	// 前者是"落地时会撞车"的定义，后者保证撞车**可被检出**（同内容会让
	// 摘要无从分辨谁覆盖了谁）。
	names := strings.Split(fixtureGit(t, contentDir, "", "ls-tree", "-r", "--name-only", "refs/tags/v1.0.0^{}"), "\n")
	collisions := map[string]int{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		collisions[strings.ToLower(n)]++
	}
	if collisions["foo.txt"] != 2 {
		t.Errorf("want exactly two entries that collide when lowercased (Foo.txt / foo.txt), got %d:\n%v",
			collisions["foo.txt"], names)
	}
	upper := fixtureGit(t, contentDir, "", "show", "refs/tags/v1.0.0^{}:Foo.txt")
	lower := fixtureGit(t, contentDir, "", "show", "refs/tags/v1.0.0^{}:foo.txt")
	if upper == lower {
		t.Error("Foo.txt and foo.txt must hold different bytes, otherwise a case collision is undetectable")
	}

	// BOM 必须真的在字节里（3 字节 EF BB BF）。
	bom := fixtureGit(t, contentDir, "", "show", "refs/tags/v1.0.0^{}:bom.txt")
	if !strings.HasPrefix(bom, "\ufeff") {
		t.Error("bom.txt must start with a UTF-8 BOM")
	}

	// 单仓多模块：两个 ngm.json 必须都在。
	mono := fixtureGit(t, filepath.Join(root, "ngm-fixture-monorepo"), "", "ls-tree", "-r", "refs/heads/main")
	for _, want := range []string{"packages/a/ngm.json", "packages/b/ngm.json"} {
		if !strings.Contains(mono, want) {
			t.Errorf("ngm-fixture-monorepo is missing %q:\n%s", want, mono)
		}
	}
}

// TestV112PushInstructions 是人工那一步的入口：
//
//	NGM_FIXTURES_OUT=D:\tmp\fixtures go test ./cmd/ngm -run TestV112PushInstructions -v
//
// 不设该变量时它什么也不做（只验证生成器可用），因此 CI 不需要凭据。
func TestV112PushInstructions(t *testing.T) {
	out := strings.TrimSpace(os.Getenv("NGM_FIXTURES_OUT"))
	if out == "" {
		t.Skip("set NGM_FIXTURES_OUT=<dir> to materialise the fixture repositories for pushing")
	}
	repos := buildFixtures(t, out)
	t.Logf("三个裸仓库已生成在 %s", out)
	for _, r := range repos {
		t.Logf("")
		t.Logf("%s", r.Name)
		for _, ref := range fixtureSortedRefs(r.Refs) {
			t.Logf("  %-40s %s", ref, r.Refs[ref])
		}
		t.Logf("  推送：git -C %s push --mirror https://gitee.com/idcu/%s.git",
			filepath.Join(out, r.Name), r.Name)
	}
}

func fixtureSortedRefs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
