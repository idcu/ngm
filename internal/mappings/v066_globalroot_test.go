package mappings

import (
	"os"
	"path/filepath"
	"testing"
)

// v0.66：`vendor.mode: global` 下 `to` 是绝对路径（`ToJoin` 的原样保留），
// 而此前的校验硬编码 "必须以 ./ngm.vendor/ 开头" ⇒ ngm 自己生成的 mappings
// 会被 ngm 自己的校验判为 fatal（评估报告 P2#14）。
func TestV66ValidateAcceptsAbsoluteVendorRoot(t *testing.T) {
	proj := t.TempDir()
	globalRoot := t.TempDir()

	depDir := filepath.Join(globalRoot, "github.com", "x", "dep")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "index.js"), []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFile()
	f.Mappings = []Mapping{{
		From: "github:x/dep",
		// 与 ToJoin(globalRoot, rel) 的产物同一形态。
		To:   filepath.ToSlash(depDir),
		Main: "./index.js",
	}}

	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		VendorRoot: filepath.ToSlash(globalRoot),
		LockNames:  map[string]bool{"github:x/dep": true},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, fd := range findings {
		if fd.Fatal {
			t.Errorf("global 模式的绝对 `to` 被误判为 fatal: %s", fd.Message)
		}
	}
}

// 对照：`to` 落在 vendor 根**之外**时仍必须被拒绝（修复不得放松原来的边界）。
func TestV66ValidateStillRejectsPathsOutsideTheVendorRoot(t *testing.T) {
	proj := t.TempDir()
	globalRoot := t.TempDir()
	elsewhere := t.TempDir()

	f := NewFile()
	f.Mappings = []Mapping{{
		From: "github:x/dep",
		To:   filepath.ToSlash(filepath.Join(elsewhere, "dep")),
	}}

	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		VendorRoot: filepath.ToSlash(globalRoot),
		LockNames:  map[string]bool{"github:x/dep": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	fatal := false
	for _, fd := range findings {
		if fd.Fatal {
			fatal = true
		}
	}
	if !fatal {
		t.Errorf("根外的 `to` 必须仍是 fatal，findings=%v", findings)
	}
}
