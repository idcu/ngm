package verify

import (
	"context"
	"testing"

	"gitee.com/idcu-go/modproof"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestModproofMatchesInternalMaterial 是外部模块的消费契约：
// ngm 仍用 core/git 计算 content-store 材料；verify 用外部 modproof 计算 proof。
// 两者必须对同一个 commit/subPath 给出相同的 moduleDigest 与 subtreeOID，
// 否则“切换外部依赖”只是把两套实现的分叉藏了起来。
func TestModproofMatchesInternalMaterial(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "export const a = 1\n")
	repo.WriteFile("packages/a/src/util.ts", "export const u = 1\n")
	repo.WriteFile("packages/b/index.ts", "export const b = 1\n")
	commit := repo.Commit("monorepo")

	ctx := context.Background()
	ext, err := modproof.Prove(ctx, modproof.Repo{Path: repo.Dir}, commit, "packages/a")
	if err != nil {
		t.Fatalf("modproof.Prove: %v", err)
	}
	mat, err := git.BuildModuleMaterial(ctx, git.Options{}, repo.Dir, commit, "packages/a")
	if err != nil {
		t.Fatalf("BuildModuleMaterial: %v", err)
	}
	oid, err := git.TreeOIDAtPath(ctx, git.Options{}, repo.Dir, commit, "packages/a")
	if err != nil {
		t.Fatalf("TreeOIDAtPath: %v", err)
	}
	if ext.ModuleDigest != mat.Digest {
		t.Errorf("module digest diverged: modproof=%s internal=%s", ext.ModuleDigest, mat.Digest)
	}
	if ext.SubtreeOID != oid {
		t.Errorf("subtree OID diverged: modproof=%s internal=%s", ext.SubtreeOID, oid)
	}
}
