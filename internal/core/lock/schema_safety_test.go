package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// IsSafeRelPath 是"把上游给的路径拼到根目录下"之前的唯一一道格式闸门。
// 它防的不是格式好看，而是**逃出根目录**：绝对路径、盘符、`..`、空段、反斜杠/
// NUL。这条判据逐类钉住"拒绝"，并保留一个"正常相对路径必须放行"的正向用例——
// 否则一道只会拒绝的闸门会误杀掉所有合法输入。
func TestIsSafeRelPathBoundaries(t *testing.T) {
	accept := []string{
		"a",
		"a/b/c",
		"packages/ui/src/index.ts",
		".hidden/file", // `.hidden` 是普通文件名，不是 "." 段
		"a/b c/d",      // 空格是合法文件名字符
	}
	for _, p := range accept {
		if !IsSafeRelPath(p) {
			t.Errorf("IsSafeRelPath(%q) = false, want true", p)
		}
	}

	reject := []string{
		"",
		"/etc/passwd",
		"/",
		"C:/Windows",
		"C:x",
		`a\b`,
		"a\x00b",
		"a//b",
		"a/./b",
		"a/../b",
		"..",
		".",
		"../x",
		"https://evil.example/x",
		"git://evil.example/x",
		// slug / scp 形状同样是"值不是路径"：两者都会被调用方 filepath.Join 到
		// vendor 根之下（verify 的 vendor 树、--sandbox 的脚本定位），必须先拒绝。
		"github:org/repo",
		"git@github.com:org/repo",
	}
	for _, p := range reject {
		if IsSafeRelPath(p) {
			t.Errorf("IsSafeRelPath(%q) = true, want false (it can escape the root)", p)
		}
	}
}

// isSubtreeOid 的边界：算法前缀必须认识、长度必须与算法一致、字符必须是 hex。
// 它挡的是"把任意字符串当 Git tree oid 写进 lock"——那会让 verify 拿它去
// `git ls-tree` 一个不存在的对象。
func TestIsSubtreeOidBoundaries(t *testing.T) {
	accept := []string{
		"sha1:" + strings.Repeat("a", 40),
		"sha256:" + strings.Repeat("b", 64),
		"sha1:" + strings.Repeat("A", 40), // Git 输出是小写，但校验不该因大小写拒绝同一个 oid
	}
	for _, s := range accept {
		if !isSubtreeOid(s) {
			t.Errorf("isSubtreeOid(%q) = false, want true", s)
		}
	}

	reject := []string{
		"",
		"sha1",
		"sha1:" + strings.Repeat("a", 39),
		"sha1:" + strings.Repeat("a", 41),
		"sha256:" + strings.Repeat("a", 40), // 长度与算法不符
		"md5:" + strings.Repeat("a", 32),    // 未认识的算法
		"sha1:" + strings.Repeat("z", 40),   // 非 hex
		"sha1:" + strings.Repeat("a", 40) + "x",
	}
	for _, s := range reject {
		if isSubtreeOid(s) {
			t.Errorf("isSubtreeOid(%q) = true, want false", s)
		}
	}
}

// Read 的前置检查与路径推导：目录不算 lock（否则会在目录上做 JSON 解码），
// 不存在的文件不算；Find 必须把文件名拼在项目根下而不是当前目录。
func TestLockFileLocationHelpers(t *testing.T) {
	dir := t.TempDir()

	if LooksLikeLock(dir) {
		t.Error("a directory must not look like a lockfile")
	}
	if LooksLikeLock(filepath.Join(dir, FileName)) {
		t.Error("a missing lockfile must not look like one")
	}
	if got, want := Find(dir), filepath.Join(dir, FileName); got != want {
		t.Errorf("Find(%q) = %q, want %q", dir, got, want)
	}

	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !LooksLikeLock(p) {
		t.Error("a regular file must look like a lockfile")
	}
}

// 条目身份的唯一定义处：Key 是 requiredBy 边的引用口径，ContentDigest 是
// "这个条目到底拿什么证明"的分派点。调用方若自己按 SubPath 分支，两处会漂。
func TestDependencyIdentityAndDigest(t *testing.T) {
	root := Dependency{Name: "github:acme/root", ArchiveDigest: "sha256:root"}
	if root.Key() != "github:acme/root" {
		t.Errorf("root Key = %q", root.Key())
	}
	if root.IsModule() {
		t.Error("an empty subPath is the root entry, not a module")
	}
	if got := root.ContentDigest(); got != "sha256:root" {
		t.Errorf("root ContentDigest = %q, want the archive digest", got)
	}

	mod := Dependency{
		Name:          "github:acme/root",
		SubPath:       "packages/ui",
		ArchiveDigest: "sha256:should-not-be-used",
		ModuleDigest:  "sha256:module",
	}
	if got, want := mod.Key(), "github:acme/root#packages/ui"; got != want {
		t.Errorf("module Key = %q, want %q", got, want)
	}
	if !mod.IsModule() {
		t.Error("a non-empty subPath must be a module entry")
	}
	if got := mod.ContentDigest(); got != "sha256:module" {
		t.Errorf("module ContentDigest = %q, want the module digest (not the archive one)", got)
	}

	f := &File{Dependencies: []Dependency{root, mod}}
	if d, ok := f.Find("github:acme/root", ""); !ok || d != &f.Dependencies[0] {
		t.Errorf("Find root = %v/%v", d, ok)
	}
	if d, ok := f.Find("github:acme/root", "packages/ui"); !ok || d != &f.Dependencies[1] {
		t.Errorf("Find module = %v/%v", d, ok)
	}
	if _, ok := f.Find("github:acme/root", "packages/web"); ok {
		t.Error("Find must not ignore subPath: same repo at another subPath is a different entry")
	}
	if _, ok := (*File)(nil).Find("x", ""); ok {
		t.Error("a nil lock must not find anything")
	}

	idx := f.Index()
	if len(idx) != 2 || idx["github:acme/root"] == nil || idx["github:acme/root#packages/ui"] == nil {
		t.Errorf("Index keys = %v", idx)
	}
}

// nil 接收者必须像 Find 一样安全：消费端（Validate/why）会在"lock 不存在"时
// 拿到 nil，而 Index 的返回值是一张表，空表就是诚实的"没有条目"。
func TestNilFileIndexIsEmpty(t *testing.T) {
	var f *File
	idx := f.Index()
	if len(idx) != 0 {
		t.Fatalf("(*File)(nil).Index() = %v, want an empty map", idx)
	}
}
