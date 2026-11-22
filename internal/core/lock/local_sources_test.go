package lock

import (
	"testing"
)

// TestLocalDirLockEntry 覆盖 local-dir 来源的 lock 条目契约（ADR-052）：
//   - 用 dirDigest 作内容锚点，不带任何 git 证明字段；
//   - ContentDigest() 返回 dirDigest；
//   - 缺 dirDigest / 混入 commit 都被拒。
func TestLocalDirLockEntry(t *testing.T) {
	good := Dependency{
		Name:       "local:kernel",
		Ref:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RefType:    "local-dir",
		SourcePath: "./packages/kernel",
		DirDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ResolvedAt: "2026-11-22T00:00:00Z",
		VendorPath: "local/kernel",
		RequiredBy: []string{RootRequirer},
	}
	if err := good.validate(); err != nil {
		t.Fatalf("valid local-dir lock entry rejected: %v", err)
	}
	if got := good.ContentDigest(); got != good.DirDigest {
		t.Fatalf("ContentDigest = %q, want dirDigest %q", got, good.DirDigest)
	}

	// 缺 dirDigest
	bad := good
	bad.DirDigest = ""
	if err := bad.validate(); err == nil {
		t.Fatal("local-dir entry without dirDigest must be rejected")
	}

	// 混入 git 证明字段
	bad = good
	bad.Commit = "abc123def4567890abcdef1234567890abcdef12"
	if err := bad.validate(); err == nil {
		t.Fatal("local-dir entry with `commit` must be rejected")
	}

	// 不安全的 vendorPath
	bad = good
	bad.VendorPath = "../escape"
	if err := bad.validate(); err == nil {
		t.Fatal("local-dir entry with escaping vendorPath must be rejected")
	}
}
