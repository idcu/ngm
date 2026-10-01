package git

import (
	"context"
	"os/exec"
	"sync/atomic"
)

// newGitCommand 是**唯一**构造 git 子进程的地方：门禁、计数与进程构造都在这里。
//
// 为什么必须唯一：安全模型的"施加点"表写着 `run:git` 的施加点是 git 包的所有
// spawn 出口。而在 v0.5 之前，包里其实有三个各自 exec 的地方——
// `Run` 有门禁，`RunAllowFailure`（`cat-file -e` / `merge-base --is-ancestor`）
// 与 `CatFileBatch`（digest 重放）**没有**：写下 `deny: ["run:git"]` 的用户，
// 在这两条路径上仍然会执行 git（v0.5 实测：`TestSpawn_GateOnEveryExit` 当场报出
// "deny run:git must refuse this path" 与 "spawned 1"）。
//
// 三个出口分散着写，就一定会漏——这与 `field_wiring_test.go` 要治的是同一种病：
// **规则写在第二个地方，就一定会有一处不一致**。因此这里收成一个函数，
// 并由 `TestSpawn_OnlyOneFileBuildsProcesses` 机械地守住"没有第二个地方"。
//
// 顺序：**先门禁、再计数**。被拒绝的调用没有启动任何进程，
// 把它算进计数会让那个数字与真实成本脱钩。
func newGitCommand(ctx context.Context, opts Options, dir string, args ...string) (*exec.Cmd, error) {
	if opts.Policy != nil {
		if err := opts.Policy.CheckRun("git"); err != nil {
			return nil, err
		}
	}
	noteSpawn()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = buildEnv(opts)
	return cmd, nil
}

// spawnCount 记录本进程启动 git 子进程的次数。
//
// 存在的理由（v0.5 A 组）：verify 的成本**几乎全在 git 子进程启动**上
// （v0.1 复盘 §3.3），但"一次 verify 到底起了几个 git"在此之前只能靠读代码数——
// 于是任何"少起一个子进程"的改动都只能靠掐表验证，而掐表受机器噪声影响：
// 同一台机器上的在线 verify 在 2.2s ~ 3.1s 之间摆动（v0.5 复测）。
//
// 这个计数器把那个数字变成**可断言**的：测试可以要求"解析一个 commit 型依赖起
// **0** 个 git"，而不是"看起来变快了"。掐表测的是噪声加信号，计数只测信号。
//
// 只在进程内累计（`go test` 与基准够用）；不对外暴露、不参与任何判定、
// 不影响任何输出——它是仪器，不是功能。
var spawnCount atomic.Int64

// SpawnCount 返回自上次 ResetSpawnCount 以来启动的 git 子进程数。
func SpawnCount() int64 { return spawnCount.Load() }

// ResetSpawnCount 把计数器清零（测试与基准用）。
//
// 刻意不叫 Reset：那个名字会被读成"重置仓库/状态"，而这里清的只是一个读数。
func ResetSpawnCount() { spawnCount.Store(0) }

// noteSpawn 在**真的启动**一个 git 进程之前调用。
//
// 位置很重要：要放在权限门禁与参数校验之后、`cmd.Run()`/`cmd.Start()` 之前。
// 被门禁拒掉的调用没有启动任何进程，把它算进去会让这个数字与"真实成本"脱钩。
func noteSpawn() { spawnCount.Add(1) }
