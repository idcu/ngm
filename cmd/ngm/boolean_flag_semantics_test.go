package main

import (
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.43：**布尔 flag 的三条语义**。
//
// 依据有两处，都是产品**自己写下的**：
//
//  1. Go 标准库 flag 对布尔 flag 的语义：`--flag` ≡ `--flag=true`；`--flag=false` 等同于
//     **不给**它；`--flag=x`（不是 true/false）是**错误**；而布尔 flag **从不消费下一个 token**。
//  2. `normalizeArgs` 的注释：「`--all`（布尔）**绝不能吞掉**紧随其后的 positional」——
//     它存在的理由就是"重排时别把位置参数吃掉"。
//
// 于是四条断言：
//
//	① `--flag=true` ≡ `--flag`（逐字节相同）
//	② `--flag=false` ≡ 不给这个 flag（逐字节相同）
//	③ `--flag=x` **必须失败**（Go 说这是非法布尔值；静默当成 true 是最坏的结果）
//	④ `--flag ZZ-JUNK` **必须失败**（那个 token 是 positional，不是 flag 的值——
//	   若被吞掉，命令会像"只给了 --flag"一样跑下去，而那是**静默丢了一个参数**）
//
// 表**从源码派生**，不手抄。
//
// 为什么（v0.43 实测）：初版是我凭记忆写的一张 map，里面有几条**根本不存在的组合**
// （`remove --force`、`typecheck --json`、`css --sourcemap` …）——于是判据把
// 「flag provided but not defined」当成"空值不等价"报了出来，红的是**我的表**。
// 这与 v0.33 那条经验同源：**核对对象应来自源码/运行时，而不是我抄的一份常数**。
//
// 派生方式（v0.45 修正）：**按 flagset 块**取"属于这个命令自己那一块"的布尔 flag，
// 并且**在所有源文件里找**——不是只看 `<命令名>.go`。
//
// 两处修正，都是实测出来的：
//
//	① v0.43 原版是"整个文件里的 `fs.Bool`"，而有的文件里住着**不止一个命令**——
//	   `typecheck.go` 里既有 `typecheck` 也有 `css`。于是 `typecheck --minify` 被派了出来，
//	   运行时报 "flag provided but not defined"，被当成"它属于某个子命令"**跳过**。
//	   真相是我的派生规则太粗。
//	② 而按"文件名 == 命令名"去找，`css` 就**一条也派生不出来**（没有 `css.go`，
//	   它住在 `typecheck.go` 里）——也就是说 `css --minify` / `css --dry-run`
//	   **从来没有被任何一张网看过**。这是 v0.45 修 ① 时顺带发现的一个更大的洞：
//	   修 ① 之前它被 `typecheck` 那条**假跳过**遮住了（看起来"测过了，只是跳过了"）。
//
// 现在按块、跨文件：名字等于命令名的块归这张网，
// 名字带空格的块（`store prune`）归 v0.45 的 `TestV45…`，
// 其余的由它那条**覆盖并集**守卫抓（那条守卫刻意用与这里同一条路去找，
// 而不是核对名字——第一版就是核对名字，于是没抓到 ②）。
func boolFlagsOfCommand(t *testing.T, cmd string) []string {
	t.Helper()
	for _, file := range sourceFiles(t) {
		for _, b := range flagsetBlocksOf(t, file) {
			if b.name == cmd {
				return b.boolFlags()
			}
		}
	}
	return nil
}

func TestV43BooleanFlagsHoldGoSemantics(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	checked := 0
	for _, spec := range commands {
		flags := boolFlagsOfCommand(t, spec.Name)
		if len(flags) == 0 {
			continue
		}
		for _, flag := range flags {
			// 全局命令不接受 --dir：只跳过 `--dir`，命令本身仍要测。
			t.Run(spec.Name+"/--"+flag, func(t *testing.T) {
				// run 造一份**全新**的世界跑一次（夹具状态耦合是本项目的老坑）。
				run := func(t *testing.T, extra ...string) (int, string) {
					t.Helper()
					home := isolateUserEnv(t)
					proj := newProject(t)
					if spec.Name == "transform" || spec.Name == "css" || spec.Name == "build" {
						writeSurfaceFile(t, proj, "in.ts", "export const a = 1\n")
						writeSurfaceFile(t, proj, "a.css", "a{color:red}\n")
					}
					args := append([]string{spec.Name}, withoutFlag(matrixArgs[spec.Name], "--"+flag)...)
					if spec.Name == "transform" {
						args = append(args, "in.ts")
					}
					args = append(args, extra...)
					if !globalCommands[spec.Name] {
						args = append(args, "--dir="+proj)
					}
					c, out := runCaptureCode(t, args...)
					// 两次 run 各自 isolate 到**不同的** temp 目录，而输出里会印出
					// 本次生成的路径（v0.51：错误信封里的锁文件路径，且在 JSON 里是转义的）——
					// 统一走共享助手，别在每个网里各补一次（那是同一个形状的第四次）。
					return c, normalizeRunPaths(t, out, proj, home)
				}

				// 四条断言与 v0.45（子命令层）**共用**同一个函数：两层对同一件事
				// 只能有一套判据，否则两份迟早会各自漂移。
				checkBoolFlagSemantics(t, spec.Name, flag, run)
				checked++
			})
		}
	}

	// 覆盖守卫分两层。
	//
	// ① **结构性**：派生表里的每一对都必须跑完——两边数目必须相等。
	//
	//    v0.43 时右边还有一个"被公开跳过"的加数（3 对属于子命令的 flagset）。
	//    v0.45 把派生改成**按块**之后跳过归零：子命令层的 2 对由 `TestV45…` 覆盖，
	//    而同一文件里第二个命令的 flag（`typecheck --minify`，其实是 `css` 的）
	//    不再被算错到第一个命令头上。这条现在不依赖任何我拍的阈值。
	expected := 0
	for _, spec := range commands {
		expected += len(boolFlagsOfCommand(t, spec.Name))
	}
	if checked != expected {
		t.Fatalf("the derived table has %d pair(s) but %d ran — some pair was neither run nor "+
			"accounted for elsewhere (subcommand-layer flags belong to TestV45…)", expected, checked)
	}
	// ② **规模**：跑完的必须够多（否则这张表会退化成"什么都跑不了"）。
	//    35 是实测值 **38** 之下的一个下限，不是估的。
	if checked < 35 {
		t.Fatalf("only %d flag(s) were fully checked — the derived table no longer "+
			"covers what it claims", checked)
	}
	t.Logf("boolean flags: %d × 4 assertions (① =true ≡ --flag · ② =false ≡ 不给 · ③ =x 被拒 · ④ 不吞 token); "+
		"skipped: 0（子命令层的 flag 归 TestV45… 的网）", checked)
}
