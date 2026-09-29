package mappings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// memReader 用内存文件表构造 FileReader（入口推断是纯逻辑，无需真实文件）。
func memReader(files map[string]string) FileReader {
	return func(rel string) ([]byte, bool, error) {
		if c, ok := files[rel]; ok {
			return []byte(c), true, nil
		}
		return nil, false, nil
	}
}

// ---------------------------------------------------------------------------
// 入口推断：三层规则（guides/configuration.md §入口推断）
// ---------------------------------------------------------------------------

func TestInferEntry_Layer1_NgmJSON(t *testing.T) {
	read := memReader(map[string]string{
		"ngm.json":     `{"main":"./src/index.ts","types":"./src/index.d.ts"}`,
		"package.json": `{"main":"./from-package.js"}`, // 应被第 1 层压制
		"index.ts":     "x",
	})
	e, warns := InferEntry(read)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if e.Main != "./src/index.ts" {
		t.Errorf("main=%q want ./src/index.ts", e.Main)
	}
	if e.Types != "./src/index.d.ts" {
		t.Errorf("types=%q", e.Types)
	}
	if e.Source != "ngm.json" {
		t.Errorf("source=%q", e.Source)
	}
}

func TestInferEntry_Layer1_PartialDeclared(t *testing.T) {
	// 只声明 types，也应认作命中第 1 层（main 留空由上游负责）
	read := memReader(map[string]string{
		"ngm.json":     `{"types":"./dist/index.d.ts"}`,
		"package.json": `{"main":"./fallback.js"}`,
	})
	e, _ := InferEntry(read)
	if e.Main != "" {
		t.Errorf("main should stay empty when only types is declared, got %q", e.Main)
	}
	if e.Types != "./dist/index.d.ts" {
		t.Errorf("types=%q", e.Types)
	}
	if e.Source != "ngm.json" {
		t.Errorf("source=%q", e.Source)
	}
}

func TestInferEntry_Layer2_PackageJSONVariants(t *testing.T) {
	cases := []struct {
		name      string
		pkg       string
		wantMain  string
		wantTypes string
	}{
		{
			name:     "exports string",
			pkg:      `{"exports":"./lib/main.js"}`,
			wantMain: "./lib/main.js",
		},
		{
			name:     "exports dot string",
			pkg:      `{"exports":{".":"./lib/main.js"}}`,
			wantMain: "./lib/main.js",
		},
		{
			name: "exports conditions",
			pkg: `{"exports":{".":{"types":"./lib/main.d.ts","import":"./lib/main.mjs",` +
				`"require":"./lib/main.cjs","default":"./lib/main.js"}}}`,
			wantMain:  "./lib/main.mjs", // import 优先
			wantTypes: "./lib/main.d.ts",
		},
		{
			name:      "main/types fallback",
			pkg:       `{"main":"./dist/index.cjs","types":"./dist/index.d.ts"}`,
			wantMain:  "./dist/index.cjs",
			wantTypes: "./dist/index.d.ts",
		},
		{
			name:     "exports takes precedence over main",
			pkg:      `{"main":"./old.js","exports":{".":"./new.js"}}`,
			wantMain: "./new.js",
		},
		{
			name:     "bare path gets ./ prefix",
			pkg:      `{"main":"index.js"}`,
			wantMain: "./index.js",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, warns := InferEntry(memReader(map[string]string{"package.json": tc.pkg}))
			if len(warns) != 0 {
				t.Errorf("unexpected warnings: %v", warns)
			}
			if e.Main != tc.wantMain {
				t.Errorf("main=%q want %q", e.Main, tc.wantMain)
			}
			if e.Types != tc.wantTypes {
				t.Errorf("types=%q want %q", e.Types, tc.wantTypes)
			}
			if e.Source != "package.json" {
				t.Errorf("source=%q", e.Source)
			}
		})
	}
}

func TestInferEntry_Layer3_IndexConvention(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		wantMain  string
		wantTypes string
	}{
		{
			name:      "index.ts with d.ts",
			files:     map[string]string{"index.ts": "x", "index.d.ts": "y"},
			wantMain:  "./index.ts",
			wantTypes: "./index.d.ts",
		},
		{
			name:     "index.js without types",
			files:    map[string]string{"index.js": "x"},
			wantMain: "./index.js",
		},
		{
			name:      "ts preferred over js",
			files:     map[string]string{"index.ts": "x", "index.js": "y", "index.d.ts": "z"},
			wantMain:  "./index.ts",
			wantTypes: "./index.d.ts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, warns := InferEntry(memReader(tc.files))
			if len(warns) != 0 {
				t.Errorf("unexpected warnings: %v", warns)
			}
			if e.Main != tc.wantMain {
				t.Errorf("main=%q want %q", e.Main, tc.wantMain)
			}
			if e.Types != tc.wantTypes {
				t.Errorf("types=%q want %q", e.Types, tc.wantTypes)
			}
			if e.Source != "index convention" {
				t.Errorf("source=%q", e.Source)
			}
		})
	}
}

// TestInferEntry_NoEntryWarns 锁定规范要求：
// "全部缺失时 ngm install 输出警告，映射仍生成但不带入口字段"。
func TestInferEntry_NoEntryWarns(t *testing.T) {
	e, warns := InferEntry(memReader(map[string]string{"README.md": "hi"}))
	if len(warns) == 0 {
		t.Fatalf("expected a warning when no entry point exists")
	}
	if e.Main != "" || e.Types != "" {
		t.Errorf("entry should be empty, got %+v", e)
	}
	if !strings.Contains(warns[0], "no entry point") {
		t.Errorf("warning should explain the situation: %v", warns)
	}
}

func TestInferEntry_MalformedFilesFallThrough(t *testing.T) {
	// ngm.json 坏 json → 落到 package.json；package.json 也坏 → 落到 index 约定
	read := memReader(map[string]string{
		"ngm.json":     `{not json`,
		"package.json": `also not json`,
		"index.ts":     "x",
	})
	e, warns := InferEntry(read)
	if len(warns) != 0 {
		t.Errorf("should have fallen through to the index convention: %v", warns)
	}
	if e.Main != "./index.ts" {
		t.Errorf("main=%q", e.Main)
	}
}

// ---------------------------------------------------------------------------
// 生成
// ---------------------------------------------------------------------------

func TestGenerate(t *testing.T) {
	inputs := []GenerateInput{
		{
			From:          "github:org/utils",
			VendorRelPath: "github.com/org/utils",
			Read:          memReader(map[string]string{"index.ts": "x", "index.d.ts": "y"}),
		},
		{
			From:          "github:org/logger",
			VendorRelPath: "github.com/org/logger",
			Read: memReader(map[string]string{
				"ngm.json": `{"main":"./dist/logger.js","types":"./dist/logger.d.ts"}`,
			}),
		},
		{
			From:          "github:org/noentry",
			VendorRelPath: "github.com/org/noentry",
			Read:          memReader(map[string]string{"README.md": "nothing here"}),
		},
	}

	f, warns := Generate(inputs)
	if err := f.Validate(); err != nil {
		t.Fatalf("generated file failed validation: %v", err)
	}

	// 排序（from 字节序）
	got := make([]string, len(f.Mappings))
	for i, m := range f.Mappings {
		got[i] = m.From
	}
	want := []string{"github:org/logger", "github:org/noentry", "github:org/utils"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order: %v want %v", got, want)
			break
		}
	}

	// to 形状
	for _, m := range f.Mappings {
		if !strings.HasPrefix(m.To, "./ngm.vendor/") {
			t.Errorf("%s: to=%q must be under ./ngm.vendor/", m.From, m.To)
		}
	}

	// 缺失入口的那条：无 main/types，且有警告
	for _, m := range f.Mappings {
		if m.From == "github:org/noentry" {
			if m.Main != "" || m.Types != "" {
				t.Errorf("noentry should have no entry fields, got %+v", m)
			}
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "github:org/noentry") {
		t.Errorf("expected exactly one warning naming the dependency, got %v", warns)
	}

	// golden：锁定序列化格式
	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	testutils.GoldenString(t, "vectors/mappings-basic.golden", string(data))
}

func TestGenerate_MonorepoSubPath(t *testing.T) {
	f, _ := Generate([]GenerateInput{{
		From:          "github:org/mono",
		VendorRelPath: "github.com/org/mono/packages/core",
		Read:          memReader(map[string]string{"index.ts": "x"}),
	}})
	if len(f.Mappings) != 1 {
		t.Fatalf("mappings=%d", len(f.Mappings))
	}
	if f.Mappings[0].To != "./ngm.vendor/github.com/org/mono/packages/core" {
		t.Errorf("to=%q", f.Mappings[0].To)
	}
}

// ---------------------------------------------------------------------------
// 序列化与读写
// ---------------------------------------------------------------------------

func TestMarshal_ExactShape(t *testing.T) {
	f := NewFile()
	f.Mappings = []Mapping{
		{From: "github:org/a", To: "./ngm.vendor/github.com/org/a", Main: "./index.js", Types: "./index.d.ts"},
		{From: "github:org/b", To: "./ngm.vendor/github.com/org/b"},
	}
	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	if !strings.Contains(s, `"version": 1`) {
		t.Errorf("missing version:\n%s", s)
	}
	if !strings.HasSuffix(s, "}\n") {
		t.Errorf("must end with a single LF")
	}
	if strings.Contains(s, "\r") {
		t.Errorf("must use LF only")
	}
	// 无入口字段的条目不应出现 main/types 键（omitempty）
	last := s[strings.Index(s, `"github:org/b"`):]
	if strings.Contains(last, `"main"`) || strings.Contains(last, `"types"`) {
		t.Errorf("empty main/types must be omitted:\n%s", s)
	}
}

func TestReadWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	original := NewFile()
	original.Mappings = []Mapping{{From: "github:org/a", To: "./ngm.vendor/github.com/org/a", Main: "./index.js"}}
	if err := Write(path, original); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got.Mappings) != 1 || got.Mappings[0].Main != "./index.js" {
		t.Errorf("round-trip failed: %+v", got)
	}
}

func TestRead_MissingAndCorrupt(t *testing.T) {
	// 缺失不是错误
	got, err := Read(filepath.Join(t.TempDir(), FileName))
	if err != nil || got != nil {
		t.Errorf("missing file should yield (nil,nil), got (%v,%v)", got, err)
	}

	// 损坏 → exit 3 + 可操作 hint
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Errorf("corrupt file should fail")
	}

	// 版本不支持
	if err := os.WriteFile(path, []byte(`{"version":99,"mappings":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Errorf("unsupported version should fail")
	}
}

// ---------------------------------------------------------------------------
// 校验（M4.4）
// ---------------------------------------------------------------------------

func writeVendorTree(t *testing.T, projectDir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(projectDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidate_OK(t *testing.T) {
	proj := t.TempDir()
	writeVendorTree(t, proj, map[string]string{
		"ngm.vendor/github.com/org/utils/index.js":          "x",
		"ngm.vendor/github.com/org/utils/index.d.ts":        "y",
		"ngm.vendor/github.com/org/logger/dist/logger.js":   "x",
		"ngm.vendor/github.com/org/logger/dist/logger.d.ts": "y",
	})

	f := NewFile()
	f.Mappings = []Mapping{
		{From: "github:org/utils", To: "./ngm.vendor/github.com/org/utils", Main: "./index.js", Types: "./index.d.ts"},
		{From: "github:org/logger", To: "./ngm.vendor/github.com/org/logger",
			Main: "./dist/logger.js", Types: "./dist/logger.d.ts"},
	}

	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		LockNames:  map[string]bool{"github:org/utils": true, "github:org/logger": true},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %v", findings)
	}
}

func TestValidate_DetectsProblems(t *testing.T) {
	proj := t.TempDir()
	writeVendorTree(t, proj, map[string]string{
		"ngm.vendor/github.com/org/ok/index.js": "x",
		// missing-entry 的目录存在但没有入口文件
		"ngm.vendor/github.com/org/missing-entry/README.md": "x",
	})

	f := NewFile()
	f.Mappings = []Mapping{
		// from 不在 lock 中
		{From: "github:org/notlocked", To: "./ngm.vendor/github.com/org/notlocked"},
		// to 不存在
		{From: "github:org/nodir", To: "./ngm.vendor/github.com/org/nodir"},
		// main 不存在
		{From: "github:org/ok", To: "./ngm.vendor/github.com/org/ok", Main: "./gone.js"},
		// 无入口字段（警告）
		{From: "github:org/missing-entry", To: "./ngm.vendor/github.com/org/missing-entry"},
		// to 不在 ngm.vendor 下
		{From: "github:org/elsewhere", To: "./somewhere/else"},
	}

	// LockNames 只包含"应该通过 lock 维度的"条目：
	// notlocked 故意缺席以测 lock 检查；其余条目均已锁定，
	// 这样每个 case 只暴露它想暴露的那一个问题。
	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		LockNames: map[string]bool{
			"github:org/nodir":         true,
			"github:org/ok":            true,
			"github:org/missing-entry": true,
			"github:org/elsewhere":     true,
		},
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	byFrom := map[string][]Finding{}
	for _, fd := range findings {
		byFrom[fd.From] = append(byFrom[fd.From], fd)
	}

	expectFatal := func(from, substr string) {
		t.Helper()
		for _, fd := range byFrom[from] {
			if fd.Fatal && strings.Contains(fd.Message, substr) {
				return
			}
		}
		t.Errorf("%s: expected fatal finding containing %q, got %v", from, substr, byFrom[from])
	}
	expectFatal("github:org/notlocked", "not present in ngm.lock")
	expectFatal("github:org/nodir", "does not exist on disk")
	expectFatal("github:org/ok", "`main` file does not exist")
	expectFatal("github:org/elsewhere", "must point inside ./ngm.vendor/")

	// missing-entry 只应有警告（非致命）
	var nonFatal bool
	for _, fd := range byFrom["github:org/missing-entry"] {
		if !fd.Fatal {
			nonFatal = true
		}
		if fd.Fatal {
			t.Errorf("missing entry should be a warning, not fatal: %v", fd)
		}
	}
	if !nonFatal {
		t.Errorf("expected a non-fatal warning for missing-entry, got %v", byFrom["github:org/missing-entry"])
	}

	// fatal 计数：notlocked 命中两个维度（lock 缺失 + to 不存在），
	// nodir / ok / elsewhere 各一个 → 5
	text, fatalCount := FormatFindings(findings)
	if fatalCount != 5 {
		t.Errorf("fatalCount=%d want 5\n%s", fatalCount, text)
	}
	if !strings.Contains(text, "error:") || !strings.Contains(text, "warning:") {
		t.Errorf("formatted output should label both severities:\n%s", text)
	}
}

func TestValidate_LockNamesNilSkipsThatCheck(t *testing.T) {
	proj := t.TempDir()
	writeVendorTree(t, proj, map[string]string{
		"ngm.vendor/github.com/org/a/index.js": "x",
	})
	f := NewFile()
	f.Mappings = []Mapping{{From: "github:org/a", To: "./ngm.vendor/github.com/org/a", Main: "./index.js"}}

	findings, err := Validate(f, ValidateEnvironment{ProjectDir: proj, LockNames: nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("with LockNames=nil the lock check should be skipped, got %v", findings)
	}
}
