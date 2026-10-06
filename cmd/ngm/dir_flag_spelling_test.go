package main

import (
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.41：**`--dir` 的四种拼法等价，空值必须被拒绝。**
//
// 两件事：
//
//	① 四种拼法（`--dir=X` / `-dir=X` / `--dir X` / `-dir X`）必须**输出逐字节相同**。
//	   依据：`normalizeArgs` 的 flagSpec 同时登记了长短两种前缀，而 Go 标准库
//	   把 `-` 与 `--` 当同义——两句都写在代码里，这里把它们变成断言。
//
//	② **`--dir=`（显式给了空值）必须被拒绝**，而且**绝不能在当前目录里留下东西**。
//
// ② 是怎么来的（v0.41 实测）：Go 的 flag 把 `--dir=` 解析成空串，而空串与"没给这个 flag"
// 在后续代码里无法区分——于是命令**静默地把空值当成 CWD**。实测后果：
// `ngm add github:x/dep@v1 --dir=` 改的是**当前目录**的 ngm.json
// （探针跑出来的那一瞬间，它把仓库的 `cmd/ngm/` 里写出了 ngm.json 与 src/）。
//
// 判据刻意**只看副作用**：把 CWD 换成一个空目录，跑完之后**它必须还是空的**——
// 这比读输出可靠得多（v0.40 已经吃过"输出会回显"的亏）。
func TestV41DirFlagSpellingsAndTheEmptyValue(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	same, refused := 0, 0

	for _, spec := range commands {
		if globalCommands[spec.Name] {
			continue // 全局命令不接受 --dir，这两件事对它无从谈起
		}
		base := matrixArgs[spec.Name]

		t.Run(spec.Name, func(t *testing.T) {
			norm := func(s, d string) string { return strings.ReplaceAll(s, d, "<dir>") }
			spell := func(t *testing.T, mk func(dir string) []string) (int, string) {
				t.Helper()
				isolateUserEnv(t)
				d := newProject(t)
				args := append([]string{spec.Name}, base...)
				args = append(args, mk(d)...)
				c, out := runCaptureCode(t, args...)
				return c, norm(out, d)
			}

			// ---- ① 四种拼法等价 ----
			c1, o1 := spell(t, func(d string) []string { return []string{"--dir=" + d} })
			for _, v := range []struct {
				label string
				mk    func(dir string) []string
			}{
				{"-dir=<dir>", func(d string) []string { return []string{"-dir=" + d} }},
				{"--dir <dir>", func(d string) []string { return []string{"--dir", d} }},
				{"-dir <dir>", func(d string) []string { return []string{"-dir", d} }},
			} {
				c, o := spell(t, v.mk)
				if c != c1 || o != o1 {
					t.Errorf("`%s` 与 `--dir=<dir>` 不等价（exit %d vs %d）:\n  %s\n  %s",
						v.label, c, c1, firstLine(o), firstLine(o1))
				}
			}
			same++

			// ---- ② 空值必须被拒绝，且 CWD 必须原封不动 ----
			for _, v := range []struct {
				label string
				args  []string
			}{
				{"--dir=", []string{"--dir="}},
				{"-dir=", []string{"-dir="}},
				{"--dir \"\"", []string{"--dir", ""}},
			} {
				t.Run("empty "+v.label, func(t *testing.T) {
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

					code, out := runCaptureCode(t, append(append([]string{spec.Name}, base...), v.args...)...)
					if code == 0 {
						t.Errorf("`%s` 被接受了（exit 0）——空目录值最可能是没展开的变量，必须拒绝:\n%s",
							v.label, firstLine(out))
					}
					// **副作用**：换成的那个空目录必须还是空的。
					// 这是不会说谎的证据——比读输出可靠（v0.40 吃过"输出会回显"的亏）。
					if entries, err := os.ReadDir(scratch); err == nil && len(entries) > 0 {
						names := make([]string, 0, len(entries))
						for _, e := range entries {
							names = append(names, e.Name())
						}
						t.Errorf("`%s` 静默退化成了 CWD：命令在**当前目录**里留下了 %v —— "+
							"这正是空值必须被拒绝的理由", v.label, names)
					}
				})
			}
			refused++
		})
	}

	if same < 19 || refused < 19 {
		t.Fatalf("coverage shrank: spellings=%d empty=%d (each should cover 21 − 2 global commands)",
			same, refused)
	}
	t.Logf("dir flag: %d commands × {四种拼法等价 · 三种空值写法都被拒绝且 CWD 原封不动}",
		same)
}
