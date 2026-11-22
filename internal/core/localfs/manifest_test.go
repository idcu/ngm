package localfs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSourcePath(t *testing.T) {
	good := []string{"./packages/kernel", "packages/packs/base", "core/util", "a/b/c"}
	for _, p := range good {
		if err := ValidateSourcePath(p); err != nil {
			t.Errorf("ValidateSourcePath(%q) should pass: %v", p, err)
		}
	}
	bad := []string{
		"", "   ", "/abs/path", `C:\abs\path`, `\\server\share`,
		"file:///x", "https://x/y", "../up", "a/../b", `a\..\b`, "a\x00b", "a\nb",
	}
	for _, p := range bad {
		if err := ValidateSourcePath(p); err == nil {
			t.Errorf("ValidateSourcePath(%q) should fail", p)
		}
	}
}

func TestResolveSourcePathStaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveSourcePath(root, "./packages/kernel")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "packages", "kernel")
	if got != want {
		t.Fatalf("resolved %q, want %q", got, want)
	}
	// 纵深防御：即便声明绕过语法校验（直接调 Resolve），也不能逃出根。
	if _, err := ResolveSourcePath(root, "../escape"); err == nil {
		t.Fatal("escaping sourcePath must be rejected")
	}
}

func TestBuildDirDigestDeterministic(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.js", "module.exports = 1;\n")
	write("docs/readme.md", "# doc\n")
	write("test/x.js", "test\n")
	write("node_modules/should-be-excluded.js", "noise\n")

	d1, err := BuildDirDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d1, "sha256:") || len(d1) != len("sha256:")+64 {
		t.Fatalf("bad digest shape: %q", d1)
	}
	d2, err := BuildDirDigest(dir)
	if err != nil || d2 != d1 {
		t.Fatalf("not deterministic: %q vs %q (%v)", d1, d2, err)
	}

	// docs/ 与 test/ 纳入身份（spec §10 决策 3）
	if err := os.WriteFile(filepath.Join(dir, "docs", "readme.md"), []byte("# changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d3, err := BuildDirDigest(dir)
	if err != nil || d3 == d1 {
		t.Fatalf("docs/ change must change digest: %q vs %q", d1, d3)
	}

	// node_modules/ 排除：内容变化不影响 digest
	d4before, _ := BuildDirDigest(dir)
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "should-be-excluded.js"), []byte("changed noise\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d4after, _ := BuildDirDigest(dir)
	if d4after != d4before {
		t.Fatalf("node_modules change must NOT change digest: %q vs %q", d4before, d4after)
	}
}

func TestBuildDirDigestModeMapping(t *testing.T) {
	dir := t.TempDir()
	normal := filepath.Join(dir, "normal.js")
	exec := filepath.Join(dir, "exec.sh")
	if err := os.WriteFile(normal, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exec, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	recs, err := CollectRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, r := range recs {
		byPath[r.Path] = r.Mode
	}
	if byPath["normal.js"] != "100644" || byPath["exec.sh"] != "100755" {
		t.Fatalf("mode mapping wrong: %#v", byPath)
	}
}

func TestBuildDirDigestSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.js")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.js")
	if err := os.Symlink("target.js", link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	recs, err := CollectRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recs {
		if r.Path == "link.js" && r.Mode == "120000" {
			found = true
		}
	}
	if !found {
		t.Fatalf("symlink record missing: %#v", recs)
	}
}
