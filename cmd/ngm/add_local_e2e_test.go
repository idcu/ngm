package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddLocalSourceE2E 覆盖 `ngm add local:<name>` → `ngm install` → `ngm verify`：
// 本地来源从声明到落地的完整用户路径（ADR-052）。
func TestAddLocalSourceE2E(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports=1;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// dry-run 不落盘
	code, out := runCaptureCode(t, "add", "local:kernel", "--ref-type=local-dir",
		"--source-path=./kernel", "--dir="+dir, "--dry-run")
	if code != 0 || !strings.Contains(out, `"name": "local:kernel"`) {
		t.Fatalf("add local-dir dry-run failed: exit=%d\n%s", code, out)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "ngm.json"))
	if rerr != nil {
		t.Fatalf("read manifest after dry-run: %v", rerr)
	}
	if strings.Contains(string(data), "local:kernel") {
		t.Fatal("dry-run must not write ngm.json")
	}

	// 真实写入
	code, out = runCaptureCode(t, "add", "local:kernel", "--ref-type=local-dir",
		"--source-path=./kernel", "--dir="+dir)
	if code != 0 {
		t.Fatalf("add local-dir failed: exit=%d\n%s", code, out)
	}
	var pf struct {
		SchemaVersion int `json:"schemaVersion"`
		Dependencies  []struct {
			Name       string `json:"name"`
			Ref        string `json:"ref"`
			RefType    string `json:"refType"`
			SourcePath string `json:"sourcePath"`
		} `json:"dependencies"`
	}
	data, _ = os.ReadFile(filepath.Join(dir, "ngm.json"))
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatal(err)
	}
	if pf.SchemaVersion != 3 || len(pf.Dependencies) != 1 {
		t.Fatalf("unexpected manifest: %s", data)
	}
	dep := pf.Dependencies[0]
	if dep.Name != "local:kernel" || dep.Ref != "auto" || dep.RefType != "local-dir" || dep.SourcePath != "./kernel" {
		t.Fatalf("local dependency declaration wrong: %#v", dep)
	}

	// install + verify 闭环
	code, out = runCaptureCode(t, "install", "--dir="+dir)
	if code != 0 {
		t.Fatalf("install failed: exit=%d\n%s", code, out)
	}
	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 0 {
		t.Fatalf("verify failed: exit=%d\n%s", code, out)
	}

	// local-git：ref 必填，缺 ref 必须红
	code, out = runCaptureCode(t, "add", "local:payroll", "--ref-type=local-git",
		"--source-path=./kernel", "--local-repo=./kernel", "--dir="+dir)
	if code != 3 || !strings.Contains(out, "ref") {
		t.Fatalf("local-git without ref must fail: exit=%d\n%s", code, out)
	}
	code, out = runCaptureCode(t, "add", "local:payroll@v1.0.0", "--ref-type=local-git",
		"--source-path=./kernel", "--local-repo=./kernel", "--dir="+dir)
	if code != 0 {
		t.Fatalf("local-git with ref failed: exit=%d\n%s", code, out)
	}
}
