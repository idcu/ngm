package main

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 退出码这件事**有三处事实源**：
//
//	① internal/core/errs/errs.go          —— 实现（数值 + 名字，显式常量）
//	② docs/architecture/observability.md  —— 契约的**规范**出处（"退出码约定（全局）"表）
//	③ docs/guides/cli.md                  —— 用户看到的那一行汇总
//
// v0.23 时还有第四处：一份规格文档里的 `ErrorCode = iota + 1` 规格块（该文档已随历史清理移出工作树），
// 它曾抓到过一处漏写（写着「1 非预期漂移」，而 observability 早就写准了：
// 策略失败 = verify 漂移 / adapter 引擎运行失败 / audit 超阈值）。那份规格随历史
// 文档清理移出工作树，实现侧 `internal/core/errs/errs.go` 也早已改为**显式赋值**
// （不再用 iota，顺序漂移不会再静默平移所有码），因此那一条腿随之撤销：
// 实现常量这一半由 `errsCodeNames` 从源码直接读，另外两条（observability 0–6 表、
// cli 退出码摘要）继续保留并互相核对。
func TestV24ExitCodeContractAgreesAcrossSources(t *testing.T) {
	impl := errsCodeNames(t)
	obs := observabilityExitCodes(t)
	cli := cliExitCodes(t)

	// ① 实现：0..5 六个码，数值显式且互不相同。
	want := map[string]int{
		"OK": 0, "RefDrift": 1, "DigestMismatch": 2,
		"ConfigInvalid": 3, "GitFetch": 4, "EngineNotFound": 5,
		"Internal": 6, // v0.59 · ADR-026：内部失败
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

	// ② 规范表：0..5 全部列出，且每行都有语义与来源。
	if len(obs) == 0 {
		t.Fatal("docs/architecture/observability.md: no exit-code table found — the net's scope shrank")
	}
	for code := 0; code <= 6; code++ {
		row, ok := obs[code]
		if !ok {
			t.Errorf("docs/architecture/observability.md does not list exit code %d", code)
			continue
		}
		if strings.TrimSpace(row.semantics) == "" || strings.TrimSpace(row.sources) == "" {
			t.Errorf("exit code %d is documented with an empty semantics or sources: %+v", code, row)
		}
	}
	if len(obs) != 7 {
		t.Errorf("docs/architecture/observability.md lists %d exit codes, the contract defines 7", len(obs))
	}

	// ④ 用户侧那一行：同样必须是 0..5，且每段都有说明。
	if len(cli) == 0 {
		t.Fatal("docs/guides/cli.md: no exit-code line found — the net's scope shrank")
	}
	for code := 0; code <= 6; code++ {
		text, ok := cli[code]
		if !ok {
			t.Errorf("docs/guides/cli.md's summary does not mention exit code %d", code)
			continue
		}
		if strings.TrimSpace(text) == "" {
			t.Errorf("docs/guides/cli.md mentions exit code %d with no text", code)
		}
	}
	if len(cli) != 7 {
		t.Errorf("docs/guides/cli.md's summary lists %d exit codes, the contract defines 7", len(cli))
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

	t.Logf("exit-code contract: %d impl codes · %d table rows · %d summary entries",
		len(impl), len(obs), len(cli))
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
	reObsRow    = regexp.MustCompile("^\\|\\s*`([0-9])`\\s*\\|([^|]*)\\|([^|]*)\\|")
	reCLIPart   = regexp.MustCompile(`^([0-9])\s+(.*)$`)
)

// errsCodeNames 从实现里取出 名字（去掉 Code 前缀）→ 数值。
func errsCodeNames(t *testing.T) map[string]int {
	t.Helper()
	raw := readDoc(t, "../../internal/core/errs/errs.go")
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
