package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/core/errs"
)

// writeJSONDocument 把 v 写成一份确定的 JSON 文档，成功返回 okCode。
//
// 这是 `cmd/ngm` 里**唯一**的 stdout JSON 写出点。此前有四份实现
// （`writeJSON` / `writeModulesJSON` / `writeIntegrationsJSON` / `gc` 内联），
// 三种错误文案，两种转义策略——差别没有一个说得清为什么。
//
// 格式：2 空格缩进、LF、末尾一个换行、**含 HTML 转义**（Go 默认）。
// 契约层级见 ADR-045：**解码语义**是契约，字节不是；唯一例外是 `sbom` / `attest`
// 的产物（字节本身要被人比对、签名），它们在自己的包里用 `SetEscapeHTML(false)`。
//
// 失败时退 6：结论已经得出，但送不出去（ADR-026）。
func writeJSONDocument(stdout, stderr io.Writer, v any, okCode int) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "write report: %v\n", err)
		return errs.CodeInternal.ExitCode()
	}
	if _, werr := stdout.Write(append(data, '\n')); werr != nil {
		fmt.Fprintf(stderr, "write report: %v\n", werr)
		return errs.CodeInternal.ExitCode()
	}
	return okCode
}
