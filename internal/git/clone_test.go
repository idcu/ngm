package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestCloneMirror_AndFetchIncremental(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	// 源仓库（模拟远端）
	src := t.TempDir()
	head1 := testutils.GitInit(t, src)
	testutils.GitWriteFile(t, src, "README.md", "hello\n")
	head1 = testutils.GitCommit(t, src, "docs: readme")
	testutils.GitTag(t, src, "v1.0.0", true)

	dst := filepath.Join(t.TempDir(), "mirror", "repo.git")
	if err := CloneMirror(ctx, Options{}, src, dst); err != nil {
		t.Fatalf("CloneMirror: %v", err)
	}
	if !IsBareMirror(dst) {
		t.Fatalf("cloned dir is not a bare mirror: %s", dst)
	}
	if got := revParse(t, dst, "main"); got != head1 {
		t.Errorf("mirror main=%s want %s", got, head1)
	}

	// 源仓库前进后，FetchMirror 应把新 commit 同步过来
	testutils.GitWriteFile(t, src, "src/a.ts", "export const a = 1\n")
	head2 := testutils.GitCommit(t, src, "feat: a")
	if err := FetchMirror(ctx, Options{}, dst); err != nil {
		t.Fatalf("FetchMirror: %v", err)
	}
	if got := revParse(t, dst, "main"); got != head2 {
		t.Errorf("after fetch mirror main=%s want %s", got, head2)
	}
}

func TestCloneMirror_RefusesExistingPath(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	src := t.TempDir()
	testutils.GitInit(t, src)

	dst := filepath.Join(t.TempDir(), "existing.git")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	err := CloneMirror(ctx, Options{}, src, dst)
	if err == nil {
		t.Fatalf("expected refusal when target exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCloneMirror_EmptyURL(t *testing.T) {
	err := CloneMirror(context.Background(), Options{}, "", filepath.Join(t.TempDir(), "x.git"))
	if err == nil || !strings.Contains(err.Error(), "empty remote URL") {
		t.Fatalf("expected empty-url error, got %v", err)
	}
}

func TestIsBareMirror_NonRepo(t *testing.T) {
	if IsBareMirror(filepath.Join(t.TempDir(), "nope")) {
		t.Errorf("nonexistent path reported as bare mirror")
	}
	// 普通目录
	plain := t.TempDir()
	if IsBareMirror(plain) {
		t.Errorf("plain dir reported as bare mirror")
	}
}

func TestMirrorRemoteURL(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	src := t.TempDir()
	testutils.GitInit(t, src)
	dst := filepath.Join(t.TempDir(), "r.git")
	if err := CloneMirror(ctx, Options{}, src, dst); err != nil {
		t.Fatalf("clone: %v", err)
	}
	url, err := MirrorRemoteURL(ctx, Options{}, dst)
	if err != nil {
		t.Fatalf("MirrorRemoteURL: %v", err)
	}
	if url == "" {
		t.Errorf("empty remote url")
	}
}

// TestMirrorConfigContainsNoToken 锁定安全纪律：
// mirror 的 .git/config 中只能出现原始 remote URL，不得含 ngm 注入的凭证。
func TestMirrorConfigContainsNoToken(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	src := t.TempDir()
	testutils.GitInit(t, src)
	dst := filepath.Join(t.TempDir(), "r.git")
	if err := CloneMirror(ctx, Options{}, src, dst); err != nil {
		t.Fatalf("clone: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(dst, "config"))
	if err != nil {
		t.Fatal(err)
	}
	// 源是本地路径，config 里也只有本地路径；断言不含 GITHUB_TOKEN 之类字面量
	for _, suspect := range []string{"GITHUB_TOKEN", "ghp_", "x-access-token", "GITLAB_TOKEN"} {
		if strings.Contains(string(cfg), suspect) {
			t.Errorf("mirror config unexpectedly contains %q:\n%s", suspect, cfg)
		}
	}
}

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(testutils.GitOutput(t, dir, "rev-parse", ref))
}
