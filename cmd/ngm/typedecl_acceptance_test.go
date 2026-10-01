package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v3TypeDeclProject 建一个把 `typeDecl` 能力交给假引擎的项目。
//
// 关键在于 `ngm.json` 里写的是 **`"typeDecl": "fake"`** ——这条断言是本次交付的
// 核心：`engines.typeDecl` 曾是一个对用户没有任何效果的配置键（没有命令读它），
// 现在它真的决定用哪个引擎。
func v3TypeDeclProject(t *testing.T, fakePath string) string {
	t.Helper()
	proj := m6Project(t, `{"typeDecl": "fake"}`)
	m6Catalog(t, proj, fakePath, "typeDecl")
	return proj
}

// TestV04TypeDeclAcceptance 是 `ngm typedecl` 的验收。
//
// 它同时把 `engines.typeDecl` 从"配置接线检查"的豁免清单里摘出来：
// 有了入口，那个键才可能被断言，而豁免表只允许变短。
func TestV04TypeDeclAcceptance(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	t.Run("emits declarations and reports the files that appeared", func(t *testing.T) {
		proj := v3TypeDeclProject(t, fake)
		out := filepath.Join(proj, "dist", "types")

		code, got := runCaptureCode(t, "typedecl", "--outdir=dist/types", "--dir="+proj)
		if code != 0 {
			t.Fatalf("typedecl exit=%d:\n%s", code, got)
		}
		if _, err := os.Stat(filepath.Join(out, "index.d.ts")); err != nil {
			t.Fatalf("the declaration was not written: %v", err)
		}
		// 报的是**实际出现的文件**（ngm 自己列的目录），不是引擎的一面之词。
		if !strings.Contains(got, "index.d.ts") {
			t.Errorf("the report must name what actually appeared:\n%s", got)
		}
	})

	t.Run("the config key decides which engine runs", func(t *testing.T) {
		proj := v3TypeDeclProject(t, fake)
		dump := filepath.Join(t.TempDir(), "argv.txt")
		t.Setenv("FAKE_DUMP_ARGS", dump)

		code, got := runCaptureCode(t, "typedecl", "--outdir=dist/types", "--dir="+proj)
		if code != 0 {
			t.Fatalf("typedecl exit=%d:\n%s", code, got)
		}
		argv, err := os.ReadFile(dump)
		if err != nil {
			t.Fatalf("the engine was not the one declared under engines.typeDecl: %v", err)
		}
		// 能力类别必须传对：同一个二进制可以同时注册多条清单条目，
		// 没有 --kind 它无从判断这次该做哪件事。
		if !strings.Contains(string(argv), "--kind=typeDecl") {
			t.Errorf("the engine must be told which capability to perform:\n%s", argv)
		}
		if !strings.Contains(string(argv), "--outfile=dist/types") {
			t.Errorf("the output directory must reach the engine:\n%s", argv)
		}
	})

	t.Run("--outdir is required", func(t *testing.T) {
		proj := v3TypeDeclProject(t, fake)

		code, got := runCaptureCode(t, "typedecl", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a missing --outdir is a usage error (exit 3), got %d:\n%s", code, got)
		}
		if !strings.Contains(got, "--outdir") {
			t.Errorf("the error must name the flag to add:\n%s", got)
		}
	})

	t.Run("an engine that produces nothing is reported, not silent", func(t *testing.T) {
		proj := v3TypeDeclProject(t, fake)
		t.Setenv("FAKE_EMIT_NOTHING", "1")

		code, got := runCaptureCode(t, "typedecl", "--outdir=dist/types", "--dir="+proj)
		if code != 0 {
			t.Fatalf("exiting 0 with no output is legal, got %d:\n%s", code, got)
		}
		// 合法，但必须明说：它与"命令悄悄什么都没做"长得一模一样。
		if !strings.Contains(got, "no files appeared") {
			t.Errorf("silence here is indistinguishable from a no-op:\n%s", got)
		}
	})

	t.Run("--dry-run resolves the command without running it", func(t *testing.T) {
		proj := v3TypeDeclProject(t, fake)
		t.Setenv("FAKE_EMIT_NOTHING", "1")

		code, got := runCaptureCode(t, "typedecl", "--outdir=dist/types", "--dry-run", "--dir="+proj)
		if code != 0 {
			t.Fatalf("dry-run exit=%d:\n%s", code, got)
		}
		if !strings.Contains(got, "--kind=typeDecl") {
			t.Errorf("dry-run should print the resolved argv:\n%s", got)
		}
		if _, err := os.Stat(filepath.Join(proj, "dist")); err == nil {
			t.Error("--dry-run must not create the output directory")
		}
	})
}
