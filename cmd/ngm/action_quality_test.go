package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.33：**行动行不能只是"有一行"**。
//
// v0.32 断言"每一条失败项都配了一行 `→ …` / `Fixed in: …`"——但那只数条数，
// 于是一行 `→ 出错了` 也能过。这一版给那一行加一条内容判据：
//
//	**它必须点名一个用户能照着做的东西。**
//
// 三种锚点都算，且**都要能被核对**：
//
//	① 反引号里的命令（`` `ngm audit` ``）—— 子命令必须**真的在命令表里**
//	② 配置键（`supplyChain.osvIgnoreSeverities`）—— 必须**真的在 schema 里**
//	③ 修复版本（`Fixed in: 1.2.4`）—— 一个可对比的版本号
//
// ①② 的核对是这张网真正的牙齿：**一个指错地方的指引，比没有指引更糟**——
// 没有指引用户还会去查，指错了用户会照着做然后失败。
var (
	// 反引号里的 ngm 调用：`ngm audit` / `ngm audit --json` / `ngm tree --osv`
	reBacktickCmd = regexp.MustCompile("`ngm\\s+([a-z][a-z-]*)")
	// 配置键的写法：<小驼峰>.<小驼峰>（如 supplyChain.osvIgnoreSeverities）
	reConfigKey = regexp.MustCompile(`\b([a-z][a-zA-Z0-9]*\.[a-z][a-zA-Z0-9]*)\b`)
	// 修复版本
	reFixedIn = regexp.MustCompile(`^Fixed in:\s*\S+`)
)

// TestV33ActionLinesNameSomethingExecutable 给 v0.32 的行加"它说了什么"的判据。
//
// 判据刻意**不判断措辞好不好**——那是人读的事。它只检查一件事：
// **照着做有没有东西可照做**，以及那个东西**是不是真的存在**。
func TestV33ActionLinesNameSomethingExecutable(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 可核对的"真东西"：配置键来自 schema 源码，命令来自运行时的命令表。
	_, configKeys := collectSchemaKeys(t, repoRoot(t))
	if len(configKeys) == 0 {
		t.Fatal("no config key was read from the schema sources — the cross-check would pass vacuously")
	}
	if len(commands) == 0 {
		t.Fatal("the command table is empty — the cross-check would pass vacuously")
	}

	kinds := map[string]int{} // 锚点种类 → 出现次数
	lines := 0

	for _, rc := range reportCases {
		t.Run(rc.name, func(t *testing.T) {
			home := isolateUserEnv(t)
			dir := rc.setup(t, home)
			args := append(append([]string{}, rc.args...), "--dir="+dir)

			var out, errb bytes.Buffer
			if code := dispatch(args, &out, &errb); code == 0 {
				t.Fatalf("this case is meant to fail and report, but exited 0:\n%s", out.String())
			}
			text := out.String() + errb.String()

			for _, line := range reActionLine.FindAllString(text, -1) {
				lines++
				anchors := []string{}

				for _, m := range reBacktickCmd.FindAllStringSubmatch(line, -1) {
					sub := m[1]
					if !commandExists(sub) {
						t.Errorf("the advice points at `ngm %s`, which is not a command — "+
							"a wrong pointer is worse than no pointer:\n  %s", sub, strings.TrimSpace(line))
						continue
					}
					anchors = append(anchors, "command")
				}
				for _, m := range reConfigKey.FindAllStringSubmatch(line, -1) {
					if !allSegmentsAreConfigKeys(m[1], configKeys) {
						// 不是配置键就**不算锚点**（例如 https://osv.dev/… 里的 osv.dev、
						// 或文件名 ngm.json）。只有每一段都能在 schema 里找到才算数——
						// 这条**同时**挡住的是"建议里点了一个不存在的键"：
						// 写错一个字母，锚点消失，行就该红。
						continue
					}
					anchors = append(anchors, "config")
				}
				if reFixedIn.MatchString(strings.TrimSpace(line)) {
					anchors = append(anchors, "version")
				}

				if len(anchors) == 0 {
					t.Errorf("this next-step line names nothing a user can act on "+
						"(no `ngm <cmd>`, no existing config key, no fixed version):\n  %s",
						strings.TrimSpace(line))
					continue
				}
				kinds[anchors[0]]++
			}
		})
	}

	// 可达性守卫：三种锚点都要有样本。
	// 只走一种的话，这张网会退化成"只查有没有那一行"——正是它要修掉的毛病。
	if lines < 7 {
		t.Fatalf("only %d next-step line(s) were inspected — the reports stopped carrying them", lines)
	}
	for _, k := range []string{"command", "config", "version"} {
		if kinds[k] == 0 {
			t.Errorf("no next-step line used a **%s** anchor (%v) — "+
				"one kind going missing silently narrows what this net can notice", k, kinds)
		}
	}
	t.Logf("next-step lines: %d inspected, anchors: %v (config keys read: %d, commands: %d)",
		lines, kinds, len(configKeys), len(commands))
}

// allSegmentsAreConfigKeys 判断一个**点分路径**是否逐段都是真键。
//
// 为什么是逐段而不是整串：schema 文件里存的是**单个键名**
// （`supplyChain`、`osvIgnoreSeverities` 各是一条），而建议里写的是路径。
// 初版拿整串去查表，于是 `supplyChain.osvIgnoreSeverities` 查不到——
// 网因此报"这一行什么都没点名"，而**那一行明明是好的**：
// 又一次"红的不是产品，是我的检查"。
func allSegmentsAreConfigKeys(path string, keys map[string]string) bool {
	segs := strings.Split(path, ".")
	if len(segs) < 2 {
		return false
	}
	for _, s := range segs {
		if _, ok := keys[s]; !ok {
			return false
		}
	}
	return true
}

// commandExists 报告 name 是否在命令表里。
func commandExists(name string) bool {
	for _, spec := range commands {
		if spec.Name == name {
			return true
		}
	}
	return false
}
