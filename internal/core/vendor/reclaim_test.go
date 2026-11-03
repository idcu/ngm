package vendor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV102VendorReclaimKeepsMonorepoSiblings 钉住前缀判定：
// 一个 monorepo 的某个子目录可达时，兄弟目录是候选，而**整仓**不能是候选。
//
// 少了这条规则，`ngm gc --apply` 会把可达路径的兄弟目录一起删掉——
// 那是"回收垃圾"变成"破坏 vendor"的边界，必须有可重复判据。
func TestV102VendorReclaimKeepsMonorepoSiblings(t *testing.T) {
	root := filepath.Join(t.TempDir(), VendorDirName)
	core := filepath.Join(root, "github.com", "org", "repo", "packages", "core")
	legacy := filepath.Join(root, "github.com", "org", "repo", "packages", "legacy")
	for _, dir := range []string{core, legacy} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	plan, err := PlanVendorReclaim(root, []string{"github.com/org/repo/packages/core"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("candidates=%v, want only the sibling subtree", plan.Items)
	}
	if got := plan.Items[0].Path; got != "github.com/org/repo/packages/legacy" {
		t.Errorf("candidate=%q, want the sibling (the reachable parent must survive)", got)
	}
	if plan.Items[0].Bytes <= 0 {
		t.Errorf("reclaimable bytes=%d, want > 0", plan.Items[0].Bytes)
	}

	// 空 root（项目还没 install）不是错误，也没有候选。
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	empty, err := PlanVendorReclaim(missing, nil)
	if err != nil {
		t.Fatalf("missing vendor root must not fail: %v", err)
	}
	if empty.Exists || !empty.Empty() {
		t.Errorf("missing root: exists=%v items=%v", empty.Exists, empty.Items)
	}
}

// TestV102StoreReclaimKeepsSharedBlobs 钉住 content store 的可达性口径：
//
//  1. 不可达 digest 成为候选，可达 digest 不动
//  2. 两棵树共享的 blob 不进入回收清单（它还被可达树引用）
//  3. 只有候选树引用的 blob 是"可回收"，但比安全窗新的会被留下并报出来
func TestV102StoreReclaimKeepsSharedBlobs(t *testing.T) {
	store := newStore(t)
	r := testutils.NewGitRepo(t)
	r.WriteFile("shared.ts", "export const shared = 1;\n")
	r.WriteFile("v1.ts", "export const v1 = 1;\n")
	r.Commit("one")
	first := r.Head()

	put := func(commit string) string {
		dg, err := git.BuildArchiveDigest(context.Background(), git.Options{}, r.Dir, commit)
		if err != nil {
			t.Fatalf("BuildArchiveDigest(%s): %v", commit, err)
		}
		if _, err := store.Put(context.Background(), git.Options{}, r.Dir, Meta{
			Repo: "github:test/fixture", Commit: commit, Digest: dg,
		}); err != nil {
			t.Fatalf("Put(%s): %v", commit, err)
		}
		return dg
	}
	keep := put(first)

	r.WriteFile("v1.ts", "export const v1 = 2;\n")
	r.Commit("two")
	drop := put(r.Head())

	plan, err := store.PlanStoreReclaim(map[string]bool{keep: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Digest != drop {
		t.Fatalf("candidates=%+v, want only %s", plan.Items, drop)
	}
	if plan.YoungBlobs == 0 {
		t.Errorf("a freshly written tool-exclusive blob must be kept young; plan=%+v", plan)
	}

	// 共享 blob 与可达树都还在；候选 digest 的清单目录被删。
	res := store.ApplyStoreReclaim(plan)
	if len(res.Errors) > 0 {
		t.Fatalf("apply errors: %v", res.Errors)
	}
	if len(res.Removed) != 1 || res.Removed[0] != drop {
		t.Errorf("removed=%v, want %s", res.Removed, drop)
	}
	if !store.Has(keep) {
		t.Error("reachable digest must survive gc")
	}
	if store.Has(drop) {
		t.Error("dropped digest should be gone")
	}
	if res.BytesFreed == 0 {
		t.Error("the candidate manifest directory should free bytes")
	}
	if res.KeptYoung == 0 {
		t.Error("young blobs should be reported as kept, not silently counted as freed")
	}

	// 幂等：同一份可达集合再扫一次，应该没有候选。
	again, err := store.PlanStoreReclaim(map[string]bool{keep: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !again.Empty() {
		t.Errorf("second scan must be empty, got %+v", again.Items)
	}
}
