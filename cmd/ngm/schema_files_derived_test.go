package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// v0.64：**`schemaFiles` 那份清单本身也要有来源**。
//
// 起点是那张清单自己的注释（v0.56 普查时被抄进复盘）：
//
//	新增一个用户可编辑的 schema（新的配置文件）时，把它加进这里——
//	这是本检查唯一需要人工维护的地方，而漏加的表现是"检查少了几个键"，不是误报。
//
// 也就是说：**检查变松是可以静默发生的** ✗——而这正是本项目一直在杀的那类缺陷
// （v0.45「按文件名找源码」· v0.46「覆盖集从哪来」· v0.57「来源按行为派生」）。
//
// 这一版把它派生化，用**两处真实来源**对账：
//
//	文档侧：`docs/guides/configuration.md` 开头那张**配置文件表**（首格是反引号包着的 .json 名）
//	        ——文档自己说"这些是配置文件"，那一列就是它的声明；
//	代码侧：名字出现在**代码行**（跳过纯注释行）里、而且是"赋给标识符"或"作为
//	        `filepath.Join(` 的参数"——**定义方要解析路径**，消费者只是提到它；
//	        再加一条过滤：该文件**至少有一个带 json tag 的字段**（它确实在定义 schema）。
//
// 三条闸门缺一不可，看两个反例：
//
//	· `internal/verify/graph.go`：`const UpstreamFile = "ngm.json"`——名字一样，
//	  但那处是**读上游清单**的地方（它没有任何 json tag 字段）⇒ 不进来 ✓；
//	· `internal/lock/schema.go`：有 11 个 json tag 字段，但它命名的是 `ngm.lock`
//	  （不是 .json、也没在配置表里）⇒ 不进来 ✓。
//
// 于是"新增一份用户可编辑的配置"的路径变成：**文档加一行 ⇒ 这张判据红 ⇒ 清单被迫补齐**，
// 而不是"我记不记得加"。
func TestV64SchemaFilesAreDerivedFromDocsAndSource(t *testing.T) {
	root := repoRoot(t)

	// ---- ① 文档侧：配置文件表里的名字 ----
	doc := readDoc(t, "../../docs/guides/configuration.md")
	names := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		m := reConfigTableRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// 首格可能是 `~/.ngm/config.json` 这样的路径，取基名。
		names[filepath.Base(m[1])] = true
	}
	if len(names) < 3 {
		t.Fatalf("配置表里只认出 %d 个文件名——先看 `docs/guides/configuration.md` 那张表是不是改了形状",
			len(names))
	}

	// ---- ② 代码侧：谁在**定义**这些文件的 schema ----
	derived := map[string]bool{}
	for _, f := range nonTestGoFiles(t, root) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !reJSONTagField.MatchString(text) {
			continue // 没有 json tag 字段 ⇒ 它不在定义 schema
		}
		if !namesItsFilesInCode(text, names) {
			continue // 只是提到名字（消息、日志、注释）⇒ 不进来
		}
		rel, rerr := filepath.Rel(root, f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		derived[filepath.ToSlash(rel)] = true
	}

	if len(derived) == 0 {
		t.Fatal("派生的 schema 文件集合是空的——这条判据的范围缩到零了")
	}

	// ---- ③ 两个方向对账 ----
	list := map[string]bool{}
	for _, rel := range schemaFiles {
		list[rel] = true
	}
	var missing, extra []string
	for rel := range derived {
		if !list[rel] {
			missing = append(missing, rel)
		}
	}
	for rel := range list {
		if !derived[rel] {
			extra = append(extra, rel)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("这些文件在**定义**用户可编辑的 schema（文档点名了它的文件名、它自己解析那个路径、"+
			"而且有 json tag 字段），却不在 `schemaFiles` 里：%s\n"+
			"漏加的后果不是误报，而是**检查少了几个键**——把它加进去", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		t.Errorf("`schemaFiles` 里的这些文件派生不出来（没有解析文档点名的 .json 名，或没有 json tag 字段）：%s\n"+
			"清单与事实对不上时，先问是哪一边陈旧了", strings.Join(extra, ", "))
	}
	t.Logf("schema files: %d derived (%s) · %d doc name(s)", len(derived),
		strings.Join(sortedKeys(derived), ", "), len(names))
}

// reConfigTableRow 抓配置文件表的一行：首格是反引号包着的、以 .json 结尾的 token。
//
// 只看首格：`configuration.md` 里其它表的第一格是键名（`types` / `vendor` 之类），
// 不会以 .json 结尾——这条形状限制就是"哪些行在说配置文件"的判据。
var reConfigTableRow = regexp.MustCompile("(?m)^\\|\\s*`([^`]+\\.json)`\\s*\\|")

// reJSONTagField 与 `config_coverage_test.go` 的 fieldWithTag 同形，
// 但在本判据里只用来回答"这个文件在定义 schema 吗"。
var reJSONTagField = regexp.MustCompile("(?m)^\\s*[A-Z][A-Za-z0-9]*\\s+\\S.*?json:\"")

// namesItsFilesInCode 判断一个源文件是否**在代码行里**给这些文件名落了地。
//
// 两种形态（都要**跳过纯注释行**——注释里提名字不算：`config.go` 的注释里
// 写着 `// ProjectFile 是项目根目录的 ngm.json。`，那不是定义）：
//
//	X = "ngm.json"                    —— 名字常量（catalog.go / mappings/schema.go）
//	filepath.Join(…, "ngm.json")      —— 解析路径（config.go 的两处）
//
// 已知边界：`filepath.Join` 的参数若跨行书写，这里看不见——那时判据会走"少了一份"的
// 分支红一次，作者照它说的把名字写成常量即可（比静默变松好）。
func namesItsFilesInCode(text string, names map[string]bool) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for name := range names {
			lit := `"` + name + `"`
			if !strings.Contains(line, lit) {
				continue
			}
			if reAssignmentToName.MatchString(line) || strings.Contains(line, "filepath.Join(") {
				return true
			}
		}
	}
	return false
}

// reAssignmentToName 匹配 `X = "…"`（名字常量那一形态）。
var reAssignmentToName = regexp.MustCompile(`^\s*[A-Za-z_]\w*\s*=\s*"`)

// nonTestGoFiles 列出仓库里所有非测试的 .go 文件（跳过 testdata 与 vendor）。
func nonTestGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "testdata" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
