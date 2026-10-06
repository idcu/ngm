package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
// 派生方式：`fs.Bool("name"` 出现在 `cmd/ngm/<command>.go` 里。
// 子命令文件（cache / store / config / …）里可能有**属于某个子命令**的 flag，
// 那种组合在运行时会报 "flag provided but not defined" —— 由调用方**公开跳过并计数**。
func boolFlagsFromSource(t *testing.T, cmd string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "cmd", "ngm", cmd+".go"))
	if err != nil {
		return nil // 没有同名文件：这个命令的 flag 不在这套派生规则里
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range reBoolFlagDecl.FindAllStringSubmatch(string(data), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

var reBoolFlagDecl = regexp.MustCompile(`fs\.Bool\("([a-z][a-z-]*)"`)

func TestV43BooleanFlagsHoldGoSemantics(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	checked, skipped := 0, 0
	for _, spec := range commands {
		flags := boolFlagsFromSource(t, spec.Name)
		if len(flags) == 0 {
			continue
		}
		for _, flag := range flags {
			// 全局命令不接受 --dir：只跳过 `--dir`，命令本身仍要测。
			t.Run(spec.Name+"/--"+flag, func(t *testing.T) {
				// run 造一份**全新**的世界跑一次（夹具状态耦合是本项目的老坑）。
				run := func(t *testing.T, extra ...string) (int, string) {
					t.Helper()
					isolateUserEnv(t)
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
					return c, strings.ReplaceAll(out, proj, "<proj>")
				}

				plain, oPlain := run(t, "--"+flag)          // --flag
				yes, oYes := run(t, "--"+flag+"=true")      // --flag=true
				off, oOff := run(t, "--"+flag+"=false")     // --flag=false
				base, oBase := run(t)                       // 不给
				bad, oBad := run(t, "--"+flag+"=x")         // --flag=x
				junk, oJunk := run(t, "--"+flag, "ZZ-JUNK") // 布尔 flag 后的 token

				// 派生规则会把**属于某个子命令的** flag 也算进来（同一个文件里可能有多套
				// flagset）。那种组合在运行时会说 "flag provided but not defined" ——
				// 那不是缺陷，是这张表的粒度**公开租**下的已知误差：跳过、计数、报出来。
				if strings.Contains(oPlain, "flag provided but not defined") ||
					strings.Contains(oBase, "flag provided but not defined") {
					skipped++
					t.Skipf("`--%s` 不是 `ngm %s` 这一层的 flag（属于某个子命令）——跳过", flag, spec.Name)
				}

				if plain != yes || oPlain != oYes {
					t.Errorf("`--%s=true` 与 `--%s` 不等价（exit %d vs %d）:\n  %s\n  %s",
						flag, flag, yes, plain, firstLine(oYes), firstLine(oPlain))
				}
				if off != base || oOff != oBase {
					t.Errorf("`--%s=false` 与**不给**它不等价（exit %d vs %d）:\n  %s\n  %s",
						flag, off, base, firstLine(oOff), firstLine(oBase))
				}
				if bad == 0 {
					t.Errorf("`--%s=x` 被接受了（exit 0）——Go 说那不是合法布尔值，"+
						"静默当成 true 是最坏的处置:\n  %s", flag, firstLine(oBad))
				}
				if junk == 0 {
					t.Errorf("`--%s ZZ-JUNK` 退 0 —— 那个 token 被当成了 flag 的值**被吞掉**了，"+
						"而布尔 flag 从不消费下一个 token（normalizeArgs 的注释专门写过这件事）:\n  %s",
						flag, firstLine(oJunk))
				}
				checked++
				t.Logf("%-12s --%-14s ①✓ ②%s ③exit=%d ④exit=%d", spec.Name, flag,
					map[bool]string{true: "✓", false: "✗"}[off == base && oOff == oBase], bad, junk)
			})
		}
	}

	// 覆盖守卫分两层。
	//
	// ① **结构性**：派生表里的每一对都必须"要么跑完、要么被公开跳过"——两边数目必须相等。
	//    这条不依赖任何我拍的阈值。
	expected := 0
	for _, spec := range commands {
		expected += len(boolFlagsFromSource(t, spec.Name))
	}
	if checked+skipped != expected {
		t.Fatalf("the derived table has %d pair(s) but %d ran and %d were skipped — "+
			"some pair neither ran nor was skipped openly", expected, checked, skipped)
	}
	// ② **规模**：真跑完的必须够多（否则这张表会退化成"什么都跳过"）。
	//    35 是**实测值 38** 之下的一个下限，不是估的：`fs.Bool` 清点出来 41 对，
	//    其中 3 对属于子命令的 flagset（公开跳过）。
	if checked < 35 {
		t.Fatalf("only %d flag(s) were fully checked (skipped %d) — the derived table no longer "+
			"covers what it claims", checked, skipped)
	}
	t.Logf("boolean flags: %d × 4 assertions (① =true ≡ --flag · ② =false ≡ 不给 · ③ =x 被拒 · ④ 不吞 token); "+
		"%d skipped as subcommand-scoped", checked, skipped)
}
