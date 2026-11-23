package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV207GetBlobKeepsLargeDataOutput 固定 2026-11-23 审计 P2：
// C-05 的 boundbuf 限流最初无差别套在通用 Run 上，1 MiB 以上的
// `cat-file blob` / `ls-tree -r -z` 输出会被截断并混入标记，
// 让正常的大文件变成完整性失败。数据输出必须显式调高上限（仍是有界的）。
func TestV207GetBlobKeepsLargeDataOutput(t *testing.T) {
	repo := testutils.NewGitRepo(t)
	content := strings.Repeat("x", (1<<20)+4096)
	repo.WriteFileRaw("big.bin", []byte(content)).Commit("big blob")

	sha := strings.TrimSpace(repo.Exec("rev-parse", "HEAD:big.bin"))
	got, err := GetBlob(context.Background(), Options{}, repo.Dir, sha)
	if err != nil {
		t.Fatalf("GetBlob: %v", err)
	}
	if len(got) != len(content) {
		t.Fatalf("GetBlob returned %d bytes, want %d (truncated data output)", len(got), len(content))
	}
	if string(got) != content {
		t.Fatal("GetBlob content differs from the committed blob")
	}
}
