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

// TestSpawn_OnlyOneFileCountsSpawns 机械地守住"**计账也只有一处**"。
//
// 它与 `TestSpawn_OnlyOneFileBuildsProcesses`（守门禁）是一对：那条保证每个 git 进程
// 都经过唯一的构造点，这条保证"记一笔账"也只在那个构造点里发生。
//
// 为什么需要（v0.6 B 组开工第一天的事）：v0.5 的 C 组把 `CatFileBatch` 从"自己 exec"
// 改成走 `newGitCommand`，却漏删了它自己那句 `noteSpawn()`——于是每个
// `cat-file --batch` 被**记两次账**：commit 型实测 3.00 次/依赖、tag 型 4.00 次/依赖，
// 而真实值是 **2 与 3**。这不是用户可见的缺陷，但它让**唯一的读数**系统性偏高，
// 而那是本版用来替代秒数的门禁指标——**仪器说谎比没有仪器更糟**。
//
// 当时为什么没人发现：门禁断言测的是"该不该起"（拒绝了几个），计数偏高它看不出来；
// 而唯一的读数只被 `t.Logf` 打印、从不作断言（"印出来不等于被检查"，
// 见 v0.5 复盘 §5.2）。现在读数有门禁了（`cmd/ngm` 的 `TestV06VerifySpawnBudget`），
// 这条检查则负责让"第二个计账点"无法悄悄出现。
//
// 空跑保护：`spawn.go` 里必须仍然存在 `spawnCount.Add(1)`——唯一的自增点。
// 若计数改了实现或搬了家，这条检查会因为"扫不到东西"而静默通过，而那正是它该报警的时候。
func TestSpawn_OnlyOneFileCountsSpawns(t *testing.T) {
	const allowed = "spawn.go"
	const increment = "spawnCount.Add(1)"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sawIncrement := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		src := string(data)
		if name == allowed {
			sawIncrement = sawIncrement || strings.Contains(src, increment)
			continue
		}
		if strings.Contains(src, "spawnCount") {
			t.Errorf("%s touches the spawn counter directly; every count must happen in %s "+
				"(otherwise a path can be counted twice, or not at all)", name, allowed)
		}
		if strings.Contains(src, "noteSpawn()") {
			// 这条扫描是**文本级**的（与上面守门禁那条同一手法），因此注释里写
			// 带括号的 noteSpawn() 也会命中。那是有意的：gofmt 保证调用只能写成这个
			// 形状（CI 强制 `gofmt -l` 为空），所以"文本命中"与"真的调用"在格式化过的
			// 代码里是同一件事。若这里报的是**注释**，把注释里的括号去掉即可。
			t.Errorf("%s counts a spawn outside %s — that is exactly how `cat-file --batch` "+
				"got counted twice in v0.5 (see catfile.go).\n"+
				"If this hit is only a *comment*, drop the parentheses: this check is textual "+
				"on purpose (gofmt makes a real call impossible to write any other way).",
				name, allowed)
		}
	}
	if !sawIncrement {
		t.Errorf("no `%s` found in %s — this check would pass vacuously; "+
			"if the counter moved, update the check", increment, allowed)
	}
}
