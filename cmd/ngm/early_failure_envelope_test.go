package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.54：**解析阶段的失败也要是机器可读的**（把登记表里那条"中"做掉）。
//
// v0.51 把错误信封做进了 `runErr`，而 `runErr` 只在**命令内部**被调用——
// **解析失败**（flag 不认识）与**命令不存在**都发生在它之前。实测：
//
//	$ ngm verify --json --bogus
//	exit 3 · stdout **0 字节** · stderr 3198 字节 usage
//
// 也就是说：用户**明确提了** `--json`，而那次失败在机器那侧**什么也没留下**。
//
// 判据（四条）：
//
//	① 接受 `--json` 的命令（从**派生的 flag 表**问，不靠我记）：未知 flag
//	   ——**两种顺序**都要给一份信封；
//	② 未知命令 / 缺子命令：同样给信封；
//	③ 信封本身合格：恰好一份文档 · code 是 `Usage` · exitCode == 进程退出码 ·
//	   message 说清是哪个 flag/命令 · hint 点名一个**能照着做**的东西（`--help`）；
//	④ **两条对照必须仍然为空**：没提 `--json` 时（不许给没要的人塞 JSON）、
//	   以及命令**不接受** `--json` 时（规则④那句仍然成立——`install --json`）。
func TestV54EarlyFailuresAreMachineReadableToo(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// ---- ① 接受 --json 的命令 × 两种 flag 顺序 ----
	checked := 0
	for _, c := range commands {
		if !hasJSONFlag(t, c.Name) {
			continue
		}
		cmd := c.Name
		for _, order := range [][]string{
			{cmd, "--json", "--bogus"},
			{cmd, "--bogus", "--json"},
		} {
			label := strings.Join(order, " ")
			t.Run(label, func(t *testing.T) {
				isolateUserEnv(t)
				env, code, stderrText := runForEnvelope(t, order...)
				checked++
				// 人读那侧**只说一次**：flag 包已经把 usage（含那句 not defined）写到 stderr 了，
				// `failEarly` 不该再说一遍——同一件事说两遍不是"更清楚"，是**噪声**。
				if n := strings.Count(stderrText, "not defined"); n != 1 {
					t.Errorf("stderr 里 `not defined` 出现了 %d 次（想要 1 次）——人读那侧重复了:\n%s",
						n, firstLine(stderrText))
				}
				if env.Error.Code != "Usage" {
					t.Errorf("code = %q，想要 `Usage`（数值仍是 3——名字给人读）", env.Error.Code)
				}
				if env.Error.ExitCode != code {
					t.Errorf("信封说 exitCode=%d，而进程退 %d——机器可读输出在说谎",
						env.Error.ExitCode, code)
				}
				if !strings.Contains(env.Error.Message, "not defined") {
					t.Errorf("message 没说清是哪个 flag 不认识：%q", env.Error.Message)
				}
				if !strings.Contains(env.Error.Hint, "--help") {
					t.Errorf("hint 没有点名一个能照着做的东西：%q", env.Error.Hint)
				}
			})
		}
	}

	// ---- ② 未知命令 ----
	//
	// 注：v0.54 时这里还有一条"只有 `--json`、没有子命令"⇒ message 含 `no subcommand`。
	// v0.55 把根级校验提到子命令检查**之前**之后，`ngm --json` 报的是
	// "unknown root flag: --json"——那个答案更准确（可照着做的是"把 flag 挪到命令之后"），
	// 于是这条移交给 `TestV55…` 拥有，两处不留同一件事的两份判据。
	for _, c := range []struct {
		label string
		args  []string
		want  string
	}{
		{"nosuchcmd --json", []string{"nosuchcmd", "--json"}, "unknown command"},
	} {
		t.Run(c.label, func(t *testing.T) {
			isolateUserEnv(t)
			env, code, _ := runForEnvelope(t, c.args...)
			if env.Error.Code != "Usage" || env.Error.ExitCode != code {
				t.Errorf("信封不合格：%+v（进程退 %d）", env.Error, code)
			}
			if !strings.Contains(env.Error.Message, c.want) {
				t.Errorf("message = %q，想要包含 %q", env.Error.Message, c.want)
			}
			if !strings.Contains(env.Error.Hint, "ngm --help") {
				t.Errorf("hint 没有指到帮助：%q", env.Error.Hint)
			}
		})
	}

	// ---- ③ 对照一：**没提** `--json` ⇒ stdout 必须仍为空 ----
	t.Run("verify --bogus（没提 json）", func(t *testing.T) {
		isolateUserEnv(t)
		var out, errb bytes.Buffer
		code := dispatch([]string{"verify", "--bogus"}, &out, &errb)
		if code == 0 {
			t.Fatalf("未知 flag 竟然退 0")
		}
		if len(bytes.TrimSpace(out.Bytes())) != 0 {
			t.Errorf("用户没提 --json，stdout 却有了 %d 字节——**不许给没要的人塞 JSON**:\n%s",
				out.Len(), out.String())
		}
	})

	// ---- ④ 对照二：命令**不接受** `--json` ⇒ stdout 仍为空（规则④那句仍然成立） ----
	t.Run("install --json（这个命令没有 json）", func(t *testing.T) {
		isolateUserEnv(t)
		p := newProject(t)
		var out, errb bytes.Buffer
		code := dispatch([]string{"install", "--json", "--dir=" + p}, &out, &errb)
		if code == 0 {
			t.Fatalf("`install --json` 竟然退 0")
		}
		if len(bytes.TrimSpace(out.Bytes())) != 0 {
			t.Errorf("命令不认识这个 flag 时 stdout 仍是空的（v0.51 规则④那句没变），而现在有：\n%s",
				out.String())
		}
	})

	if checked == 0 {
		t.Fatalf("没有被查到的命令——这张网的范围缩到零了")
	}
	t.Logf("early failures: %d command×order pair(s) + 2 command-level case(s) carry an envelope", checked)
}

// runForEnvelope 跑一次**应当产出信封**的早期失败，并解析它。
func runForEnvelope(t *testing.T, args ...string) (jsonErrorEnvelope, int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := dispatch(args, &out, &errb)
	if code == 0 {
		t.Fatalf("`ngm %s` 退 0——这不是一条失败路径", strings.Join(args, " "))
	}
	body := bytes.TrimSpace(out.Bytes())
	if len(body) == 0 {
		t.Fatalf("`ngm %s` 在失败时 stdout 是空的——用户明明提了 --json:\n%s",
			strings.Join(args, " "), firstLine(errb.String()))
	}
	var env jsonErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("stdout 不是恰好一份 JSON 文档：%v\n%s", err, out.String())
	}
	return env, code, errb.String()
}
