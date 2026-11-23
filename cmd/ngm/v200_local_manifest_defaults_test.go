package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV200LocalDirWorksWithoutVendorSection 固定 P0-1：
// ReadProjectFile 返回原始清单，vendor 段省略时 pf.Vendor 为 nil。
// ae310a8 直接用 pf.Vendor.LinkMode 让 local-dir install/update 空指针 panic，
// 且仓库自带的 local-dir e2e 因 fixture 恰好也省略 vendor 段而一同崩掉。
// 本判据用最小清单（明确不含 vendor 段）锁住"默认值来自 auto，不来自原始清单"。
func TestV200LocalDirWorksWithoutVendorSection(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports='v1';\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {"name":"local:kernel","ref":"auto","refType":"local-dir","sourcePath":"./kernel"}
  ]
}`
	if strings.Contains(manifest, `"vendor"`) {
		t.Fatal("test fixture must omit the vendor section")
	}
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"install", "--dir=" + dir},
		{"install", "--dir=" + dir}, // from-lock 路径同样解引用 linkMode
		{"update", "--all", "--dir=" + dir},
		{"verify", "--dir=" + dir},
	} {
		code, out := runCaptureCode(t, args...)
		if code != 0 {
			t.Fatalf("%v: exit=%d\n%s", args, code, out)
		}
	}
}
