package main

import (
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.42 / v0.46：**取值 flag 的空值**——必须是"与不给等价"或"明确失败"。
//
// v0.42 立的是这条判据（由 v0.41 的 `--dir=` 缺陷推广而来）：
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
//
// 而 v0.46 修的是**表的来源**：v0.42 用的是一张**手抄**的 map（14 条），
// 而同一个派生在源码里给出 **37 条**——差的 **23 条从来没被这张网看过**。
// 其中一条是 `css --outfile=`：**`css` 这个命令连着两版都在漏**
// （v0.45 漏的是它的**布尔** flag，这一版漏的是它的**取值** flag），
// 而病根是同一个——派生规则按"文件名 == 命令名"找源码，`css` 却住在 `typecheck.go` 里。
//
// 所以这一版把表换成**从源码按块派生**（`flagsetBlocksOf`，跨文件、带声明种类），
// 手抄表**删掉**：它既是缺口，也是"看起来覆盖了"的假象。
// valueFlagPair 是"这张网会跑的一条"：块名 + 它的分段 + flag 名。
type valueFlagPair struct {
	block string
	parts []string
	flag  string
}

// valueFlagPairs 是这张网**实际会跑**的表——也是 v0.46 的覆盖守卫核对**取值**一侧时用的表。
//
// 守卫调它、网也调它，**不是各写一遍规则**：v0.45 的第一版守卫就是自己另写了一套
// （核对名字而不是覆盖路径），于是它放过了 `css` 的两个 flag。
// 这一版把"哪些 flag 归哪张网"收敛成**唯一一份选择函数**。
func valueFlagPairs(t *testing.T) []valueFlagPair {
	t.Helper()
	known := map[string]bool{}
	for _, c := range commands {
		known[c.Name] = true
	}
	var out []valueFlagPair
	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			parts := strings.Fields(b.name)
			if len(parts) == 0 || !known[parts[0]] {
				continue // 不属于任何命令的 flagset：由 v0.45 的覆盖并集守卫报出来
			}
			for _, f := range b.valueFlags() {
				out = append(out, valueFlagPair{block: b.name, parts: parts, flag: f})
			}
		}
	}
	return out
}

func TestV42EmptyValueNeverSilentlyChangesBehaviour(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	refused, equivalent, derived := 0, 0, 0

	for _, p := range valueFlagPairs(t) {
		parts, flag := p.parts, p.flag
		{
			derived++
			label := p.block + " --" + flag
			t.Run(label+"=", func(t *testing.T) {
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
					if parts[0] == "transform" || parts[0] == "css" || parts[0] == "build" {
						writeSurfaceFile(t, proj, "in.ts", "export const a = 1\n")
						writeSurfaceFile(t, proj, "a.css", "a{color:red}\n")
					}
					// 命令层：参数用 matrixArgs 那套"语法上够用"的底子。
					// 子命令层：底子就是块名本身（`store prune`）——matrixArgs 里
					// 那个 `store` 的 `usage` 会变成 `store usage prune`（错的）。
					args := []string{}
					if len(parts) == 1 {
						args = append(args, withoutFlag(matrixArgs[parts[0]], "--"+flag)...)
					}
					args = append(append([]string{}, parts...), args...)
					if parts[0] == "transform" {
						args = append(args, "in.ts")
					}
					if withEmpty {
						args = append(args, "--"+flag+"=")
					}
					if !globalCommands[parts[0]] {
						args = append(args, "--dir="+proj)
					}
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
					t.Logf("%-22s empty --%s= ≡ 不给（两者都成功且输出相同）", p.block, flag)
				case cEmpty != 0:
					refused++
					t.Logf("%-22s empty --%s= 被拒绝（exit %d: %s）",
						p.block, flag, cEmpty, firstLine(oEmpty))
				default:
					t.Errorf("`%s=` 成功了，而**不给**它却失败（exit %d）——说明空值触发了另一条路:\n  %s",
						flag, cPlain, firstLine(oPlain))
				}
			})
		}
	}

	// 守卫① **结构性**：派生出的每一条都必须跑过——不依赖任何我拍的数。
	if ran := refused + equivalent; ran != derived {
		t.Fatalf("the derived table has %d value flag(s) but %d ran — "+
			"some flag neither ran nor was accounted for", derived, ran)
	}
	// 守卫② **规模下限**：实测 37 条；表被改窄（或派生又漂了）时这条会响。
	if derived < 30 {
		t.Fatalf("only %d value flag(s) were derived — the table shrank", derived)
	}
	// 守卫③ **可达性**：两种合格处置都要有样本。
	// 若哪天所有空值都被拒绝（或全都被当成"没给"），这张网会退化成只查一件事。
	if refused == 0 || equivalent == 0 {
		t.Fatalf("both dispositions must exist: refused=%d equivalent=%d — "+
			"one of them vanishing means the table no longer covers what it claims", refused, equivalent)
	}
	t.Logf("empty value: %d flag(s) derived from source → %d refused, %d equivalent to omitting it; "+
		"no run touched the CWD", derived, refused, equivalent)
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
