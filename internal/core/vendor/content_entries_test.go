package vendor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestContentStore_EntriesListsEveryFile 确认条目抽象的第一条性质：
// **列出的就是存着的东西**，且顺序确定（按 path 升序）。
func TestContentStore_EntriesListsEveryFile(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, simpleRepo)

	entries, err := store.Entries(dg)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("a stored tree must list at least one entry")
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Path >= entries[i].Path {
			t.Errorf("entries are not sorted / duplicated at %d: %q then %q",
				i, entries[i-1].Path, entries[i].Path)
		}
	}
	for _, e := range entries {
		if e.Path == "" {
			t.Error("an entry without a path")
		}
		if !e.Symlink && e.Full == "" {
			t.Errorf("%s: a regular entry must expose where its bytes are", e.Path)
		}
		if !e.Symlink {
			if _, serr := os.Stat(e.Full); serr != nil {
				t.Errorf("%s: Full points at an unreadable location: %v", e.Path, serr)
			}
		}
	}
}

// TestVerifyVendorAgainstEntriesAgreesWithVerifyVendorTree 是 v0.8 A 组的验收：
// **两个入口不能漂移**。
//
// 层 2 换布局后（ADR-019），右侧不再是一棵目录树，于是校验多了一个入口。
// 若两条路的判定逻辑各写一份，就会出现"浅校验检得出、深校验检不出"这种
// 最坏形态的漂移——所以它们必须走同一段 compareTrees，而这个用例把这一点钉住：
// 同一对树，两种入口给出**完全相同**的结果（含篡改场景）。
func TestVerifyVendorAgainstEntriesAgreesWithVerifyVendorTree(t *testing.T) {
	dir := t.TempDir()
	storeTree := filepath.Join(dir, "store")
	vendorTree := filepath.Join(dir, "vendor")

	mk := func(root string) {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("world!"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(storeTree)
	mk(vendorTree)

	// 符号链接：Windows 上可能没有权限，因此建不出来就跳过这一项（不因此失败）。
	haveSymlink := true
	if err := os.Symlink("a.txt", filepath.Join(storeTree, "link.txt")); err != nil {
		haveSymlink = false
	} else if err := os.Symlink("a.txt", filepath.Join(vendorTree, "link.txt")); err != nil {
		haveSymlink = false
	}

	entries, err := entriesFromDirForTest(storeTree)
	if err != nil {
		t.Fatal(err)
	}

	// 两个公开入口本来就分深浅（VerifyVendorTree = 深、Shallow = 浅），
	// 因此"走同一段判定"指的是它们与条目版给出同样的结论。
	treeCompare := func(deep bool) func(string, string) (VerifyResult, error) {
		if deep {
			return VerifyVendorTree
		}
		return VerifyVendorTreeShallow
	}

	compareBoth := func(when string, deep bool) {
		t.Helper()
		want, werr := treeCompare(deep)(vendorTree, storeTree)
		if werr != nil {
			t.Fatalf("%s: VerifyVendorTree: %v", when, werr)
		}
		got, gerr := VerifyVendorAgainstEntries(vendorTree, entries, deep)
		if gerr != nil {
			t.Fatalf("%s: VerifyVendorAgainstEntries: %v", when, gerr)
		}
		if want.OK() != got.OK() || len(want.Mismatches) != len(got.Mismatches) {
			t.Fatalf("%s: the two entries disagree: tree=%v entries=%v", when, want.Mismatches, got.Mismatches)
		}
		for i := range want.Mismatches {
			if want.Mismatches[i] != got.Mismatches[i] {
				t.Errorf("%s: mismatch %d differs:\n  tree:    %s\n  entries: %s",
					when, i, want.Mismatches[i], got.Mismatches[i])
			}
		}
		if want.Files != got.Files {
			t.Errorf("%s: Files %d vs %d", when, want.Files, got.Files)
		}
		if want.Symlinks != got.Symlinks {
			t.Errorf("%s: Symlinks %d vs %d", when, want.Symlinks, got.Symlinks)
		}
	}

	compareBoth("identical (shallow)", false)
	compareBoth("identical (deep)", true)
	if haveSymlink && (verifySymlinkCount(t, vendorTree, storeTree) == 0) {
		t.Error("symlinks were not counted although they exist")
	}

	// 篡改：等长改写——浅校验看不出、深校验必须看出，而**两个入口都要一致**。
	if err := os.WriteFile(filepath.Join(vendorTree, "a.txt"), []byte("HELLO"), 0o644); err != nil {
		t.Fatal(err)
	}
	shallowViaTree, _ := VerifyVendorTreeShallow(vendorTree, storeTree)
	shallowViaEntries, _ := VerifyVendorAgainstEntries(vendorTree, entries, false)
	if shallowViaTree.OK() != shallowViaEntries.OK() {
		t.Error("shallow: the two entries disagree after an equal-length edit")
	}
	compareBoth("tampered (deep)", true)
	deepViaTree, _ := VerifyVendorTree(vendorTree, storeTree)
	if deepViaTree.OK() {
		t.Error("a deep check must catch an equal-length in-place edit")
	}

	// 缺失：vendor 少一个文件，两个入口都要报"同一句"。
	if err := os.Remove(filepath.Join(vendorTree, "sub", "b.txt")); err != nil {
		t.Fatal(err)
	}
	compareBoth("missing file (shallow)", false)
}

func verifySymlinkCount(t *testing.T, vendorTree, storeTree string) int {
	t.Helper()
	res, err := VerifyVendorTreeShallow(vendorTree, storeTree)
	if err != nil {
		t.Fatal(err)
	}
	return res.Symlinks
}

// entriesFromDirForTest 按 ContentEntry 的口径扫一个目录（测试用）。
func entriesFromDirForTest(root string) ([]ContentEntry, error) {
	var out []ContentEntry
	err := filepath.Walk(root, func(path string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		e := ContentEntry{Path: filepath.ToSlash(rel), Size: fi.Size(), Full: path}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			e.Symlink = true
			e.LinkTarget = target
			e.Full = ""
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// 保证 testutils 在本文件被引用（与同包其它用例共用 fixture 形态）。
var _ = testutils.NewGitRepo
