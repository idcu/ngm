package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// readVector 读取 testdata/vectors 下的冻结向量（去掉首尾空白）。
func readVector(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(testutils.GoldenPath(t, name))
	if err != nil {
		t.Fatalf("read vector %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

// TestM2Acceptance 是 development/v0.1-plan.md 中 M2（archiveDigest）阶段的**可执行验收**。
//
// 逐条对应 M2 的验收标准：
//
//  1. 同一 fixture 仓库的同一 commit 输出相同 digest；重复执行幂等
//  2. CRLF 不做转换（字节原样参与哈希）
//  3. 规则变更必须同时更新向量与规范版本
//
// 三平台一致性由 CI 的 os matrix 保证：本测试读的是**同一份冻结向量**，
// 因此在 ubuntu / macos / windows 上都必须得到相同 digest。
func TestM2Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	// 冻结向量（由 internal/git 的向量测试生成与守护）
	wantDigest := readVector(t, "vectors/archive-kitchen-sink.digest")
	wantManifest := readVector(t, "vectors/archive-kitchen-sink.manifest")

	// ---------------------------------------------------------------------
	// 验收 1：同一 fixture 的内容 → 冻结向量的 digest
	// ---------------------------------------------------------------------
	t.Run("digest matches frozen vector", func(t *testing.T) {
		repo := testutils.BuildKitchenSinkRepo(t)

		manifest, err := git.BuildArchive(ctx, git.Options{}, repo.Dir, repo.Head())
		if err != nil {
			t.Fatalf("BuildArchive: %v", err)
		}
		if got := digest.RenderManifest(manifest); got != wantManifest+"\n" {
			t.Errorf("manifest drifted from frozen vector:\n--- got ---\n%s\n--- want ---\n%s\n", got, wantManifest)
		}
		if got := digest.Digest(manifest); got != wantDigest {
			t.Errorf("digest drifted!\n got: %s\nwant: %s\n"+
				"this means either the rule changed (bump the manifest version + migrate lock) "+
				"or the fixture content changed (update the vectors deliberately)", got, wantDigest)
		}
	})

	// ---------------------------------------------------------------------
	// 验收 1（续）：幂等 —— 重复执行得到同一 digest
	// ---------------------------------------------------------------------
	t.Run("idempotent", func(t *testing.T) {
		repo := testutils.BuildKitchenSinkRepo(t)
		for i := 0; i < 3; i++ {
			got, err := git.BuildArchiveDigest(ctx, git.Options{}, repo.Dir, repo.Head())
			if err != nil {
				t.Fatal(err)
			}
			if got != wantDigest {
				t.Fatalf("run %d produced %s, want %s", i, got, wantDigest)
			}
		}
	})

	// ---------------------------------------------------------------------
	// 验收 2：CRLF 原样参与哈希
	// ---------------------------------------------------------------------
	t.Run("CRLF is not converted", func(t *testing.T) {
		lfRepo := testutils.NewGitRepo(t)
		lfRepo.WriteFileRaw("f.txt", []byte("a\nb\n"))
		lfHead := lfRepo.Commit("lf")

		crlfRepo := testutils.NewGitRepo(t)
		crlfRepo.WriteFileRaw("f.txt", []byte("a\r\nb\r\n"))
		crlfHead := crlfRepo.Commit("crlf")

		lfDigest, err := git.BuildArchiveDigest(ctx, git.Options{}, lfRepo.Dir, lfHead)
		if err != nil {
			t.Fatal(err)
		}
		crlfDigest, err := git.BuildArchiveDigest(ctx, git.Options{}, crlfRepo.Dir, crlfHead)
		if err != nil {
			t.Fatal(err)
		}
		if lfDigest == crlfDigest {
			t.Errorf("LF and CRLF hashed identically (%s): newline conversion must not happen", lfDigest)
		}
		// 冻结向量中确实包含一个 CRLF 文件（kitchen sink 的 crlf.txt）
		if !strings.Contains(wantManifest, "crlf.txt") {
			t.Errorf("frozen vector should include a CRLF file to prove byte-level hashing")
		}
	})

	// ---------------------------------------------------------------------
	// 异常路径：必须报错（exit 3），不得静默哈希
	// ---------------------------------------------------------------------
	t.Run("rejects LFS pointer", func(t *testing.T) {
		repo := testutils.BuildLFSRepo(t)
		_, err := git.BuildArchiveDigest(ctx, git.Options{}, repo.Dir, repo.Head())
		if err == nil {
			t.Fatal("LFS pointer must be rejected")
		}
		if !strings.Contains(err.Error(), "LFS") {
			t.Errorf("error should mention LFS: %v", err)
		}
	})

	t.Run("rejects gitlink", func(t *testing.T) {
		repo := testutils.BuildGitlinkRepo(t)
		_, err := git.BuildArchiveDigest(ctx, git.Options{}, repo.Dir, repo.Head())
		if err == nil {
			t.Fatal("gitlink must be rejected")
		}
		if !strings.Contains(err.Error(), "gitlink") && !strings.Contains(err.Error(), "submodule") {
			t.Errorf("error should mention gitlink/submodule: %v", err)
		}
	})

	// ---------------------------------------------------------------------
	// 端到端：init → add → update --digest --store --offline（零网络）
	// ---------------------------------------------------------------------
	t.Run("offline end-to-end with digest and content store", func(t *testing.T) {
		isolateUserEnv(t)
		layout, err := vendor.DefaultLayout()
		if err != nil {
			t.Fatal(err)
		}

		src := testutils.BuildKitchenSinkRepo(t)
		seedMirror(t, "github:m2-test/libs", src.Dir)

		proj := t.TempDir()
		if code, out := runCaptureCode(t, "init", "github.com:m2-test/app", "--dir="+proj); code != 0 {
			t.Fatalf("init: %s", out)
		}
		if code, out := runCaptureCode(t, "add", "github:m2-test/libs@main", "--ref-type=branch", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		code, out := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline", "--digest", "--store")
		if code != 0 {
			t.Fatalf("update --digest --store exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, wantDigest) {
			t.Errorf("output should contain the frozen digest %s:\n%s", wantDigest, out)
		}
		if !strings.Contains(out, "archiveDigest:") {
			t.Errorf("output should label the digest:\n%s", out)
		}

		// content store 落地
		store := vendor.NewContentStore(layout.ContentRoot())
		if !store.Has(wantDigest) {
			t.Fatalf("content store does not contain %s after --store", wantDigest)
		}
		meta, err := store.ReadMeta(wantDigest)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Repo != "github:m2-test/libs" {
			t.Errorf("meta.Repo=%q", meta.Repo)
		}
		if meta.ManifestVersion != digest.ManifestVersion {
			t.Errorf("meta.ManifestVersion=%q", meta.ManifestVersion)
		}
		// 解包内容抽查
		body, err := os.ReadFile(filepath.Join(store.TreePath(wantDigest), "nested", "deep", "dir", "mod.ts"))
		if err != nil {
			t.Fatalf("unpacked file missing: %v", err)
		}
		if string(body) != "export const x = 1\n" {
			t.Errorf("unpacked content=%q", body)
		}
		// 解包的 CRLF 文件必须保留原始字节
		crlf, err := os.ReadFile(filepath.Join(store.TreePath(wantDigest), "crlf.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(crlf) != "line1\r\nline2\r\n" {
			t.Errorf("content store must preserve CRLF bytes, got %q", crlf)
		}

		// 二次执行：digest 不变 + store 报告 already present
		code2, out2 := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline", "--digest", "--store")
		if code2 != 0 {
			t.Fatalf("second update exit=%d out=%s", code2, out2)
		}
		if !strings.Contains(out2, wantDigest) {
			t.Errorf("second run digest changed:\n%s", out2)
		}
		if !strings.Contains(out2, "already present") {
			t.Errorf("second run should report the store entry as already present:\n%s", out2)
		}
	})

	// ---------------------------------------------------------------------
	// 端到端：--digest 在 LFS 依赖上必须失败（exit 3）
	// ---------------------------------------------------------------------
	t.Run("offline end-to-end rejects LFS dependency", func(t *testing.T) {
		isolateUserEnv(t)
		src := testutils.BuildLFSRepo(t)
		seedMirror(t, "github:m2-test/lfs", src.Dir)

		proj := t.TempDir()
		if code, out := runCaptureCode(t, "init", "github.com:m2-test/app", "--dir="+proj); code != 0 {
			t.Fatalf("init: %s", out)
		}
		if code, out := runCaptureCode(t, "add", "github:m2-test/lfs@main", "--ref-type=branch", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline", "--digest")
		if code != 3 {
			t.Fatalf("exit=%d want 3 (config/policy error); out=%s", code, out)
		}
		if !strings.Contains(out, "LFS") {
			t.Errorf("out should explain the LFS limitation:\n%s", out)
		}
	})
}
