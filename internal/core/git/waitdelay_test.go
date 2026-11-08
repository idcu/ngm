package git

import (
	"context"
	"testing"
)

// TestV11GitWaitDelayIsConfigured 钉住 v0.11 修掉的一个缺陷：
// ctx 到期只杀**直接子进程**，孙进程继承 stdout 管道时 `cmd.Wait()`
// 会越过期限一直等 `io.Copy`。
//
// 判据从**墙钟 20s 上界**换成**结构断言**：git 子进程只在 newGitCommand 一处
// 构造（TestSpawn_OnlyOneFileBuildsProcesses 机械地守住"没有第二个地方"），
// 因此读它的 WaitDelay 就能确定宽限期还在。原始端到端用例（alias 派生后台进程、
// 孙进程活 40s）在并发负载下会假红，与 TestV106 同一种病；回归形态是"这一行
// 被删掉/被绕过"，读字段就能抓住，不需要再付 40s 的墙钟成本（见 tasks.md）。
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

// TestV11GitStillWorksNormally 是**对照**：证明修复没有把正常路径弄坏。
// 一个"永远返回错误"的实现也能让上面那条变绿。
func TestV11GitStillWorksNormally(t *testing.T) {
	res, err := Run(context.Background(), Options{}, "--version")
	if err != nil {
		t.Fatalf("git --version failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", res.ExitCode)
	}
}
