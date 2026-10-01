package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// TestSpawn_OnlyOneFileBuildsProcesses 机械地守住"门禁只有一处"。
//
// 它扫的是**源码文件本身**：`internal/git` 下的非测试文件里，只允许 `spawn.go` 出现
// `exec.Command`。这条不变量比"记得在每个出口加门禁"可靠——后者在 v0.5 之前已经失效过：
// 两个出口（`RunAllowFailure` / `CatFileBatch`）漏了 `run:git` 门禁，
// 而文档当时写着门禁覆盖"所有 git 子进程的出口"。
//
// 带**空跑保护**：若 `spawn.go` 里不再有 `exec.Command`（文件被改名、构造点被搬走），
// 这条检查会因为"扫不到任何东西"而静默通过——那正是它最该报警的时候。
func TestSpawn_OnlyOneFileBuildsProcesses(t *testing.T) {
	const allowed = "spawn.go"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sawAllowed := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		hasExec := strings.Contains(string(data), "exec.Command")
		if name == allowed {
			sawAllowed = sawAllowed || hasExec
			continue
		}
		if hasExec {
			t.Errorf("%s constructs a git process directly; every git spawn must go through "+
				"newGitCommand (%s) so that the run:git gate and the spawn counter cannot drift apart",
				name, allowed)
		}
	}
	if !sawAllowed {
		t.Errorf("no `exec.Command` found in %s — this check would pass vacuously; "+
			"if the construction point moved, update the check", allowed)
	}
}

// TestSpawn_GateOnEveryExit 固定一条性质：
//
//	**`deny: ["run:git"]` 之后，没有任何一条路径能启动 git。**
//
// 为什么值得单列一条：安全模型的"施加点"表里，`run:git` 那一行写的是
// `git.Run`（**所有** git 子进程的出口）。而包里实际上有三个 spawn 出口——
// v0.5 装上子进程计数器（`git.SpawnCount`）之后，这句话第一次可以被机械核对：
//
//	Run              `git <args>`（绝大多数调用）
//	RunAllowFailure  `cat-file -e` / `merge-base --is-ancestor`（探测类）
//	CatFileBatch     `cat-file --batch`（digest 重放）
//
// 计数器只回答"起了几个"，门禁要回答"该不该起"——两者都要。
// 本用例对三个出口逐个断言，并且**同时**断言计数器为 0：
// 只看错误码会漏掉"报错了但进程还是起来了"这种最糟的形态。
func TestSpawn_GateOnEveryExit(t *testing.T) {
	pol := policyFrom(t, nil, []string{"run:git"})
	ctx := context.Background()

	// 刻意用空目录：门禁必须在**启动之前**生效，与仓库内容无关。
	// 若某个出口先去看目录、再判权限，它会以"不是仓库"的样子失败，
	// 而用户会去查仓库，不会去看权限配置。
	dir := t.TempDir()

	assertDenied := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("deny run:git must refuse this path")
		}
		if got := errs.ExitCode(err); got != 3 {
			t.Errorf("exit=%d, want 3 (a configuration decision, not a runtime failure)", got)
		}
		if !strings.Contains(errs.FormatHuman(err), "run:git") {
			t.Errorf("the error must name the permission:\n%s", errs.FormatHuman(err))
		}
	}

	t.Run("Run", func(t *testing.T) {
		ResetSpawnCount()
		_, err := Run(ctx, Options{Policy: pol, Dir: dir}, "version")
		assertDenied(t, err)
		if n := SpawnCount(); n != 0 {
			t.Errorf("no git process may be started (spawned %d)", n)
		}
	})

	t.Run("RunAllowFailure", func(t *testing.T) {
		ResetSpawnCount()
		_, err := RunAllowFailure(ctx, Options{Policy: pol, Dir: dir}, "cat-file", "-e", "HEAD^{commit}")
		assertDenied(t, err)
		if n := SpawnCount(); n != 0 {
			t.Errorf("no git process may be started (spawned %d)", n)
		}
	})

	t.Run("CatFileBatch", func(t *testing.T) {
		ResetSpawnCount()
		// 非空 shas：空列表会在启动前提前返回，那测不到 spawn 出口本身。
		_, err := CatFileBatch(ctx, Options{Policy: pol}, dir, []string{"0123456789012345678901234567890123456789"})
		assertDenied(t, err)
		if n := SpawnCount(); n != 0 {
			t.Errorf("no git process may be started (spawned %d)", n)
		}
	})
}
