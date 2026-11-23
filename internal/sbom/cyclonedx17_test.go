package sbom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
)

// TestV138CycloneDX17SchemaShape —— A 批判据（竞品全维度评估 A1）。
//
// ngm 的 CycloneDX 输出长期停在 1.5，而 pnpm 12 已产 1.7（官方 schema 校验）。
// 本判据断言 1.7 的**关键形状**：specVersion 是 4 字符字符串（不是数字，
// 1.7 起 specVersion 恒为字符串 "1.7"），文档头三件套、每个组件的必需字段，
// 以及依赖图段按 bom-ref 引用（v0.73 的 ADR-028 契约在 1.7 下仍成立）。
//
// 不做完整 JSON Schema 校验（那要引入外部依赖）：这里用的是"1.7 里在本项目
// 实际出现的字段"这一子集，缺一个就不是合法的目标形状。
func TestV138CycloneDX17SchemaShape(t *testing.T) {
	opts := fixedOptions(FormatCycloneDX)
	opts.GraphComplete = true
	// 带一张完整的图：root → utils → mono（否则 cyclonedxDependencies 返回 nil，
	// dependencies 段被 omitempty 省略，判据会假红）。
	entries := testEntries()
	entries[0].RequiredBy = []string{lock.RootRequirer}
	entries[1].RequiredBy = []string{entries[0].Key()}
	data, err := Build(Project{Name: "app", Version: "1.0.0"}, entries, opts)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		BomFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Schema      string `json:"$schema"`
		Version     int    `json:"version"`
		Metadata    struct {
			Tools struct {
				Components []struct {
					Type      string `json:"type"`
					Name      string `json:"name"`
					Publisher string `json:"publisher"`
				} `json:"components"`
			} `json:"tools"`
		} `json:"metadata"`
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
		Components []struct {
			Type    string `json:"type"`
			BomRef  string `json:"bom-ref"`
			Name    string `json:"name"`
			Version string `json:"version"`
			Purl    string `json:"purl"`
			Hashes  []struct {
				Alg     string `json:"alg"`
				Content string `json:"content"`
			} `json:"hashes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("CycloneDX 输出不是合法 JSON: %v", err)
	}

	// specVersion 必须是 4 字符字符串 "1.7"——若有人改回 1.5 或写成数字都会红。
	if doc.BomFormat != "CycloneDX" || doc.SpecVersion != "1.7" || doc.Version != 1 {
		t.Fatalf("文档头不是 CycloneDX 1.7: bomFormat=%q specVersion=%q version=%d",
			doc.BomFormat, doc.SpecVersion, doc.Version)
	}
	if !strings.HasSuffix(doc.Schema, "bom-1.7.schema.json") {
		t.Errorf("$schema 必须指向 1.7 结构，got %q", doc.Schema)
	}
	// 1.7 schema 仍接受 legacy tools 数组（标 deprecated），但不该再产它。
	tools := doc.Metadata.Tools.Components
	if len(tools) != 1 || tools[0].Type != "application" || tools[0].Name != "ngm" {
		t.Errorf("metadata.tools 必须是 1.5+ 的 components 形态: %+v", doc.Metadata.Tools)
	}
	if len(doc.Components) == 0 {
		t.Fatal("components 为空——SBOM 没有内容")
	}

	// 每个 component 的必需字段（1.5→1.7 这些字段名与类型未变，但必须都在）。
	for i, c := range doc.Components {
		if c.Type == "" || c.BomRef == "" || c.Name == "" || c.Version == "" || c.Purl == "" {
			t.Errorf("components[%d] 缺必需字段: %+v", i, c)
		}
		if len(c.Hashes) == 0 || c.Hashes[0].Alg == "" || c.Hashes[0].Content == "" {
			t.Errorf("components[%d] 缺合法 hashes（CycloneDX 的内容证明全靠它）: %+v", i, c.Hashes)
		}
	}

	// 图段：root 的边必须指向真实存在的组件 bom-ref（1.7 的 dependencies 按 bom-ref 解析）。
	if len(doc.Dependencies) == 0 {
		t.Fatal("GraphComplete=true 时 dependencies 段不应省略")
	}
	refs := make(map[string]bool, len(doc.Components))
	for _, c := range doc.Components {
		refs[c.BomRef] = true
	}
	for i, d := range doc.Dependencies {
		if d.Ref != RootBomRef && !refs[d.Ref] {
			t.Errorf("dependencies[%d].ref=%q 指向不存在的组件", i, d.Ref)
		}
		for _, child := range d.DependsOn {
			if !refs[child] {
				t.Errorf("dependencies[%d].dependsOn 含不存在的 bom-ref %q", i, child)
			}
		}
	}
}
