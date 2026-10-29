package lock

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

const (
	dupCommit = "0123456789abcdef0123456789abcdef01234567"
	dupDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func rootDep(name, ref string) Dependency {
	return Dependency{
		Name: name, Ref: ref, RefType: "tag",
		Commit: dupCommit, ArchiveDigest: dupDigest,
		VendorPath: "./ngm.vendor/github.com/acme/lib",
	}
}

func moduleDep(name, subPath, digest string) Dependency {
	return Dependency{
		Name: name, Ref: "v1", RefType: "tag", Commit: dupCommit,
		SubPath: subPath, ModuleDigest: "sha256:" + digest,
		VendorPath: "./ngm.vendor/github.com/acme/mono",
		SubtreeOid: "sha1:" + dupCommit,
	}
}

// v0.107：同一个依赖在 lock 里只能有一条记录。
//
// 缺陷形态：`Index()` 让**后**一条覆盖前一条，而 `Find()` 返回**前**一条。
// 于是同一份 lock 里，幽灵边判据看的是 A、`install` 落地的是 B，
// 而两者可以指向不同的 commit。"锁文件里同一个依赖有两个版本"没有任何
// 合法用途——它只可能来自手工编辑或一次写坏的合并。
func TestV107DuplicateDependencyIsRejected(t *testing.T) {
	f := &File{
		Version:         SchemaVersion,
		LockfileVersion: FileVersion,
		Dependencies: []Dependency{
			rootDep("github:acme/lib", "v1"),
			rootDep("github:acme/lib", "v2"),
		},
	}
	err := f.Validate()
	if err == nil {
		t.Fatal("two entries for the same name must be rejected")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3 (a lock describing one dependency twice is a config error)", code)
	}
	msg := errs.FormatHuman(err)
	if !strings.Contains(msg, "dependencies[0]") || !strings.Contains(msg, "[1]") {
		t.Errorf("the error must name both entries so the reader can delete one: %s", msg)
	}
	if !strings.Contains(errs.Hint(err), "ngm install") {
		t.Errorf("the error must say what to do next: %s", msg)
	}
}

// 同一个 name 的**不同 subPath** 是合法的（一仓多模块，ADR-031）——
// 判据按 name+subPath 去重，不是按 name。
func TestV107SameNameDifferentSubPathIsLegal(t *testing.T) {
	f := &File{
		Version:         SchemaVersion,
		LockfileVersion: FileVersion,
		Dependencies: []Dependency{
			moduleDep("github:acme/mono", "packages/a", strings.Repeat("a", 64)),
			moduleDep("github:acme/mono", "packages/b", strings.Repeat("b", 64)),
		},
	}
	if err := f.Validate(); err != nil {
		t.Errorf("two modules of one repository must both be lockable: %v", err)
	}
}
