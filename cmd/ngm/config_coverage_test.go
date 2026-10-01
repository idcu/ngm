package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// schemaFiles 是**用户可编辑**的 schema 定义文件（相对仓库根）。
//
// 新增一个用户可编辑的 schema（新的配置文件）时，把它加进这里——这是本检查
// 唯一需要人工维护的地方，而漏加的表现是"检查少了几个键"，不是误报。
var schemaFiles = []string{
	"internal/config/config.go",   // ngm.json、~/.ngm/config.json
	"internal/mappings/schema.go", // ngm.mappings.json
	"internal/adapter/catalog.go", // ngm.engines.json
}

// coverageExempt 是目前**已知**没有任何测试碰过的特征键。
//
// 规则：这条清单只允许变短。一个键若已被测试覆盖，它留在这里会让本检查失败——
// 那是刻意的，防止"修好了但没人把豁免删掉"，久了清单就没人信了。
// 目前**为空**，而这正是它该有的样子。
//
// 它曾有一项：`engines.typeDecl`——能力在 adapter 层有实现与单测，却没有任何命令
// 驱动它，因此那个配置键对用户没有任何效果。修法不是"补一条断言"，而是先给能力
// 一个入口：`ngm typedecl`（v0.4），随后这条豁免被删掉。
var coverageExempt = map[string]string{}

// camelKey 匹配**多段驼峰**的配置键。
//
// 为什么只查这一类：单段名（`name` / `version` / `path` / `permissions`）在任何
// 测试文件里都会偶然出现，拿它们当判据等于没判据。多段名（`verifyOnLock` /
// `minimumReleaseAge` / `globalPath`）不会偶然出现，所以"它从没在测试里出现过"
// 是一个强信号。
//
// **本检查拦不住什么**（写清楚，免得被当成比实际更强的东西）：
//
//  1. 单段名的漏接 —— `permissions` 就是这么漏的（v0.3 D 组）
//  2. "出现了但没断言行为" —— 在测试里写一个 JSON 键不等于断言了它的效果
//  3. 非 JSON 的配置面（Go 结构体字段、命令行 flag）
//
// 它拦得住的是"特征名从来没有被任何测试碰过"——而三处同类历史缺陷
// （verifyOnLock / permissions / SubPaths）里，两处正是特征名。
var camelKey = regexp.MustCompile(`^[a-z]+(?:[A-Z][a-z0-9]+)+$`)

// fieldWithTag 抓一行结构体字段：**字段名** + json 键名。
//
// 字段名必须从源码里取，不能从键名猜：`osvIgnoreSeverities` 对应的 Go 字段是
// `OSVIgnoreSeverities`（缩写大写化是 Go 的惯例，机械转换推不出来）。
// 初版正是靠猜，于是把一处**已经接好线**的字段误报成"没有任何测试碰过"。
var fieldWithTag = regexp.MustCompile("(?m)^\\s*([A-Z][A-Za-z0-9]*)\\s+\\S.*?json:\"([a-zA-Z][a-zA-Z0-9]*)")

// TestConfigKeysAreExercisedByTests 是"声明了、没接线"那类缺陷的机械化检查。
//
// 背景（[v0.3 复盘](../docs/development/v0.3-retrospective.md) §4）：同类缺陷
// **连续三个版本**出现——`verifyOnLock`（v0.2）、`permissions`（v0.3 D）、
// `SubPaths`（v0.3 E）——共同特征都是"字段齐备、注释完整、没有任何地方读它"，
// 而且全都是靠人工核对才发现的。v0.2 复盘提过"让验收条目成为锚点"，没落地，
// 于是又出现两次。
//
// 判据刻意定在"能做到"的地方：**一个特征键必须在某个测试文件里出现过**
// （以 JSON 键的形式，或以 Go 字段选择器的形式）。它比人工清单弱，但它是机械的、
// 每次 CI 都跑的，而人工清单已经连续失效三次。
func TestConfigKeysAreExercisedByTests(t *testing.T) {
	root := repoRoot(t)

	keys, fields := collectSchemaKeys(t, root)
	if len(keys) == 0 {
		t.Fatal("no config keys were found: the scan is broken, not the code")
	}

	testText, files := collectTestText(t, root)
	if files == 0 {
		t.Fatal("no test files were scanned: the check would pass vacuously")
	}

	var missing []string
	for _, k := range keys {
		if !exercised(testText, k, fields) {
			if _, ok := coverageExempt[k]; !ok {
				missing = append(missing, k)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these config keys are never exercised by any test: %s\n"+
			"a key that no test reads is how verifyOnLock, permissions and SubPaths each shipped unwired;\n"+
			"add an assertion that sets it from config and observes the behaviour, or record why it cannot be",
			strings.Join(missing, ", "))
	}

	// 反向：已被覆盖的键不该留在豁免清单里（清单只允许变短）。
	for k, why := range coverageExempt {
		if !contains(keys, k) {
			t.Errorf("coverageExempt lists %q, which is no longer a config key; drop it", k)
			continue
		}
		if exercised(testText, k, fields) {
			t.Errorf("coverageExempt still lists %q (%s), but a test now exercises it; drop the exemption", k, why)
		}
	}
}

// exercised 判断某个配置键是否在测试里被"碰过"。
//
// 接受三种写法，都是"确实在设置这个字段"的常见形态：
//
//	"key":            JSON 配置片段
//	.FieldName        结构体字段选择器
//	FieldName:        结构体字面量的键
//
// 第三种不可省：表驱动的配置测试几乎都写成 `SupplyChainConfig{OSVIgnoreSeverities: …}`。
func exercised(testText, key string, fields map[string]string) bool {
	if strings.Contains(testText, `"`+key+`"`) {
		return true
	}
	f, ok := fields[key]
	if !ok {
		return false
	}
	return strings.Contains(testText, "."+f) || strings.Contains(testText, f+":")
}

// repoRoot 从当前目录向上找 go.mod。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found in any parent directory")
		}
		dir = parent
	}
}

// collectSchemaKeys 读出所有 schema 文件里的 JSON 键（只取特征名），
// 并返回 键 → Go 字段名 的对应关系。
func collectSchemaKeys(t *testing.T, root string) ([]string, map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	fields := map[string]string{}
	var out []string
	for _, rel := range schemaFiles {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for _, m := range fieldWithTag.FindAllStringSubmatch(string(data), -1) {
			field, k := m[1], m[2]
			fields[k] = field
			if !camelKey.MatchString(k) || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, fields
}

// collectTestText 把仓库里所有测试文件的文本拼起来。
//
// 排除本文件：否则豁免清单和键名本身会成为"证据"，检查就自我满足了。
func collectTestText(t *testing.T, root string) (string, int) {
	t.Helper()
	var sb strings.Builder
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "config_coverage_test.go" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		files++
		sb.Write(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return sb.String(), files
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
