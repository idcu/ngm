package main

import (
	"context"
	"encoding/json"
	"errors"
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

// jsonFlagKey 是"这个命令被要求输出 `--json`"这件事在 ctx 里的键。
//
// 为什么放在 ctx 里（v0.51）：错误渲染只有**一个**入口（runErr），
// 而"用户要的是机器可读输出"这件事只有**命令内部**才知道（它刚解析完自己的 flags）。
// ctx 本来就从命令流到 runErr，于是它是最短的那条路——
// 不必给 300 个 `runErr(ctx, …)` 调用点各加一个参数。
type jsonFlagKey struct{}

// withJSON 标记"这条 ctx 上的命令处在 --json 模式"。
func withJSON(ctx context.Context) context.Context {
	return context.WithValue(ctx, jsonFlagKey{}, true)
}

// markJSONIfRequested 是给命令在 `fs.Parse` 之后调的一次性开关。
func markJSONIfRequested(ctx context.Context, requested bool) context.Context {
	if requested {
		return withJSON(ctx)
	}
	return ctx
}

func wantsJSON(ctx context.Context) bool {
	v, _ := ctx.Value(jsonFlagKey{}).(bool)
	return v
}

// jsonErrorEnvelope 是 `--json` 模式下失败时写到 **stdout** 的那份文档（v0.51）。
//
// 它**不是报告**——v0.16 的规则④说的是"我没有报告可给时，不能用半份 JSON 表达"。
// 一份**完整**的错误信封不违反那条规矩：它表达的是"我失败了，原因与下一步在这里"。
//
// 在此之前，脚本在失败路径上只能拿到"退出码 + 空的 stdout + 一段人读文本"：
// 知道出了事，却读不出是什么事（v0.50 实测）。
//
// 字段与人读那侧**同源**（`errs.FormatHuman` 用的同一个 hint），
// 于是两个通道说的仍是同一句话——这是 v0.50 立的那条纪律，在这里继续成立。
type jsonErrorEnvelope struct {
	Version int           `json:"version"`
	Error   jsonErrorBody `json:"error"`
}

type jsonErrorBody struct {
	Code     string `json:"code"`
	ExitCode int    `json:"exitCode"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

// runErr 是子命令调用的统一错误装饰：把 Go error 转 NgmError，再写到 stderr。
//
// 它打印 errs.FormatHuman(err) 并返回其退出码。子命令实现可以用
//
//	return runErr(ctx, stdout, stderr, err)
//
// 而不必关心退出码与错误格式。
//
// 在 `--json` 模式下（命令解析出该 flag 后把 ctx 标过），它**额外**往 stdout
// 写一份完整的错误信封；人读文本仍然照旧写 stderr——两个通道各取所需。
func runErr(ctx context.Context, stdout, stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	code := errs.ExitCode(err)
	if wantsJSON(ctx) {
		if werr := writeJSONError(stdout, err, code); werr != nil {
			fmt.Fprintf(stderr, "write json error: %v\n", werr)
		}
	}
	fmt.Fprintln(stderr, errs.FormatHuman(err))
	return code
}

// writeJSONError 把 err 写成一份错误信封。
func writeJSONError(w io.Writer, err error, code int) error {
	var ne *errs.NgmError
	body := jsonErrorBody{
		Code:     "Error",
		ExitCode: code,
		Message:  err.Error(),
		Hint:     errs.Hint(err),
	}
	if errors.As(err, &ne) {
		body.Code = ne.DisplayName() // 与人读那侧同一个名字（`Labeled` 覆盖也算）
		body.Message = ne.Message
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // 与人读那侧一致：路径里的 `<` 不该变成 `\u003c`
	return enc.Encode(jsonErrorEnvelope{Version: 1, Error: body})
}
