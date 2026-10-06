package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.40：**参数怎么写**（v0.39 枚举的是"有没有参数"）。
//
// 这一族每一句都对应产品自己写下的一句话：
//
//	顺序   root.go 的 normalizeArgs 注释：「flag.Parse 在遇到第一个 positional 时停止，
//	       因此 `ngm init <name> --dir=x` 需要重排」——重排**为了两种顺序等价**。
//	重复   Go 标准库 flag 的语义：**最后一个胜出**。
//	`--`   root.go 的注释：「`--` 之后一律视为 positional」。
//
// 三句都不是我编的判据，而是**把它们变成可执行的断言**：
//
//	① 两种顺序 ⇒ **输出逐字节相同**（路径归一化后）
//	② 重复 `--dir` ⇒ 最后的那个生效（好→坏会坏、坏→好会好）
//	③ `--` 之后的 `--dir=<坏>` ⇒ **绝不让坏目录出现在输出里**
//
// projectDirWasUsed 报告 `dir` 有没有被**当成项目目录**用过。
//
// 两种证据，都**不会说谎**：
//
//	① 文件系统：创建型命令（`init`）会在这里留下 `ngm.json`；
//	② 输出：把 dir 当**目录**用的命令会写出「dir + 分隔符」（`<dir>\ngm.json`）。
//
// 刻意**不**用"输出里出现过 <dir>"当判据：把 token 当 positional 的命令会**回显整个 token**
// （实测：`why` 说「--dir=<bad> is not in the dependency graph」、
// `engines` 说「unknown subcommand: --dir=<bad>」）——那是**正确**行为。
// 第一版判据正是靠"出现过 <dir>"判的，于是把三个做对了的命令报成了缺陷。
func projectDirWasUsed(dir, out string) bool {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return true
	}
	return strings.Contains(out, dir+string(filepath.Separator))
}

func TestV40ArgumentSpellingHoldsItsContracts(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// norm 把各自的临时目录折叠成 <dir>，好让两次运行可比。
	norm := func(s, dir string) string {
		return strings.ReplaceAll(s, dir, "<dir>")
	}
	run := func(t *testing.T, args []string) (int, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := dispatch(args, &out, &errb)
		return code, out.String() + errb.String()
	}

	orderPairs, dupGoodLast, dupBadLast, dashDash, dupSkipped := 0, 0, 0, 0, 0

	for _, spec := range commands {
		if globalCommands[spec.Name] {
			continue // 全局命令不接受 --dir，这三族对它无从谈起
		}
		t.Run(spec.Name, func(t *testing.T) {
			base := matrixArgs[spec.Name]

			// ---- ① 两种顺序必须产出同一份输出 ----
			isolateUserEnv(t)
			d1 := newProject(t)
			c1, o1 := run(t, append([]string{spec.Name, "--dir=" + d1}, base...))

			isolateUserEnv(t)
			d2 := newProject(t)
			c2, o2 := run(t, append(append([]string{spec.Name}, base...), "--dir="+d2))

			if c1 != c2 || norm(o1, d1) != norm(o2, d2) {
				t.Errorf("the two spellings disagree:\n"+
					"  --dir first  → exit %d: %s\n"+
					"  --dir last   → exit %d: %s",
					c1, strings.ReplaceAll(firstLine(o1), d1, "<dir>"),
					c2, strings.ReplaceAll(firstLine(o2), d2, "<dir>"))
			}
			orderPairs++

			// ---- ② 重复 --dir：最后一个胜出（两个方向都试） ----
			//
			// 判据是**与单 flag 那次运行逐字节比对**（路径归一化后）——
			// 比"看输出里有没有提到某个目录"可靠：那种启发式分不清
			// "**回显** token"与"把 token 当**目录**用"（见 projectDirWasUsed 的说明）。
			//
			// **每一次运行都用全新的目录**：第一版共用了一对目录，于是
			// `init` 的第一次运行把坏目录**建了出来**、`add` 的第一次把依赖**加了进去**——
			// 后面几次看到的是别的状态（`created` 变 `already exists`、`added` 变 `updated`）。
			// 那是夹具的状态耦合（v0.16 / v0.21 / v0.31 那一族的又一次），不是产品缺陷。
			spelling := func(t *testing.T, dirs ...string) (int, string) {
				t.Helper()
				isolateUserEnv(t)
				fresh := make([]string, 0, len(dirs))
				for _, d := range dirs {
					if d == "good" {
						fresh = append(fresh, newProject(t))
						continue
					}
					fresh = append(fresh, filepath.Join(t.TempDir(), "nope"))
				}
				args := append([]string{spec.Name}, base...)
				for _, d := range fresh {
					args = append(args, "--dir="+d)
				}
				c, o := run(t, args)
				for _, d := range fresh {
					o = norm(o, d)
				}
				return c, o
			}

			cGood, oGood := spelling(t, "good")
			cBad, oBad := spelling(t, "bad")

			if cGood == cBad && oGood == oBad {
				// 好目录与坏目录给出同一份结果 ⇒ 这一对**区分不开**，重复 --dir 在这里没有牙齿。
				// 如实记下来，而不是让它静默通过。
				t.Logf("skip: 好目录与坏目录对 `ngm %s` 给出同一份结果，重复 --dir 在这里没有牙齿", spec.Name)
				dupSkipped++
			} else {
				if c, o := spelling(t, "bad", "good"); c != cGood || o != oGood {
					t.Errorf("the last --dir must win: `--dir=<bad> --dir=<good>` did not behave like "+
						"`--dir=<good>` alone (exit %d vs %d):\n  %s\n  %s",
						c, cGood, firstLine(o), firstLine(oGood))
				}
				dupGoodLast++

				if c, o := spelling(t, "good", "bad"); c != cBad || o != oBad {
					t.Errorf("the last --dir must win: `--dir=<good> --dir=<bad>` did not behave like "+
						"`--dir=<bad>` alone (exit %d vs %d):\n  %s\n  %s",
						c, cBad, firstLine(o), firstLine(oBad))
				}
				dupBadLast++
			}

			// ---- ③ `--` 之后的 `--dir=<坏>` 绝不该生效 ----
			isolateUserEnv(t)
			d3 := newProject(t)
			bad3 := filepath.Join(t.TempDir(), "nope")
			c3, o3 := run(t, []string{spec.Name, "--dir=" + d3, "--", "--dir=" + bad3})
			if c3 == 0 {
				t.Errorf("`--` 之后的 `--dir=<bad>` 成了 positional，这次调用本该失败，却退 0")
			}
			if projectDirWasUsed(bad3, o3) {
				t.Errorf("`--` 之后的 `--dir=` **被当成了 flag**——root.go 承诺的是"+
					"「`--` 之后一律视为 positional」:\n  %s",
					strings.ReplaceAll(firstLine(o3), bad3, "<bad>"))
			}
			dashDash++
		})
	}

	// 判据：**每个命令要么通过那一族、要么被公开跳过**（不许有命令悄悄溜走）。
	// 跳过是允许的——但必须有理由（好/坏目录对它给出同一份结果，那一族就没有牙齿），
	// 而且数目要报出来。
	if orderPairs < 19 || dashDash < 19 {
		t.Fatalf("coverage shrank: order=%d dashdash=%d (each should be at least 19 = 21 commands − 2 global)",
			orderPairs, dashDash)
	}
	if dupGoodLast+dupSkipped < 19 || dupBadLast+dupSkipped < 19 {
		t.Fatalf("some command neither passed the repeated-flag check nor was openly skipped: "+
			"passed=%d/%d skipped=%d (each column should total at least 19)",
			dupGoodLast, dupBadLast, dupSkipped)
	}
	t.Logf("argument spelling: %d commands × {两种顺序 · 两个方向的重复 · `--`} all held",
		orderPairs)
}
