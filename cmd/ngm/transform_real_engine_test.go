package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV05RealEsbuildTransform 用**真实 esbuild** 验证 `ngm transform`。
//
// 与 TestV05TransformAcceptance 的分工（照 tsc / postcss / deno 的先例）：
//
//	假引擎那一组   证明 ngm 侧的行为——argv、stdin、退出码、落盘（hermetic、三平台一致）
//	本文件         证明**我们调用真实工具的方式与它的接口一致**
//
// 后者是假引擎永远证明不了的：假引擎接受任何参数，真实工具不会——
// 本项目两次真工具验收各自抓到过一条真缺陷，都是这个原因。
//
// 缺 esbuild 时跳过；CI 的 engine-adapter job 装了它并**禁止 skip**，
// 因此跳过不会把验收变成假象。
func TestV05RealEsbuildTransform(t *testing.T) {
	if _, err := exec.LookPath("esbuild"); err != nil {
		t.Skip("esbuild is not installed; TestV05TransformAcceptance covers the wiring without it")
	}
	testutils.AllowEngines(t, "esbuild")
	isolateUserEnv(t)

	proj := newProject(t)
	testutils.WriteFile(t, proj, "src/helper.ts",
		"export const helper = (s: string): string => s.toUpperCase();\n")
	testutils.WriteFile(t, proj, "src/index.ts",
		"import { helper } from \"./helper\";\n"+
			"export const greet = (who: string): string => helper(who);\n")

	t.Run("types are stripped and imports are left alone", func(t *testing.T) {
		code, out := runCaptureCode(t, "transform", "src/index.ts", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		// 类型标注必须消失（那是 transform 的全部意义）
		if strings.Contains(out, "(who: string)") {
			t.Errorf("the type annotation should be gone:\n%s", out)
		}
		if !strings.Contains(out, "greet") {
			t.Errorf("the output should still define the symbol:\n%s", out)
		}
		// **transform 不解析导入**——这正是它区别于 `ngm build` 的地方。
		// 断言"import 还在、且被导入模块的代码没有出现"，把这条边界钉住：
		// 否则哪天有人把它实现成 bundle，用户会在不知不觉中拿到不一样的东西。
		if !strings.Contains(out, "\"./helper\"") {
			t.Errorf("transform must keep the import statement untouched:\n%s", out)
		}
		if strings.Contains(out, "toUpperCase") {
			t.Errorf("transform must NOT pull in the imported module (that is `ngm build`):\n%s", out)
		}
	})

	t.Run("--minify really minifies", func(t *testing.T) {
		_, plain := runCaptureCode(t, "transform", "src/index.ts", "--dir="+proj)
		code, min := runCaptureCode(t, "transform", "src/index.ts", "--minify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform --minify exit=%d:\n%s", code, min)
		}
		// 真实 esbuild 收到了 --minify 才会变短。若哪天它又被静默丢掉
		// （adapter 的 generic 分支曾经就是这样），这条断言会红。
		if len(min) >= len(plain) {
			t.Errorf("--minify should shrink the output: %d bytes vs %d unminified\n%s",
				len(min), len(plain), min)
		}
	})

	t.Run("--outfile lands where asked", func(t *testing.T) {
		code, out := runCaptureCode(t, "transform", "src/index.ts",
			"--outfile=dist/out.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		// 产物由 **ngm** 落盘（transform 的引擎接口只有 stdout 一条出口），
		// 因此这里同时验证了"报告说的路径"与"实际写下的路径"是同一个。
		body := readProjectFile(t, proj, "dist/out.js")
		if !strings.Contains(body, "greet") {
			t.Errorf("the written file should hold the transformed source:\n%s", body)
		}
	})
}
