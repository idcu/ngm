package git

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV11GitCommandWaitDelayIsBounded 钉住 v0.11 修掉的一个缺陷：
// ctx 到期只杀**直接子进程**，孙进程继承 stdout 管道时 `cmd.Wait()`
// 会越过期限一直等 `io.Copy`。
//
// 症状是实测的：`go test ./...` 全量跑时，一条 git 相关的验收测试
// 被拖到 **15 分钟超时**（单独跑只要 65s）——即"上限生效与否"取决于机器负载。
// 与 `internal/adapter` 的同名测试是同一个形状，但这条守的是**git 路径**，
// 而 git 是 ngm 安全关键路径上的每一次 fetch / ls-remote / digest 重放。
//
// 构造方式：让 git 跑一个 alias，它派生一个后台进程（持有 stdout 管道）后退出。
// 修复后应在 ctx(2s) + WaitDelay(2s) 量级返回；修复前要等满孙进程寿命（40s）。
func TestV11GitCommandWaitDelayIsBounded(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available; cannot construct the grandchild case")
	}
	testutils.MustHaveGit(t)

	const probeTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	start := time.Now()
	// `!` 前缀让 alias 走 shell；后台进程继承 stdout 后父 shell 立刻退出。
	_, err := Run(ctx, Options{}, "-c", "alias.grandchild=!sleep 40 &", "grandchild")
	elapsed := time.Since(start)

	// 关键断言：返回**有界**。孙进程活 40s——若Wait() 越过 ctx 期限，
	// 这里会读到 ~40s；修复后是 ctx(2s) + WaitDelay(2s) 的量级。
	//
	// 阈值取 20s：远小于 40s（抓得住回归），又远大于 4s（不在慢机器上假红）。
	if elapsed > 20*time.Second {
		t.Fatalf("Run 未被 WaitDelay 兜住：耗时 %v，远超 ctx 的 %v —— "+
			"孙进程仍持有 stdout 管道，cmd.Wait() 越过了期限",
			elapsed.Round(time.Millisecond), probeTimeout)
	}
	t.Logf("bounded at %v (ctx %v, WaitDelay %v), err=%v",
		elapsed.Round(time.Millisecond), probeTimeout, waitDelayAfterCancel, err)
}

// TestV11GitWaitDelayIsConfigured 是一条**更窄**的断言：只钉住
// "唯一的 git 进程构造点确实设了 WaitDelay"。
//
// 为什么要两条：上面那条在某些环境可能 skip（需要 sh），
// 而这条只需要读字段——**任何环境都能守住"这个值没被删掉"**。
// 与 v0.6 那两条"互相独立的网"是同一个思路：一条能误报、另一条能漏报时，
// 两条一起才不留缝。
func TestV11GitWaitDelayIsConfigured(t *testing.T) {
	if waitDelayAfterCancel <= 0 {
		t.Fatalf("waitDelayAfterCancel = %v, want a positive grace period", waitDelayAfterCancel)
	}
	cmd, cerr := newGitCommand(context.Background(), Options{}, "", "--version")
	if cerr != nil {
		t.Fatalf("newGitCommand: %v", cerr)
	}
	if cmd.WaitDelay != waitDelayAfterCancel {
		t.Errorf("唯一的 git 构造点 WaitDelay = %v, want %v",
			cmd.WaitDelay, waitDelayAfterCancel)
	}
}

// TestV11GitStillWorksNormally 是**对照**：证明修���没有把正常路径弄坏。
// 一个"永远返回错误"的实现也能让上面两条变绿。
func TestV11GitStillWorksNormally(t *testing.T) {
	res, err := Run(context.Background(), Options{}, "--version")
	if err != nil {
		t.Fatalf("git --version failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
}
