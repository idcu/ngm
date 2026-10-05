package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 这一组网守同一件事：**文档里手工写下的字段清单，不许与实现脱节。**
//
// 两处：
//
//	① `cli.md` 的《`--json` 的形状》表        —— 机器接口的顶层键（v0.16 实测写下的）
//	② `configuration.md` 的三张「字段」表      —— `ngm.json` / `dependencies` / `supplyChain` 的键
//
// 它们都是**第二份事实源**：实现是份一，文档是份二。
// v0.24 刚证明过这类漂移真实存在（退出码那四处），而**其中两处此前没有任何网**。

// jsonShapeSourceFiles 把 `--json` 形状表里的命令映射到实现文件的 glob。
//
// 为什么是"按命令"而不是"到处找"：字段名是**按命令**成立的契约。
// "这个键在仓库某处存在"太弱——`version` 在好几个报告里都有，
// 而 `verify` 少写 `allowDrift` 时，靠"某处存在"是抓不到的。
//
// 后两条指向 **CLI 层自己**：`integrations` 与 `engines` 的报告结构体就写在
// `cmd/ngm/` 里，不在 internal。第一版把它们指到 internal 的同名包，
// 网于是对着"没有这些字段的地方"找字段——**映射写错时，网会报一大堆假缺陷**。
var jsonShapeSourceFiles = map[string][]string{
	"verify":       {"../../internal/verify/*.go"},
	"audit":        {"../../internal/supplychain/*.go"},
	"tree":         {"../../internal/observability/*.go"},
	"why":          {"../../internal/observability/*.go"},
	"outdated":     {"../../internal/observability/*.go"},
	"integrations": {"./integrations*.go"},
	"engines":      {"./engines*.go"},
}

// configSectionFiles 把 configuration.md 的**章节**映射到实现文件的 glob。
//
// 注意 `supplyChain` 指向 `internal/config`：那张表描述的是**你在 ngm.json 里写的键**
// （`SupplyChainConfig` 的 json tag），而 `internal/supplychain` 是**策略的解析与判定**
// （方法形态：`pol.VerifyOnLock()`），里面没有这些 tag。
// 第一版指错了地方，6 个键全成了假缺陷——**映射是这类网唯一会骗人的地方**。
var configSectionFiles = map[string][]string{
	"ngm.json":     {"../../internal/config/*.go"},
	"dependencies": {"../../internal/config/*.go"},
	"supplyChain":  {"../../internal/config/*.go"},
}

// notAShapeKey 列出形状表里被反引号引着、但**不是字段名**的词。
//
// 目前只有一处：`engines info` 那行括号里举了个引擎名当例子。
// 名单**自己会过期**——每条都必须仍出现在表里，否则测试红（v0.15 的负例名单同一套路）。
var notAShapeKey = map[string]string{
	"esbuild": "`engines info` 行括号里的**举例**（一个引擎名字），不是字段名",
}

var (
	reShapeRow = regexp.MustCompile("^\\|\\s*`ngm ([a-z-]+)")
	reBacktick = regexp.MustCompile("`([A-Za-z][A-Za-z0-9_.\\[\\]]*)`")
	reJSONTag  = regexp.MustCompile(`json:"([^",]+)`)
	reH2       = regexp.MustCompile(`^## ([^#].*)`)
	reFieldRow = regexp.MustCompile("^\\|\\s*`([A-Za-z][A-Za-z0-9]*)`")
)

// TestV25JSONShapeTableMatchesTheImplementation 让那张**手工写下的**形状表自己站住。
//
// `cli.md` 的《`--json` 的形状》是 v0.16 **实测**出来的：在那之前机器接口的形状
// 只存在于代码里，写脚本的人只能靠试。但表是手写的——**列一个实现里不存在的键，
// 或者实现改了名字而表没跟着改，没有任何东西会红。**
//
// 与 v0.16 的网分工：那张网证明"运行时的输出确实长这样"，
// 这张网证明"文档写下来的形状没跟实现脱节"。
//
// **它证明什么、不证明什么**：它证明"表里写了的键都真的存在"；
// **不**证明"该写的键都写了"——从文档里删掉一个键，这张网不会红。
// 反向（把顶层结构体的字段集与文档集合逐一对齐）需要知道哪个类型是顶层报告，
// 列为 v0.26 候选。
func TestV25JSONShapeTableMatchesTheImplementation(t *testing.T) {
	rows := parseJSONShapeTable(t)
	if len(rows) == 0 {
		t.Fatal("no `--json` shape rows found in docs/guides/cli.md — the net's scope shrank to nothing")
	}

	exemptSeen := map[string]bool{}
	checked := 0

	for _, row := range rows {
		globs, ok := jsonShapeSourceFiles[row.id()]
		if !ok {
			globs, ok = jsonShapeSourceFiles[row.command]
		}
		if !ok {
			t.Errorf("the shape table documents `ngm %s --json`, but the net has no implementation "+
				"mapping for it — add one (a new command's shape must be checked too)", row.id())
			continue
		}
		tags := jsonTagsOf(t, globs)
		if len(tags) == 0 {
			t.Errorf("no json tags found for `ngm %s` via %v — the mapping is wrong", row.id(), globs)
			continue
		}
		for _, key := range row.keys {
			if _, exempt := notAShapeKey[key]; exempt {
				exemptSeen[key] = true
				continue
			}
			checked++
			if !tags[key] {
				t.Errorf("`ngm %s --json` is documented with the key %q, but no such json tag exists in %v",
					row.id(), key, globs)
			}
		}
	}

	for tok, why := range notAShapeKey {
		if !exemptSeen[tok] {
			t.Errorf("notAShapeKey[%q] (%s) no longer appears in the shape table — the exemption must go", tok, why)
		}
	}
	if checked == 0 {
		t.Fatal("no documented key was checked — a net that checks nothing is not a net")
	}
	t.Logf("`--json` shape table: %d commands, %d documented keys checked against the implementation",
		len(rows), checked)
}

// TestV25ConfigFieldTablesMatchTheSchema 让 `configuration.md` 的三张「字段」表自己站住。
//
// 它们是 `ngm.json` / `dependencies` / `supplyChain` 的**第二份事实源**（第一份是结构体）。
// 用户的动作是照着表写配置——**表里多一个键，用户就写不出一份能读的配置。**
func TestV25ConfigFieldTablesMatchTheSchema(t *testing.T) {
	tables := parseConfigFieldTables(t)
	if len(tables) == 0 {
		t.Fatal("no 「字段」 tables found in docs/guides/configuration.md — the net's scope shrank to nothing")
	}

	checked := 0
	for _, tbl := range tables {
		globs, ok := configSectionFiles[tbl.section]
		if !ok {
			t.Errorf("configuration.md has a field table under `## %s`, but the net has no implementation "+
				"mapping for that section — add one", tbl.section)
			continue
		}
		tags := jsonTagsOf(t, globs)
		if len(tags) == 0 {
			t.Errorf("no json tags found for section %q via %v — the mapping is wrong", tbl.section, globs)
			continue
		}
		for _, key := range tbl.keys {
			checked++
			if !tags[key] {
				t.Errorf("configuration.md documents the %s field %q, but no such json tag exists in %v",
					tbl.section, key, globs)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no documented config field was checked — a net that checks nothing is not a net")
	}
	t.Logf("configuration.md: %d field tables, %d documented keys checked against the schema", len(tables), checked)
}

type shapeRow struct {
	command string
	sub     string // 子命令（如 `engines validate` 的 validate）；没有则为空
	keys    []string
}

// id 返回这一行的查找键：优先 `<命令> <子命令>`，退回 `<命令>`。
//
// 为什么要把子命令分出来（v0.26）：`engines` 的三行里，
// `list` / `info` 的顶层是**数组**（元素是 `engineRow`），`validate` 是**对象**
// （`enginesValidateReport`）——混成一行就没法说清"这些键属于哪个类型"。
func (r shapeRow) id() string {
	if r.sub != "" {
		return r.command + " " + r.sub
	}
	return r.command
}

// parseJSONShapeTable 从 cli.md 的《`--json` 的形状》表里取出 命令 → 字段名。
func parseJSONShapeTable(t *testing.T) []shapeRow {
	t.Helper()
	raw, err := os.ReadFile("../../docs/guides/cli.md")
	if err != nil {
		t.Fatalf("the shape table is unreadable: %v", err)
	}

	var rows []shapeRow
	inTable := false
	sawRow := false
	for _, line := range strings.Split(string(raw), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "## `--json` 的形状") {
			inTable = true
			continue
		}
		if !inTable {
			continue
		}
		// 标题与表之间还有一段正文——只有**开始见过表格行之后**，
		// 遇到非表格行才算结束。（第一版在这里直接 break，一行都没解析到。）
		if l == "" || !strings.HasPrefix(l, "|") {
			if sawRow {
				break
			}
			continue
		}
		if strings.HasPrefix(l, "|--") || strings.HasPrefix(l, "| 命令") {
			continue
		}
		sawRow = true
		m := reShapeRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		row := shapeRow{command: m[1]}
		// 第二个词是子命令吗？`integrations add <tool>` / `engines validate` 是；
		// `why <dep>` 与 `verify --json` 不是（含 `<` 或以 `-` 开头）。
		if rest := strings.Fields(strings.TrimPrefix(l, "| `ngm "+m[1])); len(rest) > 1 {
			if w := rest[0]; !strings.HasPrefix(w, "-") && !strings.ContainsAny(w, "<>") {
				row.sub = w
			}
		}
		for _, k := range reBacktick.FindAllStringSubmatch(l, -1) {
			tok := k[1]
			if i := strings.LastIndex(tok, "."); i >= 0 {
				tok = tok[i+1:] // `summary.exitCode` ⇒ `exitCode`
			}
			if strings.Contains(tok, "--json") || tok == row.command {
				continue
			}
			row.keys = append(row.keys, tok)
		}
		sort.Strings(row.keys)
		rows = append(rows, row)
	}
	return rows
}

type fieldTable struct {
	section string
	keys    []string
}

// parseConfigFieldTables 取 configuration.md 里以「字段」为表头首格的表，
// 以及它所属的 `## 章节`。
func parseConfigFieldTables(t *testing.T) []fieldTable {
	t.Helper()
	raw, err := os.ReadFile("../../docs/guides/configuration.md")
	if err != nil {
		t.Fatalf("configuration.md is unreadable: %v", err)
	}

	var tables []fieldTable
	section := ""
	inFields := false
	for _, line := range strings.Split(string(raw), "\n") {
		l := strings.TrimSpace(line)
		if m := reH2.FindStringSubmatch(l); m != nil {
			section = strings.TrimSpace(m[1])
			inFields = false
			continue
		}
		if strings.HasPrefix(l, "| 字段 |") {
			inFields = true
			tables = append(tables, fieldTable{section: section})
			continue
		}
		if !inFields {
			continue
		}
		if l == "" || !strings.HasPrefix(l, "|") {
			inFields = false
			continue
		}
		m := reFieldRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		tables[len(tables)-1].keys = append(tables[len(tables)-1].keys, m[1])
	}
	return tables
}

// jsonTagsOf 收集给定 glob 命中的**非测试** Go 文件里的全部 json tag。
func jsonTagsOf(t *testing.T, globs []string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	files := 0
	for _, g := range globs {
		matches, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if strings.HasSuffix(m, "_test.go") {
				continue
			}
			body, rerr := os.ReadFile(m)
			if rerr != nil {
				t.Fatal(rerr)
			}
			files++
			for _, tag := range reJSONTag.FindAllStringSubmatch(string(body), -1) {
				out[tag[1]] = true
			}
		}
	}
	if files == 0 {
		t.Errorf("no non-test Go files matched %v — the mapping's scope silently shrank", globs)
	}
	return out
}
