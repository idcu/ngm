package main

import (
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
)

// gc 的"可达性"是删除的唯一凭据：lock 里出现过的 digest、mirror、vendor 路径
// 一个都不能删。这条判据钉住三处易错点：模块条目用 moduleDigest（不是 archive）、
// 同一仓库的多个子模块只算一个 mirror、vendor 路径去重且排序后再交给删除层。
func TestGCReachability(t *testing.T) {
	t.Run("empty lock is empty reachability", func(t *testing.T) {
		r, err := gcReachability(&lock.File{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.digests) != 0 || len(r.mirrors) != 0 || len(r.vendorPaths) != 0 {
			t.Errorf("reachability=%+v, want empty", r)
		}
	})

	// gcReachability 对 nil lock 必须安全：命令层已经拦过"没有 lock"，但函数本身
	// 不该在 nil 上 panic——空的可达集是唯一诚实的答案。
	// （见 TestGCReachabilityNilLock。）

	t.Run("digests mirrors and vendor paths", func(t *testing.T) {
		lf := &lock.File{Dependencies: []lock.Dependency{
			{Name: "github:acme/z", VendorPath: "vendor/z", ArchiveDigest: "sha256:z"},
			{Name: "github:acme/a", VendorPath: "vendor/a", ArchiveDigest: "sha256:archive-a",
				SubPath: "packages/ui", ModuleDigest: "sha256:module-a"},
			{Name: "github:acme/a", VendorPath: "vendor/a", ArchiveDigest: "sha256:archive-a2"},
			{Name: "github:acme/m", VendorPath: "/vendor/m/", ArchiveDigest: "sha256:m"},
		}}

		r, err := gcReachability(lf)
		if err != nil {
			t.Fatal(err)
		}

		for _, want := range []string{"sha256:z", "sha256:module-a", "sha256:archive-a2", "sha256:m"} {
			if !r.digests[want] {
				t.Errorf("digest %q must be reachable", want)
			}
		}
		if r.digests["sha256:archive-a"] {
			t.Error("a module entry is proven by moduleDigest; the archive digest is not its content")
		}

		// 同一仓库三个条目（含子模块）→ 一条 mirror。
		if len(r.mirrors) != 3 {
			t.Errorf("mirrors=%v, want 3 distinct repos (z, a, m)", r.mirrors)
		}
		if !r.mirrors["github.com/acme/a.git"] {
			t.Errorf("mirrors=%v: the sibling module must not add a second mirror", r.mirrors)
		}

		// vendor/a 是 root，vendor/a/packages/ui 是同一仓库的子模块；两条都要保留。
		// 重复出现的 vendor/a（两条 root 记录）只删一次；首尾斜杠被裁掉。
		want := []string{"vendor/a", "vendor/a/packages/ui", "vendor/m", "vendor/z"}
		if len(r.vendorPaths) != len(want) {
			t.Fatalf("vendorPaths=%v, want %v", r.vendorPaths, want)
		}
		for i := range want {
			if r.vendorPaths[i] != want[i] {
				t.Errorf("vendorPaths=%v, want %v (sorted and deduped)", r.vendorPaths, want)
			}
		}
	})

	t.Run("an invalid slug is a lock defect, not a gc decision", func(t *testing.T) {
		lf := &lock.File{Dependencies: []lock.Dependency{
			{Name: "not-a-slug", VendorPath: "vendor/x", ArchiveDigest: "sha256:x"},
		}}
		_, err := gcReachability(lf)
		if err == nil {
			t.Fatal("an unparseable name must not be treated as 'unreachable, delete it'")
		}
		if got := errs.ExitCode(err); got != 3 {
			t.Errorf("exit=%d, want 3 (configuration)", got)
		}
		if hint := errs.Hint(err); hint == "" {
			t.Error("the hint must tell the user to regenerate the lock")
		}
	})
}

// nil lock 在命令层已经被拦下（"no ngm.lock"），但函数本身也要安全：
// 可达集是一张"还能留什么"的表，nil 表示什么都不知道，空表就是诚实的答案。
func TestGCReachabilityNilLock(t *testing.T) {
	r, err := gcReachability(nil)
	if err != nil {
		t.Fatalf("a nil lock must not be an error: %v", err)
	}
	if len(r.digests) != 0 || len(r.mirrors) != 0 || len(r.vendorPaths) != 0 {
		t.Errorf("reachability=%+v, want empty", r)
	}
}
