package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// jsonShapeRowTypes 把形状表的**每一行**映射到它背后的**具名类型**。
//
// 这是 v0.25 那条限制的反面：那张网只证明"表里写了的键都真的存在"，
// **不**证明"该写的键都写了"——从文档里删掉一个键，它不会红。
// 反向判据需要知道"哪个类型是这个形状"，所以这张表必须存在。
//
// 于是它同时钉住一件事：**能让文档对得上的形状，必须有一个名字。**
// v0.26 之前 `integrations` 与 `engines validate` 用的都是**匿名结构体字面量**——
// 没有名字，就没有任何机械判据能把它与文档对齐。两处都已提成具名类型。
//
// 数组行（`engines list` / `info`）映射到**元素类型**：顶层是数组时，
// 文档里列的是元素的键，比对的对象自然也是元素类型。
var jsonShapeRowTypes = map[string]struct {
	file string
	typ  string
}{
	"verify":           {"../../internal/verify/report.go", "Report"},
	"audit":            {"../../internal/supplychain/report.go", "AuditReport"},
	"tree":             {"../../internal/observability/tree.go", "TreeReport"},
	"why":              {"../../internal/observability/why.go", "WhyReport"},
	"outdated":         {"../../internal/observability/outdated.go", "OutdatedReport"},
	"integrations add": {"./integrations.go", "integrationsReport"},
	"engines list":     {"./engines.go", "engineRow"},
	"engines info":     {"./engines.go", "engineRow"},
	"engines validate": {"./engines.go", "enginesValidateReport"},
}

// TestV26EveryDocumentedShapeIsANamedType 固定：**形状表里的每一行，都要有一个具名类型兜着。**
//
// 没有它，反向判据会静默地少看几行——而"少看几行"的表现与"全都对得上"一模一样。
func TestV26EveryDocumentedShapeIsANamedType(t *testing.T) {
	rows := parseJSONShapeTable(t)
	if len(rows) == 0 {
		t.Fatal("no shape rows found — the net's scope shrank to nothing")
	}
	for _, row := range rows {
		spec, ok := jsonShapeRowTypes[row.id()]
		if !ok {
			t.Errorf("the shape table documents `ngm %s --json`, but it is not mapped to a named type — "+
				"give the payload a name (an anonymous struct literal cannot be checked against the docs)",
				row.id())
			continue
		}
		if _, err := os.Stat(spec.file); err != nil {
			t.Errorf("`ngm %s` is mapped to %s, which is unreadable: %v", row.id(), spec.file, err)
		}
	}
	// 反向：映射表里不许有形状表里已经没有的行（否则它只是在纸上活着）。
	ids := map[string]bool{}
	for _, row := range rows {
		ids[row.id()] = true
	}
	for id := range jsonShapeRowTypes {
		if !ids[id] {
			t.Errorf("jsonShapeRowTypes[%q] no longer matches a row in the shape table — the entry must go", id)
		}
	}
}

// TestV26EveryFieldOfTheReportIsDocumented 是 v0.25 那张网的反面：
// **报告里的每个顶层字段，都必须在形状表里被点名。**
//
// 为什么两边都要：正向那张网挡的是"文档写了一个不存在的键"，
// 这一张挡的是"实现加了一个字段而文档没说"——**用户看得见、而文档没有名字的字段，
// 是一份被藏起来一半的契约。**
func TestV26EveryFieldOfTheReportIsDocumented(t *testing.T) {
	rows := parseJSONShapeTable(t)
	if len(rows) == 0 {
		t.Fatal("no shape rows found — the net's scope shrank to nothing")
	}

	checked := 0
	for _, row := range rows {
		spec, ok := jsonShapeRowTypes[row.id()]
		if !ok {
			continue // 上面那张网已经报过这一条
		}
		fields := structJSONTags(t, spec.file, spec.typ)
		if len(fields) == 0 {
			t.Errorf("no top-level json fields parsed out of %s in %s — the parser or the type name is wrong",
				spec.typ, spec.file)
			continue
		}
		documented := map[string]bool{}
		for _, k := range row.keys {
			documented[k] = true
		}
		for field := range fields {
			checked++
			if !documented[field] {
				t.Errorf("%s.%s has the json field %q, but the shape table for `ngm %s --json` does not list it — "+
					"a field users can see and the docs do not name is half a contract",
					spec.typ, field, field, row.id())
			}
		}
	}
	if checked == 0 {
		t.Fatal("no struct field was checked — a net that checks nothing is not a net")
	}
	t.Logf("shape table reverse check: %d top-level report fields verified as documented", checked)
}

// structJSONTags 取出**具名类型**在**顶层**声明的全部 json tag。
//
// 只取顶层：嵌套结构体的字段是另一份契约（由各自的类型/表负责），
// 混进来会让"该写的都写了"变成一句无法满足的话。
func structJSONTags(t *testing.T, path, typeName string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	header := regexp.MustCompile(`type ` + regexp.QuoteMeta(typeName) + ` struct \{`)
	loc := header.FindStringIndex(string(raw))
	if loc == nil {
		t.Fatalf("type %s not found in %s — the mapping is stale", typeName, path)
	}

	out := map[string]bool{}
	depth := 1
	for _, line := range strings.Split(string(raw)[loc[1]:], "\n") {
		if depth == 1 {
			for _, m := range reJSONTag.FindAllStringSubmatch(line, -1) {
				out[m[1]] = true
			}
		}
		depth += strings.Count(stripLiteralsAndComment(line), "{") -
			strings.Count(stripLiteralsAndComment(line), "}")
		if depth <= 0 {
			break
		}
	}
	return out
}

// stripLiteralsAndComment 去掉一行的字符串字面量与行末注释，**只用于数括号**。
//
// 不做这一步的话，`json:"{"` 之类会骗过括号计数——而括号计数错了，
// 网会安静地把嵌套字段也算成顶层字段（或者相反）。
func stripLiteralsAndComment(line string) string {
	var b strings.Builder
	inDouble, inBacktick := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inDouble:
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case inBacktick:
			if c == '`' {
				inBacktick = false
			}
		case c == '"':
			inDouble = true
		case c == '`':
			inBacktick = true
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
