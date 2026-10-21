package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func fileURL(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return "file://" + filepath.ToSlash(abs)
}

func TestClonePartial_AppliesBlobFilter(t *testing.T) {
	testutils.MustHaveGit(t)

	src := testutils.NewGitRepo(t)
	src.WriteFile("a.txt", "a\n")
	commit := src.Commit("feat: a")
	src.Tag("v1", false)

	cache := filepath.Join(t.TempDir(), "cache.git")
	if err := ClonePartial(context.Background(), Options{}, fileURL(t, src.Dir), cache); err != nil {
		t.Fatalf("ClonePartial: %v", err)
	}
	if !IsPartialClone(cache) {
		t.Fatal("filter was not applied; the cache is not a partial clone")
	}
	if ok, err := CommitExists(context.Background(), Options{}, cache, commit); err != nil || !ok {
		t.Fatalf("commit missing from partial clone: ok=%v err=%v", ok, err)
	}
}

func TestFetchPartialCommit_ExactAndUnreachable(t *testing.T) {
	testutils.MustHaveGit(t)

	remote := testutils.NewBareRemote(t)
	src := testutils.NewGitRepo(t)
	src.SetRemote("origin", remote)
	src.WriteFile("a.txt", "a\n")
	first := src.Commit("feat: a")
	src.Push("origin", "main")

	cache := filepath.Join(t.TempDir(), "cache.git")
	if err := ClonePartial(context.Background(), Options{}, fileURL(t, remote), cache); err != nil {
		t.Fatalf("ClonePartial: %v", err)
	}

	// 上游前进后，精确 commit 仍可取（这里用新 commit 验证 fetch 路径）。
	src.WriteFile("b.txt", "b\n")
	second := src.Commit("feat: b")
	src.Push("origin", "main")
	if err := FetchPartialCommit(context.Background(), Options{}, cache, second, "main", "branch"); err != nil {
		t.Fatalf("FetchPartialCommit: %v", err)
	}
	if ok, err := CommitExists(context.Background(), Options{}, cache, second); err != nil || !ok {
		t.Fatalf("fetched commit missing: ok=%v err=%v", ok, err)
	}

	// 从未推送的 commit 必须明确失败，而不是静默留下一个“看似成功”的缓存。
	other := testutils.NewGitRepo(t)
	other.WriteFile("c.txt", "c\n")
	orphan := other.Commit("feat: c")
	err := FetchPartialCommit(context.Background(), Options{}, cache, orphan, "main", "branch")
	if err == nil || !strings.Contains(err.Error(), "could not be fetched") && !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("expected unreachable commit failure, got %v", err)
	}
	if ok, _ := CommitExists(context.Background(), Options{}, cache, orphan); ok {
		t.Error("orphan commit must not be present after a failed fetch")
	}
	_ = first
}
