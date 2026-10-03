package adapter

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestV11RunProcessWaitDelayIsBounded 钉住v0.11 C 组实测发现的一个缺陷：
// ctx 到期只杀**直接子进程**，孙进程继承 stdout 管道时 `cmd.Wait()`
// 会越过 ctx 期限一直等io.Copy。
//
// 复现方式不需要真的引擎：一个派生后台进程、立刻退出的父进程即可。
// Windows 上 npm 装的 CLI 恰好就是这种形状（`.cmd` / sh 包装再起一个进程），
// 所以这不是 contrived 的构造——`go test ./...` 全量跑时，
// 版本探测（声称上限 5s）实测卡了 9 分钟。
//
// 这条断言的形状是**比较量**：返回耗时必须远小于孙进程的存活时间。
// 写成"必须小于 5s"会在慢机器上假红；写成"必须小于 25s"又抓不住回归
// （修复前是 25.3s，卡在边界上）。取中间：孙进程活40s，
// 修复后应在WaitDelay(2s) + ctx(2s) 量级内返回，修复前要等满 40s。
func TestV11RunProcessWaitDelayIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		// 造一个"父进程派生后台子进程、然后自己退出"的脚本。
		// 用 sh（Git Bash 随 Windows 提供），参数形态与 POSIX 版一致。
		shell, args := "sh", []string{"-c", `sleep 40 & exit 0`}
		if _, err := os.Stat("/bin/sh"); err != nil {
			shell, args = "sh", []string{"-c", `sleep 40 & exit 0`}
		}
		runWaitDelayCase(t, shell, args)
		return
	}
	runWaitDelayCase(t, "sh", []string{"-c", `sleep 40 & exit 0`})
}

func runWaitDelayCase(t *testing.T, program string, args []string) {
	t.Helper()

	const probeTimeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	start := time.Now()
	res, err := runProcess(ctx, program, args, "", nil, nil)
	elapsed := time.Since(start)

	// 关键断言：返回**有界**。孙进程活 40s，若Wait() 越过 ctx 期限，
	// 这里会读到 ~40s；修复后是 ctx(2s) + WaitDelay(2s) 的量级。
	//
	// 阈值取 20s：它同时满足两件事——远小于 40s（抓得住回归），
	// 又远大于 4s（不会在慢机器上假红）。这是本项目一贯的取舍：
	// 断言要能判别，又不能因为机器噪声而失效。
	if elapsed > 20*time.Second {
		t.Fatalf("runProcess 未被 WaitDelay 兜住：耗时 %v，远超 ctx 的 %v —— "+
			"孙进程仍持有 stdout 管道，cmd.Wait() 越过了期限",
			elapsed.Round(time.Millisecond), probeTimeout)
	}

	// 超时路径必须**报错而不是假装成功**：ctx 已到期，
	// 静默返回一个"成功"会让上层以为引擎探测通过了。
	if err == nil && ctx.Err() == nil {
		t.Errorf("ctx 已到期却返回 err=nil（err=%v, res=%+v）："+
			"超时的外部命令不能被读成成功", err, res)
	}
	t.Logf("bounded at %v (ctx %v, WaitDelay %v), err=%v",
		elapsed.Round(time.Millisecond), probeTimeout, waitDelayAfterCancel, err)
}

// TestV11RunProcessStillSucceedsNormally 是上一条的**对照**：
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
