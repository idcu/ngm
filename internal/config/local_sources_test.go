package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLocalSourceFieldsAreWired 为 Dependency.SourcePath / LocalRepo 提供 wiring：
// 从 ngm.json 解码 → 本地来源字段生效 → 逃逸路径被 Validate 拒绝。
//
// 这两个字段由 ADR-052 的 local-git / local-dir 来源引入；
// 没有这条断言，字段就只是 JSON 里的死声明（field_wiring_test.go 的纪律）。
func TestLocalSourceFieldsAreWired(t *testing.T) {
	const manifest = `{
  "schemaVersion": 3,
  "name": "github.com:demo/local-app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {
      "name": "local:kernel",
      "ref": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "refType": "local-dir",
      "sourcePath": "./packages/kernel"
    },
    {
      "name": "local:payroll",
      "ref": "v1.0.0",
      "refType": "local-git",
      "sourcePath": "./packages/packs/payroll",
      "localRepo": "."
    }
  ]
}`
	var project ProjectFile
	if err := json.Unmarshal([]byte(manifest), &project); err != nil {
		t.Fatal(err)
	}
	if len(project.Dependencies) != 2 {
		t.Fatalf("decoded %d dependencies, want 2", len(project.Dependencies))
	}
	d := &project.Dependencies[0]
	if d.SourcePath != "./packages/kernel" {
		t.Fatalf("SourcePath not wired: %q", d.SourcePath)
	}
	if d.RefType != RefTypeLocalDir {
		t.Fatalf("RefType not wired: %q", d.RefType)
	}
	if d.Validate() != nil {
		t.Fatalf("valid local-dir dependency rejected: %v", d.Validate())
	}
	g := &project.Dependencies[1]
	if g.LocalRepo != "." {
		t.Fatalf("LocalRepo not wired: %q", g.LocalRepo)
	}
	if g.Validate() != nil {
		t.Fatalf("valid local-git dependency rejected: %v", g.Validate())
	}

	// 逃逸路径必须被拒（spec §10 决策 1：拒绝跨项目根）
	bad := *d
	bad.SourcePath = "../escape"
	if err := bad.Validate(); err == nil || !strings.Contains(err.Error(), "sourcePath") {
		t.Fatalf("escaping sourcePath must be rejected with a named field: %v", err)
	}
}
