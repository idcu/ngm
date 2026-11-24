package main

import (
	"regexp"
	"strings"
	"testing"
)

// W0/E3：能力矩阵进 docs 门禁。
//
// 解析边界（刻意写死，避免"判据看着空气"）：
//   - 只读 docs/internals/capability-matrix.md 里 <!-- v222:begin/end --> 之间的三列表格；
//     其它正文段落不是本判据的输入。
//   - 命令事实源是 cmd/ngm/root.go 的 commands 表（只读，不改命令注册）。
//   - adapter 事实源是 internal/adapter/catalog.go 的 AdapterKind 常量声明。
//
// 双向守：矩阵说 done 的必须真注册；矩阵说 excluded 的不得偷偷复活。
// canonical 集合保证"整行删掉"也会变红，而不只是"改名成不存在的名字"才红。
func TestV222CapabilityMatrixMatchesCode(t *testing.T) {
	const rel = "docs/internals/capability-matrix.md"
	doc := readRepoFile(t, rel)
	// 标记必须**独占一行**：导言里用反引号提到标记名不算边界。
	const begin, end = "\n<!-- v222:begin -->\n", "\n<!-- v222:end -->\n"
	i, j := strings.Index(doc, begin), strings.Index(doc, end)
	if i < 0 || j <= i {
		t.Fatalf("%s 缺少成对的 %s / %s 解析标记——门禁没有输入", rel, begin, end)
	}
	section := doc[i+len(begin) : j]

	rowRe := regexp.MustCompile(`^\|\s*([a-z]+)\s*\|\s*([a-z0-9_-]+)\s*\|\s*([a-z-]+)\s*\|\s*$`)
	rows := map[string]map[string]string{"command": {}, "adapter": {}}
	count := 0
	for _, line := range strings.Split(section, "\n") {
		m := rowRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		if _, ok := rows[m[1]]; !ok {
			t.Errorf("矩阵出现未知类别 %q（解析边界：三列表格）", m[1])
			continue
		}
		if _, dup := rows[m[1]][m[2]]; dup {
			t.Errorf("矩阵重复行：%s/%s", m[1], m[2])
		}
		rows[m[1]][m[2]] = m[3]
		count++
	}
	if count < 30 {
		t.Fatalf("只解析到 %d 行矩阵数据——解析边界或表格格式变了，门禁不能静默变松", count)
	}

	// ① 命令：done 必须在 commands 表；excluded 必须不在。
	root := readRepoFile(t, "cmd/ngm/root.go")
	cmdRe := regexp.MustCompile(`\{\s*Name:\s*"([a-z0-9_-]+)"`)
	registered := map[string]bool{}
	for _, m := range cmdRe.FindAllStringSubmatch(root, -1) {
		registered[m[1]] = true
	}
	if len(registered) < 20 {
		t.Fatalf("从 root.go commands 表只认出 %d 条命令——表的写法变了，门禁不能静默变松", len(registered))
	}
	for name, status := range rows["command"] {
		switch status {
		case "done":
			if !registered[name] {
				t.Errorf("矩阵标记已实现的命令 %q 在 root.go commands 表里不存在（文档漂移）", name)
			}
		case "excluded":
			if registered[name] {
				t.Errorf("矩阵标记不做的能力 %q 竟被注册为命令（偷偷复活）", name)
			}
		default:
			t.Errorf("命令 %q 的状态 %q 非法（只允许 done / excluded）", name, status)
		}
	}
	// 删掉任意一行已实现命令也必须红（只改名不足以覆盖"整行丢失"这种漂移）。
	for _, name := range []string{
		"init", "add", "install", "run", "verify", "build", "transform",
		"css", "engines", "mappings", "gc", "sbom", "attest", "modules",
	} {
		if rows["command"][name] != "done" {
			t.Errorf("矩阵缺少已实现命令行 command/%s（canonical 集合）", name)
		}
	}

	// ② adapter：矩阵行的集合必须等于 catalog 声明的 AdapterKind 集合。
	catalog := readRepoFile(t, "internal/adapter/catalog.go")
	kindRe := regexp.MustCompile(`Adapter([A-Za-z]+)\s+AdapterKind\s*=\s*"([a-z]+)"`)
	declared := map[string]bool{}
	for _, m := range kindRe.FindAllStringSubmatch(catalog, -1) {
		declared[m[2]] = true
	}
	if len(declared) < 4 {
		t.Fatalf("从 catalog.go 只认出 %d 个 adapter kind——声明写法变了，门禁不能静默变松", len(declared))
	}
	for kind := range declared {
		status, ok := rows["adapter"][kind]
		if !ok {
			t.Errorf("矩阵缺少 catalog 声明的 adapter kind %q", kind)
			continue
		}
		want := "done"
		if kind == "remote" {
			want = "excluded"
		}
		if status != want {
			t.Errorf("adapter/%s 状态为 %q，应为 %q", kind, status, want)
		}
	}
	for kind, status := range rows["adapter"] {
		if !declared[kind] {
			t.Errorf("矩阵写了 catalog 未声明的 adapter kind %q", kind)
		}
		if status != "done" && status != "excluded" {
			t.Errorf("adapter %q 的状态 %q 非法", kind, status)
		}
	}
	// ③ remote 在代码里也必须仍是"按决定排除"，不是"还没做"/"已支持"。
	if !strings.Contains(catalog, "case AdapterRemote:") ||
		!strings.Contains(catalog, "excluded by decision") ||
		!strings.Contains(catalog, "IssueUnimplemented") {
		t.Error("catalog.go 必须仍把 remote adapter 标记为按决定排除（ADR-013）")
	}
}
