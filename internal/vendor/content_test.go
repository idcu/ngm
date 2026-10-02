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

	// 布局（ADR-019）：<root>/trees/<hex>/{manifest.json,meta.json} + <root>/blobs/
	hex := strings.TrimPrefix(dg, "sha256:")
	wantV2 := filepath.Join(store.Root(), "trees", hex)
	if got := store.V2Dir(dg); got != wantV2 {
		t.Errorf("V2Dir=%q want %q", got, wantV2)
	}
	if !store.hasManifest(dg) {
		t.Fatalf("manifest.json missing at %s", store.ManifestPath(dg))
	}
	if _, err := os.Stat(store.MetaPathV2(dg)); err != nil {
		t.Errorf("meta.json missing in the v2 dir: %v", err)
	}
	// 写路径只写 v2：`sha256/` 下**不该**留下这次写入的痕迹
	// （ADR-019 把它定为"只读、不再新增"）。
	if _, err := os.Stat(store.PathForDigest(dg)); !os.IsNotExist(err) {
		t.Errorf("a v2 publish must not leave a v1 tree behind (stat err=%v)", err)
	}

	// 清单本身必须确定：条目按 path 升序（可 diff、可快照）。
	mf, err := store.readManifest(dg)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if mf.SchemaVersion != layoutSchemaVersion {
		t.Errorf("manifest schemaVersion=%d want %d", mf.SchemaVersion, layoutSchemaVersion)
	}
	if got := len(mf.Entries); got != 2 {
		t.Fatalf("manifest entries=%d want 2", got)
	}
	if mf.Entries[0].Path != "README.md" || mf.Entries[1].Path != "src/index.ts" {
		t.Errorf("manifest must be sorted by path, got %q then %q",
			mf.Entries[0].Path, mf.Entries[1].Path)
	}
	// blob 在池里（两位 hex 分片），且**名字不是内容的裸 sha256**——
	// mode 掺进了 blob 的身份，理由见 blobKey（hardlink 共享 inode）。
	for _, e := range mf.Entries {
		if e.SHA == "" {
			continue
		}
		if st, serr := os.Stat(store.BlobPath(e.SHA)); serr != nil || st.IsDir() {
			t.Errorf("blob %s for %s is not in the pool: %v", e.SHA, e.Path, serr)
		}
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

	// 内容：经**与布局无关**的入口读（v2 读 blob，v1 读树）。
	body, ok, err := store.ReadFile(dg, "src/index.ts")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !ok {
		t.Fatal("ReadFile reports src/index.ts missing right after Put")
	}
	if string(body) != "export const x = 1\n" {
		t.Errorf("stored content=%q", body)
	}
	if _, ok, _ := store.ReadFile(dg, "does/not/exist.ts"); ok {
		t.Errorf("ReadFile must report a missing path as (nil, false, nil)")
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
	// 失败时不得留下任何内容树（两种布局都不该有痕迹）
	if store.Has(bogus) {
		t.Errorf("store must not contain an entry after a rejected Put")
	}
	for _, dir := range []string{store.PathForDigest(bogus), store.V2Dir(bogus)} {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("failed Put left %s behind", dir)
		}
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

// execRepo 是一个含可执行文件的 fixture（多处复用）。
func execRepo(t *testing.T) *testutils.GitRepo {
	r := testutils.NewGitRepo(t)
	r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho hi\n")
	r.WriteFile("plain.txt", "plain\n")
	r.Commit("feat: exec")
	return r
}

// TestContentStore_ExecutableBitPreserved 锁定 v0.8 实现时发现的那处缺口：
// 可执行位必须**穿过 store** 保留下来，而且不能靠"文件系统反推"。
func TestContentStore_ExecutableBitPreserved(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, execRepo)

	// 1) 清单里的 mode 来自 Git——**与平台无关**（Windows 上也可断言，
	//    这是 v0.8 把 Put 改成直接读 Git 条目换来的）。
	entries, err := store.Entries(dg)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	byPath := map[string]ContentEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if got := byPath["scripts/run.sh"].Mode; got != digest.ModeExecutable {
		t.Errorf("scripts/run.sh mode=%q want %q", got, digest.ModeExecutable)
	}
	if got := byPath["plain.txt"].Mode; got != digest.ModeRegular {
		t.Errorf("plain.txt mode=%q want %q", got, digest.ModeRegular)
	}

	// 2) 内容读得出来
	for name, want := range map[string]string{
		"scripts/run.sh": "#!/bin/sh\necho hi\n",
		"plain.txt":      "plain\n",
	} {
		body, ok, rerr := store.ReadFile(dg, name)
		if rerr != nil || !ok {
			t.Fatalf("ReadFile(%s): ok=%v err=%v", name, ok, rerr)
		}
		if string(body) != want {
			t.Errorf("%s content=%q want %q", name, body, want)
		}
	}

	// 3) POSIX 上 blob 本身带可执行位——落地层用 hardlink，inode 的权限位
	//    就是它落地后的权限（见 blobKey）。这一条是"hardlink 也能保住可执行位"
	//    的**充分条件**：blob 错，落地就是错的。
	if runtime.GOOS == "windows" {
		t.Skip("可执行位在 Windows 上不可表示；清单里的 mode 已在上方断言")
	}
	st, serr := os.Stat(byPath["scripts/run.sh"].Full)
	if serr != nil {
		t.Fatalf("stat blob: %v", serr)
	}
	if st.Mode().Perm()&0o111 == 0 {
		t.Errorf("blob must carry the exec bit so hardlink can inherit it: mode=%v", st.Mode())
	}
}

// TestContentStore_SymlinkEntry 锁定 v2 对 symlink 的处理：
// **目标字符串进清单，store 里不创建 symlink**。
//
// 这比 v1 强在可用性：v1 要在 store 里真的 `os.Symlink`，于是"仓库里有 symlink"
// 会让 Windows 用户必须先开开发者模式才能填充 store。v2 不需要——store 里
// 只有 blob 与清单，symlink 是**落地层**（项目目录里）才发生的事。
func TestContentStore_SymlinkEntry(t *testing.T) {
	store := newStore(t)
	_, dg := putFixture(t, store, func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("target.txt", "content\n")
		r.Commit("target")
		r.AddSymlinkEntry("link.txt", "target.txt")
		r.Commit("link")
		return r
	})

	entries, err := store.Entries(dg)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	var link ContentEntry
	for _, e := range entries {
		if e.Path == "link.txt" {
			link = e
		}
	}
	if link.Path == "" {
		t.Fatal("link.txt missing from the entry list")
	}
	if !link.Symlink {
		t.Errorf("link.txt must be reported as a symlink, got mode=%q", link.Mode)
	}
	if link.Mode != digest.ModeSymlink {
		t.Errorf("link.txt mode=%q want %q", link.Mode, digest.ModeSymlink)
	}
	if link.LinkTarget != "target.txt" {
		t.Errorf("symlink target=%q want %q", link.LinkTarget, "target.txt")
	}
	if link.Full != "" {
		t.Errorf("a symlink entry must not point at a blob, got %q", link.Full)
	}
	// 读出来的是目标字符串本身（与 digest 层的口径一致）
	body, ok, rerr := store.ReadFile(dg, "link.txt")
	if rerr != nil || !ok {
		t.Fatalf("ReadFile(link.txt): ok=%v err=%v", ok, rerr)
	}
	if string(body) != "target.txt" {
		t.Errorf("ReadFile of a symlink=%q want the target string", body)
	}

	// store 里**不该**有任何 symlink（这正是上面那条可用性好处）。
	// 逐个 blob 检查即可——清单是 JSON 文件。
	mf, merr := store.readManifest(dg)
	if merr != nil {
		t.Fatal(merr)
	}
	for _, e := range mf.Entries {
		if e.Mode != digest.ModeSymlink {
			continue
		}
		if e.SHA != "" {
			t.Errorf("a symlink entry must not carry a blob: %+v", e)
		}
		if e.Target != "target.txt" {
			t.Errorf("symlink target in the manifest=%q", e.Target)
		}
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
