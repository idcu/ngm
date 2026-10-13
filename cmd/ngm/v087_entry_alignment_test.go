package main

import (
	"os"
	"path/filepath"
	"regexp"
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

var reEntry = regexp.MustCompile(`入口 (\S+)（来自`)

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
		{"两侧都找不到入口（ngm.json 存在但没有任何入口线索）", map[string]string{
			"package.json": `{"name":"a","version":"1.0.0","type":"module","license":"MIT"}`,
			"README.md":    "# a\n",
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

			// 自检器侧：解析它自己打印的那句话（`入口 <path>（来自 …`）。
			out, _ := runChecker(t, bash, script, dir)
			rep := parseStdReport(t, out)
			checkerEntry := ""
			status, found := findStatus(rep, "entry-resolvable")
			if !found {
				t.Fatalf("自检器没报 entry-resolvable：\n%s", out)
			}
			switch status {
			case "pass":
				m := reEntry.FindStringSubmatch(whyOf(rep, "entry-resolvable"))
				if m == nil {
					t.Fatalf("判据说通过了，但报告里读不出口径（措辞变了？）：%q", whyOf(rep, "entry-resolvable"))
				}
				checkerEntry = m[1]
			case "fail":
				checkerEntry = "" // 三层都没找到
			default:
				t.Fatalf("entry-resolvable 的状态是 %q（包形态下不该是这种）：\n%s", status, out)
			}

			if checkerEntry != entry.Main {
				t.Errorf("两侧入口不一致：\n  产品 InferEntry  = %q（%s）\n  自检器报告      = %q\n  形态：%s",
					entry.Main, entry.Source, checkerEntry, sh.name)
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
