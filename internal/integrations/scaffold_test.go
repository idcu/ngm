package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/mappings"
)

func testMappings() *mappings.File {
	return &mappings.File{
		Version: 1,
		Mappings: []mappings.Mapping{
			{
				From: "github:org/plain", To: "./ngm.vendor/github.com/org/plain",
				Main: "./src/main.ts", Types: "./src/main.d.ts",
			},
			{
				From: "github:org/mono", Path: "packages/core",
				To:   "./ngm.vendor/github.com/org/mono/packages/core",
				Main: "./src/index.ts",
			},
		},
	}
}

// 导入标识符是消费方拼配置时唯一的键，它必须把 `path` 拼进去。
//
// 规则本身定义在协议层（mappings.Mapping.Specifier），这里只固定
// "生成器用的是它"——`ngm build` 与 `ngm integrations` 必须给出同一个键。
func TestSpecifier(t *testing.T) {
	plain := testMappings().Mappings[0]
	if got := plain.Specifier(); got != "github:org/plain" {
		t.Errorf("Specifier = %q", got)
	}
	sub := testMappings().Mappings[1]
	if got := sub.Specifier(); got != "github:org/mono/packages/core" {
		t.Errorf("Specifier = %q", got)
	}

	entries := ResolveEntries(testMappings())
	for _, e := range entries {
		if e.Key == "github:org/mono" {
			t.Errorf("the key must be the specifier, never the bare `from`: %+v", entries)
		}
	}
}

// 别名必须指向**文件**：目录只在恰好含 index.* 时才解析得到，
// 而这个 fixture 的 main 是 ./src/main.ts（非 index），正是目录写法会失败的情况。
func TestEntryFile_PrefersTheDeclaredMain(t *testing.T) {
	m := testMappings().Mappings[0]
	if got := EntryFile(m); got != "./ngm.vendor/github.com/org/plain/src/main.ts" {
		t.Errorf("EntryFile = %q", got)
	}

	noMain := mappings.Mapping{From: "github:org/bare", To: "./ngm.vendor/github.com/org/bare"}
	if got := EntryFile(noMain); got != "./ngm.vendor/github.com/org/bare" {
		t.Errorf("without main the entry falls back to the directory, got %q", got)
	}
}

// 类型解析优先用 `types`：打包器要的是可运行的入口，TS 要的是声明，两者常常不同。
func TestTypeFile_PrefersTypes(t *testing.T) {
	withTypes := testMappings().Mappings[0]
	if got := TypeFile(withTypes); got != "./ngm.vendor/github.com/org/plain/src/main.d.ts" {
		t.Errorf("TypeFile = %q", got)
	}
	onlyMain := testMappings().Mappings[1]
	if got := TypeFile(onlyMain); got != "./ngm.vendor/github.com/org/mono/packages/core/src/index.ts" {
		t.Errorf("TypeFile = %q", got)
	}
}

// 长键必须排在前面：字符串别名是前缀替换，短键会把子路径吞掉（这是实测出来的顺序）。
func TestResolveEntries_LongestKeyFirst(t *testing.T) {
	entries := ResolveEntries(testMappings())
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Key != "github:org/mono/packages/core" {
		t.Errorf("the longer key must come first, got %+v", entries)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Key, "/") {
			t.Errorf("bundler entries must be exact keys, got %q", e.Key)
		}
	}
}

func TestTsconfigPaths_UsesTypeFilesAndWildcard(t *testing.T) {
	paths := TsconfigPaths(testMappings())

	if got := paths["github:org/plain"]; len(got) != 1 || got[0] != "./ngm.vendor/github.com/org/plain/src/main.d.ts" {
		t.Errorf("paths[github:org/plain] = %v", got)
	}
	if got := paths["github:org/mono/packages/core"]; len(got) != 1 ||
		got[0] != "./ngm.vendor/github.com/org/mono/packages/core/src/index.ts" {
		t.Errorf("subpath entry = %v", got)
	}
	// 通配项让未逐条声明的子路径也能解析
	if got := paths["github:org/plain/*"]; len(got) != 1 || got[0] != "./ngm.vendor/github.com/org/plain/*" {
		t.Errorf("wildcard entry = %v", got)
	}
	// 有子路径的依赖**不能**有通配项：from/* 的含义是仓库下任意子路径，
	// 而它的 to 已经落在某个子目录里，拼出来会指错地方。
	if _, ok := paths["github:org/mono/*"]; ok {
		t.Errorf("a subpath dependency must not get a repository-wide wildcard: %v", paths)
	}
}

// Deno 解析不了目录，因此没有入口的依赖**不能**生成精确键——
// 生成一条看起来能用、实际解析失败的映射，比明说做不到更糟。
func TestDenoImports_NoExactKeyWithoutEntry(t *testing.T) {
	f := &mappings.File{
		Version: 1,
		Mappings: []mappings.Mapping{
			{From: "github:org/bare", To: "./ngm.vendor/github.com/org/bare"},
			{From: "github:org/ok", To: "./ngm.vendor/github.com/org/ok", Main: "./index.ts"},
		},
	}
	_, imports, warns := DenoImports(f)

	if _, ok := imports["github:org/bare"]; ok {
		t.Errorf("a dependency without an entry must not get an exact Deno key: %v", imports)
	}
	if _, ok := imports["github:org/bare/"]; !ok {
		t.Errorf("the prefix entry should still be present: %v", imports)
	}
	if imports["github:org/ok"] != "./ngm.vendor/github.com/org/ok/index.ts" {
		t.Errorf("imports[github:org/ok] = %q", imports["github:org/ok"])
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "github:org/bare") {
		t.Errorf("the reason must be reported: %v", warns)
	}
}

func TestPlan_EveryToolPlansTsconfig(t *testing.T) {
	for _, tool := range Tools() {
		arts, _, err := Plan(tool, testMappings())
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		paths := map[string]bool{}
		for _, a := range arts {
			paths[a.Path] = true
		}
		if !paths[TsconfigSidecarPath] {
			t.Errorf("%s must also plan %s (the github: prefix needs tsconfig paths)", tool, TsconfigSidecarPath)
		}
		if len(arts) < 3 {
			t.Errorf("%s: expected the tool config, the tsconfig sidecar and tsconfig.json, got %v", tool, paths)
		}
	}
}

func TestPlan_RejectsAnEmptyMappingsFileForDeno(t *testing.T) {
	// Deno 的 import map 必须是静态 JSON：没有条目就没有可写的东西。
	// 静默生成一个空的 imports 会让构建工具什么都不解析，而用户以为配好了。
	if _, _, err := Plan(ToolDeno, &mappings.File{Version: 1}); err == nil {
		t.Error("deno with no mappings must fail")
	}
}

func TestParseTool(t *testing.T) {
	if _, err := ParseTool("vite"); err != nil {
		t.Errorf("vite should parse: %v", err)
	}
	if _, err := ParseTool(" VITE "); err != nil {
		t.Errorf("parsing should be case/space tolerant: %v", err)
	}
	_, err := ParseTool("rollup")
	if err == nil {
		t.Fatal("an unknown tool must fail")
	}
	// 用户看到的是格式化后的输出（message + hint），因此清单要在**那里面**出现，
	// 否则等于让用户去猜支持哪些工具。
	shown := errs.FormatHuman(err)
	if !strings.Contains(shown, "vite") || !strings.Contains(shown, "webpack") {
		t.Errorf("the rendered error should list the supported tools:\n%s", shown)
	}
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func TestApply_CreateThenUpToDate(t *testing.T) {
	dir := t.TempDir()
	arts, _, err := Plan(ToolEsbuild, testMappings())
	if err != nil {
		t.Fatal(err)
	}

	outcomes, err := Apply(dir, arts, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		if o.Status != StatusCreated {
			t.Errorf("%s: status = %s, want created", o.Path, o.Status)
		}
	}

	// 幂等：第二次运行内容一致，不该报冲突也不该改写
	outcomes, err = Apply(dir, arts, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes {
		if o.Status != StatusUpToDate {
			t.Errorf("%s: status = %s, want up to date", o.Path, o.Status)
		}
	}
}

func TestApply_NeverOverwritesAUserFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ViteConfigPath, "export default { mine: true }\n")

	arts, _, err := Plan(ToolVite, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := Apply(dir, arts, false)
	if err != nil {
		t.Fatal(err)
	}

	if !HasConflict(outcomes) {
		t.Fatal("an existing vite.config.ts with different content must be a conflict")
	}
	// 用户文件一个字节都不能动
	if got := readFile(t, dir, ViteConfigPath); got != "export default { mine: true }\n" {
		t.Errorf("the user's file was modified:\n%s", got)
	}
	// 全有或全无：其余产物也不该被写
	if _, err := os.Stat(filepath.Join(dir, TsconfigSidecarPath)); err == nil {
		t.Errorf("%s must not be written when another artifact conflicts", TsconfigSidecarPath)
	}

	// 冲突提示要能定位到差异
	text := FormatConflicts(outcomes)
	if !strings.Contains(text, ViteConfigPath) || !strings.Contains(text, "@@") {
		t.Errorf("the conflict report should point at the file and a diff:\n%s", text)
	}
}

// 已存在的 tsconfig.json 不是冲突：文件没有错，只是 ngm 无权改它。
// 报成冲突会让用户以为出了问题。
func TestApply_ExistingCreateOnlyFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, TsconfigPath, `{"compilerOptions":{"strict":true}}`+"\n")

	arts, _, err := Plan(ToolVite, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := Apply(dir, arts, false)
	if err != nil {
		t.Fatal(err)
	}

	if HasConflict(outcomes) {
		t.Fatalf("an existing tsconfig.json must not be a conflict: %+v", outcomes)
	}
	var skipped *Outcome
	for i := range outcomes {
		if outcomes[i].Path == TsconfigPath {
			skipped = &outcomes[i]
		}
	}
	if skipped == nil || skipped.Status != StatusSkipped {
		t.Fatalf("tsconfig.json should be skipped, got %+v", outcomes)
	}
	if !strings.Contains(skipped.Hint, TsconfigSidecarPath) {
		t.Errorf("the hint must name what to add: %q", skipped.Hint)
	}
	// 其余产物照常写入：skipped 不是失败
	if _, err := os.Stat(filepath.Join(dir, TsconfigSidecarPath)); err != nil {
		t.Errorf("the sidecar should still be written: %v", err)
	}
	if got := readFile(t, dir, TsconfigPath); !strings.Contains(got, `"strict":true`) {
		t.Errorf("the user's tsconfig was modified:\n%s", got)
	}
}

// ngm 自己的文件（ngm.*）可以重生成：否则依赖一变，esbuild 的脚本就再也更新不了。
func TestApply_RegeneratesOwnFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, EsbuildScriptPath, "// stale\n")

	arts, _, err := Plan(ToolEsbuild, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := Apply(dir, arts, false)
	if err != nil {
		t.Fatal(err)
	}
	if HasConflict(outcomes) {
		t.Fatalf("ngm's own file must be regenerable: %+v", outcomes)
	}
	if !strings.Contains(readFile(t, dir, EsbuildScriptPath), "module.exports = { alias }") {
		t.Error("the stale script was not regenerated")
	}
}

func TestApply_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	arts, _, err := Plan(ToolDeno, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(dir, arts, true); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry run must not create files, found %d entries", len(entries))
	}
}

// 生成的配置必须在**运行时**读 mappings：内联会让配置随依赖变化而过期，
// 而过期的配置最难被发现——构建仍然成功，只是解析到旧路径。
func TestGeneratedBundlerConfigsReadMappingsAtRuntime(t *testing.T) {
	for _, tool := range []Tool{ToolVite, ToolEsbuild, ToolWebpack} {
		arts, _, err := Plan(tool, testMappings())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, a := range arts {
			if a.Path == TsconfigSidecarPath || a.Path == TsconfigPath {
				continue
			}
			body := string(a.Content)
			if !strings.Contains(body, "ngm.mappings.json") {
				t.Errorf("%s config does not read ngm.mappings.json:\n%s", tool, body)
			}
			if !strings.Contains(body, "sort(") {
				t.Errorf("%s config must order keys longest-first:\n%s", tool, body)
			}
			found = true
		}
		if !found {
			t.Errorf("%s: no tool config was planned", tool)
		}
	}
}

// 静态生成的部分（Deno import map、tsconfig paths）必须真的带上条目，
// 否则"生成了配置"只是生成了一个空壳。
func TestGeneratedStaticConfigsCarryEntries(t *testing.T) {
	denoArts, _, err := Plan(ToolDeno, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	importMap := ""
	for _, a := range denoArts {
		if a.Path == DenoImportMapPath {
			importMap = string(a.Content)
		}
	}
	if !strings.Contains(importMap, "github:org/mono/packages/core") {
		t.Errorf("the deno import map must carry the subpath specifier:\n%s", importMap)
	}

	tsArts, _, err := Plan(ToolVite, testMappings())
	if err != nil {
		t.Fatal(err)
	}
	sidecar := ""
	for _, a := range tsArts {
		if a.Path == TsconfigSidecarPath {
			sidecar = string(a.Content)
		}
	}
	if !strings.Contains(sidecar, "github:org/plain/*") {
		t.Errorf("the tsconfig sidecar must carry the wildcard entry:\n%s", sidecar)
	}
	// TypeScript 7 移除了 baseUrl，写上去会直接报错（已实测）
	if strings.Contains(sidecar, "baseUrl") {
		t.Errorf("the tsconfig sidecar must not set baseUrl:\n%s", sidecar)
	}
}
