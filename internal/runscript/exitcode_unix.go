//go:build !windows

package runscript

import (
	"os"
	"syscall"
)

// exitCodeOf 把进程状态转成退出码：被信号终止 → 128 + signal。
func exitCodeOf(ps *os.ProcessState) int {
	if ps == nil {
		return 0
	}
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ps.ExitCode()
}
