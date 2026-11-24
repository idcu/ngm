package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/config"
)

// TestV231ThirdPartyEngineSampleShape 固定 E12：examples/third-party-engine 是一个
// 自足的第三方引擎样例——自带清单、自带最小 subprocess 脚本、命令序列走
// `engines validate` 与 `build --engine=<name>`，且只认 P4 契约、不认内置清单。
//
// 变异检验：删掉 examples/third-party-engine/ngm.engines.json → "自带清单"子测试
// 立刻红；恢复 → 绿。把清单里的 name 改成内置引擎名（如 esbuild）→ "不引用内置
// 引擎名"红；恢复 → 绿。把 README 的 build 命令换成 `--engine=esbuild` →
// "命令序列"红；恢复 → 绿。
func TestV231ThirdPartyEngineSampleShape(t *testing.T) {
	root := repoRoot(t)
	base := "examples/third-party-engine"
	dir := filepath.Join(root, filepath.FromSlash(base))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("第三方引擎样例目录缺失：%s (%v)", base, err)
	}

	builtins := map[string]bool{}
	for _, e := range adapter.BuiltinCatalog().Engines {
		builtins[e.Name] = true
	}
	if len(builtins) == 0 {
		t.Fatal("内置清单为空——'不引用内置引擎名'会变成空洞断言")
	}

	// ① ngm.json 合法且显式指向一个非内置引擎名。
	manifestRel := base + "/ngm.json"
	manifest := readRepoFile(t, manifestRel)
	var raw struct {
		SchemaVersion int               `json:"schemaVersion"`
		Engines       map[string]string `json:"engines"`
	}
	if err := json.Unmarshal([]byte(manifest), &raw); err != nil {
		t.Fatalf("%s 不是合法 JSON: %v", manifestRel, err)
	}
	if raw.SchemaVersion != config.SchemaVersion {
		t.Errorf("%s: schemaVersion=%d，要求显式 %d", manifestRel, raw.SchemaVersion, config.SchemaVersion)
	}
	pf, err := config.ParseProjectFile([]byte(manifest), manifestRel)
	if err != nil {
		t.Fatalf("%s 解析失败: %v", manifestRel, err)
	}
	if err := pf.Validate(); err != nil {
		t.Fatalf("%s 清单不合法: %v", manifestRel, err)
	}
	engineName := raw.Engines["bundle"]
	if engineName == "" {
		t.Fatalf("%s: engines.bundle 未声明", manifestRel)
	}
	if builtins[engineName] {
		t.Errorf("样例引擎名 %q 是内置引擎名——本样例要证明协议不依赖内置清单", engineName)
	}

	// ② 自带引擎清单：严格解码、版本正确、有非内置的 subprocess bundle 条目。
	catRel := base + "/ngm.engines.json"
	catRaw := readRepoFile(t, catRel)
	dec := json.NewDecoder(strings.NewReader(catRaw))
	dec.DisallowUnknownFields()
	var cat adapter.Catalog
	if err := dec.Decode(&cat); err != nil {
		t.Fatalf("%s 严格解码失败: %v", catRel, err)
	}
	if cat.Version != adapter.CatalogVersion {
		t.Errorf("%s: version=%d，要求 %d", catRel, cat.Version, adapter.CatalogVersion)
	}
	declared := false
	for _, e := range cat.Engines {
		if builtins[e.Name] {
			t.Errorf("%s 引用了内置引擎名 %q——样例必须自足", catRel, e.Name)
		}
		if e.Name == engineName && e.Kind == adapter.KindBundle && e.Adapter == adapter.AdapterSubprocess {
			declared = true
			if e.Command == "" {
				t.Errorf("%s: %s 的 command 为空", catRel, e.Name)
			}
		}
	}
	if !declared {
		t.Errorf("%s 缺少 %q 的 subprocess bundle 条目；清单=%+v", catRel, engineName, cat.Engines)
	}

	// ③ 最小脚本存在、是脚本、且明确实现了 P4 的三个协议锚点。
	scriptRel := base + "/bin/thirdparty-bundle.sh"
	script := readRepoFile(t, scriptRel)
	if !strings.HasPrefix(script, "#!") {
		t.Errorf("%s 缺少 shebang，不是可直接 spawn 的脚本", scriptRel)
	}
	for _, marker := range []string{"--version", "--kind=", "--outfile="} {
		if !strings.Contains(script, marker) {
			t.Errorf("%s 未实现 P4 协议锚点 %q", scriptRel, marker)
		}
	}
	if !strings.Contains(catRaw, "thirdparty-bundle.sh") {
		t.Errorf("%s 的 command 未指向样例自带的脚本", catRel)
	}

	// ④ README 链回唯一契约，命令序列成形，且不偷用内置引擎名。
	readme := readRepoFile(t, base+"/README.md")
	const contractLink = "](../../docs/architecture/engine-adapter.md)"
	if !strings.Contains(readme, contractLink) {
		t.Error("README 必须链回引擎契约 " + contractLink)
	}
	for _, cmd := range []string{
		"ngm engines validate",
		"ngm build --engine=" + engineName,
	} {
		if !strings.Contains(readme, cmd) {
			t.Errorf("README 缺少可识别的命令 %q", cmd)
		}
	}
	for name := range builtins {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`).MatchString(readme) {
			t.Errorf("README 引用了内置引擎名 %q——本样例证明的是协议而非内置清单", name)
		}
	}

	// ⑤ 冒烟骨架存在（结构守门，不冒充已经跑过构建）。
	if _, err := os.Stat(filepath.Join(dir, "smoke.sh")); err != nil {
		t.Errorf("缺少 smoke.sh 骨架: %v", err)
	}
}
