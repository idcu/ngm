package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/testutils"
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

		// 再加一个**没有钩子**的依赖：这次失败的 install 因而会写出与调用前
		// 不同的 mappings（两条而不是一条）。只回滚 vendor、不回滚 mappings 的话，
		// 磁盘上会留下"新 mappings + 旧 lock"，而 mappings 指向的目录已被删掉。
		second := testutils.NewGitRepo(t)
		second.WriteFile("index.ts", "export const s = 2\n")
		second.Commit("feat: second fixture")
		second.Tag("v1", false)
		seedMirror(t, "github:v3/second", second.Dir)
		if code, out := runCaptureCode(t, "add", "github:v3/second@v1",
			"--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add second dependency: %s", out)
		}

		mappingsPath := mappings.Find(proj)
		before, err := os.ReadFile(mappingsPath)
		if err != nil {
			t.Fatalf("the successful fixture install must have written mappings: %v", err)
		}

		if err := os.RemoveAll(filepath.Join(proj, "ngm.vendor")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(proj, "ngm.lock")); err != nil {
			t.Fatal(err)
		}
		brokenDenoDir(t)
		// --allow-fresh：本测的 fixture 是刚 commit 的仓库，ADR-050 的默认 P1D
		// 晾晒期会先把它挡在 exit 3——那测的是另一道门禁，与本测"沙箱 runner 坏了→
		// exit 5 + 回滚"无关。用单命令逃生口隔离掉无关变量（白名单等其他门禁不变）。
		code, out := runCaptureCode(t, "install", "--allow-fresh", "--dir="+proj)
		assertRollback(t, code, out, proj, 5)

		after, rerr := os.ReadFile(mappingsPath)
		if rerr != nil {
			t.Fatalf("mappings must still exist after rollback: %v", rerr)
		}
		if string(after) != string(before) {
			t.Errorf("mappings was left at the failed run's content — it points at vendor "+
				"directories that rollback removed:\nbefore:\n%s\nafter:\n%s", before, after)
		}
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
