package adapter

import (
	"strings"
	"testing"
)

// TestEngineError_KeepsStdoutDiagnostics 固定一个**由真实 tsc 抓出来**的缺陷：
//
// 协议说"stdout 是产物、stderr 是诊断"，但那是约定，不是引擎的保证——
// tsc 把 `error TS2322: …` 写在 **stdout**。此前 runProcess 在引擎非零退出时
// 只保留 stderr，于是类型检查最该给用户的那条诊断被丢掉了：
// 用户只看到 "tsc failed (engine exit 1)"，不知道该改哪一行。
//
// 假引擎抓不到这个——它总是安静地按约定行事。
func TestEngineError_KeepsStdoutDiagnostics(t *testing.T) {
	ee := &EngineError{
		Code:    1,
		Message: "tsc failed",
		Stdout:  "src/index.ts(1,14): error TS2322: Type 'string' is not assignable to type 'number'.",
	}

	diags := ee.Diagnostics()
	if len(diags) != 1 || !strings.Contains(diags[0], "error TS2322") {
		t.Errorf("Diagnostics() must surface what the engine wrote to stdout: %v", diags)
	}
	if !strings.Contains(ee.Error(), "error TS2322") {
		t.Errorf("Error() must include the engine's own output, got: %s", ee.Error())
	}
}

// Diagnostics 的拼接顺序：stdout 在前、stderr 在后。
// 类型检查器的诊断通常在 stdout，而它们的噪声（deprecation 提示等）在 stderr——
// 把噪声排在上面会让人以为那才是结论。
func TestEngineError_DiagnosticsOrder(t *testing.T) {
	ee := &EngineError{
		Code:   1,
		Stdout: "the finding",
		Stderr: "the noise",
	}
	got := ee.Diagnostics()
	if len(got) != 2 || got[0] != "the finding" || got[1] != "the noise" {
		t.Errorf("Diagnostics() = %v, want [the finding, the noise]", got)
	}
}

// nil 接收者不应 panic：调用方常在 errors.As 之后直接取诊断。
func TestEngineError_NilDiagnostics(t *testing.T) {
	var ee *EngineError
	if got := ee.Diagnostics(); got != nil {
		t.Errorf("nil receiver should yield no diagnostics, got %v", got)
	}
}
