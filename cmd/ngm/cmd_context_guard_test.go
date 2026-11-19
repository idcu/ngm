package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdContextIsTheOnlySkeleton 机械地守住"命令开头那一段只有一个实现"（P2-3）。
//
// 为什么需要它：`cmdContext` 抽出来的理由是**每条通道都可能各自漂移**——
// v0.54 修过一条 parse 失败的信封，v0.105 又发现 doctor 漏了，v0.108 又发现 why
// 的位置参数分支漏了，v0.135 又发现 doctor 的位置参数走的是第三种写法。
// 抽成一处之后，新命令仍然可以**绕过去**：手写 `fs.Parse` 与 `fs.Usage` 编译得过、
// 也跑得动，只是它的失败面与别人不一样。这条判据把"绕过去"变成编译期之后第一道红灯。
//
// 判据是三条机械检查（扫源码文本，不看行为）：
//
//  1. `fs.Parse(normalizeArgs(...))` 只允许出现在 `cmd_context.go`（唯一实现）
//     与 `run.go`（`--` 的拆分是它自己的语义，见 cmd_context.go 顶部注释）；
//  2. `fs.Usage = func() {` 只允许出现在 `cmd_context.go`（用法文本经 c.usage 接线）；
//  3. 每个造了 flagset 的文件，`newFlagSet` 出现几次就要有几个
//     `newCmdContext`/`newCmdContextNoJSON`——手写骨架的命令在这里对不上数。
//
// 三条都带**空跑保护**：扫不到应扫的东西时报警，而不是静默通过。
func TestCmdContextIsTheOnlySkeleton(t *testing.T) {
	// 例外表：文件 → 理由。加一条要写清"为什么它不是骨架"。
	exempt := map[string]string{
		"cmd_context.go": "骨架本身",
		"run.go": "`--` 之后的参数不进 flagset；解析 head 与它的错误通道（runArgError）" +
			"是 run 自己的语义。它仍然按同一条规则给信封——v0.136 起解析失败也从" +
			"原始参数里问一次，与 cmdContext.parse 一致；这里例外的只是骨架本身",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	sawParseInContext, sawUsageInContext := false, false
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		src := string(data)
		scanned++

		hasParse := strings.Contains(src, ".Parse(normalizeArgs(")
		hasUsageWiring := strings.Contains(src, "fs.Usage = func() {")
		flagSets := strings.Count(src, "newFlagSet(")
		if name == "cmd_context.go" {
			// 骨架自己：它应当**确实**含有这两样，否则这条检查会空跑。
			sawParseInContext = sawParseInContext || hasParse
			sawUsageInContext = sawUsageInContext || hasUsageWiring
			continue
		}

		if hasParse || hasUsageWiring {
			if reason, ok := exempt[name]; ok {
				t.Logf("%s: 手写骨架（已登记例外）——%s", name, reason)
			} else {
				t.Errorf("%s 自己解析参数或接线用法：命令开头那段必须走 cmdContext"+
					"（newCmdContext / c.parse / c.usage），否则它的失败面会与别的命令不同", name)
			}
		}

		// 第 3 条：flagset 与 cmdContext 必须一一对应。
		//
		// 两处不参与计数：定义 newFlagSet 的自己（root.go），与已登记例外的文件。
		if strings.Contains(src, "func newFlagSet(") {
			t.Logf("%s: 定义了 newFlagSet 本身，不参与 flagset 计数", name)
			continue
		}
		if _, ok := exempt[name]; ok {
			continue
		}
		calls := strings.Count(src, "newCmdContext(") + strings.Count(src, "newCmdContextNoJSON(")
		if got := flagSets - calls; got > 0 {
			t.Errorf("%s: %d 个 newFlagSet 只配了 %d 个 newCmdContext——有命令仍在手写骨架",
				name, flagSets, calls)
		}
	}

	// 第四件事：stdout 上的 JSON 只有一个写出点（ADR-045）。
	//
	// 此前有四份实现（`writeJSON` / `writeModulesJSON` / `writeIntegrationsJSON` /
	// `gc` 内联），三种错误文案、两种转义策略，没人说得清哪个是对的。
	// `json.NewEncoder` 只准出现在 main.go——那是**错误信封**，装的是人话、刻意不转义。
	checkEncoderIsSingle(t)

	if scanned == 0 {
		t.Fatal("一个 .go 文件都没扫到——这条判据空跑了")
	}
	if !sawParseInContext || !sawUsageInContext {
		t.Fatalf("cmd_context.go 里找不到骨架实现（parse=%v usage=%v）——"+
			"若实现搬到别处，这条检查会静默通过，必须同步更新", sawParseInContext, sawUsageInContext)
	}
}

// checkEncoderIsSingle 检查 `json.NewEncoder(` 只出现在错误信封那一个文件里。
//
// 为什么单独一条：报告类写出点当时有四种写法，差别正是转义策略与错误文案；
// 光靠"记得用 writeJSONDocument"守不住，下一个命令照样能自己 `json.NewEncoder`。
func checkEncoderIsSingle(t *testing.T) {
	t.Helper()
	const allowed = "main.go" // 错误信封：装的是人话，刻意不转义（ADR-045）
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	saw := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(".", name))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !strings.Contains(string(data), "json.NewEncoder(") {
			continue
		}
		if name == allowed {
			saw = true
			continue
		}
		t.Errorf("%s 自己造了一个 JSON encoder：stdout 上的报告必须走 writeJSONDocument"+
			"（ADR-045：解码语义是契约，字节不是），否则转义策略会再一次各写各的", name)
	}
	if !saw {
		t.Errorf("在 %s 里找不到 json.NewEncoder——这条检查会空跑；若错误信封换了写法，请同步更新", allowed)
	}
}
