package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/localfs"
)

// TestLocalDirInstallVerifyE2E 是 ADR-052 的端到端判据：
// local-dir 声明 → install 落地 vendor + 写 lock(dirDigest) → verify 通过
// → 改动源目录 → verify 转 Critical。
//
// 它把 schema → 解析 → vendor → verify 四层串起来，是"不是纸上设计"的证明。
func TestLocalDirInstallVerifyE2E(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "localmod")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports = 'v1';\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 覆盖 ngm.json 为 local-dir 声明（ref: auto 让 install 计算摘要）
	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {
      "name": "local:localmod",
      "ref": "auto",
      "refType": "local-dir",
      "sourcePath": "./localmod"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "install", "--dir="+dir)
	if code != 0 {
		t.Fatalf("install local-dir failed: exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "local-dir") {
		t.Fatalf("install output should mention local-dir: %s", out)
	}

	// lock 必须有 dirDigest
	lockData, err := os.ReadFile(filepath.Join(dir, "ngm.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lf struct {
		LockfileVersion string `json:"lockfileVersion"`
		Dependencies    []struct {
			Name       string `json:"name"`
			RefType    string `json:"refType"`
			DirDigest  string `json:"dirDigest"`
			SourcePath string `json:"sourcePath"`
			VendorPath string `json:"vendorPath"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(lockData, &lf); err != nil {
		t.Fatal(err)
	}
	if lf.LockfileVersion != "3.0.0" || len(lf.Dependencies) != 1 {
		t.Fatalf("unexpected lock: %s", lockData)
	}
	dep := lf.Dependencies[0]
	if dep.DirDigest == "" || dep.RefType != "local-dir" || dep.SourcePath != "./localmod" {
		t.Fatalf("lock entry missing local fields: %#v", dep)
	}
	if dep.VendorPath != "local/localmod" {
		t.Fatalf("vendor path namespace wrong: %q", dep.VendorPath)
	}

	// vendor 落地字节正确
	vendorFile := filepath.Join(dir, "ngm.vendor", "local", "localmod", "index.js")
	got, err := os.ReadFile(vendorFile)
	if err != nil || !strings.Contains(string(got), "v1") {
		t.Fatalf("vendor content wrong: %q (%v)", got, err)
	}

	// verify 通过
	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 0 {
		t.Fatalf("verify clean local-dir failed: exit=%d\n%s", code, out)
	}

	// 改源目录 → verify 必须转红（exit 2 = DriftCritical）
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports = 'v2';\n"), 0644); err != nil {
		t.Fatal(err)
	}
	newDigest, err := localfs.BuildDirDigest(src)
	if err != nil || newDigest == dep.DirDigest {
		t.Fatalf("digest should change: %q -> %q (%v)", dep.DirDigest, newDigest, err)
	}
	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 2 {
		t.Fatalf("verify after source change must exit 2 (critical), got %d:\n%s", code, out)
	}
}
