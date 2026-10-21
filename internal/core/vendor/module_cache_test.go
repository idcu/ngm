package vendor

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

func vFileURL(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return "file://" + filepath.ToSlash(abs)
}

func TestModuleCacheEnsure(t *testing.T) {
	testutils.MustHaveGit(t)

	remote := testutils.NewBareRemote(t)
	src := testutils.NewGitRepo(t)
	src.SetRemote("origin", remote)
	src.WriteFile("packages/a/index.ts", "export const a = 1\n")
	commit := src.Commit("feat: a")
	src.Tag("packages/a/v1.0.0", false)
	src.Push("origin", "main")
	src.Push("origin", "refs/tags/packages/a/v1.0.0")

	root := t.TempDir()
	c := NewModuleCache(root, git.Options{})
	repo := resolve.MustNormalize("127.0.0.1/acme/modules")

	res, err := c.Ensure(context.Background(), repo, vFileURL(t, remote), resolve.ProtocolHTTPS,
		commit, "packages/a/v1.0.0", "tag")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !res.Created {
		t.Error("first Ensure should report Created=true")
	}
	if !c.Exists(repo) {
		t.Fatal("module cache is not a partial clone")
	}
	if ok, err := c.HasCommit(context.Background(), repo, commit); err != nil || !ok {
		t.Fatalf("HasCommit: ok=%v err=%v", ok, err)
	}

	u, err := c.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if !u.Exists || u.Repos != 1 || u.Bytes <= 0 {
		t.Fatalf("usage = %+v", u)
	}
	if len(u.Entries) != 1 || u.Entries[0].NotPartial {
		t.Errorf("entry = %+v, want one usable partial clone", u.Entries)
	}
	if u.Entries[0].Repo != "127.0.0.1/acme/modules.git" {
		t.Errorf("entry repo = %q", u.Entries[0].Repo)
	}
	if u.NotPartialCount != 0 {
		t.Errorf("NotPartialCount = %d", u.NotPartialCount)
	}

	// 第二次：命中 commit，不应重新 clone/fetch。
	res2, err := c.Ensure(context.Background(), repo, vFileURL(t, remote), resolve.ProtocolHTTPS,
		commit, "packages/a/v1.0.0", "tag")
	if err != nil {
		t.Fatalf("Ensure (second): %v", err)
	}
	if res2.Created {
		t.Error("second Ensure should reuse the cache")
	}

	// 未推送的 commit 必须明确失败。
	other := testutils.NewGitRepo(t)
	other.WriteFile("b.txt", "b\n")
	orphan := other.Commit("feat: b")
	if _, err := c.Ensure(context.Background(), repo, vFileURL(t, remote), resolve.ProtocolHTTPS,
		orphan, "packages/a/v1.0.0", "tag"); err == nil {
		t.Fatal("unreachable commit must fail")
	}
}
