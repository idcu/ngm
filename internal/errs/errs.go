// Package errs 定义 ngm 的错误模型与全局退出码契约。
//
// 退出码与 architecture/observability.md 对齐：
//
//	0 成功（verify 的"仅预期更新"也归此）
//	1 策略失败（verify 非预期漂移；audit 超阈值漏洞；**引擎运行了但失败**）
//	2 完整性失败（verify digest 重放不匹配）
//	3 配置/策略/lock 错误
//	4 Git/网络失败（含 --offline 资源缺失）
//	5 引擎不可用
//
// 错误码常量在 modules/p0-core.md 中定义为 ErrorCode iota+1。
package errs

import (
	"errors"
	"fmt"
)

// Code 错误码。与退出码一一对应（除 0），便于在内部传递而不丢失语义。
type Code int

const (
	// CodeOK 表示成功（保留给需要返回 Code 的内部接口；退出码 0 不通过本类型表达）。
	CodeOK Code = 0

	// CodeRefDrift 1：策略失败。**它现在有三个来源**：
	//
	//   - verify 非预期漂移（tag 被移动 / 分支历史被改写）——名字的来源；
	//   - audit 超阈值漏洞；
	//   - **引擎运行了但失败**（`typecheck` / `typedecl` / `build` / `transform` / `css`
	//     的退出码 1 都走这里，见 internal/adapter/engine.go）。
	//
	// 第三种用途为 v0.22 实测所确认，而此前这段注释**一处未提**——注释与事实不符
	// 是"读数说谎"的一种：它不会让任何测试变红。
	//
	// 已知代价：String() 只有一个名字，于是那 5 个命令打印的错误前缀是
	// `RefDrift: …`，读起来像"引用漂移"。是否改名见 v0.22 复盘的候选（改动
	// 人类可读输出，需权衡；数值契约不受影响）。
	CodeRefDrift Code = 1
	// CodeDigestMismatch 2：完整性失败（verify digest 重放不匹配）。
	CodeDigestMismatch Code = 2
	// CodeConfigInvalid 3：配置/策略/lock 错误。
	CodeConfigInvalid Code = 3
	// CodeGitFetch 4：Git/网络失败（含 --offline 资源缺失）。
	CodeGitFetch Code = 4
	// CodeEngineNotFound 5：引擎不可用。
	CodeEngineNotFound Code = 5
)

// ExitCode 返回对应全局退出码。Code 与退出码一一对应。
func (c Code) ExitCode() int {
	return int(c)
}

// String 返回错误码的可读名，便于日志与 --json 输出。
func (c Code) String() string {
	switch c {
	case CodeRefDrift:
		return "RefDrift"
	case CodeDigestMismatch:
		return "DigestMismatch"
	case CodeConfigInvalid:
		return "ConfigInvalid"
	case CodeGitFetch:
		return "GitFetch"
	case CodeEngineNotFound:
		return "EngineNotFound"
	default:
		return "OK"
	}
}

// NgmError 是 ngm 内部统一错误类型，包含给用户的可操作提示。
//
// Hint 是面向终端用户的下一步建议；不是机器可解析字段。
// Cause 保留底层 error 便于 %w 包装与 errors.Is/As。
type NgmError struct {
	Code    Code
	Message string
	Cause   error
	Hint    string
}

// Error 实现 error 接口。
func (e *NgmError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 让 errors.Is/As 能穿透到 Cause。
func (e *NgmError) Unwrap() error { return e.Cause }

// New 构造一个无 Cause 的 NgmError。
func New(code Code, msg, hint string) *NgmError {
	return &NgmError{Code: code, Message: msg, Hint: hint}
}

// Wrap 构造带 Cause 的 NgmError；nil cause 退化为 New。
func Wrap(code Code, msg, hint string, cause error) *NgmError {
	if cause == nil {
		return New(code, msg, hint)
	}
	return &NgmError{Code: code, Message: msg, Cause: cause, Hint: hint}
}

// ExitCode 从 error 中取出退出码；非 NgmError 返回 1（通用失败）。
//
// 约定：调用方在 main 末尾用
//
//	if code := errs.ExitCode(err); code != 0 { os.Exit(code) }
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ne *NgmError
	if errors.As(err, &ne) {
		return ne.Code.ExitCode()
	}
	return 1
}

// FormatHuman 输出人类可读的错误文本，hint 单独成行，便于复制。
//
// 输出格式：
//
//	<Code>: <Message>
//	  cause: <cause>
//	  hint:  <hint>
//
// 顶层 CLI 在非 --json 模式下调用此函数。
func FormatHuman(err error) string {
	if err == nil {
		return ""
	}
	var ne *NgmError
	if !errors.As(err, &ne) {
		return "error: " + err.Error()
	}
	out := ne.Code.String() + ": " + ne.Message
	if ne.Cause != nil {
		out += "\n  cause: " + ne.Cause.Error()
	}
	if ne.Hint != "" {
		out += "\n  hint:  " + ne.Hint
	}
	return out
}
