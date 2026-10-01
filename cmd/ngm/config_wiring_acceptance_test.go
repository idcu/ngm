package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/config"
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
