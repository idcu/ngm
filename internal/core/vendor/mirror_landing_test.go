package vendor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestEnsureLandsAtomicallyAndLeavesNothingBehind 是最小的一条：
// 成功之后，最终路径上是**一个可用的 mirror**，而落地目录一个都不剩。
func TestEnsureLandsAtomicallyAndLeavesNothingBehind(t *testing.T) {
	m, repo, _ := newFixtureMirror(t)
	src := testutils.NewGitRepo(t)
	src.WriteFile("a.ts", "export const a = 1\n").Commit("feat: a")

	res, err := m.EnsureLocal(context.Background(), repo, src.Dir)
	if err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	if !res.Created {
		t.Error("Created must be true for a first clone")
	}
	if !git.IsMirror(res.Path) {
		t.Errorf("the landed path must be a usable mirror: %s", res.Path)
	}

	u, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(u.Landings) != 0 {
		t.Errorf("a successful clone must leave no landing directory, got %v", u.Landings)
	}
}

// TestEnsureNeverLeavesAHalfMirrorWhenTheCloneFails 是 v0.101 的**核心性质**：
//
//	失败之后，最终路径上**什么都没有**。
//
// 这正是 v0.100 实测到的那个形状的来源：从前克隆直接落在最终路径上，一次中断
// 就在那里留下一个"长得像能用的 mirror"（本机量到一个 66.82 MiB 的）。
func TestEnsureNeverLeavesAHalfMirrorWhenTheCloneFails(t *testing.T) {
	testutils.MustHaveGit(t)
	m, repo, _ := newFixtureMirror(t)

	// 一个不可能克隆成功的地址（本地不存在的路径，不触网）。
	_, err := m.EnsureLocal(context.Background(), repo, filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("cloning a nonexistent source must fail")
	}

	path := m.PathFor(repo)
	if _, serr := os.Stat(path); serr == nil {
		t.Errorf("a failed clone must not leave anything at the final path: %s", path)
	}

	// 而且**它自己的临时目录也要收掉**（进程被强杀时靠 `store prune` 兜底）。
	u, uerr := m.Usage()
	if uerr != nil {
		t.Fatalf("Usage: %v", uerr)
	}
	if len(u.Landings) != 0 {
		t.Errorf("a failed clone must clean up after itself, got %v", u.Landings)
	}
}

// TestLandCloneOutcomes 固定"落地"这一步的三个结局。
//
// 中间的"对手赢了"是**并发**那条路的全部逻辑：报成失败会让并发的两个
// `ngm install` 之一莫名其妙地失败，而那一次其实什么坏事都没发生。
func TestLandCloneOutcomes(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	newMirrorAt := func(t *testing.T, dst string) {
		t.Helper()
		src := testutils.NewGitRepo(t)
		src.WriteFile("a.ts", "export const a = 1\n").Commit("feat: a")
		if err := git.CloneMirror(ctx, git.Options{}, src.Dir, dst); err != nil {
			t.Fatalf("CloneMirror: %v", err)
		}
	}

	t.Run("nobody else was there", func(t *testing.T) {
		dir := t.TempDir()
		tmp := filepath.Join(dir, "m.git"+CloneLandingInfix+"aaaa")
		final := filepath.Join(dir, "m.git")
		newMirrorAt(t, tmp)

		created, err := landClone(tmp, final)
		if err != nil || !created {
			t.Fatalf("landClone = (%v, %v) want (true, nil)", created, err)
		}
		if !git.IsMirror(final) {
			t.Error("the final path must hold the mirror after landing")
		}
		if _, serr := os.Stat(tmp); serr == nil {
			t.Error("the landing path must be gone (rename, not copy)")
		}
	})

	t.Run("somebody else landed first", func(t *testing.T) {
		dir := t.TempDir()
		tmp := filepath.Join(dir, "m.git"+CloneLandingInfix+"bbbb")
		final := filepath.Join(dir, "m.git")
		newMirrorAt(t, tmp)
		newMirrorAt(t, final) // 对手先落地了

		created, err := landClone(tmp, final)
		if err != nil {
			t.Fatalf("losing the race is not an error, got %v", err)
		}
		if created {
			t.Error("Created must be false: the mirror that is there was built by the other process")
		}
		if !git.IsMirror(final) {
			t.Error("the winner's mirror must still be there")
		}
		// 我们那一份仍然在（由调用方决定清掉），而它**不是**残骸判据里的东西：
		// 它就是那个"没落地的克隆"，正因为对手赢了才没落地。
		if _, serr := os.Stat(tmp); serr != nil {
			t.Errorf("landClone itself must not delete the loser's directory: %v", serr)
		}
	})

	t.Run("the target is occupied by something that is not a mirror", func(t *testing.T) {
		dir := t.TempDir()
		tmp := filepath.Join(dir, "m.git"+CloneLandingInfix+"cccc")
		final := filepath.Join(dir, "m.git")
		newMirrorAt(t, tmp)
		// 非空的普通目录（不用空目录：不同平台上"rename 到空目录"的行为不一致）
		if err := os.MkdirAll(final, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(final, "not-a-repo.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		created, err := landClone(tmp, final)
		if err == nil {
			t.Fatalf("landing onto a foreign directory must fail, got created=%v", created)
		}
		if !strings.Contains(err.Error(), "could not land") {
			t.Errorf("the error must say what failed, got %v", err)
		}
	})
}

// TestEnsureRefusesAWreckInsteadOfFetchingNothing 是本版修掉的那个**真 bug**。
//
// 实测的因果链：`git fetch --prune` 在一个没有镜像 refspec 的裸仓库里
// **退出 0 且什么都不抓** ⇒ `Ensure` 报"我复用了既有 mirror" ⇒ 失败跑到下游，
// 用户看到的是"你的分支上游不存在"。
//
// 因此这条判据同时钉住三件事：报错（不是静默成功）、**一个 git 都没跑**、
// 以及**不删那个目录**（它是用户的磁盘）。
func TestEnsureRefusesAWreckInsteadOfFetchingNothing(t *testing.T) {
	testutils.MustHaveGit(t)
	m, repo, _ := newFixtureMirror(t)

	path := m.PathFor(repo)
	if err := os.MkdirAll(filepath.Join(path, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 实测形状：有 url、**没有** refspec
	cfg := "[core]\n\tbare = true\n[remote \"origin\"]\n\turl = https://example.invalid/o/r.git\n"
	if err := os.WriteFile(filepath.Join(path, "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	git.ResetSpawnCount()
	_, err := m.Ensure(context.Background(), repo, "https://example.invalid/o/r.git", "")
	if err == nil {
		t.Fatal("a mirror whose clone never finished must be refused, not silently reused")
	}
	if !strings.Contains(err.Error(), git.MirrorRefspec) {
		t.Errorf("the error must name the missing refspec (that is the evidence): %v", err)
	}
	if n := git.SpawnCount(); n != 0 {
		t.Errorf("refusing must not need git at all, spawned %d", n)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Errorf("ngm must not delete a directory it did not build: %v", serr)
	}
}

// TestUnusablePathErrorNamesTheRightShape 固定"两句话分开说"。
//
// 处置的前提不同：不是裸仓库的路径可能是用户放在那里的别的东西，
// 而"是裸仓库但没走完"是可以精确核对的（config 里没有那条 refspec）。
func TestUnusablePathErrorNamesTheRightShape(t *testing.T) {
	t.Run("a wrecked clone", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "wreck.git")
		if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[core]\n\tbare = true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := unusablePathError(dir)
		if !strings.Contains(err.Error(), "never finished") {
			t.Errorf("a bare repo without the mirror refspec must be reported as an unfinished clone: %v", err)
		}
	})

	t.Run("not a bare repo at all", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := unusablePathError(dir)
		if strings.Contains(err.Error(), "never finished") {
			t.Errorf("the two shapes must not share a message: %v", err)
		}
		if !strings.Contains(err.Error(), "not a valid bare repository") {
			t.Errorf("unexpected message: %v", err)
		}
	})
}

// TestLandingPathIsASiblingOfTheFinalPath 固定原子性的**前提**。
//
// `os.Rename` 的原子性只在同一个文件系统内成立；跨目录（尤其是跨挂载点）
// 时它会退化成 copy+delete，而那正好把"看不到半个 mirror"这句话变成假的。
func TestLandingPathIsASiblingOfTheFinalPath(t *testing.T) {
	m, repo, _ := newFixtureMirror(t)
	final := m.PathFor(repo)
	tmp := m.landingPathFor(final)

	if filepath.Dir(tmp) != filepath.Dir(final) {
		t.Errorf("the landing path must be in the same directory as the final one:\n  %s\n  %s",
			tmp, final)
	}
	if !strings.HasPrefix(filepath.Base(tmp), filepath.Base(final)+CloneLandingInfix) {
		t.Errorf("the landing name must be the final name plus the infix: %s", filepath.Base(tmp))
	}
	if !isLandingDirName(filepath.Base(tmp)) {
		t.Errorf("the landing name must be recognised as ours by the maintain layer: %s", filepath.Base(tmp))
	}
}
