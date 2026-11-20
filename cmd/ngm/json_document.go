package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/core/errs"
)

// writeJSONDocument 把 v 写成一份确定的 JSON 文档，成功返回 okCode。
//
// 这是 `cmd/ngm` 里**成功报告类** JSON 的唯一 stdout 写出点。此前有四份实现
// （`writeJSON` / `writeModulesJSON` / `writeIntegrationsJSON` / `gc` 内联），
// 三种错误文案，两种转义策略——差别没有一个说得清为什么。
//
// "唯一"有两个明确例外（v0.136 澄清，此前注释被读作"所有 JSON"，与事实不符）：
//   - **产物**：`sbom` / `attest`（及 `adopt --out` 的证明）的 stdout 字节本身是
//     交付物，按 ADR-045 的产物档**不转义**，在各自包里序列化；
//   - **错误信封**：`failEarly` / `runErr` 走 `writeJSONError`（不转义，装的是人话）。
//
// 格式：2 空格缩进、LF、末尾一个换行、**含 HTML 转义**（Go 默认）。
// 契约层级见 ADR-045：**解码语义**是契约，字节不是；唯一例外是上面那些产物。
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
