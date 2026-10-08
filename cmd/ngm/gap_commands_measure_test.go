package main

import (
	"testing"
)

// v0.62：**把缺口表里能兑现的兑现**。
//
// v0.59 给码 6 立了"声明了就要能测"的规矩，当时只能测到 5 个命令，
// 另 6 个写进了 `exitCodeGaps` 并逐条写明原因。这一版把那 6 个**逐个试过去**，
// 两个当场兑现（夹具本来就现成）：
//
//	✅ audit        —— `v3AuditProject` 把 OSV 指向**本地替身**
//	                   （`NGM_OSV_URL` 注入 ⇒ 不依赖公网），`audit --json` 就能到写报告那一步。
//	✅ integrations —— 一个**已安装**的项目（`ngm.mappings.json` 由 install 生成）
//	                   + 一个**真实存在**的工具名（`vite` / `esbuild` / `webpack` / `deno`）。
//
// 另四个（build / typecheck / css / transform）当时**判成"到不了"**，理由写的是
// "权限层拒 + 那个钩子是死的"。**v0.63 把那个判断翻了过来**：
// 钩子是通的（消费者在 `testenv_test.go` 的 `init()` 里），
// 真正的差别是**夹具里少了一行 `isolateUserEnv`**——命令读的是真实 HOME，
// 于是没有 `run:fake-engine` 授权。补上之后四个也退 6，
// 于是它们在 v0.63 有了自己的测试（`TestV63EngineCommandsAreMeasuredForCodeSix`），
// 本文件从此只管 audit 与 integrations。
//
// 每个用例都用 v0.59 那套手法：给 `dispatch` 传一个**永远写失败的 writer**，
// 命令算完结论、写报告时失败 ⇒ 退 **6**。
//
// 子测试名写成字面量：`exitCodeElsewhere` 里的指针会被
// `TestV56RegisteredPointersResolve` 从**源码**核对（表驱动的名字它看不见）。
func TestV62TheRemainingGapCommandsAreMeasuredForCodeSix(t *testing.T) {
	t.Run("audit", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		measureInternalFailure(t, "audit", "--json", "--dir="+proj)
	})

	t.Run("integrations", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
		if code, out := runCaptureCode(t, "add", "github:x/dep@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		// `--json` 属于 `add` 那一层，而不是 integrations 自己那一层。
		measureInternalFailure(t, "integrations", "add", "vite", "--dry-run", "--json", "--dir="+proj)
	})
}
