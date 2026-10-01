package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV04RealTscTypeDecl 用**真实 tsc** 验证 `ngm typedecl`。
//
// 为什么 hermetic 那组不够：那一组用的是假引擎，它接受任何参数、写死一个文件名。
// 假引擎证明的是 ngm 侧的 argv 翻译；它**永远证明不了**"我们调用真实工具的方式与
// 它的接口一致"——而这正是本项目两次真工具验收各自抓到一个真缺陷的地方
// （v0.2：tsc 把诊断写在 stdout 被丢掉；v0.3：esbuild 的 .mjs 脚本在只有全局安装时
// 报 ERR_MODULE_NOT_FOUND）。
//
// 缺 tsc 时跳过；CI 的 engine-integration job 装了它，因此跳过不会把验收变成假象。
func TestV04RealTscTypeDecl(t *testing.T) {
	if _, err := exec.LookPath("tsc"); err != nil {
		t.Skip("tsc is not installed; the hermetic acceptance covers the argv translation")
	}
	testutils.AllowEngines(t, "tsc")
	isolateUserEnv(t)

	proj := newProject(t)
	testutils.WriteFile(t, proj, "src/index.ts",
		"export const greet = (who: string): string => \"hi \" + who;\n")

	code, out := runCaptureCode(t, "typedecl", "--engine=typescript", "--outdir=dist/types", "--dir="+proj)
	if code != 0 {
		t.Fatalf("typedecl exit=%d:\n%s", code, out)
	}

	// 报出的路径必须是**项目内**的相对路径：用户会照着它去找文件。
	// （这正是本轮修掉的那个缺陷的断言——曾经它列的是相对进程 CWD 的路径。）
	if !strings.Contains(out, "dist/types/index.d.ts") {
		t.Errorf("the report must name the file where it actually is:\n%s", out)
	}

	decl, err := os.ReadFile(filepath.Join(proj, "dist", "types", "index.d.ts"))
	if err != nil {
		t.Fatalf("the declaration was not written: %v", err)
	}
	// 内容必须**描述了那个导出符号及其类型**。只断言"文件存在"会让一个空文件也算通过，
	// 而空声明正是这个命令最想避免的那种"看起来成功了"。
	// 断言写成 tsc 真实产出的那一行（`export declare const greet: (who: string) => string;`），
	// 而不是"两个词出现在某处"——后者对一份只有文件头的产物也成立。
	if !strings.Contains(string(decl), "export declare const greet") {
		t.Errorf("the declaration does not describe the exported symbol:\n%s", decl)
	}
}
