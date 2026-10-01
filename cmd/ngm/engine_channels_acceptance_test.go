package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV05EngineChannelsAcceptance 固定"把话说到用户那里"的两条通道：
//
//  1. **引擎自己**写到 stderr 的诊断 —— 原样转发，不转述、不吞掉
//  2. **ngm 自己**的说明（adapter 的 Notes）—— 加 `note: ` 前缀转发
//
// 为什么单列一条验收：这两条通道此前**各丢了一半**，而且根因是同一个——
// 两种结果类型里同名的 `Warnings` 字段含义不同：
//
//	bundle 丢 Notes   `deno bundle` 的"实验特性"提示写好了却没人读
//	css    丢 stderr  postcss 的警告（弃用提示、插件的非致命报错）根本到不了用户
//
// 第一项尤其要紧：那句提示是"我们没说过它稳定"的**全部依据**；
// 没有它，用户会以为自己在用一个稳定接口。
//
// 形态与 `internal/config/field_wiring_test.go` 同源：**写了没人读**必须有一条断言钉住，
// 而不是靠下一个人记得核对。
func TestV05EngineChannelsAcceptance(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 引擎名决定走哪个翻译器：`deno` 走 deno 分支、`postcss` 走 postcss 分支。
	// 命令里的 `bundle` 是子命令前缀（deno 的翻译器只补可变参数）。
	newProj := func(name, kind, command string) string {
		t.Helper()
		proj := m6Project(t, `{"`+kind+`":"`+name+`"}`)
		m6WriteCatalog(t, proj, m6CatalogEntry{
			Name: name, Kind: kind, Adapter: "subprocess",
			Command: `"` + fake + `"` + command,
		})
		testutils.WriteFile(t, proj, "app.css", ".a{color:red}\n")
		return proj
	}

	t.Run("deno bundle below 2.4 is refused, and points at esbuild", func(t *testing.T) {
		proj := newProj("deno", "bundle", " bundle")
		// 假引擎的 --version 由 FAKE_VERSION 决定；默认值 1.0.0-fake 低于 2.4。
		t.Setenv("FAKE_VERSION", "1.0.0-fake")

		code, out := runCaptureCode(t, "build", "--engine=deno", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a deno without `bundle` must be exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "2.4") {
			t.Errorf("the error must name the version it needs:\n%s", out)
		}
		if !strings.Contains(out, "esbuild") {
			t.Errorf("the error must point at the working alternative:\n%s", out)
		}
	})

	t.Run("deno bundle says it is experimental", func(t *testing.T) {
		proj := newProj("deno", "bundle", " bundle")
		t.Setenv("FAKE_VERSION", "2.4.0")

		code, out := runCaptureCode(t, "build", "--engine=deno", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build exit=%d:\n%s", code, out)
		}
		// 这条断言此前会红：note 由 deno 的翻译器产出，却没人读。
		if !strings.Contains(out, "experimental") {
			t.Errorf("using an experimental feature must be said out loud:\n%s", out)
		}
	})

	t.Run("the engine's own stderr reaches the user (bundle)", func(t *testing.T) {
		proj := newProj("deno", "bundle", " bundle")
		t.Setenv("FAKE_VERSION", "2.4.0")
		t.Setenv("FAKE_STDERR", "engine says something important")

		code, out := runCaptureCode(t, "build", "--engine=deno", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "engine says something important") {
			t.Errorf("the engine's diagnostics must not be swallowed:\n%s", out)
		}
	})

	t.Run("css carries both channels", func(t *testing.T) {
		proj := newProj("postcss", "css", "")
		t.Setenv("FAKE_STDERR", "postcss deprecation notice")

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--minify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("css exit=%d:\n%s", code, out)
		}
		// ngm 的说明（+ postcss 没有内建压缩，--minify 被忽略）
		if !strings.Contains(out, "minifier") {
			t.Errorf("ngm must say that --minify was ignored, and why:\n%s", out)
		}
		// 引擎自己的 stderr——这条此前被整个丢掉
		if !strings.Contains(out, "postcss deprecation notice") {
			t.Errorf("the engine's own stderr must reach the user:\n%s", out)
		}
	})
}
