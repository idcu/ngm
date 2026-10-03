package vendor

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// writeBlobFile 直接在池里放一个给定名字的 blob 文件（绕开 `Put`）。
// 它没有任何清单引用，因此是**孤儿**——正是回收要处理的对象。
func writeBlobFile(t *testing.T, store *ContentStore, name string, body []byte, mtime time.Time) string {
	t.Helper()
	p := store.BlobPath(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return p
}

func orphanName(i int) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("orphan-%d", i))))
}

// TestContentStore_PruneOrphansRemovesOnlyOldUnreferenced 是这条命令的核心契约：
// **只删"没人引用且够老"的 blob**，其余一律不动。
func TestContentStore_PruneOrphansRemovesOnlyOldUnreferenced(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	entries := storeEntries(t, store, dg)

	// 被引用的 blob：抽一个出来，跑完必须还在。
	var referenced string
	for _, e := range entries {
		if e.Full != "" {
			referenced = e.Full
			break
		}
	}
	if referenced == "" {
		t.Fatal("fixture 里没有带 blob 的条目")
	}

	now := time.Now()
	old := now.Add(-48 * time.Hour)
	// 三个孤儿：两个够老（该删）、一个刚写下（**不该删**——那是并发安装的窗口）。
	oldA := writeBlobFile(t, store, orphanName(1), []byte("old-a"), old)
	oldB := writeBlobFile(t, store, orphanName(2), []byte("old-b"), old)
	young := writeBlobFile(t, store, orphanName(3), []byte("young"), now)

	res, err := store.PruneOrphans(24*time.Hour, now, false)
	if err != nil {
		t.Fatalf("PruneOrphans: %v", err)
	}

	if len(res.Removed) != 2 {
		t.Errorf("应当只删两个够老的孤儿，实际删了 %d：%v", len(res.Removed), res.Removed)
	}
	if res.KeptYoung != 1 {
		t.Errorf("刚写下的孤儿必须被留下（否则会删掉正在安装的内容），KeptYoung=%d", res.KeptYoung)
	}
	if res.Referenced == 0 {
		t.Error("被引用的 blob 数应当 > 0（安全声明要有个数）")
	}
	for _, p := range []string{oldA, oldB} {
		if _, serr := os.Stat(p); serr == nil {
			t.Errorf("够老的孤儿应当被删：%s", p)
		}
	}
	if _, serr := os.Stat(young); serr != nil {
		t.Errorf("刚写下的孤儿不该被删：%s", young)
	}
	if _, serr := os.Stat(referenced); serr != nil {
		t.Errorf("被引用的 blob 绝不能删：%s", referenced)
	}

	// 不变量（比上面逐条更强）：跑完之后，池里**每个 blob 要么被引用、要么是年轻的**。
	u, uerr := store.Usage()
	if uerr != nil {
		t.Fatal(uerr)
	}
	if u.BlobOrphans != 1 {
		t.Errorf("跑完之后应当只剩 1 个年轻孤儿，实际 %d", u.BlobOrphans)
	}
}

// TestContentStore_PruneOrphansRefusesWhenAManifestIsUnreadable 是本命令的**安全阀**。
//
// 读不出的清单引用了什么是看不见的 → 它的 blob 会被算成孤儿。那不是"多删了一点"，
// 而是可能删掉那份清单还需要的字节。因此必须**什么都不删**，并说明出路。
func TestContentStore_PruneOrphansRefusesWhenAManifestIsUnreadable(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)

	now := time.Now()
	orphan := writeBlobFile(t, store, orphanName(1), []byte("x"), now.Add(-72*time.Hour))

	// 把那份清单写成垃圾：`readManifest` 解不出来，于是"引用集"只是下界。
	if err := os.WriteFile(store.ManifestPath(dg), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := store.PruneOrphans(time.Hour, now, false)
	if err == nil {
		t.Fatal("有清单读不出来时必须拒绝回收，而不是按下界删")
	}
	if human := errs.FormatHuman(err); !strings.Contains(human, "读不出来") {
		t.Errorf("错误要说清原因：\n%s", human)
	}
	if _, serr := os.Stat(orphan); serr != nil {
		t.Error("拒绝时一个 blob 都不能删")
	}

	// dry-run 同样拒绝：一份"将会删 X"的报告在这里是有害的（它不会发生）。
	if _, derr := store.PruneOrphans(time.Hour, now, true); derr == nil {
		t.Error("dry-run 也应当拒绝——否则它会打印一份不会执行的删除清单")
	}
}

// TestContentStore_PruneOrphansDryRunDeletesNothing 与 `prune` 的既有要求一致：
// 破坏性操作先看清楚。
func TestContentStore_PruneOrphansDryRunDeletesNothing(t *testing.T) {
	_, store := contentFixture(t, basicRepo)
	now := time.Now()
	orphan := writeBlobFile(t, store, orphanName(1), []byte("y"), now.Add(-72*time.Hour))

	res, err := store.PruneOrphans(24*time.Hour, now, true)
	if err != nil {
		t.Fatalf("PruneOrphans(dry-run): %v", err)
	}
	if len(res.Removed) != 1 || !res.DryRun {
		t.Errorf("dry-run 应当报告 1 个将被删除、且标记 DryRun：%+v", res)
	}
	if res.BytesFreed != 1 {
		t.Errorf("dry-run 的 BytesFreed 应当是「将会释放」的字节数，得到 %d", res.BytesFreed)
	}
	if _, serr := os.Stat(orphan); serr != nil {
		t.Error("dry-run 不能真的删")
	}
}

// TestContentStore_PruneOrphansOnEmptyStore 幂等 + 空 store 不是错误。
func TestContentStore_PruneOrphansOnEmptyStore(t *testing.T) {
	store := newStore(t)
	res, err := store.PruneOrphans(24*time.Hour, time.Now(), false)
	if err != nil {
		t.Fatalf("空 store 上不该报错: %v", err)
	}
	if len(res.Removed) != 0 || res.BytesFreed != 0 {
		t.Errorf("空 store 上不该删任何东西：%+v", res)
	}
	if res.Referenced != 0 {
		t.Errorf("空 store 上不该有被引用的 blob：%d", res.Referenced)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
