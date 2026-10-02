package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// TestV07StoreAcceptance 端到端验证 `ngm store usage` 与 `ngm store prune`（v0.7 A/B 组）。
//
// 它们存在的理由（[ADR-018](../docs/adr/adr-018-store-reclaim.md)）：层 2 **只增不减**，
// 而"按可达性自动删除"被裁定为不可做。于是至少要让两件事成立——
//
//  1. **占用可见**：`usage` 说出 store 里有什么、各占多少（且**只读**）
//  2. **残骸可回收**：`prune` 清掉中断留下的解包目录，而**内容树一个都不能少**
//
// 第 2 条是本用例真正要守的边界：一个删除类命令最危险的形态不是"没删干净"，
// 而是"顺手删了别的东西"——那会让别的项目的 `ngm verify` 在某天突然验不过。
func TestV07StoreAcceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	ctx := context.Background()

	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	store := vendor.NewContentStore(layout.ContentRoot())

	putTree := func(name string, size int) string {
		t.Helper()
		r := testutils.NewGitRepo(t)
		r.WriteFile("payload.txt", strings.Repeat("x", size))
		r.Commit("feat: " + name)
		commit := r.Head()
		dg, derr := git.BuildArchiveDigest(ctx, git.Options{}, r.Dir, commit)
		if derr != nil {
			t.Fatal(derr)
		}
		if _, perr := store.Put(ctx, git.Options{}, r.Dir, vendor.Meta{
			Repo:   "github:v07/" + name,
			Commit: commit,
			Digest: dg,
		}); perr != nil {
			t.Fatal(perr)
		}
		return dg
	}
	dgSmall := putTree("one", 2048)
	dgBig := putTree("two", 8192)

	residue := filepath.Join(layout.ContentRoot(), digest.Algorithm, ".unpack-acceptance")
	if err := os.MkdirAll(filepath.Join(residue, "tree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(residue, "tree", "partial.txt"), []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("usage reports what the store holds", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "usage")
		if code != 0 {
			t.Fatalf("store usage exit=%d:\n%s", code, out)
		}
		// v0.8（ADR-019）后写路径落的是 blob 池 + 清单，因此"两棵树"现在
		// 表现为两份清单；v1 那一行必须仍是 0（写路径没有回到整棵树）。
		if !strings.Contains(out, "tree manifests (v2): 2") {
			t.Errorf("must report 2 v2 tree manifests:\n%s", out)
		}
		if !strings.Contains(out, "content trees (v1 legacy): 0") {
			t.Errorf("a store written by v0.8 must have no v1 leftovers:\n%s", out)
		}
		if !strings.Contains(out, "blobs (deduplicated content): ") {
			t.Errorf("must report the blob pool (it is where the real bytes are):\n%s", out)
		}
		for _, dg := range []string{dgSmall, dgBig} {
			if !strings.Contains(out, dg) {
				t.Errorf("usage does not mention %s:\n%s", dg, out)
			}
		}
		if !strings.Contains(out, "github:v07/one") {
			t.Errorf("must say where a tree came from (repo@commit):\n%s", out)
		}
		if !strings.Contains(out, "unpack residue: 1") {
			t.Errorf("must report the unpack residue:\n%s", out)
		}
	})

	t.Run("usage is read-only", func(t *testing.T) {
		before := v07DirBytes(t, layout.ContentRoot())
		code, out := runCaptureCode(t, "store", "usage")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		after := v07DirBytes(t, layout.ContentRoot())
		if before != after {
			t.Errorf("usage changed the store: %d -> %d bytes", before, after)
		}
	})

	t.Run("prune --dry-run removes nothing", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune", "--dry-run")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "would remove 1") {
			t.Errorf("dry run must say what it would remove:\n%s", out)
		}
		if _, err := os.Stat(residue); err != nil {
			t.Errorf("dry run must not delete anything: %v", err)
		}
	})

	t.Run("prune removes residue and keeps every tree", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "removed 1") {
			t.Errorf("must report what was removed:\n%s", out)
		}
		// 这句是给用户的安全声明：留下来的数量要说出来，别让人自己猜。
		if !strings.Contains(out, "kept 2 content tree(s) untouched") {
			t.Errorf("must state how many trees were left alone:\n%s", out)
		}
		if _, err := os.Stat(residue); !os.IsNotExist(err) {
			t.Errorf("residue should be gone, stat err=%v", err)
		}
		for _, dg := range []string{dgSmall, dgBig} {
			if !store.Has(dg) {
				t.Errorf("a content tree (%s) was affected by prune", dg)
			}
		}
	})

	t.Run("prune is idempotent", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "nothing to do") {
			t.Errorf("a second prune must be a no-op:\n%s", out)
		}
		if !strings.Contains(out, "kept 2") {
			t.Errorf("must still report the trees it left alone:\n%s", out)
		}
	})
}

// v07DirBytes 递归求和目录里所有文件的字节数（只读，用于"usage 不改动 store"的断言）。
func v07DirBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return total
}
