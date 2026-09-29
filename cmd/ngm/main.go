package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/idcu/ngm/internal/errs"
)

func main() {
	os.Exit(runMain())
}

// runMain 是 main 函数的纯函数版本（无 os.Exit），便于测试在子进程外执行。
func runMain() int {
	return runWithRecovery(func() int {
		return dispatch(os.Args[1:], os.Stdout, os.Stderr)
	})
}

// runErr 是子命令调用的统一错误装饰：把 Go error 转 NgmError，再写到 stderr。
//
// 它打印 errs.FormatHuman(err) 并返回其退出码。子命令实现可以用
//
//	return runErr(ctx, stdout, stderr, err)
//
// 而不必关心退出码与错误格式。
func runErr(ctx context.Context, stdout, stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintln(stderr, errs.FormatHuman(err))
	return errs.ExitCode(err)
}
