package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// brokenDenoDir 造一个"看起来能执行、但探针必失败"的 deno，并把放它到 PATH 最前面。
//
// 用它而不是清空 PATH：清空会让 git 也找不到，install 在落地之前就退 5，
// 那就测不到"物化完成后钩子失败 → 回滚"这段。
func brokenDenoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, "deno.cmd"), []byte("@exit /b 1\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, "deno"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// 判据（D2）：钩子阶段失败不得留下"新建的 vendor + 没有 lock"的半写状态。
//
// 夹具刻意把 vendor 与 lock 都删掉，让这次 install 走**全新物化**路径。
// 沙箱运行器探针失败（exit 5）与钩子自己失败（exit 2）走的是同一个回滚点。
func TestV124HookFailureRollsBackNewVendor(t *testing.T) {
	assertRollback := func(t *testing.T, code int, out, proj string, want int) {
		t.Helper()
		if code != want {
			t.Fatalf("install exit = %d, want %d:\n%s", code, want, out)
		}
		vendorDir := filepath.Join(proj, "ngm.vendor", "github.com", "v3", "hook")
		if _, err := os.Stat(vendorDir); !os.IsNotExist(err) {
			t.Errorf("the vendor entry created by this run must be rolled back, stat err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(proj, "ngm.lock")); !os.IsNotExist(err) {
			t.Errorf("no lock must be written when the hooks failed, stat err = %v", err)
		}
		if !strings.Contains(out, "github:v3/hook") {
			t.Errorf("the failure must name the dependency:\n%s", out)
		}
	}

	t.Run("sandbox runner is broken", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": "Deno.exit(0);\n",
		}, `{"postInstallPolicy": "allow"}`)
		if err := os.RemoveAll(filepath.Join(proj, "ngm.vendor")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(proj, "ngm.lock")); err != nil {
			t.Fatal(err)
		}
		brokenDenoDir(t)
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		assertRollback(t, code, out, proj, 5)
	})

	t.Run("the hook itself fails", func(t *testing.T) {
		requireSupportedDeno(t, "this subtest needs a supported Deno to make the hook itself fail")
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": `console.error("cannot build"); Deno.exit(1);` + "\n",
		}, `{"postInstallPolicy": "allow"}`)
		if err := os.RemoveAll(filepath.Join(proj, "ngm.vendor")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(proj, "ngm.lock")); err != nil {
			t.Fatal(err)
		}
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		assertRollback(t, code, out, proj, 2)
	})
}
