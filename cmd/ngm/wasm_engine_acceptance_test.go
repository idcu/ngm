package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/testutils"
)

// v3WasmProject 建一个用 wasm 引擎做 bundle 的项目。
//
// 模块是**手写字节**（testutils.WASIModuleStdout）：CI 里没有 tinygo / emscripten，
// 而这条验收要证明的正是"ngm 能把一个 WASI 模块当成引擎跑起来"。
func v3WasmProject(t *testing.T, marker string) string {
	t.Helper()
	home := isolateUserEnv(t)

	// `run:` 的默认档位是"需配置"（D 组）：执行第三方产物要显式授权，
	// wasm 模块与外部 CLI 在这一点上**没有区别**。除了那条拒绝用例，
	// 其余用例都先给出这条授权。
	writeGlobalConfig(t, home, `{"permissions":{"allow":["run:engine.wasm"]}}`)

	proj := t.TempDir()

	testutils.WriteFile(t, proj, "ngm.json", `{
  "schemaVersion": 1,
  "name": "github.com/v3/wasm-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "src/index.ts",
  "dependencies": [],
  "engines": {"bundle": "wasm-bundler"}
}
`)
	testutils.WriteFile(t, proj, "ngm.engines.json", `{
  "version": 1,
  "engines": [
    {"name": "wasm-bundler", "kind": "bundle", "adapter": "wasm", "command": "tools/engine.wasm"}
  ]
}
`)
	testutils.WriteFile(t, proj, "src/index.ts", "export const x = 1\n")

	// 二进制用 os.WriteFile 直写：测试助手面向文本，可能在写入时规整换行，
	// 而 wasm 模块的字节一个都不能动。
	dir := filepath.Join(proj, "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "engine.wasm"), testutils.WASIModuleStdout(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

// TestV03WasmEngineAcceptance 是 A 组 wasm adapter 的端到端验收。
func TestV03WasmEngineAcceptance(t *testing.T) {
	const marker = "wasm bundle output\n"

	// 主干：声明为 wasm 的引擎真的被跑起来了，产物进了 stdout。
	t.Run("a wasm engine is selected and runs", func(t *testing.T) {
		proj := v3WasmProject(t, marker)

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "wasm bundle output") {
			t.Errorf("the module's stdout should be the artifact:\n%s", out)
		}
	})

	// `--outfile` 由 ngm 代写：模块没有写权限（ADR-011 决策 3），
	// 但用户给的 outfile 必须真的出现。
	t.Run("--outfile is written by ngm, not by the module", func(t *testing.T) {
		proj := v3WasmProject(t, marker)

		code, out := runCaptureCode(t, "build", "--outfile=dist/out.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build exit=%d:\n%s", code, out)
		}
		data, err := os.ReadFile(filepath.Join(proj, "dist", "out.js"))
		if err != nil {
			t.Fatalf("the artifact was not written: %v", err)
		}
		if string(data) != marker {
			t.Errorf("artifact = %q, want the module's stdout", data)
		}
	})

	// 模块自己的退出码必须是引擎失败码——CI 据此判断引擎为什么失败。
	t.Run("a failing module fails the build with its own exit code", func(t *testing.T) {
		proj := v3WasmProject(t, marker)
		if err := os.WriteFile(filepath.Join(proj, "tools", "engine.wasm"),
			testutils.WASIModuleExit(7), 0o644); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a failing engine is exit 1 (ngm's contract, not the module's 7): got %d\n%s", code, out)
		}
	})

	// **权限门禁对 wasm 同样成立**：模块是第三方产物，执行它与执行一个外部 CLI
	// 是同一类授权。这里刻意拒绝 `run:engine.wasm`（目标的 basename）。
	t.Run("the run: permission gates wasm engines too", func(t *testing.T) {
		// 先按常规布置（v3WasmProject 会授权那份 allow 配置），再**换成**拒绝——
		// 顺序反了的话，后面的授权会覆盖这里的拒绝，测试就成了"什么都没验证"。
		proj := v3WasmProject(t, marker)
		home, herr := os.UserHomeDir()
		if herr != nil {
			t.Fatal(herr)
		}
		writeGlobalConfig(t, home, `{"permissions":{"deny":["run:engine.wasm"]}}`)

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a denied wasm engine must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "run:engine.wasm") {
			t.Errorf("the report should name the permission:\n%s", out)
		}
	})

	// 模块不存在时是**可用性**问题（exit 5），不是"未实现"——
	// 后者曾是 wasm 的报法，现在它是实现了的 adapter。
	t.Run("a missing module is an availability problem", func(t *testing.T) {
		proj := v3WasmProject(t, marker)
		if err := os.Remove(filepath.Join(proj, "tools", "engine.wasm")); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a missing module must exit 5, got %d:\n%s", code, out)
		}
		if strings.Contains(out, "not implemented") {
			t.Errorf("wasm is implemented; it must not be reported as unimplemented:\n%s", out)
		}
	})
}

// 权限门禁的目标名：subprocess 与 wasm 都用被执行的**文件名 basename**，
// embed 不给（它是 ngm 进程内的逻辑，不是"执行了谁的代码"）。
func TestEngineRunTarget(t *testing.T) {
	cases := []struct {
		entry adapter.Entry
		want  string
	}{
		{adapter.Entry{Adapter: adapter.AdapterSubprocess, Program: "esbuild"}, "esbuild"},
		{adapter.Entry{Adapter: adapter.AdapterSubprocess, Program: "/usr/bin/esbuild"}, "esbuild"},
		{adapter.Entry{Adapter: adapter.AdapterSubprocess, Program: "esbuild.cmd"}, "esbuild"},
		{adapter.Entry{Adapter: adapter.AdapterWasm, Program: "tools/engine.wasm"}, "engine.wasm"},
		{adapter.Entry{Adapter: adapter.AdapterEmbed, Program: "self"}, ""},
		{adapter.Entry{Adapter: adapter.AdapterSubprocess, Program: ""}, ""},
	}
	for _, c := range cases {
		if got := engineRunTarget(c.entry); got != c.want {
			t.Errorf("engineRunTarget(%s/%s) = %q, want %q", c.entry.Adapter, c.entry.Program, got, c.want)
		}
	}
}
