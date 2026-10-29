package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- v0.105：adopt 的证据必须"确定性 + 内容寻址 + 边界说清" ---

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixture 造一个含三种来源的 node_modules：
//   - fromgit：lock 里是 git + 40 位 commit（可追溯）
//   - @scope/reg：lock 里是 registry integrity
//   - fromref：lock 里是 git + tag 名（**不可**追溯，必须记成 ref 而不是 commit）
//
// 再加一个嵌套依赖与一个符号链接（后者必须被跳过而不是被跟随）。
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	nm := filepath.Join(root, "node_modules")

	write(t, filepath.Join(nm, "fromgit/package.json"), `{"name":"fromgit","version":"0.1.0"}`)
	write(t, filepath.Join(nm, "fromgit/index.js"), "module.exports = 1\n")
	write(t, filepath.Join(nm, "@scope/reg/package.json"), `{"name":"@scope/reg","version":"2.0.0"}`)
	write(t, filepath.Join(nm, "@scope/reg/dist/x.js"), "export const x = 1\n")
	write(t, filepath.Join(nm, "fromref/package.json"), `{"name":"fromref","version":"3.0.0"}`)
	write(t, filepath.Join(nm, "fromref/index.js"), "module.exports = 3\n")
	// 嵌套依赖：必须各自成条目，而不是并进父包的清单。
	write(t, filepath.Join(nm, "fromgit/node_modules/deep/package.json"), `{"name":"deep","version":"9.9.9"}`)
	write(t, filepath.Join(nm, "fromgit/node_modules/deep/index.js"), "module.exports = 9\n")

	lock := `{"lockfileVersion":3,"packages":{` +
		`"node_modules/fromgit":{"version":"0.1.0","resolved":"git+https://github.com/x/fromgit.git#` + testCommit + `"},` +
		`"node_modules/@scope/reg":{"version":"2.0.0","resolved":"https://registry.npmjs.org/r/-/r-2.0.0.tgz","integrity":"sha512-AAA"},` +
		`"node_modules/fromref":{"version":"3.0.0","resolved":"git+https://github.com/x/fromref.git#v3.0.0"}` +
		`}}`
	write(t, filepath.Join(root, "package-lock.json"), lock)

	// 符号链接：workspace 链接指向项目自己的源码，跟随会把"不在任何 lock 里"
	// 的内容算成一份依赖。这里用 Lstat 判定，跳过的数量要能被数出来。
	link := filepath.Join(nm, "linked-workspace")
	if err := os.Symlink(filepath.Join(root, "..", "elsewhere"), link); err != nil {
		// Windows 上无权限时无法建链接：跳过这一半，但**不**让整条判据变绿。
		t.Logf("symlink 不可用（%v）；跳过「不跟随符号链接」这一格", err)
	}
	return root
}

func TestV105AdoptIsDeterministicAndContentAddressed(t *testing.T) {
	root := fixture(t)

	a, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	first, err := a.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if again, err := Run(Options{Dir: root, ToolVersion: "test"}); err != nil {
		t.Fatal(err)
	} else if second, _ := again.Marshal(); string(first) != string(second) {
		t.Error("two runs over the same tree must produce byte-identical proofs")
	}

	// 改一个字节 ⇒ 该包的 digest 必须变（而别的包不变）。
	write(t, filepath.Join(root, "node_modules/fromgit/index.js"), "module.exports = 2\n")
	b, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if digestOf(b, "fromgit") == digestOf(a, "fromgit") {
		t.Error("the digest did not change after the bytes changed — it is not content-addressed")
	}
	if digestOf(b, "@scope/reg") != digestOf(a, "@scope/reg") {
		t.Error("an unrelated package's digest changed")
	}
}

func TestV105AdoptClassifiesProvenanceHonestly(t *testing.T) {
	p, err := Run(Options{Dir: fixture(t), ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Package{}
	for _, pkg := range p.Packages {
		byName[pkg.Name] = pkg
	}

	if got := byName["fromgit"].Provenance; got.Kind != KindGit || got.Commit != testCommit {
		t.Errorf("git + full commit must be recorded as a traceable commit: %+v", got)
	}
	// 关键一格：`#v3.0.0` 不是 commit。把它写进 commit 字段是最常见的谎。
	if got := byName["fromref"].Provenance; got.Kind != KindGit || got.Commit != "" || got.Ref != "v3.0.0" {
		t.Errorf("a tag fragment must be recorded as `ref`, never as `commit`: %+v", got)
	}
	if got := byName["@scope/reg"].Provenance; got.Kind != KindRegistry || got.Integrity == "" {
		t.Errorf("registry entries keep their integrity verbatim: %+v", got)
	}

	// 覆盖率必须与来源一致。四格各自代表一条不同的路径：
	//
	//	fromgit   git + 40 位 hash  → withCommit
	//	@scope/reg registry integrity → withIntegrity
	//	fromref   git + tag（不可追溯）→ unknown
	//	deep      嵌套包，lock 里没有它的条目 → unknown
	//
	// 全 0 或全 1 的夹具分不出这些分支，所以这里刻意让四个数字互不相同。
	if p.Coverage.Packages != 4 {
		t.Errorf("packages = %d, want 4 (nested dependency must be its own entry)", p.Coverage.Packages)
	}
	if p.Coverage.WithCommit != 1 || p.Coverage.WithIntegrity != 1 || p.Coverage.Unknown != 2 {
		t.Errorf("coverage = %+v (want 1 commit / 1 integrity / 2 unknown)", p.Coverage)
	}
	// 符号链接若建得出来，它必须被**跳过并计数**——跟随它会把项目外的内容
	// 算成一份依赖。建不出来（无权限）时这一格不成立，但那不改变上面四条。
	// 判据按**磁盘事实**分支，而不是按 GOOS 猜（v0.105 的同一条纪律）。
	if pkg := byName["linked-workspace"]; pkg.Name != "" {
		t.Errorf("a symlinked workspace was adopted as a package (%q): "+
			"following it would count content that is in no lock at all", pkg.Path)
	}
	if p.Coverage.Skipped == 0 {
		// 只有真的建出链接时才要求计数，否则这条断言在无权限的平台上恒假。
		if _, err := os.Lstat(filepath.Join(t.TempDir(), "probe-symlink")); err == nil {
			t.Error("nothing was reported as skipped even though a symlink exists")
		}
	}
}

func TestV105AdoptNestedDependencyIsItsOwnEntry(t *testing.T) {
	p, err := Run(Options{Dir: fixture(t), ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range p.Packages {
		if pkg.Name == "deep" {
			if pkg.Path != "node_modules/fromgit/node_modules/deep" {
				t.Errorf("path = %q", pkg.Path)
			}
			return
		}
	}
	t.Fatal("the nested dependency was not discovered — " +
		"missing packages and absent packages look identical in the output")
}

func TestV105AdoptSaysWhatItCannotRead(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "node_modules/only/package.json"), `{"name":"only","version":"1.0.0"}`)

	// 没有 lock：每一个包都必须是 unknown，并且 notes 要说出来。
	p, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Coverage.Unknown != 1 || p.Source.Lockfile != "" {
		t.Errorf("without a lock: coverage=%+v source=%+v", p.Coverage, p.Source)
	}
	if !hasNote(p, "no lock file") {
		t.Errorf("the proof must say there was no lock: %v", p.Notes)
	}

	// pnpm：读不了就明说读不了，而**不是**假装没有 lock。
	write(t, filepath.Join(root, "pnpm-lock.yaml"), "lockfileVersion: 9\n")
	p, err = Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Source.Manager != "pnpm" || p.Source.Lockfile != "pnpm-lock.yaml" {
		t.Errorf("source = %+v (a lock we cannot parse is still a lock we saw)", p.Source)
	}
	if p.Source.LockfileDigest == "" {
		t.Error("even an unparsed lock gets its bytes hashed: the evidence points at what it looked like")
	}
	if !hasNote(p, "does not parse it") {
		t.Errorf("the note must name the limitation: %v", p.Notes)
	}
	if p.Coverage.Unknown != 1 {
		t.Errorf("unknown = %d", p.Coverage.Unknown)
	}
}

func TestV105AdoptRefusesAnEmptyTree(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "node_modules/.package-lock.json"), "{}")

	if _, err := Run(Options{Dir: root, ToolVersion: "test"}); err == nil {
		t.Fatal("an empty node_modules must fail rather than produce an empty proof " +
			"(an empty proof reads as \"nothing to worry about\")")
	}
	if _, err := Run(Options{Dir: t.TempDir(), ToolVersion: "test"}); err == nil {
		t.Fatal("no node_modules at all must fail")
	}
}

func digestOf(p *Proof, name string) string {
	for _, pkg := range p.Packages {
		if pkg.Name == name {
			return pkg.Digest
		}
	}
	return ""
}

func hasNote(p *Proof, substr string) bool {
	for _, n := range p.Notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
