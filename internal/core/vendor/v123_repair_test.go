package vendor

import (
	"context"
	"os"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/git"
)

// 判据（D1 保留部分）：`writeBlob` 只在**大小一致**时才复用已有 blob；
// 截断的 blob（中断写入/磁盘错误）必须按完整内容重建。
//
// 这里直接调 `writeBlob`，而不是再走一次 `Put`：`Put` 先问 `Has`，
// 而 `Has` 有意**只确认字节位置存在、不比对大小**（默认 hardlink 布局下
// vendor 与 blob 同 inode，就地篡改会同时改变两侧；把大小不符当作"未就绪"
// 会把已确证的篡改误报成 exit 4）。因此"截断后 Has 为假"不是保留行为。
func TestV123TruncatedBlobIsRewrittenByWriteBlob(t *testing.T) {
	store := newStore(t)
	repo := simpleRepo(t)
	commit := repo.Head()
	dg, err := git.BuildArchiveDigest(context.Background(), git.Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo: "github:test/fixture", Commit: commit, Digest: dg,
	}); err != nil {
		t.Fatal(err)
	}
	if !store.Has(dg) {
		t.Fatalf("fresh Put should be usable")
	}

	m, err := store.readManifest(dg)
	if err != nil {
		t.Fatal(err)
	}
	var victim ManifestEntry
	for _, e := range m.Entries {
		if e.Mode != digest.ModeSymlink && e.Size > 0 {
			victim = e
			break
		}
	}
	if victim.SHA == "" {
		t.Fatalf("fixture has no regular file to truncate")
	}
	blob := store.BlobPath(victim.SHA)
	raw, err := os.ReadFile(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 {
		t.Fatalf("fixture blob is too small to truncate")
	}
	if err := os.WriteFile(blob, raw[:len(raw)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := store.writeBlob(raw, victim.Mode)
	if err != nil {
		t.Fatalf("writeBlob must repair a truncated blob: %v", err)
	}
	if sha != victim.SHA {
		t.Fatalf("rebuilt blob has sha %s, want %s", sha, victim.SHA)
	}
	st, err := os.Stat(blob)
	if err != nil {
		t.Fatalf("stat rebuilt blob: %v", err)
	}
	if st.Size() != victim.Size {
		t.Fatalf("blob was not rebuilt to its manifest size (%d != %d)", st.Size(), victim.Size)
	}
}

// 判据（D1 后半）：清单本身损坏时，Put 不得把坏条目当成"并发已就位"而永久留下。
func TestV123CorruptManifestIsReplacedByPut(t *testing.T) {
	store := newStore(t)
	repo := simpleRepo(t)
	commit := repo.Head()
	dg, err := git.BuildArchiveDigest(context.Background(), git.Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo: "github:test/fixture", Commit: commit, Digest: dg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ManifestPath(dg), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if store.Has(dg) {
		t.Fatalf("a corrupt manifest must not count as ready")
	}
	if _, err := store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo: "github:test/fixture", Commit: commit, Digest: dg,
	}); err != nil {
		t.Fatalf("Put must replace a corrupt manifest: %v", err)
	}
	if !store.Has(dg) {
		t.Fatalf("the entry must be usable after the replacement")
	}
}
