package adopt

import (
	"strings"
	"testing"
)

// V213 把 adopt 的多锁迁移边界钉成判据（ADR-054）：
//   - pnpm / yarn / bun.lock（文本）/ deno.lock（JSON）各一个最小样例必须提取到 commit；
//   - 畸形或无法映射的声明必须逐条进 skipped[] 并带原因，不能静默吞掉，
//     也不能把无锚点的 Git URL 编造成 commit；
//   - 同一棵树的输出逐字节确定。
//
// 全部用 t.TempDir 写小 fixture，不碰网络。

func v213FixtureRoot(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	write(t, root+"/node_modules/"+name+"/package.json", `{"name":"`+name+`","version":"1.0.0"}`)
	write(t, root+"/node_modules/"+name+"/index.js", "module.exports = 1\n")
	return root
}

func v213ProvenanceOf(t *testing.T, p *Proof, name string) Provenance {
	t.Helper()
	pkg, ok := byNameOf(t, p, name)
	if !ok {
		t.Fatalf("package %q missing: %+v", name, p.Packages)
	}
	return pkg.Provenance
}

func v213CommitOf(t *testing.T, p *Proof, name string) string {
	t.Helper()
	got := v213ProvenanceOf(t, p, name)
	if got.Kind != KindGit {
		t.Fatalf("provenance of %q = %+v, want git", name, got)
	}
	return got.Commit
}

func v213SkippedReason(t *testing.T, p *Proof, keyPart string) string {
	t.Helper()
	for _, s := range p.Skipped {
		if strings.Contains(s.Key, keyPart) {
			if s.Reason == "" {
				t.Fatalf("skipped %q has no reason", s.Key)
			}
			return s.Reason
		}
	}
	t.Fatalf("no skipped entry for %q: %+v", keyPart, p.Skipped)
	return ""
}

func TestV213AdoptReadsPnpmYarnBunAndDenoLocks(t *testing.T) {
	const sha = "2222222222222222222222222222222222222222"

	t.Run("pnpm", func(t *testing.T) {
		root := v213FixtureRoot(t, "fromgit")
		write(t, root+"/pnpm-lock.yaml", `lockfileVersion: '9.0'
packages:
  github.com/foo/fromgit/`+sha+`:
    name: fromgit
    version: 1.0.0
    resolution: {git: https://github.com/foo/fromgit.git, commit: `+sha+`}
  github.com/foo/broken/abc:
    name: broken
    version: 1.0.0
    resolution: {git: https://github.com/foo/broken.git}
  /registry@1.0.0:
    resolution: {integrity: sha512-AAA}
`)
		p, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Source.Manager != "pnpm" || p.Source.Lockfile != "pnpm-lock.yaml" {
			t.Fatalf("source = %+v", p.Source)
		}
		if got := v213CommitOf(t, p, "fromgit"); got != sha {
			t.Errorf("commit = %q, want %q", got, sha)
		}
		if reason := v213SkippedReason(t, p, "broken"); !strings.Contains(reason, "no commit anchor") {
			t.Errorf("bare git URL reason = %q", reason)
		}
		if reason := v213SkippedReason(t, p, "registry"); !strings.Contains(reason, "registry") {
			t.Errorf("registry reason = %q", reason)
		}
	})

	t.Run("yarn", func(t *testing.T) {
		root := v213FixtureRoot(t, "fromgit")
		write(t, root+"/yarn.lock", `# yarn lockfile v1
"fromgit@git+https://github.com/foo/fromgit.git":
  version "1.0.0"
  resolved "git+https://github.com/foo/fromgit.git#`+sha+`"
"broken@git+https://github.com/foo/broken.git":
  version "2.0.0"
  resolved "git+https://github.com/foo/broken.git"
`)
		p, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Source.Manager != "yarn" || p.Source.Lockfile != "yarn.lock" {
			t.Fatalf("source = %+v", p.Source)
		}
		if got := v213CommitOf(t, p, "fromgit"); got != sha {
			t.Errorf("commit = %q, want %q", got, sha)
		}
		if reason := v213SkippedReason(t, p, "broken"); !strings.Contains(reason, "no commit/ref anchor") {
			t.Errorf("bare git URL reason = %q", reason)
		}
	})

	t.Run("bun-text", func(t *testing.T) {
		root := v213FixtureRoot(t, "fromgit")
		write(t, root+"/bun.lock", `{
  "lockfileVersion": 0,
  "packages": {
    "fromgit": ["fromgit@git+https://github.com/foo/fromgit.git#`+sha+`", "", {}, "sha512-AAA"],
    "broken": ["broken@git+https://github.com/foo/broken.git", "", {}, "sha512-BBB"],
    "registry": ["registry@1.0.0", "", {}, "sha512-CCC"],
    "weird": "not-an-array"
  }
}
`)
		p, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Source.Manager != "bun" || p.Source.Lockfile != "bun.lock" {
			t.Fatalf("source = %+v", p.Source)
		}
		if got := v213CommitOf(t, p, "fromgit"); got != sha {
			t.Errorf("commit = %q, want %q", got, sha)
		}
		if reason := v213SkippedReason(t, p, "broken"); !strings.Contains(reason, "no commit/ref anchor") {
			t.Errorf("bare git URL reason = %q", reason)
		}
		if reason := v213SkippedReason(t, p, "registry"); !strings.Contains(reason, "registry") {
			t.Errorf("registry reason = %q", reason)
		}
		if reason := v213SkippedReason(t, p, "weird"); !strings.Contains(reason, "single-line array") {
			t.Errorf("malformed entry reason = %q", reason)
		}
	})

	t.Run("deno-json", func(t *testing.T) {
		root := v213FixtureRoot(t, "fromgit")
		write(t, root+"/deno.lock", `{
  "version": "4",
  "specifiers": {
    "git+https://github.com/foo/fromgit.git#`+sha+`": "`+sha+`",
    "git+https://github.com/foo/broken.git": "",
    "npm:registry-spec@1.0.0": "1.0.0"
  },
  "npm": {"registry@1.0.0": {"integrity": "sha512-AAA"}}
}
`)
		p, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if p.Source.Manager != "deno" || p.Source.Lockfile != "deno.lock" {
			t.Fatalf("source = %+v", p.Source)
		}
		if got := v213CommitOf(t, p, "fromgit"); got != sha {
			t.Errorf("commit = %q, want %q", got, sha)
		}
		if reason := v213SkippedReason(t, p, "broken.git"); !strings.Contains(reason, "no commit/ref anchor") {
			t.Errorf("bare git URL reason = %q", reason)
		}
		if reason := v213SkippedReason(t, p, "npm:registry-spec"); !strings.Contains(reason, "registry") {
			t.Errorf("npm specifier reason = %q", reason)
		}
		if reason := v213SkippedReason(t, p, "npm:registry@1.0.0"); !strings.Contains(reason, "integrity") {
			t.Errorf("npm section reason = %q", reason)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		root := v213FixtureRoot(t, "fromgit")
		write(t, root+"/bun.lock", `{"lockfileVersion":0,"packages":{
  "fromgit": ["fromgit@git+https://github.com/foo/fromgit.git#`+sha+`", "", {}, "sha512-AAA"],
  "broken": ["broken@git+https://github.com/foo/broken.git", "", {}, "sha512-BBB"],
  "weird": "not-an-array"
}}`)
		a, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		first, err := a.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		b, err := Run(Options{Dir: root, ToolVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		second, err := b.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Error("two runs over the same lock must produce byte-identical proofs")
		}
		for i := 1; i < len(a.Skipped); i++ {
			if a.Skipped[i-1].Key > a.Skipped[i].Key {
				t.Errorf("skipped[] is not sorted: %+v", a.Skipped)
			}
		}
		if !hasNote(a, "skipped[]") {
			t.Errorf("the note must point at skipped[]: %v", a.Notes)
		}
	})
}

// TestV213UnsupportedDenoVersionIsSkipped 固定版本边界：v3 不是支持的 JSON 形状，
// 整份 lock 的声明都进 skipped，绝不退化成"看起来没有来源"。
func TestV213UnsupportedDenoVersionIsSkipped(t *testing.T) {
	root := v213FixtureRoot(t, "fromgit")
	write(t, root+"/deno.lock", `{"version":"3","specifiers":{
  "git+https://github.com/foo/fromgit.git#2222222222222222222222222222222222222222":"x"}}`)
	p, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Source.Manager != "deno" || len(p.Skipped) != 1 {
		t.Fatalf("source=%+v skipped=%+v", p.Source, p.Skipped)
	}
	if !strings.Contains(p.Skipped[0].Reason, "unsupported deno.lock version") {
		t.Errorf("reason = %q", p.Skipped[0].Reason)
	}
	if got := v213ProvenanceOf(t, p, "fromgit"); got.Kind != KindUnknown {
		t.Errorf("unsupported version must not yield provenance: %+v", got)
	}
}

// TestV213BinaryBunLockIsNotRead 固定"文本不是老的 bun.lockb"这条边界。
func TestV213BinaryBunLockIsNotRead(t *testing.T) {
	root := v213FixtureRoot(t, "fromgit")
	write(t, root+"/bun.lockb", "not a text lock\n")
	p, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Source.Lockfile != "" || p.Source.Manager != "unknown" {
		t.Fatalf("a binary lock must not be claimed as parsed: %+v", p.Source)
	}
	if !hasNote(p, "bun.lockb is a binary lockfile") {
		t.Errorf("note missing: %v", p.Notes)
	}
}
