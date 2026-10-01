package git

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// RunAllowFailure 执行 `git <args...>`，把非零退出视为**正常结果**而非错误。
//
// 与 Run 的区别：Run 在非零退出时返回 *errs.NgmError；本函数返回 Result，
// 由调用方检查 ExitCode。适用于"探测"类调用（如 `cat-file -e` 判断对象是否存在）。
//
// 仍然会把"git 不存在"这类启动失败当作错误返回。
func RunAllowFailure(ctx context.Context, opts Options, args ...string) (*Result, error) {
	// 与 Run 共用同一个构造点（门禁 + 计数）——这里此前**没有** `run:git` 门禁，
	// 于是 `deny run:git` 的配置在探测类调用上完全失效（v0.5 实测）。
	cmd, cerr := newGitCommand(ctx, opts, opts.Dir, args...)
	if cerr != nil {
		// 与"启动失败"同一形状：调用方只看 err，不该去看 res 的退出码。
		return &Result{ExitCode: -1}, cerr
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
	}
	if err == nil {
		return res, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil // 非零退出是调用方要观察的结果
	}
	// 启动类失败（git 不存在等）
	return res, errs.Wrap(errs.CodeGitFetch, "failed to run git", NotInstalledHint, err)
}

// StderrText 返回脱敏后的 stderr 文本（供诊断消息使用）。
func StderrText(res *Result, secrets []string) string {
	if res == nil {
		return ""
	}
	return redact(strings.TrimSpace(string(res.Stderr)), secrets)
}
