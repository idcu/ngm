package vendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/testutils"
)

// TestContentStore_UsageReportsWhatTheStoreHolds 确认 `ngm store usage` 的三条性质：
//
//  1. 报出来的就是存着的东西（含体积与来自哪个 repo/commit）
//  2. **顺序是确定的**（体积降序、同体积按 digest 升序）——依赖目录遍历顺序的输出
//     会让人误以为"内容变了"，而内容树是不可变的
//  3. **只读**：跑完 usage 之后 store 的字节数一个都不变
func TestContentStore_UsageReportsWhatTheStoreHolds(t *testing.T) {
	store := newStore(t)

	_, small := putFixture(t, store, simpleRepo)
	_, big := putFixture(t, store, func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("payload.txt", strings.Repeat("x", 4096))
		r.Commit("feat: a bigger tree")
		return r
	})

	before, err := dirBytes(store.Root())
	if err != nil {
		t.Fatal(err)
	}

	u, err := store.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if !u.Exists {
		t.Fatal("the store has two content trees; Usage must report it exists")
	}
	if len(u.Trees) != 2 {
		t.Fatalf("trees=%d, want 2", len(u.Trees))
	}
	if u.TreeBytes <= 0 {
		t.Errorf("TreeBytes=%d, want > 0", u.TreeBytes)
	}
	for _, e := range u.Trees {
		if !e.MetaReadable {
			t.Errorf("%s: meta.json was written by Put, so it must be readable", e.Digest)
		}
	}

	// 顺序确定：跑两次（乃至多次）得到同一串 digest。
	var first []string
	for round := 0; round < 3; round++ {
		again, aerr := store.Usage()
		if aerr != nil {
			t.Fatal(aerr)
		}
		var got []string
		for _, e := range again.Trees {
			got = append(got, e.Digest)
		}
		if round == 0 {
			first = got
			continue
		}
		if strings.Join(got, ",") != strings.Join(first, ",") {
			t.Errorf("round %d order differs: %v vs %v", round, got, first)
		}
	}
	// 大的排在前面。
	if u.Trees[0].Bytes < u.Trees[1].Bytes {
		t.Errorf("not sorted by size desc: %d then %d", u.Trees[0].Bytes, u.Trees[1].Bytes)
	}
	// 两份都在（不是只报了一份）。
	reported := map[string]bool{}
	for _, e := range u.Trees {
		reported[e.Digest] = true
	}
	for _, dg := range []string{small, big} {
		if !reported[dg] {
			t.Errorf("usage does not report %s", dg)
		}
	}

	after, err := dirBytes(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("usage must be read-only: store bytes %d -> %d", before, after)
	}
}

// TestContentStore_PruneOnlyRemovesUnpackResidue 是这条命令的安全边界：
// **内容树一个都不能少**。
func TestContentStore_PruneOnlyRemovesUnpackResidue(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, simpleRepo)

	algDir := filepath.Join(store.Root(), digest.Algorithm)
	residue := filepath.Join(algDir, ".unpack-leftover123")
	if err := os.MkdirAll(residue, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(residue, "tree", "partial.txt"), []byte("half"), 0o644); err != nil {
		// tree 子目录不存在：先建它，再写文件
		if merr := os.MkdirAll(filepath.Join(residue, "tree"), 0o755); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(filepath.Join(residue, "tree", "partial.txt"), []byte("half"), 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	// 一个"看起来像残骸、但前缀不对"的目录：必须被留下。
	lookalike := filepath.Join(algDir, ".unpackX-not-a-temp")
	if err := os.MkdirAll(lookalike, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1) dry-run：报出来，但不动。
	dry, err := store.Prune(true)
	if err != nil {
		t.Fatalf("Prune(dry): %v", err)
	}
	if len(dry.Removed) != 1 || !dry.DryRun {
		t.Errorf("dry run removed=%d dryRun=%v, want 1 / true", len(dry.Removed), dry.DryRun)
	}
	if _, err := os.Stat(residue); err != nil {
		t.Errorf("a dry run must not delete anything: %v", err)
	}

	// 2) 真删：残骸走，内容树留。
	res, err := store.Prune(false)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(res.Removed) != 1 {
		t.Errorf("removed=%d, want 1", len(res.Removed))
	}
	if res.BytesFreed <= 0 {
		t.Errorf("BytesFreed=%d, want > 0", res.BytesFreed)
	}
	if _, err := os.Stat(residue); !os.IsNotExist(err) {
		t.Errorf("residue should be gone, stat err=%v", err)
	}
	if !store.Has(dg) {
		t.Error("a content tree was affected by prune — that is exactly what ADR-018 forbids")
	}
	if _, err := os.Stat(lookalike); err != nil {
		t.Errorf("a directory that only looks like residue must be kept: %v", err)
	}
	// "留下了 N 份" 要能被报出来（它是给用户的安全声明）。
	if res.KeptTrees < 1 {
		t.Errorf("KeptTrees=%d, want >= 1 (the report must say what was left alone)", res.KeptTrees)
	}

	// 3) 幂等：没有残骸时 0 删除、不报错。
	again, err := store.Prune(false)
	if err != nil {
		t.Fatalf("second Prune: %v", err)
	}
	if len(again.Removed) != 0 {
		t.Errorf("second prune removed %d, want 0 (idempotent)", len(again.Removed))
	}
}

// TestContentStore_UsageAndPruneOnEmptyStore 确认"没有东西"不是错误。
func TestContentStore_UsageAndPruneOnEmptyStore(t *testing.T) {
	store := newStore(t)

	u, err := store.Usage()
	if err != nil {
		t.Fatalf("Usage on an empty store: %v", err)
	}
	if u.Exists {
		t.Error("a freshly created store has no sha256 dir yet; Exists must be false")
	}
	if len(u.Trees) != 0 || u.TreeBytes != 0 {
		t.Errorf("empty store reported %d trees / %d bytes", len(u.Trees), u.TreeBytes)
	}

	p, err := store.Prune(false)
	if err != nil {
		t.Fatalf("Prune on an empty store: %v", err)
	}
	if len(p.Removed) != 0 {
		t.Errorf("removed=%d, want 0", len(p.Removed))
	}
}
