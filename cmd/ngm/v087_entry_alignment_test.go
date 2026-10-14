package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/mappings"
)

// v0.87：**自检器与产品必须在同一批形态上给出同一个入口**。
//
// 起因是 v0.78 抓到的那个形态：标准第 1 条推荐 `exports`，而自检器不读它——
// 于是"照着标准做"的库被判「三层都没找到入口」，而 `mappings.InferEntry` 一直读。
// 当时的处置是把候选层次与 `parseExports` **逐字对齐**，但那种对齐是**人工的**：
// 下一次有人在任一侧加一层，没有任何判据会红。
//
// 这条网把它变成机械对账：同一批 `package.json` 形态，两边各推断一次，**必须相同**。
// 解析的是自检器自己打印的那句话（`入口 <path>（来自 …`），
// 因此它同时守着"报告里写出来的入口"与"产品真正会用的入口"一致。
//
// 已知边界：自检器只打印 **Main**，`Types` 侧只判"有没有"（`types-provided`），
// 所以这条网对账的是 Main；Types 的逐字一致没有机械判据（写在这里而不是留给猜）。

var (
	reEntry = regexp.MustCompile(`入口 (\S+)（来自`)
	reTypes = regexp.MustCompile(`类型入口 (\S+)（来自`)
)

// checkerEntryOf 读出某条判据报告的入口路径；判据判"没有"（fail）时返回空串。
func checkerEntryOf(t *testing.T, rep stdReport, out, id string, re *regexp.Regexp) string {
	t.Helper()
	status, found := findStatus(rep, id)
	if !found {
		t.Fatalf("自检器没报 %s：\n%s", id, out)
	}
	switch status {
	case "pass":
		m := re.FindStringSubmatch(whyOf(rep, id))
		if m == nil {
			t.Fatalf("%s 说通过了，但报告里读不出口径（措辞变了？）：%q", id, whyOf(rep, id))
		}
		return m[1]
	case "fail":
		return ""
	case "skip":
		t.Fatalf("%s 是 skip（包形态下不该是这种）：%q", id, whyOf(rep, id))
	}
	return ""
}

// v0.88：**"有没有入口"与"入口文件在不在"是两条判据**——各自都要能单独红。
//
// v0.88 之前它们是混在一起的（"第一个存在的候选"）：一个"声明了但文件不在"的包，
// 产品会用那个路径、自检器说"找不到入口"——两边都说了话，却说不清到底哪里不对。
// 拆开之后：有声明 ⇒ 入口判据过（与产品同口径）；文件不在 ⇒ **另一条**必做判据红。
func TestV88DeclaredEntryMustExist(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")
	dir := t.TempDir()
	writeFileAt(t, dir, "package.json",
		`{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./gone.js"}`)
	writeFileAt(t, dir, "README.md", "# a\n")

	out, code := runChecker(t, bash, script, dir)
	rep := parseStdReport(t, out)
	if status, _ := findStatus(rep, "entry-resolvable"); status != "pass" {
		t.Errorf("声明的入口存在与否不该影响「有没有入口」：entry-resolvable = %q（产品会用声明的路径）\n%s", status, out)
	}
	if status, _ := findStatus(rep, "entry-file-exists"); status != "fail" {
		t.Errorf("声明了不存在的入口 ⇒ entry-file-exists 必须红，got %q\n%s", status, out)
	}
	if !strings.Contains(whyOf(rep, "entry-file-exists"), "./gone.js") {
		t.Errorf("红的理由里没点名那个路径：%q", whyOf(rep, "entry-file-exists"))
	}
	if code != 1 {
		t.Errorf("必做红 ⇒ exit 1，got %d", code)
	}

	// 牙齿的另一半：把文件放进去 ⇒ 两条都过、exit 0（证明上面那次红是真的因为文件不在）。
	writeFileAt(t, dir, "gone.js", "export const a = 1;\n")
	out, code = runChecker(t, bash, script, dir)
	rep = parseStdReport(t, out)
	for _, id := range []string{"entry-resolvable", "entry-file-exists"} {
		if status, _ := findStatus(rep, id); status != "pass" {
			t.Errorf("补上文件之后 %s 应当通过，got %q\n%s", id, status, out)
		}
	}
	if code != 0 {
		t.Errorf("补上文件之后应当全绿，got exit %d\n%s", code, out)
	}
}

func TestV87EntryInferenceAgreesBetweenProductAndChecker(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")

	shapes := []struct {
		name  string
		files map[string]string
	}{
		{"ngm.json 第 1 层", map[string]string{
			"ngm.json":       `{"schemaVersion":1,"name":"a","version":"1.0.0","runtime":"node","main":"./lib/entry.js","types":"./lib/entry.d.ts"}`,
			"package.json":   `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./other.js"}`,
			"lib/entry.js":   "export const a = 1;\n",
			"lib/entry.d.ts": "export declare const a: number;\n",
			"other.js":       "export const a = 2;\n",
		}},
		{"exports 字符串", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":"./src/index.js"}`,
			"src/index.js": "export const a = 1;\n",
		}},
		{"exports 的 \".\" 是字符串", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":"./src/main.js"}}`,
			"src/main.js":  "export const a = 1;\n",
		}},
		{"exports 条件（types + import）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"types":"./t.d.ts","import":"./i.js"}}}`,
			"t.d.ts":       "export declare const a: number;\n",
			"i.js":         "export const a = 1;\n",
		}},
		{"exports 没有 \".\"（本身就是条件映射）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{"import":"./i.js","require":"./c.cjs"}}`,
			"i.js":         "export const a = 1;\n",
			"c.cjs":        "module.exports = { a: 1 };\n",
		}},
		{"exports 只有 require", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","license":"MIT","exports":{"require":"./c.cjs"}}`,
			"c.cjs":        "module.exports = { a: 1 };\n",
		}},
		{"exports 的 default 先于 require", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"require":"./c.cjs","default":"./d.js"}}}`,
			"d.js":         "export const a = 1;\n",
			"c.cjs":        "module.exports = { a: 1 };\n",
		}},
		{"main / types 字段", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./m.js","types":"./m.d.ts"}`,
			"m.js":         "export const a = 1;\n",
			"m.d.ts":       "export declare const a: number;\n",
		}},
		{"index 约定（index.js）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT"}`,
			"index.js":     "export const a = 1;\n",
		}},
		{"index 约定（index.cjs —— v0.78 补的那一层）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","license":"MIT"}`,
			"index.cjs":    "module.exports = { a: 1 };\n",
		}},
		{"index 约定 + 旁边的 index.d.ts（Types 侧）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT"}`,
			"index.js":     "export const a = 1;\n",
			"index.d.ts":   "export declare const a: number;\n",
		}},
		{"ngm.json 只声明 types（**没有 main**）", map[string]string{
			"ngm.json":     `{"schemaVersion":1,"name":"a","version":"1.0.0","runtime":"node","types":"./t.d.ts"}`,
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./m.js"}`,
			"t.d.ts":       "export declare const a: number;\n",
			"m.js":         "export const a = 1;\n",
		}},
		{"只有根级 foo.d.ts（**不是** index 约定 ⇒ 两侧都该没有类型入口）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT"}`,
			"index.js":     "export const a = 1;\n",
			"foo.d.ts":     "export declare const a: number;\n",
		}},
		{"ngm.json 声明了不存在的入口（两侧同口径：都照用声明）", map[string]string{
			"ngm.json":     `{"schemaVersion":1,"name":"a","version":"1.0.0","runtime":"node","main":"./gone.js"}`,
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./m.js"}`,
			"m.js":         "export const a = 1;\n",
		}},
		{"两侧都找不到入口（ngm.json 存在但没有任何入口线索）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT"}`,
			"README.md":    "# a\n",
		}},

		// ---- v0.98：专攻解析边界（`parseExports` 与路径规范化）----
		// 这些形态都是**合法 JSON**，但按 exports 规范属边缘。两侧必须给出同一个答案，
		// 而且答案里**打印出来的路径**必须逐字相同（"报告里写的就是产品会用的那一个"）。
		{"exports 是数组（不是对象也不是字符串）⇒ 两侧都当没有入口", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":["./a.js","./b.js"]}`,
			"index.js":     "export const a = 1;\n",
		}},
		{"exports 是 null", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":null}`,
			"index.js":     "export const a = 1;\n",
		}},
		{`exports 的 "." 是 null`, map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":null}}`,
			"index.js":     "export const a = 1;\n",
		}},
		{"exports 的 import 是嵌套对象（不是字符串）⇒ 跳过它", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"import":{"default":"./d.js"},"require":"./c.cjs"}}}`,
			"d.js":         "export const a = 1;\n",
			"c.cjs":        "module.exports = { a: 1 };\n",
		}},
		{"exports 的 types 在 import 之后（顺序不影响：types 与 main 各取各的）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"import":"./i.js","types":"./t.d.ts"}}}`,
			"i.js":         "export const a = 1;\n",
			"t.d.ts":       "export declare const a: number;\n",
		}},
		{"exports 的 import 是空白串（trim 之后为空 ⇒ 两侧都不算入口）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"import":"   "}}}`,
			"index.js":     "export const a = 1;\n",
		}},
		{"exports 的目标带 `*` 模式（按字面采用，两侧一致）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","exports":{".":{"import":"./x-*.js"}}}`,
			"x-1.js":       "export const a = 1;\n",
		}},
		{"main 用**反斜杠**分隔（npm 世界里常见于 Windows 作者）", map[string]string{
			"package.json":   `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"./src\\entry.js","types":"./src\\entry.d.ts"}`,
			"src/entry.js":   "export const a = 1;\n",
			"src/entry.d.ts": "export declare const a: number;\n",
		}},
		{"main 有**多个前导斜杠**（`//src/x.js`）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"//src/x.js"}`,
			"src/x.js":     "export const a = 1;\n",
		}},
		{"main 前后有空白（trim 两侧一致）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"   ./m.js   "}`,
			"m.js":         "export const a = 1;\n",
		}},
		{"main 是裸文件名（没有 `./` 前缀）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT","main":"m.js"}`,
			"m.js":         "export const a = 1;\n",
		}},
	}

	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range sh.files {
				writeFileAt(t, dir, rel, content)
			}

			// 产品侧：真正会写进 mappings 的那个入口。
			entry, _ := mappings.InferEntry(dirReader(dir))

			// 自检器侧：解析它自己打印的那两句话（`入口 …` / `类型入口 …`）。
			out, _ := runChecker(t, bash, script, dir)
			rep := parseStdReport(t, out)

			checkerMain := checkerEntryOf(t, rep, out, "entry-resolvable", reEntry)
			checkerTypes := checkerEntryOf(t, rep, out, "types-provided", reTypes)

			if checkerMain != entry.Main {
				t.Errorf("两侧**入口**不一致：\n  产品 InferEntry  = %q（%s）\n  自检器报告      = %q\n  形态：%s",
					entry.Main, entry.Source, checkerMain, sh.name)
			}
			if checkerTypes != entry.Types {
				t.Errorf("两侧**类型入口**不一致：\n  产品 InferEntry  = %q（%s）\n  自检器报告      = %q\n  形态：%s",
					entry.Types, entry.Source, checkerTypes, sh.name)
			}
		})
	}
}

// whyOf 取某条判据的说明文本（parsed report 里）。
func whyOf(rep stdReport, id string) string {
	for _, c := range rep.Checks {
		if c.ID == id {
			return c.Why
		}
	}
	return ""
}

// dirReader 把**真实目录**包成 mappings.FileReader（产品侧入口推断读的就是这种读器）。
func dirReader(root string) mappings.FileReader {
	return func(rel string) ([]byte, bool, error) {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, false, nil
		}
		return b, true, nil
	}
}
