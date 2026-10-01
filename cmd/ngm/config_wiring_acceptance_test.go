package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/testutils"
)

// 本文件的用例来自 v0.4 开工前复核：把"声明了、没接线"做成机械检查
// （`TestConfigKeysAreExercisedByTests`）之后，它报出六个从未被任何测试碰过的
// 配置键。逐个定性后：一个是**真缺陷**（下面第 1 条），其余是"接了线但没测"。
//
// 为什么值得单独写：这类缺陷连续三个版本出现（verifyOnLock → permissions →
// SubPaths），共同特征都是"字段齐备、注释完整、没有任何地方读它"。

// TestGitTokenEnvVarsFromConfigReachTheRedactionSet 固定本轮发现的第 4 例，
// 也是唯一位于安全路径上的一例。
//
// `git.tokenEnvVars` 允许用户声明"我们这台 git 的 token 在 CORP_TOKEN 里"，
// 使该值在输出里被脱敏。而 `newProjectEnv` 曾在**读配置之前**调用
// `git.MergeTokenEnvVars(nil)`——配置从未被读进来，用户写的映射一条都不生效。
// 症状是：用户以为自己的 token 已被脱敏，实际它会被原样打进日志。
func TestGitTokenEnvVarsFromConfigReachTheRedactionSet(t *testing.T) {
	const token = "corp-token-value-0001"
	t.Setenv("CORP_TOKEN", token)

	home := isolateUserEnv(t)
	proj := t.TempDir()

	// 先取反例：**没有**配置时，这个值不该在脱敏集合里。
	// 少了这一步，下面的断言可能只是因为"所有环境变量都被收进来了"而通过——
	// 那样它就什么都没证明。
	before, err := newProjectEnv(proj)
	if err != nil {
		t.Fatalf("newProjectEnv: %v", err)
	}
	if contains(before.Secrets, token) {
		t.Fatalf("without config, %q must not be in the redaction set; "+
			"the rest of this test would then be vacuous", token)
	}

	writeGlobalConfig(t, home, `{"git":{"tokenEnvVars":{"corp.local":"CORP_TOKEN"}}}`)

	after, err := newProjectEnv(proj)
	if err != nil {
		t.Fatalf("newProjectEnv: %v", err)
	}
	if !contains(after.Secrets, token) {
		t.Errorf("the value behind a config-declared token env var must be redacted; "+
			"the config was not read (secrets=%v)", after.Secrets)
	}
}

// TestGlobalDefaultEnginesAreUsed 覆盖 `engines.defaultTransform` /
// `defaultBundle` / `defaultTypeCheck` 三个全局默认。
//
// 它们是"项目没写引擎时生效"的文档化行为，此前没有任何测试设置过。
func TestGlobalDefaultEnginesAreUsed(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	home := isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 项目**不指定**任何引擎：能不能跑起来，完全取决于全局默认是否生效。
	proj := m6Project(t, `{}`)
	m6Catalog(t, proj, fake, "bundle")
	writeGlobalConfig(t, home, `{"engines":{
  "defaultTransform": "fake",
  "defaultBundle": "fake",
  "defaultTypeCheck": "fake"
}}`)

	code, out := runCaptureCode(t, "build", "--dry-run", "--dir="+proj)
	if code != 0 {
		t.Fatalf("build --dry-run exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, "primary: fake") {
		t.Errorf("the global default engine must fill in what the project left unset:\n%s", out)
	}

	// 三个字段到能力类别的映射：`typeDecl` 与 `css` **刻意**没有全局默认
	// （见 engine.go 的说明），因此要连"没有"一起断言——否则将来给 css
	// 加一个字段时，谁也不会注意到这里的期望已经过时。
	g := &config.GlobalEngines{DefaultTransform: "t", DefaultBundle: "b", DefaultTypeCheck: "c"}
	cases := []struct {
		kind adapter.EngineKind
		want string
	}{
		{adapter.KindTransform, "t"},
		{adapter.KindBundle, "b"},
		{adapter.KindTypeCheck, "c"},
		{adapter.KindTypeDecl, ""},
		{adapter.KindCSS, ""},
	}
	for _, c := range cases {
		if got := globalDefaultFor(g, c.kind); got != c.want {
			t.Errorf("globalDefaultFor(%s) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// TestDeclaredSupportedInputIsListed 覆盖 `supportedInput`。
//
// 它只影响展示，因此断言也落在展示上：清单里声明的输入扩展名必须出现在输出里，
// 否则那条声明等于不存在。注意它出现在 **`engines info`** 而不是 `engines list`
// ——`list` 是概览表（没有 input 列），`info` 才是逐条详情。
func TestDeclaredSupportedInputIsListed(t *testing.T) {
	isolateUserEnv(t)
	proj := t.TempDir()
	testutils.WriteFile(t, proj, "ngm.json", `{
  "schemaVersion": 1,
  "name": "github.com/v4/input-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "src/index.ts",
  "dependencies": []
}
`)
	testutils.WriteFile(t, proj, "ngm.engines.json", `{
  "version": 1,
  "engines": [
    {"name": "weird", "kind": "bundle", "adapter": "subprocess", "command": "ngm-not-a-real-binary",
     "supportedInput": [".x9"]}
  ]
}
`)

	code, out := runCaptureCode(t, "engines", "info", "weird", "--dir="+proj)
	if code != 0 {
		t.Fatalf("engines info exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, ".x9") {
		t.Errorf("a declared supportedInput must be shown (otherwise the declaration is invisible):\n%s", out)
	}
}

// TestDeclaredEngineVersionIsComparedWithTheLocalOne 覆盖 `ngm.engines.json` 的
// `version`（v0.5 复核的额外发现：接线存在——Validate 会探测实际版本并比对——
// 但从未被任何断言固定）。
//
// 为什么必须是"信息"而不是错误：旧版本的引擎常常照样能跑，把声明与实际不一致
// 判为失败会让 validate 变成版本管理器。这条测试固定的是**说出来**，不是拦下来。
func TestDeclaredEngineVersionIsComparedWithTheLocalOne(t *testing.T) {
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	t.Setenv("FAKE_VERSION", "1.2.3") // 本机实际版本

	proj := t.TempDir()
	testutils.WriteFile(t, proj, "ngm.json", `{
  "schemaVersion": 1,
  "name": "github.com/v5/version-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "src/index.ts",
  "dependencies": []
}
`)
	// 清单声明 9.9.9，与实际的 1.2.3 不一致
	cat := struct {
		Version int `json:"version"`
		Engines []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Adapter string `json:"adapter"`
			Command string `json:"command"`
			Version string `json:"version"`
		} `json:"engines"`
	}{Version: 1, Engines: []struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Adapter string `json:"adapter"`
		Command string `json:"command"`
		Version string `json:"version"`
	}{{
		Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"` + filepath.ToSlash(fake) + `"`, Version: "9.9.9",
	}}}
	body, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testutils.WriteFile(t, proj, "ngm.engines.json", string(body)+"\n")

	// 不对退出码断言：内置清单里的 esbuild 在未装 esbuild 的机器上是
	// `unavailable`（exit 5），而版本比对是**信息**（IssueVersion，退出码 0）。
	// 混在一起断言会让这条用例依赖机器的软件清单——这里只关心"说出来"这一件事。
	_, out := runCaptureCode(t, "engines", "validate", "--json", "--dir="+proj)
	var payload struct {
		Issues []struct {
			Entry   string `json:"entry"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("validate --json is not valid JSON: %v\n%s", err, out)
	}
	for _, is := range payload.Issues {
		if is.Kind == "version" && strings.Contains(is.Message, "9.9.9") {
			return
		}
	}
	t.Errorf("the declared version must be compared with the local one and reported:\n%s", out)
}

// ---------------------------------------------------------------------------
// v0.5 复核：弱接线三项的裁定与固定
// ---------------------------------------------------------------------------
//
// 裁定：**"生效"指改变行为**。被校验、被填默认值、被回显，都不算生效——
// 否则"生效"一词就没有意义了（一个只被回显的字段与一个被删掉的字段，
// 对用户的行为差别为零）。据此：
//
//	`runtime`  声明字段：不改变 ngm 的行为（architecture/runtime-model.md：
//	           ngm 不干预宿主运行时）。文档不得再把它与 name / version / dependencies
//	           并列为"已生效"；这里固定它**实际**的契约
//	`version`  同上，且是必填声明（缺失即 exit 3）
//
// 两者都保留在 schema 里：删除它们的代价是让所有现存 ngm.json 解析失败，
// 而它们承载的声明语义（宿主运行时、项目版本）是真实需求——错的只是文档措辞。

// TestRuntimeIsValidatedAcceptedAndEchoed 固定 `runtime` 的真实契约。
func TestRuntimeIsValidatedAcceptedAndEchoed(t *testing.T) {
	isolateUserEnv(t)
	proj := t.TempDir()

	if code, out := runCaptureCode(t, "init", "github.com:wiring/rt", "--runtime=deno", "--dir="+proj); code != 0 {
		t.Fatalf("init exit=%d:\n%s", code, out)
	}
	// `ngm config` 读的是 cwd（没有 --dir），因此这里必须切目录
	chdir(t, proj)

	if code, out := runCaptureCode(t, "config", "validate"); code != 0 {
		t.Fatalf("a declared runtime=deno must validate, exit=%d:\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "config", "show"); code != 0 {
		t.Fatalf("config show exit=%d:\n%s", code, out)
	} else if !strings.Contains(out, `"runtime": "deno"`) {
		t.Errorf("config show must echo the declared runtime:\n%s", out)
	}

	writeProjectManifest(t, proj, `{"schemaVersion":1,"name":"github.com:wiring/rt","version":"0.1.0","runtime":"bun","dependencies":[]}`)
	badCode, badOut := runCaptureCode(t, "config", "validate")
	if badCode != 3 {
		t.Fatalf("an unknown runtime must exit 3, got %d:\n%s", badCode, badOut)
	}
	if !strings.Contains(badOut, "runtime") {
		t.Errorf("the error must name the field it rejected:\n%s", badOut)
	}

	// 缺失即被拒：文档把 runtime 列为**必填**，校验也确实在 decode 阶段就拦下了
	// （因此 BuiltinDefaults.Runtime 那条"缺省补 node"的分支对显式 ngm.json
	// 是不可达的——那是另一处"声明了但到不了"的代码，本轮只登记不改）。
	writeProjectManifest(t, proj, `{"schemaVersion":1,"name":"github.com:wiring/rt","version":"0.1.0","dependencies":[]}`)
	missCode, missOut := runCaptureCode(t, "config", "validate")
	if missCode != 3 {
		t.Fatalf("runtime is required; an omitted one must exit 3, got %d:\n%s", missCode, missOut)
	}
}

// TestProjectVersionIsRequired 固定 `version` 的真实契约：必填声明 + 回显。
func TestProjectVersionIsRequired(t *testing.T) {
	isolateUserEnv(t)
	proj := t.TempDir()
	chdir(t, proj)

	writeProjectManifest(t, proj,
		`{"schemaVersion":1,"name":"github.com:wiring/ver","runtime":"node","dependencies":[]}`)
	code, out := runCaptureCode(t, "config", "validate")
	if code != 3 {
		t.Fatalf("a missing version must exit 3, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("the error must name the field it rejected:\n%s", out)
	}

	writeProjectManifest(t, proj,
		`{"schemaVersion":1,"name":"github.com:wiring/ver","version":"1.2.3","runtime":"node","dependencies":[]}`)
	if code, out := runCaptureCode(t, "config", "validate"); code != 0 {
		t.Fatalf("a declared version must validate, exit=%d:\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "config", "show"); code != 0 {
		t.Fatalf("config show exit=%d:\n%s", code, out)
	} else if !strings.Contains(out, `"version": "1.2.3"`) {
		t.Errorf("config show must echo the declared version:\n%s", out)
	}
}

// TestUnsupportedSchemaVersionIsRejected 固定 project 与 global 两份清单的
// `schemaVersion`：声明一个本 build 不支持的大版本必须被拒绝，而不是被忽略。
func TestUnsupportedSchemaVersionIsRejected(t *testing.T) {
	home := isolateUserEnv(t)
	proj := t.TempDir()
	chdir(t, proj)

	const valid = `{"schemaVersion":1,"name":"github.com:wiring/sv","version":"0.1.0","runtime":"node","dependencies":[]}`

	writeProjectManifest(t, proj, strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1))
	pCode, pOut := runCaptureCode(t, "config", "validate")
	if pCode != 3 {
		t.Fatalf("an unsupported project schemaVersion must exit 3, got %d:\n%s", pCode, pOut)
	}
	if !strings.Contains(pOut, "schemaVersion") {
		t.Errorf("the error must name the field it rejected:\n%s", pOut)
	}

	writeProjectManifest(t, proj, valid)
	writeGlobalConfig(t, home, `{"schemaVersion":2}`)
	gCode, gOut := runCaptureCode(t, "config", "validate")
	if gCode != 3 {
		t.Fatalf("an unsupported global schemaVersion must exit 3, got %d:\n%s", gCode, gOut)
	}
	if !strings.Contains(gOut, "schemaVersion") {
		t.Errorf("the error must name the field it rejected:\n%s", gOut)
	}
}

// TestDeclaredTypesEntryReachesTheMappings 覆盖 `ngm.json` → `types`
// （v0.5 复核第 5 项：确认未接线）。
//
// 此前 mappings 的入口推断直接按 JSON 键解析上游 ngm.json，
// `config.ProjectFile.Types` 自身从未被读过——"types 会生效"在代码里没有支撑点。
// 现在入口推断走 config.ProjectFile：字段与 schema 同源，schema 改了这里会跟着走。
func TestDeclaredTypesEntryReachesTheMappings(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	// 上游只声明 types：命中推断第 1 层，且 main 必须留空。
	// 少了"只声明 types"这个形状，下面也可能因为 index 约定而碰巧有值。
	m4LeafRepo(t, "github:wiring/types-only", map[string]string{
		"ngm.json":      `{"types":"./dist/lib.d.ts"}`,
		"dist/lib.d.ts": "export declare const lib: number;\n",
	})

	proj := m3Project(t, "github:wiring/types-only v1")
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install exit=%d:\n%s", code, out)
	}

	data, err := os.ReadFile(filepath.Join(proj, mappings.FileName))
	if err != nil {
		t.Fatal(err)
	}
	var mf struct {
		Mappings []struct {
			Main  string `json:"main"`
			Types string `json:"types"`
		} `json:"mappings"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatalf("parse %s: %v\n%s", mappings.FileName, err, data)
	}
	if len(mf.Mappings) != 1 {
		t.Fatalf("mappings=%d want 1:\n%s", len(mf.Mappings), data)
	}
	if mf.Mappings[0].Types != "./dist/lib.d.ts" {
		t.Errorf("the types declared by the dependency must reach the mapping, got %q:\n%s",
			mf.Mappings[0].Types, data)
	}
	if mf.Mappings[0].Main != "" {
		t.Errorf("main must stay empty when only types is declared, got %q", mf.Mappings[0].Main)
	}
}

// writeProjectManifest 覆盖项目根的 ngm.json。
func writeProjectManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
