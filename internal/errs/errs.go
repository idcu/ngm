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
	"strings"
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

	// Label 覆盖**显示名**（默认取 Code.String()）。
	//
	// 为什么需要它：**一个数字只能有一个名字，而同一个数字在不同命令里
	// 可能指的是不同的事**。退出码 1 至少有四种来源——verify 的引用漂移、
	// audit 的超阈值漏洞、audit 钩子否决、以及**引擎运行了但失败**。
	// 前三种里 `RefDrift` 勉强贴切，第四种完全不贴切：跑 `ngm typecheck` 的人
	// 看到 `RefDrift:` 会去找"漂移"，而实际发生的是引擎退出非零（v0.22 实测）。
	//
	// Label 只改**给人看的那个词**：Code、退出码、`--json` 一字不变。
	// 机器读到的东西仍然只有数字——这正是 v0.16 钉下的契约。
	Label string
}

// label 返回要显示的名字：优先用显式 Label，否则用码的默认名。
func (e *NgmError) label() string {
	if e.Label != "" {
		return e.Label
	}
	return e.Code.String()
}

// Labeled 返回**同一份错误的副本**，只把显示名换掉。
//
// 它存在的理由见 NgmError.Label 的注释：数值是契约，名字是给人读的。
func (e *NgmError) Labeled(label string) *NgmError {
	cp := *e
	cp.Label = label
	return &cp
}

// Error 实现 error 接口。
func (e *NgmError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.label(), e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.label(), e.Message)
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
//
// **hint 会沿着包装链找**（v0.28）：自身没给 hint 时，取 `Cause` 链上**第一条**
// 非空 hint。修的是一个实测缺陷——内层带着可行动的建议、外层 `Wrap` 时给了空
// hint，用户就再也看不到那句话了（实测输出只剩 `cause:` 一行）。
//
// 为什么在**渲染**处修而不是在 `Wrap` 处继承：一次修好全部 300 个构造点里
// 那 129 个空 hint 的站点，且**不改动错误本身的数据**（`--json` 与
// `errors.As` 的消费者看不到任何变化）。hint 是给人读的，那就该在给人读的地方接上。
func FormatHuman(err error) string {
	if err == nil {
		return ""
	}
	var ne *NgmError
	if !errors.As(err, &ne) {
		return "error: " + err.Error()
	}
	out := ne.label() + ": " + ne.Message
	if ne.Cause != nil {
		out += "\n  cause: " + causeLine(ne)
	}
	if hint := inheritedHint(ne); hint != "" {
		out += "\n  hint:  " + hint
	}
	return out
}

// causeLine 渲染 cause 行：内层是 NgmError、且**代码与顶层相同**时只写它的 message。
//
// 为什么（v0.48）：`validateDepPath` 开始给建议之后，它返回的是 errs 错误，
// 于是 cause 行变成 `cause: ConfigInvalid: path "/etc" must be relative …`——
// 同一个代码名在**同一条错误**上重复了一次，读起来像两个错误。
//
// 代码**不同**时仍保留前缀：那说明这一段是由另一类失败引起的
// （例如底层是个 `GitFetch`），那句话不该被吞掉。
func causeLine(ne *NgmError) string {
	var inner *NgmError
	if errors.As(ne.Cause, &inner) && inner.Code == ne.Code {
		return inner.Message
	}
	return ne.Cause.Error()
}

// Hint 返回这条错误的建议（沿 `Cause` 链找第一条非空的）；没有则返回空串。
//
// 与 FormatHuman 用的是**同一个** inheritedHint：任何要给出"下一步"的地方
// 都该看到同一句话。否则同一个失败会在错误文本里有建议、在报告里没有——
// v0.35 实测到的正是这种分裂：`res.Err = merr.Error()` 只留下 message，
// 而**最贴近成因的那一层**（权限层、mirror 层）写下的建议就此消失。
func Hint(err error) string {
	if err == nil {
		return ""
	}
	var ne *NgmError
	if !errors.As(err, &ne) {
		return ""
	}
	return inheritedHint(ne)
}

// inheritedHint 取这条错误自己的 hint；没有就沿 `Cause` 链找第一条非空的。
//
// 深度优先、只走**第一条**：多条建议堆在一起比没有建议更难读。
// 环（自己包自己）不会发生，但保险起见限一个深度。
func inheritedHint(ne *NgmError) string {
	for depth, cur := 0, ne; cur != nil && depth < 16; depth++ {
		if strings.TrimSpace(cur.Hint) != "" {
			return cur.Hint
		}
		var next *NgmError
		if cur.Cause != nil && errors.As(cur.Cause, &next) {
			cur = next
			continue
		}
		return ""
	}
	return ""
}
