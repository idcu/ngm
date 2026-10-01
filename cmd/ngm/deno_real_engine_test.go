package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// 真实 deno 引擎验收（v0.5）。
//
// ## 为什么补这一块
//
// `deno` adapter 自 v0.2 起就"已适配"，但它的**真实** CLI 兼容性从未被任何测试证明过：
// 假引擎会接受任何参数，真实工具不会。这与 tsc / postcss 在 v0.2 被补上真工具验收
// 是同一件事，而那次补测当场抓到一条真缺陷（tsc 把诊断写在 stdout，被 ngm 丢掉）。
//
// v0.2 ~ v0.4 之所以没做，是因为 `deno bundle` 是上游的实验特性（≥2.4，且至今仍标注
// "experimental and subject to changes"，见 https://deno.com/blog/v2.4）——
// 拿它当 CI 门禁会让红灯来自上游的接口变动，而不是我们的代码。
//
// 但**同一份 adapter 的另一半是稳定的**：`deno check`（typeCheck）不是实验特性。
// 因此这条挂账被拆成两半：稳定的一半（本文件）现在覆盖，
// bundle 那一半继续挂账，条件是上游把 bundle 标为稳定。
//
// ## 探到的事实（写在断言里，免得下次重新猜）
//
// 用真实 deno（1.45.2）实测 `deno check`：
//
//   - 干净项目 exit=0；类型错误 exit=1
//   - **所有输出都走 stderr**（连 "Check file:///..." 那条也一样），stdout 为空
//
// 第二条对本项目特别重要：v0.2 的缺陷正是"引擎把诊断写在 stdout、而 ngm 只在失败时
// 保留 stderr"，于是类型检查最该给用户的那条诊断被丢掉。deno 走 stderr，因此不会
// 重演那个 bug——但这是**测出来的**，不是推断出来的。
//
// 顺带观察到：deno 在非 TTY 下仍输出 ANSI 颜色码。它落在 stderr，不污染 `--json`
// 的 stdout，因此本轮不动它；记在这里以免将来有人把它当成"输出没被正确处理"。
func TestV05RealDenoTypeCheck(t *testing.T) {
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno is not installed; the hermetic acceptance covers the argv translation")
	}
	testutils.AllowEngines(t, "deno")
	isolateUserEnv(t)

	// deno **不在内置清单里**（见 docs/guides/build.md）：它的 bundle 是自身 ≥2.4 的
	// 实验特性，内置还会抢掉 typeCheck 的默认顺序。因此这里按**文档给用户的方式**声明，
	// 顺手证明那条路走得通。
	declareDenoEngine := func(proj string) {
		t.Helper()
		testutils.WriteFile(t, proj, "ngm.engines.json", `{
  "version": 1,
  "engines": [
    {"name": "deno", "kind": "typeCheck", "adapter": "subprocess",
     "command": "deno check", "defaultOptions": {}}
  ]
}
`)
	}

	t.Run("a clean project type-checks", func(t *testing.T) {
		proj := newProject(t)
		declareDenoEngine(proj)
		testutils.WriteFile(t, proj, "src/index.ts",
			"export const greet = (who: string): string => who;\n")

		code, out := runCaptureCode(t, "typecheck", "--engine=deno", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a valid project must type-check; exit=%d:\n%s", code, out)
		}
	})

	// 失败的一半比成功的一半更要紧：若这里报成功，CI 就会把类型错误当成通过。
	t.Run("a type error is reported instead of passing", func(t *testing.T) {
		proj := newProject(t)
		declareDenoEngine(proj)
		testutils.WriteFile(t, proj, "src/index.ts",
			"export const n: number = true;\n")

		code, out := runCaptureCode(t, "typecheck", "--engine=deno", "--dir="+proj)
		if code == 0 {
			t.Fatalf("deno reported a type error; ngm must not exit 0:\n%s", out)
		}
		// 引擎自己的诊断必须原样到达用户。断言用 deno 真实给出、且在颜色码之外
		// 仍然连续的子串——tsc 那次的教训是"只断言非零退出码"不够：
		// 丢掉诊断同样是非零退出，而用户什么也看不到。
		if !strings.Contains(out, "TS2322") && !strings.Contains(out, "not assignable") {
			t.Errorf("the engine's own diagnostic should reach the user:\n%s", out)
		}
	})
}
