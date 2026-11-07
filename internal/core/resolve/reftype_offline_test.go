package resolve

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// `--offline` 的承诺是"绝不触网"：解析层只能认**已存在**的本地仓库，
// 不允许走到 `repo.CloneURL(proto)` 那一步。这条判据逐个分支钉住这个承诺。
func TestOfflineLSRemoteURLBranches(t *testing.T) {
	repo := Canonical{Host: "github.com", Path: "acme/repo"}

	t.Run("an existing GitURL directory wins", func(t *testing.T) {
		dir := t.TempDir()
		got, err := offlineLSRemoteURL(repo, ResolveOptions{GitURL: dir, Offline: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != dir {
			t.Errorf("got %q, want %q", got, dir)
		}
	})

	t.Run("a GitURL that is a file is not a local repo", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "repo.git")
		if err := os.WriteFile(f, []byte("not a repo"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := offlineLSRemoteURL(repo, ResolveOptions{GitURL: f, Offline: true})
		if err == nil {
			t.Fatal("a regular file must not be accepted as an offline source")
		}
		if got := errs.ExitCode(err); got != 4 {
			t.Errorf("exit=%d, want 4 (Git/network class)", got)
		}
	})

	t.Run("an existing mirror repo wins", func(t *testing.T) {
		mirror := t.TempDir()
		repoPath := filepath.Join(mirror, filepath.FromSlash(repo.MirrorRelPath()+".git"))
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := offlineLSRemoteURL(repo, ResolveOptions{MirrorDir: mirror, Offline: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != repoPath {
			t.Errorf("got %q, want %q", got, repoPath)
		}
	})

	t.Run("a mirror path that is a file does not count", func(t *testing.T) {
		mirror := t.TempDir()
		p := filepath.Join(mirror, filepath.FromSlash(repo.MirrorRelPath()+".git"))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		if _, err := offlineLSRemoteURL(repo, ResolveOptions{MirrorDir: mirror, Offline: true}); err == nil {
			t.Fatal("a file at the mirror path must not count as a local repo")
		}
	})

	t.Run("a cold cache fails with exit 4 and a way out", func(t *testing.T) {
		_, err := offlineLSRemoteURL(repo, ResolveOptions{MirrorDir: t.TempDir(), Offline: true})
		if err == nil {
			t.Fatal("offline must not fall back to a remote URL")
		}
		if got := errs.ExitCode(err); got != 4 {
			t.Errorf("exit=%d, want 4", got)
		}
		if msg := err.Error(); !strings.Contains(msg, "offline and no local mirror") {
			t.Errorf("message=%q: say why offline failed", msg)
		}
		if hint := errs.Hint(err); !strings.Contains(hint, "ngm install") || !strings.Contains(hint, "--offline") {
			t.Errorf("hint=%q: give both ways out (populate the mirror, or drop --offline)", hint)
		}
	})
}

// RefNotFoundError 是"问到了、但没有"的标记，不是新错误码：退出号仍是 3，
// 但 verify 必须能据此下结论（drift），而不是把权限/网络失败也报成 drift。
func TestRefNotFoundMarker(t *testing.T) {
	inner := errs.New(errs.CodeConfigInvalid, "github:acme/repo: tag \"v9\" not found", "check the tag name")
	e := &RefNotFoundError{Err: inner}

	if !IsRefNotFound(e) {
		t.Fatal("the marker itself must be recognized")
	}
	if !IsRefNotFound(fmt.Errorf("resolve: %w", e)) {
		t.Error("errors.As semantics: a wrapped marker must still be recognized")
	}
	if IsRefNotFound(errors.New("plain failure")) {
		t.Error("an ordinary failure is not a conclusion about the ref")
	}
	if got := errs.ExitCode(e); got != 3 {
		t.Errorf("exit=%d, want 3 (the marker does not change the code)", got)
	}
	if got := errs.Hint(e); got != "check the tag name" {
		t.Errorf("hint=%q: Unwrap must keep the NgmError reachable", got)
	}
}

// 网络类失败的上下文补充：必须说清是远端还是本地 mirror、地址是什么，
// 并保留原 hint；不是 NgmError 的错误原样返回（不臆造错误码）。
func TestDecorateFetchError(t *testing.T) {
	repo := Canonical{Host: "github.com", Path: "acme/repo"}
	inner := errs.New(errs.CodeGitFetch, "boom", "configure credentials")

	out := decorateFetchError(inner, repo, "/srv/mirror/github.com/acme/repo.git", true)
	if got := errs.ExitCode(out); got != 4 {
		t.Errorf("exit=%d, want 4", got)
	}
	msg := out.Error()
	for _, want := range []string{"github:acme/repo", "local mirror", "/srv/mirror/github.com/acme/repo.git"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message=%q missing %q", msg, want)
		}
	}
	if got := errs.Hint(out); got != "configure credentials" {
		t.Errorf("hint=%q: the original hint must survive decoration", got)
	}

	remote := decorateFetchError(inner, repo, "https://github.com/acme/repo.git", false)
	if msg := remote.Error(); !strings.Contains(msg, "remote") {
		t.Errorf("message=%q: a remote failure must say remote", msg)
	}

	plain := errors.New("raw failure")
	if got := decorateFetchError(plain, repo, "x", false); got != plain {
		t.Errorf("non-NgmError must be returned unchanged, got %v", got)
	}
}
