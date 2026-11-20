package main

import "testing"

// TestIndentSanitizesUntrustedScriptOutput 钉住 v0.136：沙箱脚本的输出是不可信输入，
// ANSI/控制字符若原样转发，可在终端或 CI 日志里伪造报告行（例如假的 "✓ self-check passed"）。
// verify_sandbox 与 audit_hook 共用 indent，因此一处修好两边都覆盖。
func TestIndentSanitizesUntrustedScriptOutput(t *testing.T) {
	got := indent("ok\n\x1b[32m✓ fake pass\x1b[0m\nraw\x07bell", "  ")
	want := "  ok\n" +
		"  \\x1b[32m✓ fake pass\\x1b[0m\n" +
		"  raw\\x07bell\n"
	if got != want {
		t.Errorf("indent:/n got %q\nwant %q", got, want)
	}
	// TAB 是合法的对齐字符，必须保留
	if got := indent("a\tb", "> "); got != "> a\tb\n" {
		t.Errorf("TAB must be preserved: %q", got)
	}
	// 空串与无换行的单行仍按原语义
	if got := indent("", "> "); got != "" {
		t.Errorf("empty: %q", got)
	}
	if got := indent("x", "> "); got != "> x\n" {
		t.Errorf("single line: %q", got)
	}
}
