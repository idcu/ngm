//go:build windows

package runscript

import "os"

// exitCodeOf 在 Windows 上直接透传退出码。
//
// Windows 没有 POSIX 的信号终止语义：控制台 Ctrl+C 由进程自己映射成退出码
// （常见 0xC000013A，cmd 会折算成它自己的值）。因此这里不做 128+sig 换算，
// 子进程给什么就是什么。
func exitCodeOf(ps *os.ProcessState) int {
	if ps == nil {
		return 0
	}
	return ps.ExitCode()
}
