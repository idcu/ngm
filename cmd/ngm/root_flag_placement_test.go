package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.55：**子命令之前的 flag 不许被静默丢弃**。
//
// 实测（v0.54 的探针发现、本版量清）：`dispatch` 只把 `--version` / `-h` / `--help`
// 认作根级 flag，其余进 `head` 之后**没有下文**——而只有 `head` **之外**的参数
// 才交给子命令。于是在一个有 lock 的项目上：
//
//	$ ngm --json verify
//	exit 0 · stdout 是**人读文本**（91 字节）· stderr 0 字节
//
// 这是最坏的一种失败：**走错通道、还一声不响**。要 JSON 的脚本拿到散文，
// 退出码还是 0——它连"出事了"都不知道。
//
// 处置：根级只认那三个（cli.md 的既有契约），其余**一律拒绝**，
// 并且拒绝时把 **v0.54 的信封**带上（用户提了 `--json` 的话）——
// 于是连"你把 flag 写错位置了"也是机器可读的。
//
// 判据：
//
//	① 四种形状的根级未知 token 都要 exit 3（`--json` / 未知 flag / 带值的 flag / 裸 `--`）；
//	② 提了 `--json` 的那个要给**信封**，且 hint 点名"写在命令**之后**"并给例子；
//	③ **顺序无关**：`--version --bogus` 与 `--bogus --version` 结果必须一致
//	   （校验与动手分两个循环——"顺序敏感"不是一条能记住的规则）；
//	④ **对照不许变**：`--version` / `--help` 仍 exit 0；`verify --json` 的正确位置仍然好用。
func TestV55RootLevelTakesOnlyHelpAndVersion(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// ---- 夹具：一个**能成功**的项目，且只建一次 ----
	//
	// 为什么必须能成功：静默丢弃时 `verify` 会**照常跑完**（exit 0 · 人读文本）。
	// 若夹具是个空项目，`verify` 自己也退 3——那"退 3"这条断言就分不出
	// "拒绝了"与"命令自己失败了"（牙齿 A 实测发现的第一版弱点）。
	isolateUserEnv(t)
	proj := newProject(t)
	scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
	if code, out := runCaptureCode(t, "add", "github:x/dep@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	// ---- ① + ② 几种形状 ----
	shapes := []struct {
		label string
		args  []string
		// whole 为真时原样使用 args（不再补上子命令）——用于"根本没有子命令"那条。
		whole bool
	}{
		{label: "--json", args: []string{"--json", "verify"}},
		{label: "--bogus", args: []string{"--bogus", "verify"}},
		{label: "--dir=<p>", args: []string{"--dir=@p", "verify"}},
		{label: "--（裸分隔符）", args: []string{"--", "verify"}},
		{label: "--json（连子命令都没有）", args: []string{"--json"}, whole: true},
	}
	for _, s := range shapes {
		t.Run("根级 "+s.label, func(t *testing.T) {
			args := make([]string, len(s.args))
			for i, a := range s.args {
				args[i] = strings.ReplaceAll(a, "@p", proj)
			}
			if !s.whole {
				args = append(args, "--dir="+proj)
			}

			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			if code != 3 {
				t.Errorf("exit = %d，想要 3（根级用法错误）——**静默丢弃**回来了?"+
					"（丢弃时 verify 会照常成功，于是这里多半是 0）", code)
			}
			if !strings.Contains(errb.String(), "unknown root flag") {
				t.Errorf("stderr 没有说清是哪个 token 不认识:\n%s", firstLine(errb.String()))
			}
			// 人读那侧说了，机器那侧只有"用户提了 --json"时才该有东西。
			if strings.Contains(s.label, "json") {
				body := bytes.TrimSpace(out.Bytes())
				var env jsonErrorEnvelope
				if err := json.Unmarshal(body, &env); err != nil {
					t.Fatalf("提了 --json 的根级错误没有给信封（%v）:\n%s", err, out.String())
				}
				if env.Error.Code != "Usage" || env.Error.ExitCode != code {
					t.Errorf("信封不合格：%+v（进程退 %d）", env.Error, code)
				}
				if !strings.Contains(env.Error.Hint, "after the command") {
					t.Errorf("hint 没有点名该写在**哪儿**：%q", env.Error.Hint)
				}
				if !strings.Contains(env.Error.Hint, "ngm verify --json") {
					t.Errorf("hint 没有给一个能照着抄的例子：%q", env.Error.Hint)
				}
			} else if len(bytes.TrimSpace(out.Bytes())) != 0 {
				t.Errorf("用户没提 --json，stdout 却有了东西:\n%s", out.String())
			}
		})
	}

	// ---- ③ 顺序无关 ----
	t.Run("顺序无关", func(t *testing.T) {
		isolateUserEnv(t)
		var results []int
		for _, args := range [][]string{
			{"--version", "--bogus"},
			{"--bogus", "--version"},
		} {
			var out, errb bytes.Buffer
			results = append(results, dispatch(args, &out, &errb))
		}
		if results[0] != results[1] {
			t.Errorf("`--version --bogus` 退 %d，而 `--bogus --version` 退 %d——"+
				"同一条命令的两种写法给了不同结果，而「顺序敏感」不是一条能记住的规则",
				results[0], results[1])
		}
		if results[0] != 3 {
			t.Errorf("两个顺序都该退 3（有 token 不认识），实际 %d", results[0])
		}
	})

	// ---- ④ 对照不许变（**从运行时表取**，不手抄白名单） ----
	//
	// `rootFlags` 是 `dispatch` 校验时读的那张表——判据与实现读同一个来源。
	if len(rootFlags) == 0 {
		t.Fatal("根级白名单是空的——这条判据没有可查的对象")
	}
	t.Run("对照：白名单里的每个 token 都仍然被接受", func(t *testing.T) {
		for tok := range rootFlags {
			var out, errb bytes.Buffer
			if code := dispatch([]string{tok}, &out, &errb); code != 0 {
				t.Errorf("`ngm %s` 退 %d，想要 0（它就在白名单里）", tok, code)
			}
			if out.Len() == 0 {
				t.Errorf("`ngm %s` 什么都没打印", tok)
			}
		}
	})

	// ---- ⑤ 拒绝一侧的**样本**从派生 flag 表取（不手写） ----
	//
	// 取每个命令的第一个布尔 flag，把它放到命令**之前**：它一定不是根级 token
	// （白名单只有那三个），所以必须被拒绝。这样"该被拒绝的东西"也是从源码派生的。
	sampled := 0
	for _, c := range commands {
		flags := boolFlagsOfCommand(t, c.Name)
		if len(flags) == 0 {
			continue
		}
		tok := "--" + flags[0]
		if rootFlags[tok] {
			continue
		}
		sampled++
		t.Run("派生的样本 "+c.Name+" 的 "+tok, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := dispatch([]string{tok, c.Name, "--dir=" + proj}, &out, &errb)
			if code != 3 {
				t.Errorf("`ngm %s %s` 退 %d，想要 3——命令的 flag 写在命令之前，必须被拒绝",
					tok, c.Name, code)
			}
			if !strings.Contains(errb.String(), "unknown root flag") {
				t.Errorf("拒绝的理由没说清:\n%s", firstLine(errb.String()))
			}
			// 机器那侧照同一条规则：**用户提了 `--json`** 才该有信封。
			// 派生样本里 `why` / `engines` 的第一个布尔 flag 正好就是 `--json`——
			// 这条分支不是为它们特设的，它是"同一件事在两种输入下的同一条规则"。
			if tok == "--json" || tok == "-json" {
				var env jsonErrorEnvelope
				if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &env); err != nil {
					t.Errorf("提了 --json 的拒绝没有给信封（%v）:\n%s", err, out.String())
				}
			} else if len(bytes.TrimSpace(out.Bytes())) != 0 {
				t.Errorf("用户没提 --json，stdout 却有了东西:\n%s", out.String())
			}
		})
	}
	if sampled == 0 {
		t.Fatal("派生的样本是空的——这条判据的范围缩到零了")
	}

	t.Run("对照：正确位置（json 在命令之后）", func(t *testing.T) {
		var out, errb bytes.Buffer
		code := dispatch([]string{"verify", "--json", "--dir=" + proj}, &out, &errb)
		if code != 0 {
			t.Fatalf("正确的写法竟然退 %d:\n%s", code, errb.String())
		}
		if !strings.HasPrefix(strings.TrimSpace(out.String()), "{") {
			t.Errorf("正确的写法没给 JSON 报告，stdout 是:\n%s", out.String())
		}
	})

	t.Logf("root level: %d unknown-token shape(s) refused · order-independent · controls unchanged",
		len(shapes))
}
