package security

import (
	"runtime"
	"strings"
	"testing"
)

// TestV209DenoArgsIsSelfDefending 固定 2026-11-23 审计 P3：
// 逗号 fail-closed 此前只在 PlanSandbox 做，导出的 DenoArgs 直接构造
// Grants{Net:["a,b"]} 仍会产出 --allow-net=a,b（两条授权）。
func TestV209DenoArgsIsSelfDefending(t *testing.T) {
	args := Grants{Net: []string{"example.com,evil.example"}}.DenoArgs()
	if !strings.Contains(strings.Join(args, " "), "--deny-net") {
		t.Fatalf("a comma-containing net target must fail closed, got %v", args)
	}
	args = Grants{Env: []string{"FOO,BAR"}}.DenoArgs()
	if !strings.Contains(strings.Join(args, " "), "--deny-env") {
		t.Fatalf("a comma-containing env target must fail closed, got %v", args)
	}
}

// TestV209PathGrantUsesRunIDRule 固定 grant/identity 判定不对称：
// Windows 盘符相对写法 C:foo 在 runid.PathForm 里是路径形式，
// isPathGrant 此前把它当裸名字，导致授权配了却不生效。
func TestV209PathGrantUsesRunIDRule(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-relative paths are Windows-specific")
	}
	if !isPathGrant("C:foo") {
		t.Fatal("C:foo must be classified as a path grant, consistent with runid.PathForm")
	}
}
