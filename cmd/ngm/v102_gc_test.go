package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.102：`ngm gc` 的可达性口径与安全边界。
//
// 判据的来源刻意分开：
//   - store / mirror 的不可达对象由"装过、然后从声明里删掉"产生（真实残留）；
//   - vendor 的不可达对象手工放进 ngm.vendor（install 会清掉上一版 lock 的旧目录，
//     所以真实流程不会留下它——手工构造正是为了覆盖"用户删了项目/手工放置"的形状）。

// gcProject 建一个同时声明两个依赖的项目，返回项目目录与"将被回收"依赖的 lock 条目。
func gcProject(t *testing.T) (proj string, drop lock.Dependency) {
	t.Helper()
	m5Upstream(t, "github:m5/gckeep", "export const keep = 1;\n")
	m5Upstream(t, "github:m5/gcdrop", "export const drop = 1;\n")
	proj = m5Project(t,
		"github:m5/gckeep v1 tag",
		"github:m5/gcdrop v1 tag")
	setVendorConfig(t, proj, `{"mode":"local","linkMode":"copy"}`)
	drop = m5Install(t, proj, "github:m5/gcdrop")

	// 从声明里删掉后者，再 install：lock 不再引用它，而 mirror / content store
	// 的字节留在原处——这正是 gc 要回收的那类对象。
	if code, out := runCaptureCode(t, "remove", "github:m5/gcdrop", "--dir="+proj); code != 0 {
		t.Fatalf("remove: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("re-install: %s", out)
	}
	return proj, drop
}

func gcJSON(t *testing.T, args ...string) (int, gcReport, string) {
	t.Helper()
	code, out := runCaptureCode(t, append([]string{"gc", "--json"}, args...)...)
	var rep gcReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("gc --json is not JSON: %v\n%s", err, out)
	}
	return code, rep, out
}

// gcFind 在候选清单里找 id；found 为 false 时返回零值。
func gcFind(items []gcItem, id string) (gcItem, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return gcItem{}, false
}

func gcStoreDirs(t *testing.T, digest string) []string {
	t.Helper()
	store := vendor.NewContentStore(defaultLayoutForTest(t).ContentRoot())
	return []string{store.PathForDigest(digest), store.V2Dir(digest)}
}

func TestV102GCReclaimsUnreachableOnly(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	proj, drop := gcProject(t)
	layout := defaultLayoutForTest(t)

	dropMirror := filepath.Join(layout.MirrorRoot(), "github.com", "m5", "gcdrop.git")
	keepMirror := filepath.Join(layout.MirrorRoot(), "github.com", "m5", "gckeep.git")
	dropStore := gcStoreDirs(t, drop.ContentDigest())

	// 手工放一个"当前 lock 不引用"的 vendor 条目。
	ghost := filepath.Join(proj, vendor.VendorDirName, "github.com", "gc", "ghost")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghost, "index.ts"), []byte("export const ghost = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	keepVendor := m5VendorFile(proj, "github.com/m5/gckeep", "", "index.ts")

	// ---- dry run：报告三类候选，且一个字节都不删
	code, rep, out := gcJSON(t, "--dir="+proj)
	if code != 0 {
		t.Fatalf("dry run exit=%d:\n%s", code, out)
	}
	if !rep.DryRun || rep.Applied {
		t.Errorf("gc without --apply must be a dry run: %+v", rep)
	}
	if _, ok := gcFind(rep.Store.Candidates, drop.ContentDigest()); !ok {
		t.Errorf("store candidates should include the dropped digest %s:\n%s", drop.ContentDigest(), out)
	}
	if _, ok := gcFind(rep.Mirror.Candidates, "github.com/m5/gcdrop.git"); !ok {
		t.Errorf("mirror candidates should include the dropped repo:\n%s", out)
	}
	if _, ok := gcFind(rep.Vendor.Candidates, "github.com/gc"); !ok {
		t.Errorf("vendor candidates should include the unreachable ghost subtree:\n%s", out)
	}
	// v2 条目只有清单目录（blob 在池里），`sha256/<hex>` 通常不存在；
	// 这里只钉住"清单目录还在"。
	if _, err := os.Stat(dropStore[1]); err != nil {
		t.Fatalf("dry run deleted %s: %v", dropStore[1], err)
	}
	if _, err := os.Stat(dropMirror); err != nil {
		t.Fatalf("dry run deleted %s: %v", dropMirror, err)
	}
	if _, err := os.Stat(ghost); err != nil {
		t.Fatalf("dry run deleted %s: %v", ghost, err)
	}

	// ---- apply：候选消失，被引用的对象留下
	code, rep, out = gcJSON(t, "--apply", "--dir="+proj)
	if code != 0 {
		t.Fatalf("apply exit=%d:\n%s", code, out)
	}
	if rep.DryRun || !rep.Applied {
		t.Errorf("--apply must report applied=true: %+v", rep)
	}
	if rep.Store.Removed == 0 || rep.Mirror.Removed == 0 || rep.Vendor.Removed == 0 {
		t.Errorf("apply should remove one object per layer: store=%d mirror=%d vendor=%d",
			rep.Store.Removed, rep.Mirror.Removed, rep.Vendor.Removed)
	}
	for _, dir := range dropStore {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("store entry %s should be gone, err=%v", dir, err)
		}
	}
	if _, err := os.Stat(dropMirror); !os.IsNotExist(err) {
		t.Errorf("dropped mirror %s should be gone, err=%v", dropMirror, err)
	}
	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Errorf("ghost vendor entry should be gone, err=%v", err)
	}

	// 被 lock 引用的对象在两次运行后都还在。
	if _, err := os.Stat(keepMirror); err != nil {
		t.Errorf("reachable mirror was removed: %v", err)
	}
	if info, err := os.Stat(filepath.Join(layout.ContentRoot(), "trees")); err != nil || !info.IsDir() {
		t.Errorf("content store trees should survive gc: %v", err)
	}
	keepDirs := gcStoreDirs(t, readGCReachableDigest(t, proj, "github:m5/gckeep"))
	for _, dir := range keepDirs {
		if _, err := os.Stat(dir); err != nil && !os.IsNotExist(err) {
			t.Errorf("reachable content entry %s became unreadable: %v", dir, err)
		}
	}
	// v2 的"存在"判据是清单目录；`sha256/<hex>` 本来就不该被创建。
	if _, err := os.Stat(keepDirs[1]); err != nil {
		t.Errorf("reachable content manifest %s was removed: %v", keepDirs[1], err)
	}
	if _, err := os.Stat(keepVendor); err != nil {
		t.Errorf("reachable vendor file was removed: %v", err)
	}

	// ---- 二次运行：0 变化
	code, rep, out = gcJSON(t, "--dir="+proj)
	if code != 0 {
		t.Fatalf("second dry run exit=%d:\n%s", code, out)
	}
	if rep.Total.Objects != 0 {
		t.Errorf("second run must find nothing, got %d object(s):\n%s", rep.Total.Objects, out)
	}

	// 人类输出默认也是 dry-run，且明确要求 --apply；二次运行应报"无可回收"。
	hcode, hout := runCaptureCode(t, "gc", "--dir="+proj)
	if hcode != 0 || !strings.Contains(hout, "dry run") {
		t.Errorf("gc without --apply must print a dry-run header (exit=%d):\n%s", hcode, hout)
	}
	if !strings.Contains(hout, "nothing to reclaim") {
		t.Errorf("second run should say there is nothing to reclaim:\n%s", hout)
	}
	// 被引用的对象必须在**两次运行之后**仍然在位（dry-run 不删，apply 也不碰它们）。
	for _, p := range []string{keepMirror, keepDirs[1], keepVendor} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("reachable object %s did not survive both runs: %v", p, err)
		}
	}
}

// TestV102GCDeletionErrorsBecomeExitFour 测量"删除失败汇总 ⇒ 退出码 4"。
//
// 它刻意是**单元判据**：真实的 os.RemoveAll 失败无法跨平台确定性构造
// （Windows 用占用中的句柄、POSIX 用目录权限，两者互不通用），
// 但"收集到的错误如何折成退出码"是可以精确钉住的。
func TestV102GCDeletionErrorsBecomeExitFour(t *testing.T) {
	t.Run("deletion errors", func(t *testing.T) {
		for _, rep := range []gcReport{
			{Store: gcClass{Errors: []string{"store: boom"}}},
			{Mirror: gcClass{Errors: []string{"mirror: boom"}}},
			{Vendor: gcClass{Errors: []string{"vendor: boom"}}},
		} {
			if got := gcExitCode(rep); got != 4 {
				t.Errorf("gcExitCode(%+v)=%d, want 4", rep, got)
			}
		}
		if got := gcExitCode(gcReport{}); got != 0 {
			t.Errorf("gcExitCode(clean)=%d, want 0", got)
		}
	})
}

// readGCReachableDigest 从 lock 里读回一个仍在引用的 digest。
func readGCReachableDigest(t *testing.T, proj, slug string) string {
	t.Helper()
	lf, err := lock.Read(lock.Find(proj))
	if err != nil || lf == nil {
		t.Fatalf("read lock: %v", err)
	}
	d, ok := lf.Find(slug, "")
	if !ok {
		t.Fatalf("%s missing from lock", slug)
	}
	return d.ContentDigest()
}

// TestV102GCKeepsGlobalVendor 钉住安全边界：
// global vendor 的不可达条目**不删**、给出 warning，而 store/mirror 照常回收。
func TestV102GCKeepsGlobalVendor(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	proj, _ := gcProject(t)
	layout := defaultLayoutForTest(t)
	gp, err := json.Marshal(layout.GlobalVendorRoot())
	if err != nil {
		t.Fatal(err)
	}
	setVendorConfig(t, proj, fmt.Sprintf(`{"mode":"global","globalPath":%s}`, gp))
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install with global vendor: %s", out)
	}
	globalRoot := layout.GlobalVendorRoot()

	ghost := filepath.Join(globalRoot, "github.com", "gc", "shared-ghost")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghost, "index.ts"), []byte("export const shared = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, rep, out := gcJSON(t, "--apply", "--dir="+proj)
	if code != 0 {
		t.Fatalf("gc --apply exit=%d:\n%s", code, out)
	}
	if len(rep.Vendor.Kept) == 0 {
		t.Errorf("global vendor candidates should be reported as kept:\n%s", out)
	}
	if _, ok := gcFind(rep.Vendor.Kept, "github.com/gc"); !ok {
		t.Errorf("kept list should name the shared ghost subtree:\n%s", out)
	}
	if rep.Vendor.Removed != 0 || rep.Vendor.Objects != 0 {
		t.Errorf("global vendor must never be deleted: removed=%d objects=%d", rep.Vendor.Removed, rep.Vendor.Objects)
	}
	if _, err := os.Stat(ghost); err != nil {
		t.Errorf("global vendor entry was deleted: %v", err)
	}
	if len(rep.Warnings) == 0 || !strings.Contains(rep.Warnings[0], "vendor.mode=global") {
		t.Errorf("global vendor keep should come with a warning: %+v", rep.Warnings)
	}
	// 其它两层照常回收。
	if rep.Store.Removed == 0 || rep.Mirror.Removed == 0 {
		t.Errorf("store/mirror should still be reclaimed: store=%d mirror=%d",
			rep.Store.Removed, rep.Mirror.Removed)
	}
}
