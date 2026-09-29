package vendor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

func newFixtureMirror(t *testing.T) (*Mirror, resolve.Canonical, string) {
	t.Helper()
	testutils.MustHaveGit(t)
	root := filepath.Join(t.TempDir(), "mirror")
	return NewMirror(root, git.Options{}), resolve.MustNormalize("github:org/repo"), root
}

func TestMirror_PathFor_Layout(t *testing.T) {
	m := NewMirror("/tmp/ngm-mirror", git.Options{})
	cases := []struct {
		in   string
		want string // 期望的相对布局（用 / 表达，测试内转 filepath）
	}{
		{"github:org/repo", "github.com/org/repo.git"},
		{"gitee:my-org/utils", "gitee.com/my-org/utils.git"},
		{"gitlab:group/sub/repo", "gitlab.com/group/sub/repo.git"},
		{"gitlab.example.com:group/repo", "gitlab.example.com/group/repo.git"},
	}
	for _, tc := range cases {
		got := m.PathFor(resolve.MustNormalize(tc.in))
		want := filepath.Join("/tmp/ngm-mirror", filepath.FromSlash(tc.want))
		if got != want {
			t.Errorf("PathFor(%q)=%q want %q", tc.in, got, want)
		}
	}
}

func TestMirror_EnsureCloneThenFetch(t *testing.T) {
	m, repo, root := newFixtureMirror(t)
	ctx := context.Background()

	src := t.TempDir()
	head1 := testutils.GitInit(t, src)
	testutils.GitWriteFile(t, src, "a.ts", "export const a = 1\n")
	head1 = testutils.GitCommit(t, src, "feat: a")

	// 第一次：clone
	res, err := m.Ensure(ctx, repo, src, resolve.ProtocolHTTPS)
	if err != nil {
		t.Fatalf("Ensure(clone): %v", err)
	}
	if !res.Created {
		t.Errorf("first Ensure should report Created=true")
	}
	if !strings.HasPrefix(res.Path, root) {
		t.Errorf("mirror path %q not under root %q", res.Path, root)
	}
	if !m.Exists(repo) {
		t.Errorf("Exists should be true after Ensure")
	}

	// 第二次（无变化）：fetch，Created=false
	res2, err := m.Ensure(ctx, repo, src, resolve.ProtocolHTTPS)
	if err != nil {
		t.Fatalf("Ensure(fetch): %v", err)
	}
	if res2.Created {
		t.Errorf("second Ensure should report Created=false")
	}

	// 源前进，第三次 Ensure 应同步
	testutils.GitWriteFile(t, src, "b.ts", "export const b = 2\n")
	head2 := testutils.GitCommit(t, src, "feat: b")
	if _, err := m.Ensure(ctx, repo, src, resolve.ProtocolHTTPS); err != nil {
		t.Fatalf("Ensure(update): %v", err)
	}
	got := strings.TrimSpace(testutils.GitOutput(t, res.Path, "rev-parse", "main"))
	if got != head2 || head2 == head1 {
		t.Errorf("mirror main=%s want %s", got, head2)
	}
}

func TestMirror_Ensure_RefusesInvalidExistingPath(t *testing.T) {
	m, repo, _ := newFixtureMirror(t)
	ctx := context.Background()

	// 预先创建一个同名普通目录（非裸仓库）→ Ensure 应明确报错而非静默删除
	bad := m.PathFor(repo)
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	// 放入干扰文件，确保 IsBareMirror=false
	if err := os.WriteFile(filepath.Join(bad, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := m.Ensure(ctx, repo, t.TempDir(), resolve.ProtocolHTTPS)
	if err == nil {
		t.Fatalf("expected error for invalid existing path")
	}
	if !strings.Contains(err.Error(), "not a valid bare repository") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMirror_EnsureLocal(t *testing.T) {
	m, repo, _ := newFixtureMirror(t)
	ctx := context.Background()

	src := t.TempDir()
	head := testutils.GitInit(t, src)
	testutils.GitWriteFile(t, src, "x.ts", "export const x = 1\n")
	head = testutils.GitCommit(t, src, "feat: x")

	res, err := m.EnsureLocal(ctx, repo, src)
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if !res.Created {
		t.Errorf("expected Created=true")
	}
	got := strings.TrimSpace(testutils.GitOutput(t, res.Path, "rev-parse", "main"))
	if got != head {
		t.Errorf("mirror main=%s want %s", got, head)
	}

	// 幂等
	res2, err := m.EnsureLocal(ctx, repo, src)
	if err != nil {
		t.Fatalf("EnsureLocal(second): %v", err)
	}
	if res2.Created {
		t.Errorf("second EnsureLocal should be a no-op")
	}
}

// TestMirror_ResolveUsesLocalMirror 验证"优先本地 mirror"路径：
// 一旦 mirror 就绪，ResolveRef 应只依赖本地（不注入 GitURL、不触网）完成解析。
func TestMirror_ResolveUsesLocalMirror(t *testing.T) {
	m, repo, root := newFixtureMirror(t)
	ctx := context.Background()

	src := t.TempDir()
	testutils.GitInit(t, src)
	testutils.GitWriteFile(t, src, "a.ts", "1\n")
	head := testutils.GitCommit(t, src, "feat: a")
	testutils.GitTag(t, src, "v1.0.0", true) // annotated

	if _, err := m.EnsureLocal(ctx, repo, src); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}

	// 不注入 GitURL：解析器应选中 MirrorDir 下的本地裸仓库
	got, err := resolve.ResolveRef(ctx, repo, "v1.0.0", resolve.RefTypeTag, resolve.ResolveOptions{
		MirrorDir: root,
	})
	if err != nil {
		t.Fatalf("ResolveRef via mirror: %v", err)
	}
	if got != head {
		t.Errorf("resolved via mirror = %s want %s", got, head)
	}

	// 删除源仓库后仍然可解析（证明完全离线）
	if err := os.RemoveAll(src); err != nil {
		t.Fatal(err)
	}
	got2, err := resolve.ResolveRef(ctx, repo, "v1.0.0", resolve.RefTypeTag, resolve.ResolveOptions{
		MirrorDir: root,
	})
	if err != nil {
		t.Fatalf("ResolveRef offline: %v", err)
	}
	if got2 != head {
		t.Errorf("offline resolve = %s want %s", got2, head)
	}
}
