package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV05RefResolutionSpawns 用**子进程计数**钉住 ref 解析的成本（ADR-015）。
//
// 为什么不用时间：同一台机器上的在线 verify 在 2.2s ~ 3.1s 之间摆动（v0.5 复测），
// 掐表测的是"噪声 + 信号"。次数只测信号，而且可以被断言。
//
// 两条断言互为对照，缺一不可：
//
//	commit 型 → **0** 次（它不需要任何远端信息）
//	tag 型    → **至少 1** 次（它必须问远端）
//
// 只有前者会通过"把计数器写坏"的实现；只有后者会通过"什么都没变"的实现。
//
// 背景（v0.5 实测）：`RemoteRefResolver` 此前**无条件**先取一次 mirror 的
// remote.origin.url（`git config --get`，一个子进程），而 commit 分支根本不用那个地址——
// 于是每个 commit 型依赖白起一个进程。
func TestV05RefResolutionSpawns(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const v = 1\n")
	r.Commit("feat: dep")
	commit := r.Head()
	r.Tag("v1.0.0", false)

	proj := newProject(t)
	seedMirror(t, "github:v05/spawn", r.Dir)

	// 两种 refType 各声明一个依赖（同一个上游仓库，因此 mirror 也只有一个）。
	if code, out := runCaptureCode(t, "add", "github:v05/spawn@"+commit,
		"--ref-type=commit", "--dir="+proj); code != 0 {
		t.Fatalf("add commit: %s", out)
	}
	if code, out := runCaptureCode(t, "add", "github:v05/spawn@v1.0.0",
		"--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add tag: %s", out)
	}

	env, err := newProjectEnv(proj)
	if err != nil {
		t.Fatal(err)
	}
	resolveRemote := env.RemoteRefResolver()
	canon := resolve.MustNormalize("github:v05/spawn")

	t.Run("a commit ref costs zero git spawns", func(t *testing.T) {
		git.ResetSpawnCount()
		got, rerr := resolveRemote(context.Background(), canon, commit, resolve.RefTypeCommit)
		if rerr != nil {
			t.Fatalf("resolving a commit ref must not depend on any mirror: %v", rerr)
		}
		if got != commit {
			t.Errorf("resolved %q, want %q", got, commit)
		}
		if n := git.SpawnCount(); n != 0 {
			t.Errorf("a commit ref needs no git at all; spawned %d", n)
		}
	})

	t.Run("a tag ref does consult git", func(t *testing.T) {
		git.ResetSpawnCount()
		got, rerr := resolveRemote(context.Background(), canon, "v1.0.0", resolve.RefTypeTag)
		if rerr != nil {
			t.Fatalf("resolving a tag ref: %v", rerr)
		}
		if got != commit {
			t.Errorf("resolved %q, want %q", got, commit)
		}
		// **恰好一次**：`ls-remote` 那次。取 mirror 的远端地址（曾经是第二次）
		// 现在直接读 config 文件，见 ADR-015 / A4。
		if n := git.SpawnCount(); n != 1 {
			t.Errorf("a tag ref needs exactly one spawn (the ls-remote); got %d", n)
		}
	})
}

// TestV05VerifySpawnInventory 把一次**完整在线 verify** 的子进程次数打印出来。
//
// 它是 A2 要的那个"可打印的数字"：改任何取数策略之前，先看这次 verify 到底起了几个 git。
// 刻意**不作为门禁**（数字随 fixture 形态变化），只作为证据——门禁在
// TestV05RefResolutionSpawns 里（那两条与 fixture 规模无关）。
func TestV05VerifySpawnInventory(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	const deps = 3
	for _, refType := range []string{"commit", "tag"} {
		proj := newProject(t)
		for i := 0; i < deps; i++ {
			slug := fmt.Sprintf("github:v05/%s%02d", refType, i)
			r := testutils.NewGitRepo(t)
			r.WriteFile("index.ts", fmt.Sprintf("export const v = %d\n", i))
			r.Commit("feat: dep")
			r.Tag("v1.0.0", false)
			seedMirror(t, slug, r.Dir)

			ref := "v1.0.0"
			if refType == "commit" {
				ref = r.Head()
			}
			if code, out := runCaptureCode(t, "add", slug+"@"+ref, "--ref-type="+refType, "--dir="+proj); code != 0 {
				t.Fatalf("add %s: %s", slug, out)
			}
		}
		// verify 要求锁已存在：install 会解析并落锁（它本身也起子进程，因此计数器要在
		// 它**之后**清零——要测的是 verify 的成本，不是 install 的）。
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		git.ResetSpawnCount()
		code, out := runCaptureCode(t, "verify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("verify(%s) exit=%d:\n%s", refType, code, out)
		}
		t.Logf("online verify, %d %s dependencies: %d git spawns (%.2f per dependency)",
			deps, refType, git.SpawnCount(), float64(git.SpawnCount())/deps)
	}
}
