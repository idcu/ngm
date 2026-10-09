package vendor

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
)

// v0.68：Put 复用调用方算好的 ArchiveMaterial（评估报告 P1#4 的"6 spawn → 2"）。
//
// 判据用**仪器**（`git.SpawnCount`），不用掐表——理由是 git/spawn.go 里那条：
// "掐表测的是噪声加信号，计数只测信号"。

func TestV68PutReusesArchiveMaterial(t *testing.T) {
	repo := basicRepo(t)
	commit := repo.Head()
	opts := git.Options{}

	material, err := git.BuildArchiveMaterial(context.Background(), opts, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchiveMaterial: %v", err)
	}

	t.Run("传了 Material：零 spawn", func(t *testing.T) {
		store := newStore(t)
		git.ResetSpawnCount()
		if _, err := store.Put(context.Background(), opts, repo.Dir, Meta{
			Repo: "github:test/reuse", Commit: commit, Digest: material.Digest,
			Material: material,
		}); err != nil {
			t.Fatalf("Put(with material): %v", err)
		}
		if n := git.SpawnCount(); n != 0 {
			t.Errorf("传了 Material 时 Put 起了 %d 个 git 进程，want 0——"+
				"那 4 个重复的 spawn 正是本版要省掉的", n)
		}
		// 复用不等于放行：内容必须真的落进 store。
		if !store.Has(material.Digest) {
			t.Error("复用 Material 的 Put 没有把内容落进 store")
		}
	})

	t.Run("不带 Material：一次 BuildArchiveMaterial（2 spawn）", func(t *testing.T) {
		// 这是 installFromLock 的校验路径：digest 是 lock 的**声明值**，
		// Put 内部的重算就是那条校验本身——不能省。但重算的实现单位
		// 从"digest + tree + cat-file（4）"收敛成"一次材料构建（2）"。
		store := newStore(t)
		git.ResetSpawnCount()
		if _, err := store.Put(context.Background(), opts, repo.Dir, Meta{
			Repo: "github:test/refresh", Commit: commit, Digest: material.Digest,
		}); err != nil {
			t.Fatalf("Put(without material): %v", err)
		}
		if n := git.SpawnCount(); n != 2 {
			t.Errorf("不带 Material 时 Put 起了 %d 个 git 进程，want 2（ls-tree + cat-file 各一次）", n)
		}
	})
}

// 复用路径**仍然核对身份字段**：拼错的 Material 必须被拒绝。
//
// 这一条是"6 → 2"的安全边界："把 A 的内容写进 B 的槽位"是这个 store
// 最危险的失效模式，而"调用方传对了"不是一条可依赖的假设。
func TestV68PutRejectsMismatchedMaterial(t *testing.T) {
	repo := basicRepo(t)
	commit := repo.Head()
	opts := git.Options{}

	material, err := git.BuildArchiveMaterial(context.Background(), opts, repo.Dir, commit)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("commit 不符", func(t *testing.T) {
		store := newStore(t)
		bad := *material
		bad.Commit = strings.Repeat("f", 40)
		_, err := store.Put(context.Background(), opts, repo.Dir, Meta{
			Repo: "github:test/x", Commit: commit, Digest: material.Digest, Material: &bad,
		})
		if err == nil || !strings.Contains(err.Error(), "archive material is for commit") {
			t.Errorf("commit 不符的 Material 必须被拒绝，got: %v", err)
		}
	})

	t.Run("digest 不符", func(t *testing.T) {
		store := newStore(t)
		bad := *material
		bad.Digest = "sha256:" + strings.Repeat("00", 32)
		_, err := store.Put(context.Background(), opts, repo.Dir, Meta{
			Repo: "github:test/x", Commit: commit, Digest: material.Digest, Material: &bad,
		})
		if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
			t.Errorf("digest 不符的 Material 必须被拒绝，got: %v", err)
		}
	})
}
