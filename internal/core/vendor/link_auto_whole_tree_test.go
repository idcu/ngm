package vendor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// H1（评估报告 M1）：vendor.linkMode=auto 在 hardlink 不可用时应**整树**降级为 copy，
// 而不是"前半 hardlink、后半 copy"——后者返回 Mode=Copy 却留下硬链接，使"就地编辑
// 污染 content store"的风险悄悄回来（vendor-layers.md:141 承诺整树复制、:249 警告污染）。
//
// 本判据用 linkEntriesWithHardlink 的注入口在**跨平台**模拟"探针判定不可用"，
// 无需真的挂载第二个卷。两条不变量：
//  1. hardlinkOK=false ⇒ 全程 copy，磁盘上没有任何文件与 blob 同 inode；
//  2. 探针 OK 但中途某个 os.Link 失败 ⇒ fail-closed 报错，绝不静默产出混合树。
func TestAutoLinkFallsBackWholeTreeNotMixed(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	entries := storeEntries(t, store, dg) // basicRepo: 3 个普通文件

	t.Run("probe-unavailable-copies-every-file", func(t *testing.T) {
		vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
		lt := NewLinkTree(vendorRoot, store, LinkAuto)
		dst := filepath.Join(vendorRoot, "github.com/test/repo")

		used, files, degraded, err := lt.linkEntriesWithHardlink(entries, dst, false, os.Link)
		if err != nil {
			t.Fatalf("linkEntriesWithHardlink: %v", err)
		}
		if used != LinkCopy {
			t.Errorf("mode=%s want copy (hardlink unavailable)", used)
		}
		if !degraded {
			t.Errorf("degraded=false want true")
		}
		if files != len(entries) {
			t.Errorf("files=%d want %d", files, len(entries))
		}
		for _, e := range entries {
			vp := filepath.Join(dst, filepath.FromSlash(e.Path))
			if SameFile(vp, e.Full) {
				t.Errorf("mixed tree: %s is still a hardlink to the blob; whole-tree copy promised", e.Path)
			}
			got, rerr := os.ReadFile(vp)
			if rerr != nil || len(got) == 0 {
				t.Errorf("copy missing/empty at %s: err=%v len=%d", e.Path, rerr, len(got))
			}
		}
	})

	t.Run("probe-ok-then-link-fails-is-fatal-not-mixed", func(t *testing.T) {
		// 探针说整树可用，但中途某个链接失败（例如 blob 被并发删、目标被占用）——
		// 必须 fail-closed 报错，不能静默把该文件改成 copy 而留下半 hardlink 半 copy。
		vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
		lt := NewLinkTree(vendorRoot, store, LinkAuto)
		dst := filepath.Join(vendorRoot, "github.com/test/repo")
		calls := 0
		failingLink := func(oldpath, newpath string) error {
			calls++
			if calls == 2 {
				return errors.New("simulated mid-tree link failure")
			}
			return os.Link(oldpath, newpath)
		}

		_, _, _, err := lt.linkEntriesWithHardlink(entries, dst, true, failingLink)
		if err == nil {
			t.Fatal("expected an error when a hardlink fails mid-tree after a successful probe; " +
				"silent per-file degradation would produce a mixed tree")
		}
	})
}
