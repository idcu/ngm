package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 这一版把"文档 ↔ 实现"的对账往**最散的那一份**推进：
//
//	① `observability.md` 等散文里点名的 json 字段（它们不在任何表里，散在句子里）
//	② `configuration.md` 的**反向**（字段是否存在却没被点名）
//
// 判据的收紧方式（按 v0.25 的教训，先量再写）：
//
//	"活文档里所有反引号驼峰标识符" ⇒ 误报 27/62（Go API、配置键、Git 配置键、占位符全卷进来）
//	"**提到 --json 的那些行**里的驼峰标识符" ⇒ 21 个 token，误报 0
//
// 判据的范围也是判据的一部分。

// livingDocs 列出**活文档**（会被后续删改、因此必须与实现一致的那些）。
//
// `docs/development/` 与 `docs/adr/` 是**快照**：按项目纪律，复盘与 ADR 不随之后的删改
// 修订（ADR 用补录）。让一张会红的网去要求改写历史，等于亲手拆掉那两条纪律。
func livingDocs(t *testing.T) []string {
	t.Helper()
	docs := []string{"../../README.md", "../../docs/README.md"}
	for _, g := range []string{
		"../../docs/guides/*.md",
		"../../docs/architecture/*.md",
		"../../docs/modules/*.md",
		"../../docs/internals/*.md",
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		if len(m) == 0 {
			t.Fatalf("no documents matched %s — the net's scope silently shrank to nothing", g)
		}
		docs = append(docs, m...)
	}
	return docs
}

var (
	// 反引号里的字段名，允许后面跟 `: 取值`（散文里最常见的写法是 `` `pathsTruncated: true` ``）。
	//
	// 第一版只认**纯**标识符（`` `pathsTruncated` ``），于是那句 `--json` 里对应
	// `pathsTruncated: true` 根本不被扫到——牙齿验证（改掉那个名字）**没有红**，
	// 才暴露出判据漏了一整类写法。
	reProseTok = regexp.MustCompile("`([a-z][A-Za-z0-9]*(?:\\.[A-Za-z0-9]+|\\[\\])*)(?::\\s*[^`]*)?`")
	reAnyJSON  = regexp.MustCompile(`json:"([^",]+)`)
)

// TestV27ProseJSONFieldNamesExist 固定：**散文里点名的 json 字段，必须在实现里真的存在。**
//
// 为什么盯"提到 `--json` 的行"：那是文档在**讲机器接口**的地方。
// 这些字段名散在句子里（"`--json` 里对应 `pathsTruncated: true`"），
// 既没有表可对，也不属于任何"形状"——于是它们是**最容易被改名遗漏的一份**。
func TestV27ProseJSONFieldNamesExist(t *testing.T) {
	tags := implementationJSONTags(t)
	if len(tags) == 0 {
		t.Fatal("no json tags found in the implementation — the net would pass vacuously")
	}

	seen := map[string][]string{}
	codeIdentifiers := packageIdentifiers(t)
	scanned, skippedIdents := 0, 0
	for _, doc := range livingDocs(t) {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, "--json") {
				continue
			}
			scanned++
			for _, m := range reProseTok.FindAllStringSubmatch(line, -1) {
				tok := m[1]
				if !strings.ContainsAny(tok, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
					continue // 全小写 ⇒ 多半是取值（`expected`）或文件名（`ngm.json`）
				}
				if strings.HasPrefix(tok, "ngm") {
					continue
				}
				if codeIdentifiers[tok] {
					skippedIdents++
					continue // 是仓库里定义的标识符（函数/类型/常量/变量），不是 json 字段
				}
				seen[tok] = append(seen[tok], filepath.Base(doc))
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no line mentioning --json was found in the living docs — the net's scope shrank")
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	checked := 0
	for _, tok := range keys {
		last := tok
		if i := strings.LastIndex(last, "."); i >= 0 {
			last = last[i+1:] // `summary.exitCode` ⇒ `exitCode`
		}
		last = strings.TrimSuffix(last, "[]") // `dependencies[]` ⇒ `dependencies`
		checked++
		if !tags[last] {
			t.Errorf("the living docs name the json field %q (in %s), but no such json tag exists in the "+
				"implementation — a field name in prose is part of the machine interface too",
				tok, strings.Join(seen[tok], ", "))
		}
	}
	if checked == 0 {
		t.Fatal("no prose field name was checked — a net that checks nothing is not a net")
	}
	t.Logf("prose check: %d lines mention --json, %d field names verified against the implementation, "+
		"%d token(s) skipped as code identifiers", scanned, checked, skippedIdents)
}

// configReverseSections 把 configuration.md 的章节映射到**结构体名**，用于反向判据。
//
// 只列两张**字段表**干净、且覆盖完整的章节：`ngm.json` 与 `dependencies`。
// `vendor` 与全局配置用的是**散文 + 小标题**（`### linkMode（落地方式）`），
// 没有字段表——机械判据在那里只会误报，因此**明确不做**（而不是做一半）。
var configReverseSections = map[string]struct {
	file string
	typ  string
}{
	"ngm.json":     {"../../internal/config/config.go", "ProjectFile"},
	"dependencies": {"../../internal/config/config.go", "Dependency"},
}

// TestV27EveryConfigFieldIsDocumented 是 v0.25 那张配置表的**反向**：
// 结构体的每个 json 字段，都必须在 `configuration.md` 里被点名过。
//
// 判据比形状表那张松一档：**允许字段在文件别处被点名**（正文、散文、小标题都算），
// 因为配置文档的写法本来就是"表 + 说明"混排。它仍然挡得住"加了一个谁都没写的字段"。
func TestV27EveryConfigFieldIsDocumented(t *testing.T) {
	raw, err := os.ReadFile("../../docs/guides/configuration.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	if len(doc) == 0 {
		t.Fatal("configuration.md is empty — the net would pass vacuously")
	}

	checked := 0
	for _, spec := range configReverseSections {
		fields := structJSONTags(t, spec.file, spec.typ)
		if len(fields) == 0 {
			t.Errorf("no top-level json fields parsed out of %s — the mapping is stale", spec.typ)
			continue
		}
		for field := range fields {
			checked++
			if !strings.Contains(doc, field) {
				t.Errorf("`%s` has the json field %q, but docs/guides/configuration.md never names it — "+
					"a config key users can write and the docs never mention is half a contract",
					spec.typ, field)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no config field was checked — a net that checks nothing is not a net")
	}
	t.Logf("config reverse check: %d fields verified as named in configuration.md", checked)
}

// implementationJSONTags 收集整个仓库（非测试 Go 代码）里的全部 json tag。
// packageIdentifiers 收集仓库里**定义**的 Go 标识符（函数 / 类型 / 常量 / 变量名）。
//
// 为什么需要它（v0.43）：这条判据把「提到 --json 的行上、含大写的反引号标识符」
// 当作 json 字段名，而文档里同样会提到 **Go 函数名**（`normalizeArgs`）——
// 它不是字段，也不该被要求是字段（实测它就红在这里）。
//
// 处置：**从源码派生"哪些名字是这个仓库里的标识符"**，而不是加一张会腐烂的白名单。
func packageIdentifiers(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.Walk("../..", func(path string, info os.FileInfo, werr error) error {
		if werr != nil || info == nil || info.IsDir() {
			return nil
		}
		slash := filepath.ToSlash(path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(slash, "/.git/") || strings.Contains(slash, "/testdata/") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				out[decl.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						out[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							out[n.Name] = true
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func implementationJSONTags(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/.git/") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, m := range reAnyJSON.FindAllStringSubmatch(string(body), -1) {
			out[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
