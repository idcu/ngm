package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.47：**两 token 的写法**——同一个值的两种拼法必须是一回事。
//
// v0.41 / v0.42 管的是"一个 token 里的写法"（`--dir=` 的空值语义），
// 这一版问的是**两种 token 数**：
//
//	取值 flag：`--flag=`（一个 token）与 `--flag ""`（两个 token）必须**完全等价**
//	布尔 flag：`--flag` 后面那个空 token 是**位置参数**，它不能被**过滤掉**——
//	          于是它与 `--flag ZZ-JUNK` 必须**同处置**（同退出码）
//
// 为什么布尔那一条是这么写的（v0.47 实测）：探针第一版的判据是"`--flag ""`
// 必须**不等于**只给 `--flag`"，结果一堆命令被判红——而红的不是产品：
// 那些命令的 base 运行**本来就失败**（`verify` 在没有锁的项目里退 3），
// 三者自然一模一样。**"与 base 相同"在 base 失败时什么也证明不了。**
//
// 改成"与 `--flag ZZ-JUNK` 同处置"之后，两种状态判据都有意义：
// 命令不接受多余位置参数 ⇒ 两者都失败（且必须是同一个退出码）；忽略 ⇒ 两者都成功。
// 它挡的是**空 token 被当噪声滤掉**——那时 `--flag ""` 会退化成"只给 `--flag`"，
// 与 `--flag ZZ-JUNK` 的处置不同，判据立刻响。
//
// 表**从源码派生**（`valueFlagPairs` / `boolFlagsOfCommand` / `subcommandBoolPairs`），
// 与 v0.42 / v0.43 / v0.45 共用同一批选择函数——不手抄第二份。
func TestV47TwoTokenSpellingsAreTheSameThing(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// run 造一份全新世界跑一次：命令/子命令 + 底子（只用命令层那套 matrixArgs）+ 尾巴。
	run := func(t *testing.T, parts []string, flag string, extra ...string) (int, string) {
		t.Helper()
		home := isolateUserEnv(t)
		proj := newProject(t)
		if parts[0] == "transform" || parts[0] == "css" || parts[0] == "build" {
			writeSurfaceFile(t, proj, "in.ts", "export const a = 1\n")
			writeSurfaceFile(t, proj, "a.css", "a{color:red}\n")
		}
		args := []string{}
		if len(parts) == 1 {
			args = append(args, withoutFlag(matrixArgs[parts[0]], "--"+flag)...)
		}
		args = append(append([]string{}, parts...), args...)
		if parts[0] == "transform" {
			args = append(args, "in.ts")
		}
		args = append(args, extra...)
		if !globalCommands[parts[0]] {
			args = append(args, "--dir="+proj)
		}
		c, out := runCaptureCode(t, args...)
		// 项目目录与隔离 home 都要归一化（v0.47 实测到的唯一一处红，
		// 与本项目反复踩的同一个坑：**红的是我的夹具**）。
		// v0.51 起统一走共享助手——它同时处理 JSON 的转义形态。
		return c, normalizeRunPaths(t, out, proj, home)
	}

	// ---- 第一组：取值 flag —— `--flag ""` 与 `--flag=` 必须完全等价 ----
	//
	// **判据的边界（实测，v0.47）**：这一组对"空 token 被滤掉"那一类突变**不敏感**——
	// 让 `normalizeArgs` 丢掉空 token 时，它一条也不响（响的是下面布尔那一组的 13 条）。
	// 原因是 Go 的 flag 包把 `--flag ""` 与 `--flag=` 归一到同一件事，重排器的差异
	// 落不到最终行为上。
	//
	// 那它为什么还留着：它钉住的是一条**对外承诺**（两种拼法是一回事），
	// 而成本是 74 次进程内运行（约 0.6 秒）。它挡的是**将来换掉 Go 的 flag 包**
	// 或自己写参数解析时的那一天——那一天它就有牙齿了。
	// 记在这里，是为了让下一个读到它的人知道：**绿不等于它在盯着什么**。
	valuePairs := 0
	for _, p := range valueFlagPairs(t) {
		valuePairs++
		t.Run("value/"+p.block+"/--"+p.flag, func(t *testing.T) {
			cEq, oEq := run(t, p.parts, p.flag, "--"+p.flag+"=")
			cTwo, oTwo := run(t, p.parts, p.flag, "--"+p.flag, "")
			if cEq != cTwo || oEq != oTwo {
				t.Errorf("`--%s=` 与 `--%s \"\"` 不是一回事（exit %d vs %d）——"+
					"同一个 flag、同一个值，只有 token 数不同:\n  一个 token:\n%s\n  两个 token:\n%s",
					p.flag, p.flag, cEq, cTwo, oEq, oTwo)
			}
		})
	}

	// ---- 第二组：布尔 flag —— 空 token 必须与"非空位置参数"同处置 ----
	boolPairs := 0
	checkBool := func(t *testing.T, label string, parts []string, flag string) {
		t.Helper()
		t.Run(label, func(t *testing.T) {
			cEmpty, oEmpty := run(t, parts, flag, "--"+flag, "")
			cJunk, oJunk := run(t, parts, flag, "--"+flag, "ZZ-JUNK")
			if cEmpty != cJunk {
				t.Errorf("`--%s \"\"` 与 `--%s ZZ-JUNK` 的处置不同（exit %d vs %d）——"+
					"两者都是「布尔 flag 后面跟一个位置参数」，空 token 不该被滤掉:\n  %s\n  %s",
					flag, flag, cEmpty, cJunk, firstLine(oEmpty), firstLine(oJunk))
			}
		})
	}
	for _, c := range commands {
		for _, flag := range boolFlagsOfCommand(t, c.Name) {
			boolPairs++
			checkBool(t, "bool/"+c.Name+"/--"+flag, []string{c.Name}, flag)
		}
	}
	for _, b := range subcommandBoolPairs(t) {
		parts := strings.Fields(b.name)
		for _, flag := range b.boolFlags() {
			boolPairs++
			checkBool(t, "bool/"+b.name+"/--"+flag, parts, flag)
		}
	}

	// 守卫① 结构性：派生表的条数必须与实际跑过的一致（不依赖任何我拍的数）。
	if want := len(valueFlagPairs(t)); valuePairs != want {
		t.Fatalf("value pairs: ran %d of %d", valuePairs, want)
	}
	if got, want := boolPairs, boolPairsTotal(t); got != want {
		t.Fatalf("bool pairs: ran %d of %d", got, want)
	}
	// 守卫② 规模下限（实测 37 / 42）。
	if valuePairs < 30 || boolPairs < 35 {
		t.Fatalf("the derived tables shrank: value=%d bool=%d", valuePairs, boolPairs)
	}
	t.Logf("two-token spellings: %d value flag(s) {=, \"\"} equivalent · "+
		"%d bool flag(s) {\"\", ZZ-JUNK} same disposition", valuePairs, boolPairs)
}

// boolPairsTotal 是两张布尔网加起来会跑的条数（守卫用同一个口径）。
func boolPairsTotal(t *testing.T) int {
	t.Helper()
	n := 0
	for _, c := range commands {
		n += len(boolFlagsOfCommand(t, c.Name))
	}
	for _, b := range subcommandBoolPairs(t) {
		n += len(b.boolFlags())
	}
	return n
}
