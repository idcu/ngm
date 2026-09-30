package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// 本文件是**真实引擎**验收：tsc 与 postcss。
//
// 与 TestV02EngineAdaptersAcceptance 的分工（照 TestM6Acceptance_RealEsbuild 的先例）：
//
//	假引擎那一组   证明 ngm 侧的行为——argv 翻译、退出码、notes 通道（hermetic、三平台一致）
//	本文件         证明**我们调用真实工具的方式与它的接口一致**
//
// 后者是假引擎永远证明不了的：假引擎会接受任何参数，真实工具不会。
// 缺工具时跳过——但 CI 的 engine-integration job 会装上它们，
// 因此"跳过"不会把验收变成假象（这正是刻意与 hermetic 组分开放置的原因）。

// TestV02RealTsc 用真实 tsc 验证类型检查的两端：干净项目通过、类型错误必须被报出来。
func TestV02RealTsc(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc is not installed; the hermetic acceptance covers the argv translation")
	}
	testutils.AllowEngines(t, "tsc")
	isolateUserEnv(t)

	t.Run("a clean project type-checks", func(t *testing.T) {
		proj := newProject(t)
		testutils.WriteFile(t, proj, "src/index.ts",
			"export const greet = (who: string): string => \"hi \" + who;\n")

		code, out := runCaptureCode(t, "typecheck", "--engine=typescript", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a valid project must type-check; exit=%d:\n%s", code, out)
		}
	})

	// 这条是 `--noEmit` 之外更关键的一半：类型检查**失败**时 ngm 必须非零退出，
	// 且把引擎自己的诊断原样带给用户。若这里报成功，CI 就会把类型错误当成通过。
	t.Run("a type error is reported instead of passing", func(t *testing.T) {
		proj := newProject(t)
		testutils.WriteFile(t, proj, "src/index.ts", "export const n: number = \"not a number\";\n")

		code, out := runCaptureCode(t, "typecheck", "--engine=typescript", "--dir="+proj)
		if code == 0 {
			t.Fatalf("tsc reported a type error; ngm must not exit 0:\n%s", out)
		}
		if !strings.Contains(out, "not a number") && !strings.Contains(out, "error TS") {
			t.Errorf("the engine's own diagnostic should reach the user:\n%s", out)
		}
	})
}

// TestV02RealPostcss 用真实 postcss-cli 验证 CSS 编译与"--minify 不静默忽略"。
func TestV02RealPostcss(t *testing.T) {
	if _, err := exec.LookPath("postcss"); err != nil {
		t.Skip("postcss is not installed; the hermetic acceptance covers the argv translation")
	}
	testutils.AllowEngines(t, "postcss")
	isolateUserEnv(t)

	t.Run("css compiles and lands where asked", func(t *testing.T) {
		proj := newProject(t)
		testutils.WriteFile(t, proj, "app.css", ".a{color:red}\n")

		code, out := runCaptureCode(t, "css", "app.css",
			"--engine=postcss", "--outfile=dist/app.css", "--dir="+proj)
		if code != 0 {
			t.Fatalf("real postcss build failed; exit=%d:\n%s", code, out)
		}
		if _, err := os.Stat(filepath.Join(proj, "dist", "app.css")); err != nil {
			t.Errorf("the compiled css was not written: %v", err)
		}
	})

	// 在**真实** postcss 上验证那条 note：假引擎只能证明我们把话说了，
	// 真实工具才能证明"我们确实没有把它当成压缩器"。
	t.Run("--minify is reported as ignored", func(t *testing.T) {
		proj := newProject(t)
		testutils.WriteFile(t, proj, "app.css", ".a{color:red}\n")

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--minify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("css exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "minifier") {
			t.Errorf("the report must say --minify was ignored and why:\n%s", out)
		}
	})
}
