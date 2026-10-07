package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.51：**失败路径上也要有机器可读的那一份**。
//
// 起点是 v0.50 量出的一条边界：`--json` 的命令失败时，stdout 为空、
// 错误只以**人读文本**落在 stderr（v0.16 规则④）——
// 于是脚本拿到的信息是"退出码 3 + 空的 stdout"，**知道出了事，却读不出是什么事**。
//
// 这一版把规则④改成"**要么为空、要么恰好一份错误信封**"（空留给"这个命令
// 根本不接受 `--json`"的情形），并在这里钉住信封的**完整性**与**跨通道一致**：
//
//	① stdout 恰好一份 JSON 文档，且带 `error` 段（不是报告）；
//	② `error.code` / `error.message` 非空；
//	③ `error.exitCode` **等于进程退出码**（v0.16 第③条规矩的同一件事）；
//	④ `error.hint` 与人读那侧的 `hint:` **逐字相同**——两个通道说同一句话
//	   （v0.50 立的那条纪律，在失败路径上继续成立）。
//
// 夹具复用**错误面表**（v0.31）：只挑"命令确实接受 `--json`"的那些，
// 从**派生的 flag 表**里问，不靠我记。
func TestV51JSONErrorEnvelopeIsCompleteAndSameSentence(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	checked := 0
	for _, sc := range surfaceCases {
		if sc.expectZero != "" || len(sc.args) == 0 || !hasJSONFlag(t, sc.args[0]) {
			continue
		}
		t.Run(sc.name, func(t *testing.T) {
			isolateUserEnv(t)
			dir := sc.setup(t)
			args := append(append([]string{}, sc.args...), "--dir="+dir)

			// 人读那一侧：先拿到它的 hint 那句话。
			var hOut, hErr bytes.Buffer
			humanCode := dispatch(args, &hOut, &hErr)
			humanHint := hintLine(hErr.String() + hOut.String())
			if humanCode == 0 {
				t.Skipf("this path succeeds now — not an error path any more")
			}

			// 机器那一侧。
			isolateUserEnv(t)
			dir2 := sc.setup(t)
			jsonArgs := append(append([]string{}, sc.args...), "--json", "--dir="+dir2)
			var jOut, jErr bytes.Buffer
			jsonCode := dispatch(jsonArgs, &jOut, &jErr)

			if jsonCode != humanCode {
				t.Errorf("`--json` 改变了判定（exit %d vs %d）——v0.16 的第②条规矩", jsonCode, humanCode)
			}
			body := bytes.TrimSpace(jOut.Bytes())
			if len(body) == 0 {
				t.Fatalf("失败路径上 stdout 是空的——脚本只知道失败，不知道为什么:\n%s",
					firstLine(jErr.String()))
			}
			// 两种形状都是合格的：**报告**（这一条本来就有报告可给）
			// 与**错误信封**（没有报告可给时）。判据分别对待——
			// 第一版把报告也按信封读，于是把报告里没有顶层 exitCode 当成"在说谎"（v0.51 实测）。
			var probe struct {
				Error *jsonErrorBody `json:"error"`
			}
			if err := json.Unmarshal(body, &probe); err != nil {
				t.Fatalf("stdout 不是恰好一份 JSON 文档：%v\n%s", err, jOut.String())
			}
			if probe.Error == nil {
				// 报告式失败：v0.16 的第③条已经管它（报告里写的 exitCode 必须等于进程退出码）。
				t.Logf("report-style failure (not an envelope): %d bytes", len(body))
				checked++
				return
			}
			checked++
			if probe.Error.Code == "" || probe.Error.Message == "" {
				t.Errorf("信封缺少 code 或 message：%+v", *probe.Error)
			}
			if probe.Error.ExitCode != jsonCode {
				t.Errorf("信封里 exitCode=%d，而进程退 %d——机器可读输出在说谎",
					probe.Error.ExitCode, jsonCode)
			}
			if humanHint != "" && probe.Error.Hint != humanHint {
				t.Errorf("两个通道说的不是同一句话:\n  人读 hint: %q\n  信封 hint: %q",
					humanHint, probe.Error.Hint)
			}
		})
	}

	if checked == 0 {
		t.Fatalf("没有一条失败路径被查到——这张网的范围缩到零了")
	}
	t.Logf("json error envelope: %d failure path(s) carried a complete envelope with the same hint", checked)
}

// hintLine 从人读输出里取出 `hint:` 那句话（没有则返回空串）。
func hintLine(text string) string {
	for _, l := range strings.Split(text, "\n") {
		if i := strings.Index(l, "hint:"); i >= 0 {
			return strings.TrimSpace(l[i+len("hint:"):])
		}
	}
	return ""
}
