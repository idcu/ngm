package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestV15DocExamplesAreRealInvocations 固定一条"文档里的示例命令必须真的**成形**"：
// 每一条以 `ngm` 开头的示例命令，它的子命令必须是真实存在的命令，它带的 flag 必须属于
// 那个命令（或属于根级的两个全局 flag）。
//
// 为什么需要它：v0.14 的 D4 是这条线的第一次命中——`build.md` 给的示例命令在默认项目上
// **跑不出它展示的输出**（少了"先把 esbuild 声明为 typeCheck 引擎"这个前提）。
// 更早一次同类事故是文档里的 `ngm add --ref=…`：**那个 flag 从来不存在**
// （真名是 `--ref-type`），照着抄直接 `flag provided but not defined`。
// 它不会让任何测试变红，因为**测试从不读文档**。
//
// 判据的来源是**运行时的命令表**（`root.go` 的 `commands`）与每个命令自己的 usage 常量：
// 后者已被 `TestV12EveryRegisteredFlagIsDocumented` 证明"会写下自己的全部 flag"，
// 因此"flag 出现在该命令的 usage 文本里"与"该命令真的接受它"在当前代码上等价。
//
// **它证明什么、不证明什么**：它证明示例命令**成形**（名字与 flag 都存在），
// **不**证明它能跑通（那需要 fixture、引擎与网络）。两者是不同的网：
// 这一张挡的是"照着抄直接报错"，挡不了"抄了能跑、但结果不是你看到的"。
// 判据也**故意不过问位置参数**（`ngm add <slug>` 里的 slug 形状）——那会误报。
//
// **扫描范围只含活文档**（根 README、docs/README、guides、internals、architecture）。
// `docs/development` 与 `docs/adr` 是**快照**：按项目纪律，复盘与 ADR 不随之后的删改
// 修订（ADR 用补录）。让一张会红的网去要求改写历史，等于亲手拆掉那两条纪律。
func TestV15DocExamplesAreRealInvocations(t *testing.T) {
	// 判据来源：运行时的命令表。key = 子命令名，value = 它自己的用法文本。
	usage := map[string]string{}
	for _, spec := range commands {
		usage[spec.Name] = spec.Usage
	}
	if len(usage) == 0 {
		t.Fatal("the command table is empty — the net would pass vacuously")
	}

	// 根级 flag：它们由 dispatch 手工处理（不经 flagset），因此不在任何命令的 usage 文本里。
	rootFlags := map[string]bool{"help": true, "version": true}

	docs := []string{"../../README.md", "../../docs/README.md"}
	for _, glob := range []string{
		"../../docs/guides/*.md",
		"../../docs/internals/*.md",
		"../../docs/architecture/*.md",
	} {
		matches, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) == 0 {
			t.Fatalf("no documents matched %s — the net's scope silently shrank to nothing", glob)
		}
		docs = append(docs, matches...)
	}

	used := map[int]bool{}
	checked := 0
	for _, doc := range docs {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		display := strings.TrimPrefix(filepath.ToSlash(doc), "../../")

		for _, cand := range docCommandCandidates(string(raw)) {
			sub, flags, ok := parseNgmInvocation(cand)
			if !ok {
				continue
			}
			checked++
			if sub == "" {
				// 根级调用（`ngm --version`）：只允许全局 flag。
				for _, f := range flags {
					if !rootFlags[f] {
						t.Errorf("%s: `ngm --%s` is a root-level invocation, so only --help/--version are valid:\n    %s",
							display, f, cand)
					}
				}
				continue
			}
			text, known := usage[sub]
			if !known {
				if idx, exempt := docExceptionFor(display, cand); exempt {
					used[idx] = true
					continue
				}
				t.Errorf("%s: example uses `ngm %s`, which is not a command in this build (commands: %s):\n    %s",
					display, sub, commandNames(), cand)
				continue
			}
			for _, f := range flags {
				if rootFlags[f] {
					continue
				}
				if !mentionRe(f).MatchString(text) {
					t.Errorf("%s: `ngm %s` does not take --%s (it is not in that command's usage text):\n    %s",
						display, sub, f, cand)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no `ngm` invocations were found in the living docs — a net that matches nothing is not a net")
	}
	// 名单不会悄悄过期：条目若在文档里找不到，就说明该示例已被删改，必须把条目也删掉。
	for i, ex := range docExampleExceptions {
		if !used[i] {
			t.Errorf("docExampleExceptions[%d] (%s %q) no longer matches anything: the example was changed or removed, "+
				"so the exception must go too — a stale exception is a hole in the net", i, ex.doc, ex.text)
		}
	}
	t.Logf("checked %d documented invocation(s) across %d document(s)", checked, len(docs))
}

// docExampleExceptions 是**故意**写出来的"不存在的命令"，每条的判据是
// **文档正是在说不存在**。
//
// 为什么是一份名单、而不是把判据放宽到能容纳它们：这两处不是命令写错了，是**负例**
// （"ngm 没有 test 命令"、"迁移命令尚不存在"）。判据一旦放宽到能容纳它们，
// 就等于不再检查"命令名是否存在"——而那正是本网唯一的牙齿。
//
// 名单**不会悄悄过期**：上面的断言要求每个条目在当前文档里**确实还能匹配到**，
// 一旦那处示例被删改，测试会红并要求把条目一并删掉。
var docExampleExceptions = []struct {
	doc  string
	text string
	why  string
}{
	{"docs/guides/test.md", "ngm test", "散文明确写着 ngm 没有 test 命令——这一行是负例"},
	{"docs/guides/configuration.md", "ngm lock migrate", `写明"迁移命令尚不存在"（至今未发生 MAJOR 变更）`},
	{"docs/architecture/locking.md", "ngm lock migrate", "同上"},
}

// docExceptionFor 返回命中的例外条目下标。
func docExceptionFor(doc, cand string) (int, bool) {
	for i, ex := range docExampleExceptions {
		if ex.doc == doc && strings.Contains(cand, ex.text) {
			return i, true
		}
	}
	return 0, false
}

// commandNames 是命令表里全部名字（用于报错时点名"这里到底有哪些命令"）。
func commandNames() string {
	names := make([]string, 0, len(commands))
	for _, spec := range commands {
		names = append(names, spec.Name)
	}
	return strings.Join(names, " ")
}

var (
	// fencedBlockRe 匹配围栏代码块的内容（``` 之间的部分，含 info string 之后的行）。
	fencedBlockRe = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")
	// inlineCodeRe 匹配行内代码跨度。
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
	// commandSepRe 把一行里的多条命令拆开。
	commandSepRe = regexp.MustCompile(`&&|;|\|\s`)
)

// docCommandCandidates 从一份文档里取出"可能是命令"的片段。
//
// 只取**代码**（围栏块与行内跨度），而不是整行文本：这是本网不误报的关键。
// 整行扫描会把 `node --test`、`git merge-base --is-ancestor`、esbuild 的
// `--alias:…`、catalog 的 `--kind=` 全部算进来——它们在句子里与 `ngm` 共处一行，
// 却与 ngm 的 flag 毫无关系。**会误报的门禁会被忽略**，所以判据必须窄。
func docCommandCandidates(doc string) []string {
	var out []string
	add := func(s string) {
		for _, part := range commandSepRe.Split(s, -1) {
			out = append(out, strings.TrimSpace(part))
		}
	}
	for _, m := range fencedBlockRe.FindAllStringSubmatch(doc, -1) {
		for _, line := range strings.Split(m[1], "\n") {
			add(line)
		}
	}
	for _, m := range inlineCodeRe.FindAllStringSubmatch(doc, -1) {
		add(m[1])
	}
	return out
}

// parseNgmInvocation 判断一个片段是不是 `ngm …` 调用，并取出子命令与 flag 名。
//
// 返回 ok=false 表示"这不是一条 ngm 命令"（不是错误）。
// 占位符（`<cmd>`、`…`）出现时返回 ok=false：**它连子命令都不确定，
// 因此它带什么 flag 也就无法判定**——宁可少查一条，也不猜。
func parseNgmInvocation(cand string) (sub string, flags []string, ok bool) {
	line := strings.TrimSpace(cand)
	// `#` 开头的行是**注释**，不是命令。本仓库的示例里 `#` 一律用来写说明与输出
	// （`#   --version   输出版本信息`、`# ngm 0.1.0 (git:abc1234, …)` 是版本**输出**）。
	// 此前把 `#` 当成提示符剥掉，于是那几行被当成了命令——那是网在误报。
	if strings.HasPrefix(line, "#") {
		return "", nil, false
	}
	// 提示符：`$ ngm …` / `> ngm …` / `PS> ngm …`。
	for _, p := range []string{"$", ">", "PS>"} {
		if strings.HasPrefix(line, p+" ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, p))
			break
		}
	}
	if line != "ngm" && !strings.HasPrefix(line, "ngm ") {
		return "", nil, false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "ngm" {
		return "", nil, false
	}
	// 第二个词以数字开头 ⇒ 这是 `ngm --version` 的**输出**（`ngm 0.0.0-dev (git:dev, …)`），
	// 不是调用：命令名不以数字开头（POSIX 惯例），而输出恰好长这样。
	if len(fields) > 1 && fields[1][0] >= '0' && fields[1][0] <= '9' {
		return "", nil, false
	}

	for _, tok := range fields[1:] {
		// 占位符或省略号：这条命令的形态不可判定。
		if strings.ContainsAny(tok, "<>…") {
			return "", nil, false
		}
		if !strings.HasPrefix(tok, "-") {
			if sub == "" {
				sub = tok
			}
			continue
		}
		name := strings.TrimLeft(tok, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		if name == "" || strings.Contains(name, "*") {
			continue
		}
		// `-h` 这类单字母短 flag：只认 help。
		if len(tok) > 1 && !strings.HasPrefix(tok, "--") {
			if name == "h" {
				flags = append(flags, "help")
			}
			continue
		}
		flags = append(flags, name)
	}
	return sub, flags, true
}
