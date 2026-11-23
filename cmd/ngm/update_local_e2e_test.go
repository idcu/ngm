package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/localfs"
)

// TestUpdateRefreshesLocalDirDigest 验证 local-dir 的 update 路径：
// 内容变更后 `ngm update` 重算 dirDigest 并刷新 lock（ref: auto 语义），
// 因此不需要额外的 --refresh-digest flag。
func TestUpdateRefreshesLocalDirDigest(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("v1\n"), 0644); err != nil {
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
	before, _ := os.ReadFile(filepath.Join(dir, "ngm.lock"))
	if !strings.Contains(string(before), "dirDigest") {
		t.Fatalf("lock missing dirDigest: %s", before)
	}

	// 改内容 → update 应刷新摘要
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, out = runCaptureCode(t, "update", "--all", "--dir="+dir)
	if code != 0 {
		t.Fatalf("update: exit=%d\n%s", code, out)
	}
	after, err := os.ReadFile(filepath.Join(dir, "ngm.lock"))
	if err != nil {
		t.Fatalf("read updated lock: %v", err)
	}
	// 字节不等可能只是 resolvedAt 时间戳变了：必须钉住 dirDigest 本身。
	var lf struct {
		Dependencies []struct {
			Name      string `json:"name"`
			DirDigest string `json:"dirDigest"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(after, &lf); err != nil {
		t.Fatalf("invalid lock: %v\n%s", err, after)
	}
	if len(lf.Dependencies) != 1 || lf.Dependencies[0].DirDigest == "" {
		t.Fatalf("lock has no local dirDigest: %s", after)
	}
	want, err := localfs.BuildDirDigest(src)
	if err != nil {
		t.Fatalf("recompute dirDigest: %v", err)
	}
	if lf.Dependencies[0].DirDigest != want {
		t.Fatalf("dirDigest=%s, want recomputed %s (byte-level lock change is not enough)",
			lf.Dependencies[0].DirDigest, want)
	}
	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 0 {
		t.Fatalf("verify after update: exit=%d\n%s", code, out)
	}
}
