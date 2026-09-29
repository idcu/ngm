package vendor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/testutils"
)

func newStore(t *testing.T) *ContentStore {
	t.Helper()
	return NewContentStore(filepath.Join(t.TempDir(), "content"))
}

// putFixture 构造一个 fixture 仓库并把它写入 content store，返回 (commit, digest)。
func putFixture(t *testing.T, store *ContentStore, build func(t *testing.T) *testutils.GitRepo) (commit, dg string) {
	t.Helper()
	repo := build(t)
	commit = repo.Head()

	var err error
	dg, err = git.BuildArchiveDigest(context.Background(), git.Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchiveDigest: %v", err)
	}
	res, err := store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo:   "github:test/fixture",
		Commit: commit,
		Digest: dg,
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if res.AlreadyPresent {
		t.Errorf("first Put should not report AlreadyPresent")
	}
	return commit, dg
}

func simpleRepo(t *testing.T) *testutils.GitRepo {
	r := testutils.NewGitRepo(t)
	r.WriteFile("src/index.ts", "export const x = 1\n")
	r.WriteFile("README.md", "# hello\n")
	r.Commit("feat: simple")
	return r
}

func TestContentStore_PutAndRead(t *testing.T) {
	store := newStore(t)
	commit, dg := putFixture(t, store, simpleRepo)

	if !store.Has(dg) {
		t.Fatalf("Has() should be true after Put")
	}

	// 布局：<root>/sha256/<hex>/{tree,meta.json}
	hex := strings.TrimPrefix(dg, "sha256:")
	wantDir := filepath.Join(store.Root(), "sha256", hex)
	if got := store.PathForDigest(dg); got != wantDir {
		t.Errorf("PathForDigest=%q want %q", got, wantDir)
	}
	if _, err := os.Stat(filepath.Join(wantDir, "meta.json")); err != nil {
		t.Errorf("meta.json missing: %v", err)
	}
	if st, err := os.Stat(filepath.Join(wantDir, "tree")); err != nil || !st.IsDir() {
		t.Errorf("tree/ missing: %v", err)
	}

	// meta 内容
	m, err := store.ReadMeta(dg)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}
	if m.Repo != "github:test/fixture" {
		t.Errorf("Repo=%q", m.Repo)
	}
	if m.Commit != commit {
		t.Errorf("Commit=%q want %q", m.Commit, commit)
	}
	if m.Digest != dg {
		t.Errorf("Digest=%q want %q", m.Digest, dg)
	}
	if m.ManifestVersion != digest.ManifestVersion {
		t.Errorf("ManifestVersion=%q want %q", m.ManifestVersion, digest.ManifestVersion)
	}
	if m.SchemaVersion != MetaSchemaVersion {
		t.Errorf("SchemaVersion=%d", m.SchemaVersion)
	}

	// 解包内容
	body, err := os.ReadFile(filepath.Join(store.TreePath(dg), "src", "index.ts"))
	if err != nil {
		t.Fatalf("read unpacked file: %v", err)
	}
	if string(body) != "export const x = 1\n" {
		t.Errorf("unpacked content=%q", body)
	}
}

func TestContentStore_Idempotent(t *testing.T) {
	store := newStore(t)
	commit, dg := putFixture(t, store, simpleRepo)

	// 第二次 Put 必须是 no-op
	res, err := store.Put(context.Background(), git.Options{}, "", Meta{
		Repo:   "github:test/fixture",
		Commit: commit,
		Digest: dg,
	})
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if !res.AlreadyPresent {
		t.Errorf("second Put should report AlreadyPresent")
	}
}

// TestContentStore_RejectsDigestMismatch 是最重要的安全测试：
// 若 meta 声明的 digest 与该 commit 实际算出的不一致，必须拒绝——
// 否则会把内容写进错误的槽位，让 verify 永远通过（最危险的失效模式）。
func TestContentStore_RejectsDigestMismatch(t *testing.T) {
	store := newStore(t)
	repo := simpleRepo(t)
	commit := repo.Head()

	bogus := "sha256:" + strings.Repeat("f", 64)
	_, err := store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo:   "github:test/fixture",
		Commit: commit,
		Digest: bogus,
	})
	if err == nil {
		t.Fatalf("expected digest mismatch rejection")
	}
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error type %T", err)
	}
	if ne.Code != errs.CodeDigestMismatch {
		t.Errorf("exit code=%d want 2 (DigestMismatch)", ne.Code.ExitCode())
	}
	if !strings.Contains(ne.Hint, "verify") {
		t.Errorf("hint should point at `ngm verify`: %q", ne.Hint)
	}
	// 失败时不得留下任何内容树
	if store.Has(bogus) {
		t.Errorf("store must not contain an entry after a rejected Put")
	}
	if _, err := os.Stat(store.PathForDigest(bogus)); err == nil {
		t.Errorf("failed Put left a directory behind")
	}
}

func TestContentStore_RequiresFields(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	cases := []struct {
		name string
		meta Meta
		want string
	}{
		{"no commit", Meta{Repo: "github:a/b", Digest: "sha256:" + strings.Repeat("0", 64)}, "commit"},
		{"no repo", Meta{Commit: strings.Repeat("0", 40), Digest: "sha256:" + strings.Repeat("0", 64)}, "repo"},
		{"no digest", Meta{Repo: "github:a/b", Commit: strings.Repeat("0", 40)}, "digest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.Put(ctx, git.Options{}, t.TempDir(), tc.meta)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestContentStore_HasAndReadMeta_Missing(t *testing.T) {
	store := newStore(t)
	absent := "sha256:" + strings.Repeat("9", 64)

	if store.Has(absent) {
		t.Errorf("Has() on empty store should be false")
	}
	if store.Has("") {
		t.Errorf("Has(\"\") should be false")
	}
	if _, err := store.ReadMeta(absent); err == nil {
		t.Errorf("ReadMeta on missing entry should fail")
	}
}

func TestContentStore_ExecutableBitPreserved(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho hi\n")
		r.WriteFile("plain.txt", "plain\n")
		r.Commit("feat: exec")
		return r
	})

	// 非 POSIX 平台不表达能力位；只断言文件存在且内容正确
	for _, name := range []string{"scripts/run.sh", "plain.txt"} {
		if _, err := os.Stat(filepath.Join(store.TreePath(dg), filepath.FromSlash(name))); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(store.TreePath(dg), "scripts", "run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o111 == 0 {
			t.Errorf("exec bit not preserved: mode=%v", st.Mode())
		}
	}
}

func TestContentStore_SymlinkUnpack(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 需要开发者模式；M4 的 linkMode 会提供降级路径
		probe := filepath.Join(t.TempDir(), "probe-link")
		if err := os.Symlink("target", probe); err != nil {
			t.Skipf("symlinks unavailable in this environment: %v", err)
		}
	}

	store := newStore(t)
	_, dg := putFixture(t, store, func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("target.txt", "content\n")
		r.Commit("target")
		r.AddSymlinkEntry("link.txt", "target.txt")
		r.Commit("link")
		return r
	})

	linkPath := filepath.Join(store.TreePath(dg), "link.txt")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("unpacked entry is not a symlink: mode=%v", info.Mode())
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != "target.txt" {
		t.Errorf("symlink target=%q want %q", target, "target.txt")
	}
}

// TestContentStore_RejectsLFSEntry LFS 内容不得进入 store（与 digest 层口径一致）。
func TestContentStore_RejectsLFSEntry(t *testing.T) {
	store := newStore(t)
	repo := testutils.BuildLFSRepo(t)
	commit := repo.Head()

	// digest 生成阶段就会失败
	_, err := git.BuildArchiveDigest(context.Background(), git.Options{}, repo.Dir, commit)
	if err == nil {
		t.Fatalf("LFS must be rejected before reaching the content store")
	}

	// 即便调用方伪造一个 digest，Put 内部的重新计算也会拦住它
	_, err = store.Put(context.Background(), git.Options{}, repo.Dir, Meta{
		Repo:   "github:test/lfs",
		Commit: commit,
		Digest: "sha256:" + strings.Repeat("a", 64),
	})
	if err == nil {
		t.Fatalf("expected failure for LFS content")
	}
	if !strings.Contains(err.Error(), "LFS") {
		t.Errorf("error should mention LFS: %v", err)
	}
}
