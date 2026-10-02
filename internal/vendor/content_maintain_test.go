package vendor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/git"
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
	// v2（ADR-019）：Put 写的是 blob 池 + 清单，因此 v1 的 Trees 必须是空的——
	// 若这里变成 2，说明写路径又回到"整棵树"了。
	if len(u.Trees) != 0 || u.TreeBytes != 0 {
		t.Errorf("v1 legacy trees must be empty after v2 writes, got %d / %d bytes",
			len(u.Trees), u.TreeBytes)
	}
	if len(u.V2Entries) != 2 {
		t.Fatalf("v2 manifests=%d, want 2", len(u.V2Entries))
	}
	if u.V2TreeBytes <= 0 {
		t.Errorf("V2TreeBytes=%d, want > 0", u.V2TreeBytes)
	}
	// blob 池里应有真实内容（两份 fixture 的文件内容各不相同）
	if u.Blobs == 0 || u.BlobBytes <= 0 {
		t.Errorf("blob pool reported %d blobs / %d bytes; Put must have written content",
			u.Blobs, u.BlobBytes)
	}
	for _, e := range u.V2Entries {
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
		for _, e := range again.V2Entries {
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
	// 大的排在前面（v2 报的是**逻辑体积**，见 ContentStoreUsage.V2Entries）。
	if u.V2Entries[0].Bytes < u.V2Entries[1].Bytes {
		t.Errorf("not sorted by size desc: %d then %d", u.V2Entries[0].Bytes, u.V2Entries[1].Bytes)
	}
	// 两份都在（不是只报了一份）。
	reported := map[string]bool{}
	for _, e := range u.V2Entries {
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

// TestContentStore_UsageSplitsSharedFromExclusive 锁定 v0.9 加上的那组读数：
// blob 池按"被多少棵树引用"切成共享 / 独占 / 孤儿，每棵树能回答"丢掉它能回收多少"。
//
// 为什么值得单独一条用例：这三个数回答的是**用户唯一能问的回收问题**
// （"如果我不再用这个源，能省多少"），而它最容易的实现错误是"把共享 blob 算进
// 每一棵树"——那样所有树的数字加起来会超过整个 store，而**每一条断言看起来都合理**。
// 所以这里的断言是**不变量**（三段之和、独占不重复计数），不是固定数字。
func TestContentStore_UsageSplitsSharedFromExclusive(t *testing.T) {
	store := newStore(t)

	// **两个各自独立的仓库**，各自带一个只属于自己的文件，外加一份**内容相同**
	// 的 shared.txt（内容相同 → 同一个 blob，这正是要测的共享）。
	//
	// 为什么不用"同一个仓库的两个 commit"：第二个 commit 是从第一个长出来的，
	// 第一个的文件仍在第二棵树里——那样"第一个的独占"其实是 0（第一版 fixture 就栽在这）。
	put := func(unique string, size int) {
		t.Helper()
		r := testutils.NewGitRepo(t)
		r.WriteFile("shared.txt", "shared content\n")
		r.WriteFile(unique, strings.Repeat("u", size))
		r.Commit("feat: a tree")
		commit := r.Head()
		dg, err := git.BuildArchiveDigest(context.Background(), git.Options{}, r.Dir, commit)
		if err != nil {
			t.Fatalf("BuildArchiveDigest: %v", err)
		}
		if _, err := store.Put(context.Background(), git.Options{}, r.Dir, Meta{
			Repo:   "github:test/shared",
			Commit: commit,
			Digest: dg,
		}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	put("only-a.txt", 512)
	put("only-b.txt", 1024)

	u, err := store.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(u.V2Entries) != 2 {
		t.Fatalf("v2 manifests=%d, want 2", len(u.V2Entries))
	}

	// 不变量 1：三段之和**就是** blob 池总量——不多算（共享被重复计入）也不少算。
	if got := u.BlobSharedBytes + u.BlobExclusiveBytes + u.BlobOrphanBytes; got != u.BlobBytes {
		t.Errorf("shared(%d)+exclusive(%d)+orphan(%d)=%d, want BlobBytes=%d",
			u.BlobSharedBytes, u.BlobExclusiveBytes, u.BlobOrphanBytes, got, u.BlobBytes)
	}
	if u.BlobSharedBytes <= 0 {
		t.Errorf("两个 commit 共有 shared.txt，共享字节必须 > 0，得到 %d", u.BlobSharedBytes)
	}
	if u.BlobExclusiveBytes <= 0 {
		t.Errorf("每个 commit 各有一个独占文件，独占字节必须 > 0，得到 %d", u.BlobExclusiveBytes)
	}
	if u.BlobOrphans != 0 || u.BlobOrphanBytes != 0 {
		t.Errorf("每个 blob 都被引用，孤儿必须是 0，得到 %d (%d bytes)", u.BlobOrphans, u.BlobOrphanBytes)
	}

	// 不变量 2：每棵树的"独占"里**不能**混进共享 blob（那是这条读数唯一容易错的地方）。
	var sumExclusive int64
	for _, e := range u.V2Entries {
		if e.ExclusiveBytes <= 0 {
			t.Errorf("%s：它有独占文件，ExclusiveBytes 必须 > 0", e.Digest)
		}
		if e.ExclusiveBytes > e.Bytes {
			t.Errorf("%s：exclusive(%d) > logical(%d)——独占里混进了不属于它的东西",
				e.Digest, e.ExclusiveBytes, e.Bytes)
		}
		sumExclusive += e.ExclusiveBytes
	}
	// 紧的等式（口径刻意只算 blob，见 ExclusiveBytes 的注释）：每个独占 blob 只被一棵树
	// 引用，因此各树独占之和**恰好**是那一块。共享 blob 若被算进任何一棵树，这里就会大。
	if sumExclusive != u.BlobExclusiveBytes {
		t.Errorf("Σexclusive(%d) != BlobExclusiveBytes(%d)：共享 blob 被算进了某棵树",
			sumExclusive, u.BlobExclusiveBytes)
	}
	if sumExclusive+u.BlobSharedBytes+u.BlobOrphanBytes != u.BlobBytes {
		t.Errorf("Σexclusive(%d)+shared(%d)+orphan(%d) != BlobBytes(%d)",
			sumExclusive, u.BlobSharedBytes, u.BlobOrphanBytes, u.BlobBytes)
	}
}

// TestContentStore_UsageReportsOrphanBlobs：池里有没有人引用的 blob 时，
// 必须被报出来，并且**不能落进任何一棵树的"独占"**（那会让"能省多少"虚高）。
func TestContentStore_UsageReportsOrphanBlobs(t *testing.T) {
	store := newStore(t)
	putFixture(t, store, simpleRepo)

	// 手工放一个孤儿 blob，形状与 `Put` 写下的完全一致：<root>/blobs/<aa>/<sha>。
	sha := strings.Repeat("ab", 32)
	bp := store.BlobPath(sha)
	if err := os.MkdirAll(filepath.Dir(bp), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("orphan\n")
	if err := os.WriteFile(bp, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := store.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.BlobOrphans != 1 || u.BlobOrphanBytes != int64(len(payload)) {
		t.Errorf("orphans = %d (%d bytes), want 1 (%d)", u.BlobOrphans, u.BlobOrphanBytes, len(payload))
	}
	var sumExclusive int64
	for _, e := range u.V2Entries {
		sumExclusive += e.ExclusiveBytes
	}
	if sumExclusive+u.BlobSharedBytes+u.BlobOrphanBytes != u.BlobBytes {
		t.Errorf("Σexclusive(%d)+shared(%d)+orphan(%d) != BlobBytes(%d)：孤儿被算进了某棵树的独占",
			sumExclusive, u.BlobSharedBytes, u.BlobOrphanBytes, u.BlobBytes)
	}

	// 只读：再跑一次，孤儿还在（`prune` 只清解包残骸，**不碰 blob**，ADR-018 决策 5）。
	if _, err := store.Usage(); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(bp); serr != nil {
		t.Errorf("usage 是只读的，孤儿 blob 必须原样留着：%v", serr)
	}
}

// TestContentStore_UsageFlagsUnreadableManifests：清单读不出来时，
// 它的 blob 会**看起来像孤儿**——这个数字必须被标注，而不是当成事实报出去。
//
// 这是"仪器说谎"的又一种形状：一个损坏的清单会让"没有人回收"这句话看起来更严重。
func TestContentStore_UsageFlagsUnreadableManifests(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, simpleRepo)

	u, err := store.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if u.UnreadableManifests != 0 {
		t.Fatalf("healthy store: UnreadableManifests=%d, want 0", u.UnreadableManifests)
	}

	// 破坏清单（写一段不是 JSON 的内容）。
	if werr := os.WriteFile(store.ManifestPath(dg), []byte("not json\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	u, err = store.Usage()
	if err != nil {
		t.Fatalf("Usage 不该因为一个坏清单就失败（它要能报告损坏）: %v", err)
	}
	if u.UnreadableManifests != 1 {
		t.Errorf("UnreadableManifests=%d, want 1", u.UnreadableManifests)
	}
	if u.BlobOrphans == 0 {
		t.Errorf("坏清单的 blob 会失去引用（这正是要标注这个数的原因），orphans=%d", u.BlobOrphans)
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
