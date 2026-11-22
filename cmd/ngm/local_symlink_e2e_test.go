package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/idcu/ngm/internal/config"
)

// TestLocalDirSymlinkLanding 钉住 linkMode=symlink 的本地来源语义：
// vendor 落地是符号链接，改源码立即生效（无需 reinstall），verify 仍按源目录 dirDigest 判定。
func TestLocalDirSymlinkLanding(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// 开启 symlink 落地（用 config API 改写，避免手改 JSON 破损）
	cfgPath := filepath.Join(dir, "ngm.json")
	pf, err := config.ReadProjectFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pf.Vendor = &config.VendorConfig{Mode: "local", LinkMode: "symlink"}
	if err := config.WriteProjectFile(cfgPath, pf); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "add", "local:kernel", "--ref-type=local-dir",
		"--source-path=./kernel", "--dir="+dir)
	if code != 0 {
		t.Fatalf("add: exit=%d\n%s", code, out)
	}
	code, out = runCaptureCode(t, "install", "--dir="+dir)
	if code != 0 {
		t.Fatalf("install: exit=%d\n%s", code, out)
	}

	vendor := filepath.Join(dir, "ngm.vendor", "local", "kernel")
	st, err := os.Lstat(vendor)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("linkMode=symlink must land a symlink, got mode %v", st.Mode())
	}

	// 改源码立即反映到 vendor（软链接语义）
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(vendor, "index.js"))
	if err != nil || string(got) != "v2\n" {
		t.Fatalf("symlink must reflect source edits: %q (%v)", got, err)
	}

	// verify 必须对改动后的源报 Critical（dirDigest 变化）
	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 2 {
		t.Fatalf("verify after source change must exit 2, got %d:\n%s", code, out)
	}
}
