package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
)

// 本文件是 v0.3 复盘 §7 那条待办的机械化形态：
//
//	每个配置字段都要有一条"从配置读到行为"的端到端断言；
//	没有断言的字段，不得在文档里被描述为已生效。
//
// 它已经连续四轮证明自己有效（verifyOnLock → permissions → SubPaths →
// git.tokenEnvVars），但一直靠**人工核对**执行，于是同类缺陷持续再生：
// v0.5 复核一次系统排查又查出 4 项（types / vendor.commit / 死类型 EngineRef /
// --concurrency）。本检查把"人工核对"换成每次 CI 都跑的机械核对。
//
// 与 cmd/ngm/config_coverage_test.go 的分工：
//
//	那一条的判据是"特征键名是否在某个测试文件里出现过"，而它的 camelKey 正则
//	只匹配**多段驼峰**，因此单段名的 types / commit 按设计就拦不住（它自己在
//	:42-43 写了这条限制）。
//
// 本检查不猜名字形状：用 reflect 枚举 struct 的**字段本身**，再要求每个字段
// 都在 wiredFields 表里登记一条覆盖它的测试函数。
//
// 三条判据，缺一即红：
//
//  1. 每个被枚举到的字段都必须在 wiredFields 里（缺 = 没有任何测试为它背书）
//  2. 表里的每个键都必须是真实存在的字段（字段删了而表项留着 = 表在说谎）
//  3. 表项指名的测试函数必须在仓库里真的存在（写错名字 = 假证据）
//
// 它拦不住什么（写清楚，免得被当成比实际更强的东西）：
//
//   - "断言了行为"与"只在 JSON 里出现过"仍由人判断：本检查只能保证**有人指认**
//     了一个测试函数，不能验证那条断言的强度
//   - 命令行 flag 不在枚举范围内（`ngm update --concurrency` 这类缺陷仍要人工查）

// wiredFields 是"字段 → 覆盖它的测试函数"的对照表。
//
// 键是 "Struct.Field"（**Go 字段名**，不是 JSON 键名）。用 Struct 限定是为了
// 避开不同 struct 的同名字段（`ProjectFile.Name` 与 `Dependency.Name` 是两回事），
// 而用 Go 字段名是因为它才是"被读取的那个东西"——JSON 键可以改名而代码不动，
// 反之则不行。
//
// 值是**测试函数名**（不带包名）。运行时会扫描仓库里所有 *_test.go 验证它真的
// 存在：一张自己都核不对的表，比没有表更糟。
//
// 维护纪律：新增配置字段时，这一行会先红；**先补断言再补表项**——
// 顺序反过来，这张表就会退化成"声明已接线"的另一种说法。
var wiredFields = map[string]string{
	// ---- ngm.json：ProjectFile ----
	"ProjectFile.SchemaVersion": "TestUnsupportedSchemaVersionIsRejected",
	"ProjectFile.Name":          "TestV02ObservabilityGolden",   // ngm tree 的根节点名取项目名（快照钉住）
	"ProjectFile.Version":       "TestProjectVersionIsRequired", // 必填 + 回显（裁定见下）
	// Runtime 是**声明**而非开关：它不改变 ngm 的行为（architecture/runtime-model.md
	// 明确"ngm 不干预宿主运行时"），文档因此不得把它与 name / dependencies 并列为
	// "已生效"。表项指向的用例固定它真实的契约：取值被校验、被 config show 回显。
	"ProjectFile.Runtime":      "TestRuntimeIsValidatedAcceptedAndEchoed",
	"ProjectFile.Main":         "TestM6Acceptance", // ngm build 不带参数时用 main 作入口
	"ProjectFile.Types":        "TestDeclaredTypesEntryReachesTheMappings",
	"ProjectFile.Dependencies": "TestAdd_TagDependency",
	"ProjectFile.Engines":      "TestM6Acceptance",
	"ProjectFile.Vendor":       "TestM4Acceptance",
	"ProjectFile.SupplyChain":  "TestV02SupplyChainAcceptance",

	// ---- ngm.json：Dependency ----
	"Dependency.Name":    "TestAdd_NormalizesEquivalentNames",
	"Dependency.Ref":     "TestUpdate_OfflineResolvesFromMirror",
	"Dependency.RefType": "TestAdd_RequiresRefType",
	"Dependency.Path":    "TestV03MappingsMonorepoAcceptance",

	// ---- ngm.json：EnginesConfig ----
	//
	// 这五个字段在代码里搜不到直接引用（v0.5 复核差点把它们报成缺陷），
	// 但它们经 AsMap() → adapter.Selection → subprocess.go 的 mergedOptions
	// 真的进入 argv：那是**经由 helper 的间接接线**，功能有效。
	"EnginesConfig.Transform": "TestM6Acceptance",
	"EnginesConfig.Bundle":    "TestM6Acceptance",
	"EnginesConfig.TypeCheck": "TestM6Acceptance",
	"EnginesConfig.TypeDecl":  "TestV04TypeDeclAcceptance",
	"EnginesConfig.CSS":       "TestM6Acceptance",

	// ---- ngm.json：VendorConfig ----
	"VendorConfig.Mode":       "TestVendorValidate", // global / local；mode=global 要求 globalPath
	"VendorConfig.LinkMode":   "TestM4Acceptance",   // linkMode 矩阵：hardlink / copy / symlink
	"VendorConfig.GlobalPath": "TestVendorValidate",

	// ---- ngm.json：SupplyChainConfig ----
	"SupplyChainConfig.AllowedGitHosts":     "TestPolicy_CheckRepo",
	"SupplyChainConfig.AllowlistRepos":      "TestPolicy_CheckRepo",
	"SupplyChainConfig.MinimumReleaseAge":   "TestPolicy_MinimumReleaseAgeAndVerifyOnLock",
	"SupplyChainConfig.OSVIgnoreSeverities": "TestAudit_IgnoredIsStillCounted",
	"SupplyChainConfig.PostInstallPolicy":   "TestV03PostInstallAcceptance",
	"SupplyChainConfig.VerifyOnLock":        "TestV02VerifyOnLockAcceptance",

	// ---- ~/.ngm/config.json：GlobalFile ----
	"GlobalFile.SchemaVersion": "TestUnsupportedSchemaVersionIsRejected",
	"GlobalFile.Git":           "TestGitTokenEnvVarsFromConfigReachTheRedactionSet",
	"GlobalFile.Engines":       "TestGlobalDefaultEnginesAreUsed",
	"GlobalFile.Permissions":   "TestV03PermissionsAcceptance",

	// ---- ~/.ngm/config.json：GlobalGit ----
	"GlobalGit.DefaultProtocol": "TestLoad_PicksUpGlobal",
	"GlobalGit.TokenEnvVars":    "TestGitTokenEnvVarsFromConfigReachTheRedactionSet",

	// ---- ~/.ngm/config.json：GlobalEngines ----
	"GlobalEngines.DefaultTransform": "TestGlobalDefaultEnginesAreUsed",
	"GlobalEngines.DefaultBundle":    "TestGlobalDefaultEnginesAreUsed",
	"GlobalEngines.DefaultTypeCheck": "TestGlobalDefaultEnginesAreUsed",

	// ---- ~/.ngm/config.json：GlobalPermissions ----
	"GlobalPermissions.Allow": "TestV03PermissionsAcceptance",
	"GlobalPermissions.Deny":  "TestV03PermissionsAcceptance",

	// ---- ngm.engines.json：adapter.Entry ----
	"Entry.Name":    "TestCatalog_OverlayAndFind",
	"Entry.Kind":    "TestCatalog_OverlayAndFind",
	"Entry.Adapter": "TestCatalog_OverlayAndFind",
	"Entry.Command": "TestSplitCommand",
	"Entry.Version": "TestDeclaredEngineVersionIsComparedWithTheLocalOne",
	// SupportedInput 只影响展示（`engines info` 有 input 行，`engines list` 没有列），
	// 它不参与能力选择——这是"接了线、但只是展示"的诚实登记，不是缺陷。
	"Entry.SupportedInput": "TestDeclaredSupportedInputIsListed",
	"Entry.DefaultOptions": "TestCatalog_OverlayAndFind",
	"Entry.Optional":       "TestBuiltinCatalog_IncludesAdaptedEngines",
	"Entry.Stub":           "TestRunner_DryRunPlansTheStub",
}

// TestEveryConfigFieldHasAWiringTest 是"声明了、没接线"那类缺陷的机械化检查。
//
// 判据与人工核对时一致（v0.5 复核 §实测 1）：字段要有 (a) 定义 (b) 读取
// (c) 读到的值改变了某个分支或输出。前两条由代码保证，第三条只能由断言证明——
// 本检查保证"每一条都有人为它指认了一个测试函数"。
func TestEveryConfigFieldHasAWiringTest(t *testing.T) {
	root := wiringRepoRoot(t)
	fields := enumerateConfigFields(t)
	if len(fields) < 30 {
		t.Fatalf("only %d fields were enumerated: the scan is broken, not the code", len(fields))
	}

	funcs := collectTestFuncNames(t, root)
	if len(funcs) == 0 {
		t.Fatal("no test functions were found: the check would pass vacuously")
	}

	// (1) 字段必须有表项
	var missing []string
	for _, k := range fields {
		if _, ok := wiredFields[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)

	// (2) 表项必须指向真实存在的字段
	var stale []string
	known := map[string]bool{}
	for _, k := range fields {
		known[k] = true
	}
	for k := range wiredFields {
		if !known[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)

	// (3) 表项指名的测试函数必须真的存在
	var bogus []string
	for k, fn := range wiredFields {
		if !funcs[fn] {
			bogus = append(bogus, k+" → "+fn)
		}
	}
	sort.Strings(bogus)

	if len(missing) > 0 {
		t.Errorf("these config fields have no test function in wiredFields (%d):\n  %s\n"+
			"a field that no test reads is how verifyOnLock, permissions, SubPaths, types and\n"+
			"vendor.commit each shipped unwired; add an assertion that sets it from config and\n"+
			"observes the behaviour, then name that test here",
			len(missing), strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("wiredFields lists keys that are no longer struct fields: %s\n"+
			"the struct changed; drop the entry (or rename it to the new field)",
			strings.Join(stale, ", "))
	}
	if len(bogus) > 0 {
		t.Errorf("wiredFields names test functions that do not exist anywhere in the repo:\n  %s\n"+
			"a table entry pointing at a missing test is worse than no entry: it reads as evidence",
			strings.Join(bogus, "\n  "))
	}
}

// enumerateConfigFields 用 reflect 枚举全部配置 struct 的 JSON 字段。
//
// 为什么用 reflect 而不是正则扫源码：字段的形状不该成为判据。`camelKey` 那类
// 正则会因为"名字只有一段"而漏掉 types / commit——那正是本检查要抓的东西。
// 反射枚举的是 struct 本身，与命名风格无关。
//
// 跳过 `json:"-"` 与非导出字段：它们不参与配置解析（`Entry.Program` 由
// `Command` 派生），要求它们有"从配置读到行为"的断言是没有意义的。
func enumerateConfigFields(t *testing.T) []string {
	t.Helper()

	specs := []struct {
		structName string
		value      any
	}{
		{"ProjectFile", ProjectFile{}},
		{"Dependency", Dependency{}},
		{"EnginesConfig", EnginesConfig{}},
		// EngineRef 曾在这里：它是死类型（零实例化、零字段访问），v0.5 删除。
		// 删掉之后本检查少枚举三个键，而那正是"少几个键"的正确用法——
		// 键变少是因为配置面变小了，不是因为扫描坏了（下方 len(fields) 守卫兜底）。
		{"VendorConfig", VendorConfig{}},
		{"SupplyChainConfig", SupplyChainConfig{}},
		{"GlobalFile", GlobalFile{}},
		{"GlobalGit", GlobalGit{}},
		{"GlobalEngines", GlobalEngines{}},
		{"GlobalPermissions", GlobalPermissions{}},
		// 引擎清单（ngm.engines.json）也是用户可编辑的配置面
		{"Entry", adapter.Entry{}},
	}

	var out []string
	for _, s := range specs {
		typ := reflect.TypeOf(s.value)
		if typ.Kind() != reflect.Struct {
			t.Fatalf("%s is a %s, not a struct: the enumeration needs updating", s.structName, typ.Kind())
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.PkgPath != "" {
				continue // 非导出字段不参与 JSON
			}
			tag := f.Tag.Get("json")
			if tag == "" {
				continue // 没有 json 标签 = 不是配置面
			}
			if strings.Split(tag, ",")[0] == "-" {
				continue
			}
			out = append(out, s.structName+"."+f.Name)
		}
	}
	sort.Strings(out)
	return out
}

// testFuncDecl 抓测试函数声明：`func TestXxx(`。
var testFuncDecl = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]*)\s*\(`)

// collectTestFuncNames 扫描仓库里所有 *_test.go，收集测试函数名。
func collectTestFuncNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
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
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		files++
		for _, m := range testFuncDecl.FindAllStringSubmatch(string(data), -1) {
			out[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if files == 0 {
		t.Fatal("no *_test.go files were scanned: the check would pass vacuously")
	}
	return out
}

// wiringRepoRoot 从当前目录向上找 go.mod。
func wiringRepoRoot(t *testing.T) string {
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
