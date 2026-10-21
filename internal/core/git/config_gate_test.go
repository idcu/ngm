package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitConfigIsParsedInExactlyOnePlace 是 mirrorurl_local.go 里那句承诺的网：
//
//	**这是唯一一处按 git 的 `[section]` / `key = value` 形状解读 config 的地方**
//	（`IsBareMirror` 也读这个文件，但它只做一次宽松的 `bare=true` 行扫描，不解析键值）。
//
// 注释不会跑，这条会。为什么这句承诺需要机械检查（v0.99 的教训：能写网就别只写理由）：
// 它的两半都会悄悄变假，而且变假时没有别的测试会红——
//
//  1. 多出第二个**形状**解析者。v0.101 之前 url 与 refspec 各带一份循环式读取，
//     "哪些形状算不敢解读"于是两份实现各改各的（v0.78/v0.98："同一件事两份实现，
//     谁也没对过账"）。形状级的拒绝规则必须只有一份；值级规则才是允许分家的那一层。
//  2. 宽松扫描长出键值解析。`IsBareMirror` 的豁免**建立在"它不解析形状"之上**；
//     它一旦开始认段头、按键取值，豁免就不成立，而它看起来仍然"只是那个小函数"。
//
// 手法与 spawn_gate_test.go 一致：扫 `internal/git` 下**非测试** `.go` 文件的源码文本。
// 文本级是有意的：gofmt（CI 强制）让这些符号在格式化过的代码里只有一种写法。
// 因此注释里带括号地提到这些符号同样会命中——若报的是注释，把注释里的括号去掉即可
// （与 TestSpawn_OnlyOneFileCountsSpawns 的处置相同）。
//
// 空跑保护：两边都必须真的扫到东西。读取器一旦改名、解析一旦内联或挪进 helper，
// 本检查会因为"扫不到任何东西"而静默通过——那正是它最该报警的时候。
func TestGitConfigIsParsedInExactlyOnePlace(t *testing.T) {
	const (
		shapeHome = "originConfigKeys" // 唯一允许解读 config 形状的函数
		looseScan = "IsBareMirror"     // 唯一被豁免的第二种读法：不解析形状
	)

	// 形状解析的两个原语（mirrorurl_local.go）。只允许 shapeHome 调用它们。
	shapePrimitives := []string{"parseConfigSection(", "splitConfigKeyValue("}

	// 读一个文件的两种写法；配上词根 `config` 才算"读了 config 文件"。
	readMarkers := []string{"os.ReadFile(", "os.Open("}

	var shapeWheres []string     // 调用形状原语的位置（file:func）
	readers := map[string]bool{} // 读 config 文件的位置（file:func）

	forEachTopLevelFunc(t, func(file, name string, body []string) {
		where := file + ":" + name
		joined := strings.Join(body, "\n")
		for _, p := range shapePrimitives {
			if strings.Contains(joined, p) {
				shapeWheres = append(shapeWheres, where)
				break
			}
		}
		// 词根 `config`（**不是** `"config"`）是有意的：路径可以写成
		// `filepath.Join(dir, "config")`、`dir + "/config"` 或 `configName` 常量，
		// 只认带引号的那种写法会把后两种放过去（牙齿检查时实测漏掉过一个）。
		// 代价是"函数里恰好提到 config"也算命中，处置与注释命中相同。
		if strings.Contains(strings.ToLower(joined), "config") && containsAny(joined, readMarkers) {
			readers[where] = true
		}
	})

	// 一、形状解析只有一个家。
	switch {
	case len(shapeWheres) == 0:
		// 扫不到 = 锚点没了（改名、内联、或原语被删）。静默放行会让这句承诺
		// 从"被测的性质"退回"注释里的说法"——那是本检查存在的理由，不是它的豁免。
		t.Errorf("no call to %s found — this check would pass vacuously; "+
			"if the reader was renamed or the parsing was inlined, update this check "+
			"and the promise it guards (mirrorurl_local.go)",
			strings.Join(shapePrimitives, " / "))
	case len(shapeWheres) > 1:
		// 第二个解析者就是 v0.78/v0.98 的形态：两份实现会在"哪些形状算不敢解读"上各改各的。
		t.Errorf("git config now has %d shape parsers: %v\n"+
			"Two implementations of one shape drift apart: they end up disagreeing about "+
			"which configs are \"too ambiguous to answer\" (the v0.78/v0.98 failure). "+
			"Route the new reader through %s — only *value-level* rules may differ between readings.",
			len(shapeWheres), shapeWheres, shapeHome)
	default:
		if !strings.HasSuffix(shapeWheres[0], ":"+shapeHome) {
			t.Errorf("the config shape parser is now %q — the promise in mirrorurl_local.go names %q; "+
				"update both. (This check anchors on the *function*, not the file, so moving it "+
				"within the package is a deliberate, visible edit.)", shapeWheres[0], shapeHome)
		}
	}

	// 二、碰这个文件的函数只有两个；豁免只发给"不解析形状"的那一个。
	// 这条管的是另一半：如果有人新写一个读取器而**不复用**原语（手搓形状解析），
	// 第一条看不见它，但这里会看见它读了同一个文件。
	allowed := map[string]bool{shapeHome: true, looseScan: true}
	sawHomeRead := false
	for where := range readers {
		name := where[strings.LastIndexByte(where, ':')+1:]
		if !allowed[name] {
			t.Errorf("%s reads a git config file — the package has exactly one shape-aware reader (%s) "+
				"and one exempt line scan (%s). A third read site must be a deliberate decision: "+
				"route it through %s, or update this check (and the promise, if the promise changed)",
				where, shapeHome, looseScan, shapeHome)
		}
		if name == shapeHome {
			sawHomeRead = true
		}
	}
	if !sawHomeRead {
		t.Errorf("no config read found in %s — this check would pass vacuously; "+
			"if the reading moved (e.g. behind a helper the scanner cannot see through), "+
			"update this check", shapeHome)
	}
}

// containsAny 报告 s 里是否出现 markers 中的任意一个。
func containsAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// forEachTopLevelFunc 逐个交出 `internal/git` 下**非测试**源码文件里的顶层函数
// （含方法）与它们的函数体。
//
// 文本扫描（读 `.` 下的 `.go` 文件），与 spawn_gate_test.go 同一手法：这里要守的形状
// 是"一次调用、一次读取"，而在 gofmt 过的源码里，文本命中与真实调用是同一件事。
//
// 两处刻意的设计：
//
//   - 函数名本身不进"函数体"（声明行只取名字**之后**的部分）。否则 `func parseConfigSection(`
//     自己就会命中 `parseConfigSection(`，每个解析原语都会把自己报成第二个解析者。
//   - 函数外的行归到 `(package scope)`。包级 var/init 里藏一次配置读取是可能的，
//     直接丢弃这些行会让检查漏掉它。
func forEachTopLevelFunc(t *testing.T, fn func(file, name string, body []string)) {
	t.Helper()
	const packageScope = "(package scope)"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(".", file))
		if rerr != nil {
			t.Fatal(rerr)
		}

		name := packageScope
		var body []string
		flush := func() { fn(file, name, body) }

		for _, line := range strings.Split(string(data), "\n") {
			if n, tail, ok := declarationName(line); ok {
				flush()
				name, body = n, nil
				if tail != "" {
					// 单行函数体（本包里确实有）也要落在扫描范围内。
					body = append(body, tail)
				}
				continue
			}
			body = append(body, line)
		}
		flush()
	}
}

// declarationName 认出顶层 `func Name(...)` / `func (recv) Name(...)` 声明行，
// 返回函数名，以及名字**之后**的剩余文本（签名 + 可能的单行函数体）。
func declarationName(line string) (name, tail string, ok bool) {
	if !strings.HasPrefix(line, "func ") {
		return "", "", false
	}
	rest := strings.TrimSpace(line[len("func "):])
	if strings.HasPrefix(rest, "(") { // 方法：跳过接收者
		closeParen := strings.IndexByte(rest, ')')
		if closeParen < 0 {
			return "", "", false
		}
		rest = strings.TrimSpace(rest[closeParen+1:])
	}
	end := strings.IndexAny(rest, " (\t")
	if end <= 0 {
		return "", "", false
	}
	return rest[:end], rest[end:], true
}
