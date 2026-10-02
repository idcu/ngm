package vendor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/testutils"
)

// 本文件的主题是 **v0.8 B 阶段的双布局验收**（ADR-019 §迁移方案）：
//
//	"读路径兼容 v1 与 v2（关键）……否则升级后旧项目会**立刻**验不过，
//	 而那正是本项目最忌的失败形态。"
//
// 所以这里不测"v2 怎么写"，而测"**v1 的数据还在时，一切照旧**"。

// downgradeToV1 把刚写入的 v2 store"退回"成 v1 布局，用来模拟升级前的旧 store。
//
// 为什么不给生产代码加一个"写 v1"的开关：写路径只写 v2（ADR-019 定的是**单向升级**），
// 为测试保留一条没人走的写路径，等于让"两种写路径"长期共存。反过来构造更诚实：
// v1 的数据形态是公开的（一棵树 + meta.json），这里按它的定义从条目重建——
// 而这恰好就是"升级后旧项目仍然可读"的那个场景。
func downgradeToV1(t *testing.T, store *ContentStore, dg string) {
	t.Helper()

	treeRoot := store.TreePath(dg)
	for _, e := range storeEntries(t, store, dg) {
		p := filepath.Join(treeRoot, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if e.Symlink {
			if err := os.Symlink(e.LinkTarget, p); err != nil {
				t.Skipf("this environment cannot create symlinks, so a v1 tree cannot be built: %v", err)
			}
			continue
		}
		body, ok, rerr := store.ReadFile(dg, e.Path)
		if rerr != nil || !ok {
			t.Fatalf("ReadFile(%s): ok=%v err=%v", e.Path, ok, rerr)
		}
		if err := os.WriteFile(p, body, permForMode(e.Mode)); err != nil {
			t.Fatal(err)
		}
		// 显式 chmod：`os.WriteFile` 的 perm 会被 umask 削（例如 umask 077 会把 0755 变成 0700），
		// 而本夹具的意义正是"v1 树里带着可执行位"——被 umask 削掉就会让断言测错东西。
		if err := os.Chmod(p, permForMode(e.Mode)); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := store.ReadMeta(dg)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if werr := writeJSON(store.MetaPath(dg), meta); werr != nil {
		t.Fatal(werr)
	}
	// 去掉 v2 那半边——现在这就是一个"只有 v1 数据"的 store（blob 池留着无妨：
	// 读路径根本不会去看它，而留着正好证明"判定不依赖 blob"）。
	if rerr := os.RemoveAll(store.V2Dir(dg)); rerr != nil {
		t.Fatal(rerr)
	}
	if store.hasManifest(dg) {
		t.Fatal("precondition failed: the v2 manifest is still there")
	}
}

// TestContentStore_V1LegacyStoreStaysReadable 是 ADR-019 迁移方案的核心断言：
// **旧 store 不需要重装**，所有读路径照旧工作。
func TestContentStore_V1LegacyStoreStaysReadable(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, basicRepo)
	downgradeToV1(t, store, dg)

	// 1) 存在性与 meta：走的是双布局的入口
	if !store.Has(dg) {
		t.Fatal("Has must accept a v1 store — otherwise every old project fails right after upgrade")
	}
	meta, err := store.ReadMeta(dg)
	if err != nil {
		t.Fatalf("ReadMeta must read the v1 meta.json: %v", err)
	}
	if meta.Digest != dg || meta.Repo != "github:test/fixture" {
		t.Errorf("v1 meta read wrong: %+v", meta)
	}
	if _, merr := os.Stat(store.MetaPathV2(dg)); !os.IsNotExist(merr) {
		t.Errorf("precondition: v2 meta must not exist, stat err=%v", merr)
	}

	// 2) 条目：v1 靠**扫描树目录**得到，形状与 v2 一致
	entries := storeEntries(t, store, dg)
	if len(entries) != 3 {
		t.Fatalf("entries=%d want 3", len(entries))
	}
	for _, e := range entries {
		if e.Full == "" && !e.Symlink {
			t.Errorf("%s: a v1 entry must point at a file in the tree", e.Path)
		}
		if e.Full != "" && !pathUnder(e.Full, store.TreePath(dg)) {
			t.Errorf("%s: v1 entries must live inside the tree dir, got %s", e.Path, e.Full)
		}
	}

	// 3) 取字节：与布局无关的入口
	body, ok, rerr := store.ReadFile(dg, "src/deep/mod.ts")
	if rerr != nil || !ok {
		t.Fatalf("ReadFile on a v1 store: ok=%v err=%v", ok, rerr)
	}
	if string(body) != "export const m = 2\n" {
		t.Errorf("v1 content=%q", body)
	}

	// 4) 落地照旧（这是"旧项目升级后立刻能用"的真正含义）
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	res, merr := NewLinkTree(vendorRoot, store, LinkAuto).Materialize(dg, "github.com/test/repo", "")
	if merr != nil {
		t.Fatalf("Materialize on a v1 store: %v", merr)
	}
	if vr := verifyAgainstStore(t, store, dg, res.Path, true); !vr.OK() {
		t.Errorf("v1 store should still materialize a matching tree: %v", vr.Mismatches)
	}
}

// TestContentStore_V1KeepsWholeDirSymlinkMode 记录一个**有意保留的不对称**：
// symlink 模式在 v1 数据上仍然是"整目录链接"（老语义），只有 v2 数据才退化为逐条目。
//
// 为什么不在升级时把老 store 也统一成逐条目：那需要先把 v1 数据重写成 v2——
// 而 ADR-019 明确否掉了"就地迁移"（层 2 是派生物，用到才写）。于是"两种布局暂时
// 各自保持自己的语义"是这套方案的自然结果，写在这里免得将来被当成 bug。
func TestContentStore_V1KeepsWholeDirSymlinkMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		probe := filepath.Join(t.TempDir(), "probe")
		if err := os.Symlink("t", probe); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	store := newStore(t)
	_, dg := putFixture(t, store, basicRepo)
	downgradeToV1(t, store, dg)

	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	res, err := NewLinkTree(vendorRoot, store, LinkSymlink).Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if res.Mode != LinkSymlink {
		t.Errorf("mode=%s want %s (a v1 tree can still be linked as a whole dir)", res.Mode, LinkSymlink)
	}
	target, lerr := os.Readlink(res.Path)
	if lerr != nil {
		t.Fatalf("readlink: %v", lerr)
	}
	if target != store.TreePath(dg) {
		t.Errorf("symlink target=%q want %q", target, store.TreePath(dg))
	}
}

// TestContentStore_EntriesAgreeAcrossLayouts 是 B 阶段的验收句：
// **同一 digest 在两种布局下 `Entries` 必须一致**。
//
// 它是"布局对判定层透明"这句话的可执行版本：如果哪天有人在 v2 的转换里漏掉
// size、symlink 或 mode，唯一被放行的东西就是这条断言。
func TestContentStore_EntriesAgreeAcrossLayouts(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, execRepo)

	fromManifest := storeEntries(t, store, dg) // v2：读清单
	downgradeToV1(t, store, dg)
	fromTree := storeEntries(t, store, dg) // v1：扫树目录

	if len(fromManifest) != len(fromTree) {
		t.Fatalf("entry counts differ: v2=%d v1=%d", len(fromManifest), len(fromTree))
	}
	for i := range fromManifest {
		a, b := fromManifest[i], fromTree[i]
		if a.Path != b.Path {
			t.Fatalf("entry %d: path %q vs %q", i, a.Path, b.Path)
		}
		if a.Symlink != b.Symlink {
			t.Errorf("%s: symlink %v vs %v", a.Path, a.Symlink, b.Symlink)
		}
		if a.LinkTarget != b.LinkTarget {
			t.Errorf("%s: link target %q vs %q", a.Path, a.LinkTarget, b.LinkTarget)
		}
		if !a.Symlink && a.Size != b.Size {
			t.Errorf("%s: size %d vs %d", a.Path, a.Size, b.Size)
		}
		// mode：v2 来自 Git 的清单，v1 只能从文件权限反推。POSIX 上两者必须一致；
		// Windows 表达不了可执行位，因此 v1 那一侧**必然**只能得到 100644。
		if runtime.GOOS != "windows" && a.Mode != b.Mode {
			t.Errorf("%s: mode %q vs %q", a.Path, a.Mode, b.Mode)
		}
	}
}

// TestContentStore_BlobKeyFoldsInMode 把 `blobKey` 的一处**代价**写成断言。
//
// 同一份字节、两种 mode → **两个 blob**。这不是缺陷，是有意的：
// hardlink 共享 inode，因此 blob 必须自带正确权限（见 blobKey）。写下来是为了
// 让将来翻 store 的人看到"同一内容存了两份"时有一个确定的答案，而不是把它当 bug 修掉。
func TestContentStore_BlobKeyFoldsInMode(t *testing.T) {
	const body = "#!/bin/sh\necho hi\n"
	store := newStore(t)
	_, dg := putFixture(t, store, func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.AddExecutable("bin/run.sh", body) // 100755
		r.WriteFile("share/same.sh", body)  // 100644，字节完全相同
		r.Commit("feat: same bytes, two modes")
		return r
	})

	byPath := map[string]ContentEntry{}
	for _, e := range storeEntries(t, store, dg) {
		byPath[e.Path] = e
	}
	exe, plain := byPath["bin/run.sh"], byPath["share/same.sh"]
	if exe.Full == "" || plain.Full == "" {
		t.Fatalf("both entries must carry a blob: %+v / %+v", exe, plain)
	}
	// blob 名取自清单（它是 identity 的权威来源；`ContentEntry` 刻意只暴露"字节在哪"）。
	mf, merr := store.readManifest(dg)
	if merr != nil {
		t.Fatal(merr)
	}
	names := map[string]string{}
	for _, e := range mf.Entries {
		names[e.Path] = e.SHA
	}
	if names["bin/run.sh"] == names["share/same.sh"] {
		t.Errorf("the exec bit must be part of the blob identity (see blobKey); "+
			"got the same blob %s for both modes", names["bin/run.sh"])
	}
	// 代价只有"多一份"：内容去重在"同一文件在多个 commit 里不变"那个主导场景下不受影响。
	exeInfo, err := os.Stat(exe.Full)
	if err != nil {
		t.Fatal(err)
	}
	plainInfo, perr := os.Stat(plain.Full)
	if perr != nil {
		t.Fatal(perr)
	}
	if exeInfo.Size() != plainInfo.Size() {
		t.Errorf("blob sizes should be equal (same bytes): %d vs %d", exeInfo.Size(), plainInfo.Size())
	}
	if runtime.GOOS == "windows" {
		// 只跳过"权限位"这一段——上面的 blob 身份断言已经在所有平台跑过，
		// 所以这里用 return 而不是 Skip（Skip 会让整个用例看起来没验过）。
		return
	}
	if exeInfo.Mode().Perm()&0o111 == 0 {
		t.Errorf("the exec blob must be 0755, got %v", exeInfo.Mode())
	}
	if plainInfo.Mode().Perm()&0o111 != 0 {
		t.Errorf("the non-exec blob must not be executable, got %v", plainInfo.Mode())
	}
}

// TestContentStore_GitlinkStillRejected 锁定分类口径没被重构改变（子模块不可重放）。
func TestContentStore_GitlinkStillRejected(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, simpleRepo)

	// 直接构造一个含 gitlink 的条目集合无法走 Put（real git 需要子模块），
	// 因此这里只断言"清单里不会出现 gitlink"这条不变量在正常路径上成立。
	for _, e := range storeEntries(t, store, dg) {
		if e.Mode == digest.ModeGitlink {
			t.Errorf("%s: a gitlink must never reach the manifest", e.Path)
		}
		if e.Mode != digest.ModeRegular && e.Mode != digest.ModeExecutable && e.Mode != digest.ModeSymlink {
			t.Errorf("%s: unexpected mode %q in the manifest", e.Path, e.Mode)
		}
	}
}

// pathUnder 报告 p 是否位于 root 之下（两者都先归一化）。
func pathUnder(p, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && !hasDotDotPrefix(rel)
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}
