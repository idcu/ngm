package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.50：**"下一步"这条契约要跨通道成立**。
//
// v0.32 立的契约是：报告里**每一条失败项**都要配一行"接下来做什么"
// （`→ …` 或 `Fixed in: …`）。那条契约只看**人读**的输出。
//
// 而 `--json` 是给脚本读的——同一条报告在机器那一侧如果没有对应字段，
// 就会出现这种局面：**脚本知道失败了，但不知道该做什么**，
// 于是它只能去把人读文本再解析一遍（正好是 `--json` 要避免的事）。
//
// 判据（跨通道一致性，刻意不依赖各命令的 schema）：
//
//	人读那一侧有行动行 ⇒ 机器那一侧必须带结构化字段（`remediation` 或 `fixedIn`）
//
// 两条既有的约定就是这两个名字：`verify` 的报告用 `remediation`（v0.32），
// `audit` 的公告用 `fixedIn`（它本来就来自 OSV 的 `fixed` 事件）。
//
// **不套这条契约的路径必须登记**（`jsonNotApplicable`）：它们是
// "错误走了人读通道"的那一类（v0.16 的规则④：输入错误时 stdout 必须为空——
// "我没有报告可给"不能用半份 JSON 表达）。登记要写清为什么，
// 否则"这条路径没有 JSON"与"我忘了看它"长得一模一样。
func TestV50MachineReadableReportsCarryTheNextStepToo(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	checked, notApplicable := 0, 0
	for _, rc := range reportCases {
		t.Run(rc.name, func(t *testing.T) {
			// 第一道闸门是**机械的**：这个命令有没有 `--json`？
			// 从**派生的 flag 表**里问，不靠我记（v0.50 实测：`install --json`
			// 会撞上 "flag provided but not defined"，于是 exit 3 而不是漂移的 exit 1——
			// 那**不是**缺陷，是"这个命令没有那个 flag"；v0.16 的第②条规矩
			// 只对那 7 个带 `--json` 的命令成立）。
			if !hasJSONFlag(t, rc.args[0]) {
				notApplicable++
				t.Logf("no JSON by design: `%s` has no --json flag (derived from source)", rc.args[0])
				return
			}

			// 人读那一侧：这一条报告里有多少条行动行。
			humanHome := isolateUserEnv(t)
			humanDir := rc.setup(t, humanHome)
			humanArgs := append(append([]string{}, rc.args...), "--dir="+humanDir)
			var hOut, hErr bytes.Buffer
			humanCode := dispatch(humanArgs, &hOut, &hErr)
			human := hOut.String() + hErr.String()
			nHuman := len(reActionLine.FindAllString(human, -1))

			// 机器那一侧：同一条报告加 `--json`。
			jsonHome := isolateUserEnv(t)
			jsonDir := rc.setup(t, jsonHome)
			jsonArgs := append(append([]string{}, rc.args...), "--json", "--dir="+jsonDir)
			var jOut, jErr bytes.Buffer
			jsonCode := dispatch(jsonArgs, &jOut, &jErr)
			body := jOut.String()

			// **两边都要归一化**（v0.50 实测的第三次同一个形状；v0.51 收成共享助手）：
			// 两次 run 各自 isolate 到**不同的** temp 目录，而输出里会印出那个绝对路径。
			human = normalizeRunPaths(t, human, humanHome, humanDir)
			body = normalizeRunPaths(t, body, jsonHome, jsonDir)

			if jsonCode != humanCode {
				t.Errorf("`--json` 改变了判定（exit %d vs %d）——v0.16 的第②条规矩", jsonCode, humanCode)
			}

			if strings.TrimSpace(body) == "" {
				// 没有 JSON：只允许是**登记过**的错误通道。
				reason, ok := jsonNotApplicable[rc.name]
				if !ok {
					t.Fatalf("这条路径没有产出 JSON，但它**没有登记**——"+
						"要么它是一条错误通道（写进 jsonNotApplicable 并写清为什么），"+
						"要么它本该有报告（那就是缺陷）:\n  human exit=%d\n%s",
						humanCode, firstLine(human))
				}
				notApplicable++
				t.Logf("no JSON by design: %s", reason)
				return
			}

			checked++
			if nHuman == 0 {
				// 人读那侧也没有行动行：这一条不在本契约的范围里（由 v0.32 的网管）。
				t.Logf("human side has no next-step line either (%d bytes of JSON)", len(body))
				return
			}
			// 判据是**内容级**的（不是"有没有某个字段"）：
			// 人读那侧的每一条行动行，去掉 `→` / `driftKind: …;` / `Fixed in: ` 这些前后缀之后，
			// 那**句话本身**必须逐字出现在 JSON 里。
			//
			// 为什么不能只查字段名（v0.50 实测）：第一版判据是"body 里含 `remediation`
			// 或 `fixedIn` 即可"，而牙齿（把 tree 的报告级 remediation 去掉）**没能咬住它**——
			// 因为条目级的 vuln 字段还在，粗判据照样绿。**绿不等于它在盯着什么**（v0.47）。
			//
			// 内容级判据同时钉住了"两个通道说同一句话"这件事：不是同一个句子就不算数。
			// v0.53（把表里那条"小"的做掉）：**逐条计数**也要对得上。
			//
			// 内容级判据能发现"两处说的不是同一句话"，但发现不了
			// "人读有 7 条、机器只给 1 条"——只要那 1 条恰好是其中一条的字句，
			// 内容级判据就绿了（v0.50 的牙齿演示过这类"粗判据被替交差"）。
			nJSON := strings.Count(body, `"remediation"`) + strings.Count(body, `"fixedIn"`)
			if nJSON < nHuman {
				t.Errorf("人读那侧有 %d 行行动行，而机器那侧只有 %d 处结构化字段——"+
					"逐条对不上时，脚本只看得到其中一部分:\n%s", nHuman, nJSON, firstLine(human))
			}
			t.Logf("%-40s human=%d json=%d", rc.name, nHuman, nJSON)

			for _, line := range reActionLine.FindAllString(human, -1) {
				core := adviceCore(line)
				if core == "" {
					continue
				}
				// JSON 里的字符串值是**被转义**的文本（反斜杠会变成 `\\`），
				// 所以针也要按 JSON 的规矩转义一遍——否则 Windows 路径上的建议
				// 会被误判成"两处说的不是同一句话"（v0.50 实测：第一版就是这么红的，
				// 而红的是我把转义忘了）。
				// 转义要**与产品的编码器一致**：产品的 `--json` 不转义 HTML
				// （否则它自己的值会写成 `\u003c…`，读起来没法看），而 `json.Marshal`
				// 默认转义 `<` `>` `&`——v0.50 实测：第一版针里出现 `\u003chome\u003e`，
				// 与正文逐字对不上，而红的是我的针。
				var buf bytes.Buffer
				enc := json.NewEncoder(&buf)
				enc.SetEscapeHTML(false)
				if jerr := enc.Encode(core); jerr != nil {
					t.Fatal(jerr)
				}
				quoted := strings.TrimRight(buf.String(), "\n") // 带首尾引号
				if !strings.Contains(body, quoted) {
					t.Errorf("人读那侧的行动行在机器那侧找不到——两处说的不是同一句话:\n"+
						"  人读: %s\n  要找的: %s\n（JSON %d 字节）", line, quoted, len(body))
				}
			}
		})
	}

	// 守卫：这条判据必须**真的看到了东西**（否则它会静默通过）。
	if checked == 0 {
		t.Fatalf("no report produced JSON at all — this net no longer looks at the right thing")
	}
	t.Logf("json next step: %d report(s) produced JSON, %d path(s) are error-channel by design",
		checked, notApplicable)
}

// jsonNotApplicable 登记"**有 `--json` 却没有 JSON 报告**、而且这是对的"的路径，并写清为什么。
//
// 与 v0.38 的 exitZeroByDesign 同一套路：退 0 的格子、没有 JSON 的路径，
// 都是"契约看不见的地方"——它们必须**具名登记**，否则缺口与设计分不开。
//
// 注意这道闸门**只管第二种情形**：命令本身没有 `--json` 时由机械闸门拦下
// （见测试里对 hasJSONFlag 的调用），不需要在这里登记——
// "这个命令没有那个 flag"是**源码里查得到的事实**，不是我的判断。
var jsonNotApplicable = map[string]string{
	"verify/检查未能完成（网络被策略拒）": "错误通道：v0.16 规则④——输入错误时 stdout 必须为空，" +
		"\"我没有报告可给\"不能用半份 JSON 表达（人读文本在 stderr）",
}

// reAdvicePrefix 剥掉行动行的"前后缀"，留下那句**建议本身**。
//
// 三种前后缀都是既有的约定：`→ `（全部）、`driftKind: expected; `（verify 的分类）、
// `Fixed in: `（audit 的另一种约定）。剥掉之后剩下的话应当**逐字**出现在 JSON 里——
// 两个通道说同一句话，判据就查这句话。
var reAdvicePrefix = regexp.MustCompile(`^(?:→\s*)?(?:driftKind: \w+;\s*)?(?:Fixed in:\s*)?`)

func adviceCore(line string) string {
	return strings.TrimSpace(reAdvicePrefix.ReplaceAllString(strings.TrimSpace(line), ""))
}

// hasJSONFlag 从**派生的 flag 表**里问"这个命令有没有 `--json`"。
//
// 不靠我记：v0.50 实测踩过一次——我以为每一张报告都能加 `--json`，
// 于是在 `install --json` 上得到"flag provided but not defined"与 exit 3，
// 却把它当成了"`--json` 改变了判定"。真相是那个命令**没有这个 flag**。
func hasJSONFlag(t *testing.T, cmd string) bool {
	t.Helper()
	for _, f := range boolFlagsOfCommand(t, cmd) {
		if f == "json" {
			return true
		}
	}
	return false
}
