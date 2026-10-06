package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.39：**参数维度**。
//
// v0.37 / v0.38 枚举的是"命令 × 配置错误形状"。这一版补上另一个维度：
//
//	no-args ：`ngm <cmd>`（不给位置参数）——用户敲了个命令就回车
//	junk-arg：`ngm <cmd> <正常参数…> ZZ-JUNK-ARG`——多了一个没人认识的参数
//
// 各自的契约：
//
//	**no-args** ：非零 + **良构的用法文本**（或带建议的错误）。
//	              用法文本是"接下来做什么"在这条路上的答案：它告诉你怎么敲。
//	**junk-arg**：那个多余参数必须**被看见**——要么被拒绝（用法），要么被用（并因此失败）。
//	              **静默忽略一个参数是这一族里最坏的结果**：用户以为自己筛了依赖/改了行为。
//
// 这就是 v0.19 修过的那一类（`config validate` 把 `--dir=x` 当位置参数静默丢掉）。
// 那一次只修了一个命令；这一版把它变成**全命令不变式**。
//
// 另外一处**测量设计**上的教训：多余参数这一栏必须用**合法项目**。
// 第一版用"目录不存在"，于是配置错误掩盖了参数处理——
// `engines list ZZ-JUNK` 退的是"找不到 ngm.json"，看不出那个参数被怎么了。
// 换成合法项目后才看清：它**确实**拒绝了（打用法）。
func TestV39ArgumentDimensionHoldsItsContracts(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	counts := map[string]int{}
	zeroCells := []string{}
	runs := 0

	for _, spec := range commands {
		for _, sh := range []string{"no-args", "junk-arg"} {
			runs++
			t.Run(spec.Name+"/"+sh, func(t *testing.T) {
				isolateUserEnv(t)
				args := []string{spec.Name}
				if sh == "junk-arg" {
					args = append(args, matrixArgs[spec.Name]...)
					args = append(args, "ZZ-JUNK-ARG")
				}
				if !globalCommands[spec.Name] {
					dir := filepath.Join(t.TempDir(), "nope")
					if sh == "junk-arg" {
						// 合法项目：见文件头那条测量设计上的教训。
						dir = newProject(t)
					}
					args = append(args, "--dir="+dir)
				}

				var out, errb bytes.Buffer
				code := dispatch(args, &out, &errb)
				stderr, text := errb.String(), out.String()+errb.String()
				body := reportBody(text)

				switch {
				case code == 0:
					counts["exit-0"]++
					zeroCells = append(zeroCells, spec.Name+"/"+sh)
					if _, ok := exitZeroOnArguments[spec.Name+"/"+sh]; !ok {
						t.Errorf("this invocation exited 0 (a %s invocation) and nothing here says why — "+
							"for `junk-arg` a zero would mean **an argument nobody understands was ignored**",
							sh)
					}
				case strings.TrimSpace(text) == "":
					counts["silent"]++
					t.Errorf("exited %d without saying anything", code)
				case strings.Contains(text, "USAGE:"):
					counts["usage"]++
					// 用法文本是这条路上的"接下来做什么"——它必须**是这一个命令的**用法。
					//
					// 判据经实测收窄过一次：初版要求同时有 `USAGE:` 与 `FLAGS:` 两段，
					// 而 `cache` / `integrations` / `store` **本来就没有 flag**
					// （`store` 的只有 `PRUNE FLAGS:` 那样的小节）。
					// 现实支持的那一条是：**首行要点名这个命令**（`ngm <cmd> — …`）——
					// 它挡的是"打错了一个命令的用法"，而不是"某段标题不在"。
					if first := firstLine(text); !strings.HasPrefix(first, "ngm "+spec.Name+" ") {
						t.Errorf("the text printed is a usage, but not **this** command's usage:\n  %s", first)
					}
				case reErrPrefix.MatchString(stderr):
					counts["error-text"]++
					if !strings.Contains(stderr, "hint:") {
						t.Errorf("this error gives no next step:\n%s", stderr)
					}
				case strings.Contains(body, failMark):
					counts["report"]++
					if len(reActionLine.FindAllString(body, -1)) == 0 {
						t.Errorf("this failing report says nothing about what to do next:\n%s", text)
					}
				default:
					counts["unclassified"]++
					t.Errorf("this output fits none of the known channels:\n%s", firstLine(text))
				}
			})
		}
	}

	// 双向对账：登记表必须与**实测**一致（登记会过期）。
	for cell := range exitZeroOnArguments {
		found := false
		for _, c := range zeroCells {
			if c == cell {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("exitZeroOnArguments lists %q, but that invocation did not exit 0 — "+
				"the registration went stale, drop it (or find out what changed)", cell)
		}
	}

	// 可达性守卫：三条主要通道都要有样本，否则这张网会静默地只查一小块。
	if runs < 42 {
		t.Fatalf("only %d runs — the argument dimension shrank", runs)
	}
	if counts["usage"] < 18 || counts["error-text"] < 12 {
		t.Fatalf("the channel mix changed drastically (%v) — one path may have swallowed the others", counts)
	}
	t.Logf("argument dimension: %d runs over %d commands × 2 shapes → %v (registered zeros: %d)",
		runs, len(commands), counts, len(exitZeroOnArguments))
}

// exitZeroOnArguments 登记"退了 0"的参数形状。
//
// **目前为空，而这正是它该有的样子**（与 v0.26 的豁免清单同款）：
// 实测 42 次运行全部非零——需要参数的命令打用法、不需要的走到配置层并失败，
// 而"多余参数"没有一次被静默忽略。
//
// 留这张表是为了让"某个命令真的可以在这两种形状下退 0"有一个**被看见的位置**：
// 一旦有人往里加东西，diff 里就会多一行带理由的登记。
var exitZeroOnArguments = map[string]string{}
