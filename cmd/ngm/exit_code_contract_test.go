package main

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 退出码这件事**有四处事实源**：
//
//	① internal/errs/errs.go             —— 实现（数值 + 名字）
//	② docs/architecture/observability.md —— 契约的**规范**出处（"退出码约定（全局）"表）
//	③ docs/modules/p0-core.md            —— 规格里的错误模型（`ErrorCode` 常量块）
//	④ docs/guides/cli.md                 —— 用户看到的那一行汇总
//
// v0.23 在 ③ 里抓到过一处漏写：它写着「1 非预期漂移」，而 ② 早就写准了
// （策略失败：verify 漂移 / adapter 引擎运行失败 / audit 超阈值）。
// **规范与架构文档不一致，而错的那份是规范**——下一个人会照着规范把错的再抄一遍。
// 当时只有人眼发现它；这一张网让它以后必须自己撞上去。
//
// 另一件被钉住的事：③ 用的是 `iota + 1`，**顺序一变，所有码静默平移**。
// 规格是给人抄的，抄错一个顺序就是全线错位。
func TestV24ExitCodeContractAgreesAcrossSources(t *testing.T) {
	impl := errsCodeNames(t)
	p0 := p0CoreErrorCodes(t)
	obs := observabilityExitCodes(t)
	cli := cliExitCodes(t)

	// ① 实现：0..5 六个码，数值显式且互不相同。
	want := map[string]int{
		"OK": 0, "RefDrift": 1, "DigestMismatch": 2,
		"ConfigInvalid": 3, "GitFetch": 4, "EngineNotFound": 5,
	}
	if len(impl) != len(want) {
		t.Fatalf("internal/errs declares %d codes, the contract defines %d: %v", len(impl), len(want), impl)
	}
	for name, code := range want {
		got, ok := impl[name]
		if !ok {
			t.Errorf("internal/errs no longer declares Code%s", name)
			continue
		}
		if got != code {
			t.Errorf("Code%s = %d, the contract says %d", name, got, code)
		}
	}

	// ③ 规格：`iota + 1` 推出的名字→值，必须与实现完全一致。
	if len(p0) == 0 {
		t.Fatal("docs/modules/p0-core.md: no `ErrorCode = iota + 1` block found — the net's scope shrank")
	}
	for name, code := range want {
		if name == "OK" {
			continue
		}
		got, ok := p0[name]
		if !ok {
			t.Errorf("docs/modules/p0-core.md no longer names Err%s — the spec drifted from the implementation", name)
			continue
		}
		if got != code {
			t.Errorf("docs/modules/p0-core.md implies Err%s = %d (from iota ordering), the implementation says %d",
				name, got, code)
		}
	}
	if len(p0) != len(want)-1 {
		t.Errorf("docs/modules/p0-core.md lists %d error codes, the contract defines %d", len(p0), len(want)-1)
	}

	// ② 规范表：0..5 全部列出，且每行都有语义与来源。
	if len(obs) == 0 {
		t.Fatal("docs/architecture/observability.md: no exit-code table found — the net's scope shrank")
	}
	for code := 0; code <= 5; code++ {
		row, ok := obs[code]
		if !ok {
			t.Errorf("docs/architecture/observability.md does not list exit code %d", code)
			continue
		}
		if strings.TrimSpace(row.semantics) == "" || strings.TrimSpace(row.sources) == "" {
			t.Errorf("exit code %d is documented with an empty semantics or sources: %+v", code, row)
		}
	}
	if len(obs) != 6 {
		t.Errorf("docs/architecture/observability.md lists %d exit codes, the contract defines 6", len(obs))
	}

	// ④ 用户侧那一行：同样必须是 0..5，且每段都有说明。
	if len(cli) == 0 {
		t.Fatal("docs/guides/cli.md: no exit-code line found — the net's scope shrank")
	}
	for code := 0; code <= 5; code++ {
		text, ok := cli[code]
		if !ok {
			t.Errorf("docs/guides/cli.md's summary does not mention exit code %d", code)
			continue
		}
		if strings.TrimSpace(text) == "" {
			t.Errorf("docs/guides/cli.md mentions exit code %d with no text", code)
		}
	}
	if len(cli) != 6 {
		t.Errorf("docs/guides/cli.md's summary lists %d exit codes, the contract defines 6", len(cli))
	}

	// 五：**码 1 的来源必须是"全部"**。它是唯一一个被五类情形共用的数字，
	// 而漏掉任何一处都会让读文档的人以为"这个码与我无关"——v0.23 就是栽在这儿。
	//
	// 这一条在 **v0.57 搬去了 `TestV57ExitOneSourcesAreRegistered`**。
	// 从前的写法是一张**手写关键词表**（漂移/引擎/漏洞/钩子）**单向**核对文档：
	// 文档少一个词会红，而**代码新增第五种来源不会红**——它自己的注释也承认
	// "哪天真出现第五种来源，加进这里的同时也必须写进文档"，也就是靠人记得。
	//
	// 现在：来源由**源码派生**（谁能让进程以 1 退出）、双向对账，
	// 类别名由那条判据要求出现在这一行里。同一件事只留一份判据。

	t.Logf("exit-code contract: %d impl codes · %d spec codes · %d table rows · %d summary entries",
		len(impl), len(p0), len(obs), len(cli))
}

// readDoc 读一份仓库内的文档；读不到就 Fatal（**网的判据来源不许静默消失**）。
func readDoc(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the net's source of truth is unreadable (%s): %v", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("%s is empty — the net would pass vacuously", path)
	}
	return string(raw)
}

var (
	reErrsConst = regexp.MustCompile(`(Code[A-Z]\w*)\s+Code\s*=\s*(\d+)`)
	reP0Ident   = regexp.MustCompile(`^(Err[A-Z]\w*)`)
	reObsRow    = regexp.MustCompile("^\\|\\s*`([0-9])`\\s*\\|([^|]*)\\|([^|]*)\\|")
	reCLIPart   = regexp.MustCompile(`^([0-9])\s+(.*)$`)
)

// errsCodeNames 从实现里取出 名字（去掉 Code 前缀）→ 数值。
func errsCodeNames(t *testing.T) map[string]int {
	t.Helper()
	raw := readDoc(t, "../../internal/errs/errs.go")
	out := map[string]int{}
	for _, m := range reErrsConst.FindAllStringSubmatch(raw, -1) {
		name := strings.TrimPrefix(m[1], "Code")
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("unparsable code %q: %v", m[1], err)
		}
		out[name] = n
	}
	return out
}

// p0CoreErrorCodes 从规格的 `ErrorCode = iota + 1` 块推出 名字（去掉 Err 前缀）→ 数值。
//
// **这就是要钉的东西**：`iota` 的值取决于顺序，重排一行，所有退出码静默平移。
func p0CoreErrorCodes(t *testing.T) map[string]int {
	t.Helper()
	raw := readDoc(t, "../../docs/modules/p0-core.md")
	out := map[string]int{}
	for _, block := range regexp.MustCompile(`(?s)const \((.*?)\)`).FindAllStringSubmatch(raw, -1) {
		if !strings.Contains(block[1], "ErrorCode = iota + 1") {
			continue
		}
		val := 0
		for _, line := range strings.Split(block[1], "\n") {
			l := strings.TrimSpace(line)
			if l == "" || strings.HasPrefix(l, "//") {
				continue
			}
			m := reP0Ident.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			val++
			out[strings.TrimPrefix(m[1], "Err")] = val
		}
	}
	return out
}

type exitCodeRow struct {
	semantics string
	sources   string
}

// observabilityExitCodes 解析规范里的退出码表。
func observabilityExitCodes(t *testing.T) map[int]exitCodeRow {
	t.Helper()
	raw := readDoc(t, "../../docs/architecture/observability.md")
	out := map[int]exitCodeRow{}
	for _, line := range strings.Split(raw, "\n") {
		m := reObsRow.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		out[n] = exitCodeRow{semantics: m[2], sources: m[3]}
	}
	return out
}

// cliExitCodes 解析用户侧那一行汇总：`0 成功 / 1 策略失败 / …`。
func cliExitCodes(t *testing.T) map[int]string {
	t.Helper()
	raw := readDoc(t, "../../docs/guides/cli.md")
	out := map[int]string{}
	for _, line := range strings.Split(raw, "\n") {
		if !strings.Contains(line, "**退出码**") {
			continue
		}
		body := line[strings.Index(line, "：")+len("："):]
		for _, part := range strings.Split(body, "/") {
			m := reCLIPart.FindStringSubmatch(strings.TrimSpace(part))
			if m == nil {
				continue
			}
			n, _ := strconv.Atoi(m[1])
			out[n] = m[2]
		}
	}
	// 保持确定性（map 的键顺序无所谓，但排序便于调试输出）。
	keys := make([]int, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return out
}
