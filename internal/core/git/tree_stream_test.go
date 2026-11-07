package git

import (
	"context"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/testutils"
)

// P2-9：流式重放（keepContents=false）必须与保留内容的路径产出**完全相同**的
// 清单与 digest，且不保留任何 blob 字节。
//
// 这条用例把"优化过的路径"钉在"老路径"上：如果哪天流式实现改了哈希顺序、
// 跳过了 LFS 检测、或忘了给某个记录填 BlobSHA256，digest 会分叉。
func TestV130StreamedDigestMatchesRetainedMaterial(t *testing.T) {
	testutils.MustHaveGit(t)

	repo := testutils.NewGitRepo(t)
	repo.WriteFile("a.txt", "alpha\n")
	repo.WriteFile("dir/b.txt", "beta\n")
	repo.WriteFile("empty.txt", "")
	commit := repo.Commit("stream: three blobs, one empty")

	full, err := BuildArchiveMaterial(context.Background(), Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchiveMaterial: %v", err)
	}
	if len(full.Contents) != 3 {
		t.Fatalf("contents = %d, want 3", len(full.Contents))
	}

	streamed, err := materialFromEntries(context.Background(), Options{}, repo.Dir, commit,
		full.Entries, digest.ManifestVersion, false)
	if err != nil {
		t.Fatalf("materialFromEntries(keepContents=false): %v", err)
	}
	if streamed.Contents != nil {
		t.Errorf("the streaming path must not retain blob contents")
	}
	if streamed.Digest != full.Digest {
		t.Fatalf("streamed digest %s != retained digest %s", streamed.Digest, full.Digest)
	}
	if string(streamed.Manifest()) != string(full.Manifest()) {
		t.Fatalf("streamed manifest diverged from the retained one")
	}
	for i := range full.Records {
		if streamed.Records[i] != full.Records[i] {
			t.Fatalf("record %d differs: %+v vs %+v", i, streamed.Records[i], full.Records[i])
		}
	}

	// BuildArchive（verify 的 digest 重放入口）现在走流式路径：字节必须一致。
	manifest, err := BuildArchive(context.Background(), Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	if string(manifest) != string(full.Manifest()) {
		t.Fatalf("BuildArchive output diverged from BuildArchiveMaterial")
	}
	dg, err := BuildArchiveDigest(context.Background(), Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchiveDigest: %v", err)
	}
	if dg != full.Digest {
		t.Fatalf("BuildArchiveDigest %s != %s", dg, full.Digest)
	}
}
