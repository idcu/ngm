package adapter

import (
	"bytes"
	"context"
	"runtime"
	"testing"
)

// TestV11RunProcessWaitDelayIsConfigured 钉住 v0.11 C 组实测发现的一个缺陷：
// ctx 到期只杀**直接子进程**，孙进程继承 stdout 管道时 `cmd.Wait()`
// 会越过期限一直等 io.Copy。
//
// 判据在这里是**结构性的**：引擎子进程只在 newProcessCommand 一处构造，读它的
// WaitDelay 就能确定"宽限期还在"。v0.11 的原始用例用"孙进程活 40s、20s 内必须
// 返回"作判据，并发负载下会假红（与 TestV106 同一种病，见 tasks.md 的 flaky 节）；
// 真正的行为由 exec.WaitDelay 的语义保证，ngm 这边的回归形态只有"这一行被删掉
// 或被绕过"，不需要再付一次 40s 的墙钟成本。
func TestV11RunProcessWaitDelayIsConfigured(t *testing.T) {
	if waitDelayAfterCancel <= 0 {
		t.Fatalf("waitDelayAfterCancel = %v, want a positive grace period", waitDelayAfterCancel)
	}
	cmd := newProcessCommand(context.Background(), "sh", "-c", "true")
	if cmd.WaitDelay != waitDelayAfterCancel {
		t.Errorf("唯一的引擎构造点 WaitDelay = %v, want %v", cmd.WaitDelay, waitDelayAfterCancel)
	}
}

// TestV11RunProcessStillSucceedsNormally 是上面那条的**对照**：
// 牙齿测试必须证明"该发生的成功仍然发生"，否则一个
// "永远返回错误"的实现也能让上一条变绿。
func TestV11RunProcessStillSucceedsNormally(t *testing.T) {
	if runtime.GOOS == "windows" {
		shell, args := "cmd", []string{"/c", "echo v11-ok"}
		res, err := runProcess(context.Background(), shell, args, "", nil, nil)
		if err != nil {
			t.Fatalf("正常命令不该失败: %v", err)
		}
		if !bytes.Contains(res.Stdout, []byte("v11-ok")) {
			t.Errorf("stdout = %q, want it to contain %q", res.Stdout, "v11-ok")
		}
		return
	}
	res, err := runProcess(context.Background(), "sh", []string{"-c", "echo v11-ok"}, "", nil, nil)
	if err != nil {
		t.Fatalf("正常命令不该失败: %v", err)
	}
	if !bytes.Contains(res.Stdout, []byte("v11-ok")) {
		t.Errorf("stdout = %q, want it to contain %q", res.Stdout, "v11-ok")
	}
}
