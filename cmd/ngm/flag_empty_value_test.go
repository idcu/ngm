package main

import (
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.42：**取值 flag 的空值**——把 v0.41 的教训推广到 `--dir` 之外。
//
// v0.41 修的是 `--dir=`：空串与"没给这个 flag"无法区分，于是命令静默把空值当成 CWD，
// 结果是"改了别的项目的文件"。这一版问：其余取值 flag 呢？
//
// 实测（见 §1）：**没有一处再写 CWD**，而空值大多被**明确拒绝**。
// 于是判据写成一条更普适、也更能长期看住的规则：
//
//	**显式空值要么与"没给这个 flag"完全等价，要么失败——
//	绝不能"两个都成功、结果却不同"。**
//
// 为什么是这条："两个都成功但结果不同"正是**静默改变了行为**的定义
// （用户以为自己在设一个值，实际触发了另一条路），而那正是 v0.41 那个缺陷的形状。
// 至于"空值被明确拒绝"（如 `--runtime=` → `invalid --runtime=""`）——那是**合格**的处置，
// 因为用户当场就知道了。
//
// 另一条不变量（v0.41 的血）：**任何一次运行都不许在 CWD 里留下东西**。
// 判据把 CWD 换成一个空目录，跑完检查它是否还是空的。
var valueFlagsEmptyValue = map[string][]string{
	"add":       {"--ref-type", "--path"},
	"audit":     {"--hook"},
	"build":     {"--engine", "--outfile"},
	"transform": {"--engine", "--loader", "--target", "--format", "--outfile"},
	"typedecl":  {"--outdir", "--engine"},
	"typecheck": {"--engine"},
	"css":       {"--engine"},
	"init":      {"--runtime"},
}

func TestV42EmptyValueNeverSilentlyChangesBehaviour(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	refused, equivalent := 0, 0

	for _, spec := range commands {
		flags := valueFlagsEmptyValue[spec.Name]
		for _, flag := range flags {
			t.Run(spec.Name+flag+"=", func(t *testing.T) {
				isolateUserEnv(t)
				scratch := t.TempDir()
				old, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chdir(scratch); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chdir(old) })

				run := func(t *testing.T, withEmpty bool) (int, string) {
					t.Helper()
					isolateUserEnv(t)
					proj := newProject(t)
					if spec.Name == "transform" || spec.Name == "css" || spec.Name == "build" {
						writeSurfaceFile(t, proj, "in.ts", "export const a = 1\n")
						writeSurfaceFile(t, proj, "a.css", "a{color:red}\n")
					}
					args := append([]string{spec.Name}, withoutFlag(matrixArgs[spec.Name], flag)...)
					if spec.Name == "transform" {
						args = append(args, "in.ts")
					}
					if withEmpty {
						args = append(args, flag+"=")
					}
					args = append(args, "--dir="+proj)
					c, out := runCaptureCode(t, args...)
					return c, strings.ReplaceAll(out, proj, "<proj>")
				}

				cEmpty, oEmpty := run(t, true)
				cPlain, oPlain := run(t, false)

				// 不变量一：**CWD 必须原封不动**（v0.41 的教训）。
				if entries, err := os.ReadDir(scratch); err == nil && len(entries) > 0 {
					names := []string{}
					for _, e := range entries {
						names = append(names, e.Name())
					}
					t.Errorf("这一次运行在**当前目录**里留下了 %v —— 命令不该碰 CWD", names)
				}

				// 不变量二：两个都成功就必须**一模一样**。
				switch {
				case cEmpty == 0 && cPlain == 0 && oEmpty != oPlain:
					t.Errorf("`%s=` 与不给这个 flag **都成功、结果却不同**——"+
						"这正是「静默改变行为」的定义:\n  带空值: %s\n  不带:   %s",
						flag, firstLine(oEmpty), firstLine(oPlain))
				case cEmpty == 0 && cPlain == 0:
					equivalent++
					t.Logf("empty %s= ≡ 不给（两者都成功且输出相同）", flag)
				case cEmpty != 0:
					refused++
					t.Logf("empty %s= 被拒绝（exit %d: %s）", flag, cEmpty, firstLine(oEmpty))
				default:
					t.Errorf("`%s=` 成功了，而**不给**它却失败（exit %d）——说明空值触发了另一条路:\n  %s",
						flag, cPlain, firstLine(oPlain))
				}
			})
		}
	}

	// 可达性守卫：两种合格处置都要有样本。
	// 若哪天所有空值都被拒绝（或全都被当成"没给"），这张网会退化成只查一件事。
	total := refused + equivalent
	if total < 12 {
		t.Fatalf("only %d flag(s) were exercised — the table shrank", total)
	}
	if refused == 0 || equivalent == 0 {
		t.Fatalf("both dispositions must exist: refused=%d equivalent=%d — "+
			"one of them vanishing means the table no longer covers what it claims", refused, equivalent)
	}
	t.Logf("empty value: %d flag(s) → %d refused, %d equivalent to omitting it; no run touched the CWD",
		total, refused, equivalent)
}

// withoutFlag 从参数表里去掉某个 flag 的取值（用来造"不给这个 flag"的对照）。
//
// 参数表里用的是 `--flag=value` 形式，所以按前缀去掉即可；两 token 形式这里用不到。
func withoutFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			continue
		}
		out = append(out, a)
	}
	return out
}
