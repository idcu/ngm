package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/sbom"
)

// v0.99：**两种 SBOM 格式必须描述同一张依赖图**。
//
// 这条判据守的是 `internal/sbom/spdx.go` 里一句原话：
//
//	与 CycloneDX 那份同源（edgeIndex）：两种格式的依赖图形必须一致，
//	否则"挑一个格式看"的人会得到两棵树。
//
// 这句话一直是**人工承诺**：现有测试分别检查两种格式各自的内容（v0.69 的
// `TestV69CycloneDXCarriesHashesAndPurls` / `TestV69SPDXCarriesHashesPurlsAndNoAssertion`），
// 但从没有一条判据把两侧的边**放在一起比**。两份实现各自正确、合起来不一致——
// 这正是 v0.97/v0.98/v0.99 一路在收的那类缺口。
//
// 口径：
//   - 边一律归一成 `父名 → 子名`（两侧各自的引用方式不同：CycloneDX 用 purl、
//     SPDX 用 SPDXID，因此先从组件/包清单把引用映射回 slug）；
//   - 根在两侧都不是一个包（CycloneDX 是 `ngm:root`、SPDX 是 `SPDXRef-DOCUMENT`），
//     统一归一到 `<root>`；
//   - 图不完整（GraphComplete=false）时两种格式都必须**整段省略**依赖边
//     （v0.81 的纪律：不导出半张图），因此那一支的答案必须是"空集"，不是"少几条"。
func TestV99SBOMFormatsDescribeTheSameGraph(t *testing.T) {
	entries := sbom.Sort([]sbom.Entry{
		fixtureEntry("github:o/a", lock.RootRequirer),
		fixtureEntry("github:o/b", "github:o/a"),
		fixtureEntry("github:o/c", "github:o/a", "github:o/b", "github:o/b"), // 重复的 requiredBy 必须只算一条边
		fixtureEntry("github:o/d"),                                           // 孤立条目：两种格式都不该给它编边
	})

	t.Run("图完整：两侧边集逐条相等", func(t *testing.T) {
		cdx := sbomEdgesFromCycloneDX(t, entries, true)
		spdx := sbomEdgesFromSPDX(t, entries, true)
		if len(cdx) == 0 {
			t.Fatal("夹具没造出任何边——这条判据会退化成「空集比空集」")
		}
		if !equalEdges(cdx, spdx) {
			t.Errorf("两种格式的依赖图形不一致：\n  CycloneDX: %v\n  SPDX:      %v", cdx, spdx)
		}
		// 形状也顺手对一下：4 个条目在两侧都应当有 4 个组件/包（根不是组件）。
		if n := sbomComponentCount(t, entries, true); n != len(entries) {
			t.Errorf("CycloneDX 组件数 %d，期望 %d", n, len(entries))
		}
	})

	t.Run("图不完整：两种格式都整段省略（不导出半张图）", func(t *testing.T) {
		cdx := sbomEdgesFromCycloneDX(t, entries, false)
		spdx := sbomEdgesFromSPDX(t, entries, false)
		if len(cdx) != 0 || len(spdx) != 0 {
			t.Errorf("图不完整时两种格式都该没有依赖边：\n  CycloneDX: %v\n  SPDX:      %v", cdx, spdx)
		}
		if n := sbomComponentCount(t, entries, false); n != len(entries) {
			t.Errorf("图不完整只影响边，不影响组件：组件数 %d，期望 %d", n, len(entries))
		}
	})
}

// fixtureEntry 造一个导出条目：requiredBy 用 **slug** 写（与 lock 里一致）。
func fixtureEntry(slug string, requiredBy ...string) sbom.Entry {
	return sbom.Entry{
		Slug:       slug,
		Ref:        "v1.0.0",
		RefType:    "tag",
		Commit:     strings.Repeat("a", 40),
		Digest:     "sha256:" + strings.Repeat("b", 64),
		Canonical:  "github.com/" + strings.TrimPrefix(slug, "github:o/"),
		RequiredBy: requiredBy,
	}
}

func sbomBuild(t *testing.T, entries []sbom.Entry, complete bool, format sbom.Format) []byte {
	t.Helper()
	data, err := sbom.Build(
		sbom.Project{Name: "app", Version: "1.0.0"},
		entries,
		sbom.Options{
			Format:        format,
			Now:           time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
			ToolVersion:   "v0.99.0",
			GraphComplete: complete,
		})
	if err != nil {
		t.Fatalf("Build(%s): %v", format, err)
	}
	return data
}

type cdxDocForTest struct {
	Metadata struct {
		Component struct {
			BomRef string `json:"bom-ref"`
			Name   string `json:"name"`
		} `json:"component"`
	} `json:"metadata"`
	Components []struct {
		BomRef string `json:"bom-ref"`
		Name   string `json:"name"`
	} `json:"components"`
	Dependencies []struct {
		Ref       string   `json:"ref"`
		DependsOn []string `json:"dependsOn"`
	} `json:"dependencies"`
}

type spdxDocForTest struct {
	Packages []struct {
		SPDXID string `json:"SPDXID"`
		Name   string `json:"name"`
	} `json:"packages"`
	Relationships []struct {
		From string `json:"spdxElementId"`
		To   string `json:"relatedSpdxElement"`
		Type string `json:"relationshipType"`
	} `json:"relationships"`
}

func sbomEdgesFromCycloneDX(t *testing.T, entries []sbom.Entry, complete bool) []string {
	t.Helper()
	var doc cdxDocForTest
	if err := json.Unmarshal(sbomBuild(t, entries, complete, sbom.FormatCycloneDX), &doc); err != nil {
		t.Fatalf("CycloneDX 输出不是合法 JSON：%v", err)
	}
	// ref → 名字：组件用 slug（`name`），根用哨兵。
	name := map[string]string{doc.Metadata.Component.BomRef: "<root>"}
	for _, c := range doc.Components {
		name[c.BomRef] = c.Name
	}
	var edges []string
	for _, d := range doc.Dependencies {
		from, ok := name[d.Ref]
		if !ok {
			t.Errorf("CycloneDX 的 dependencies 引用了不存在的 ref %q", d.Ref)
			continue
		}
		for _, to := range d.DependsOn {
			toName, ok := name[to]
			if !ok {
				t.Errorf("CycloneDX 的 dependsOn 引用了不存在的 ref %q", to)
				continue
			}
			edges = append(edges, from+" → "+toName)
		}
	}
	sort.Strings(edges)
	return edges
}

func sbomEdgesFromSPDX(t *testing.T, entries []sbom.Entry, complete bool) []string {
	t.Helper()
	var doc spdxDocForTest
	if err := json.Unmarshal(sbomBuild(t, entries, complete, sbom.FormatSPDX), &doc); err != nil {
		t.Fatalf("SPDX 输出不是合法 JSON：%v", err)
	}
	name := map[string]string{"SPDXRef-DOCUMENT": "<root>"}
	for _, p := range doc.Packages {
		name[p.SPDXID] = p.Name
	}
	var edges []string
	for _, r := range doc.Relationships {
		// 只比**依赖图形**：DESCRIBES 说的是"文档讲了谁"，与图形无关（见 spdx.go 的注释）。
		if r.Type != "DEPENDS_ON" {
			continue
		}
		from, ok := name[r.From]
		if !ok {
			t.Errorf("SPDX 的 DEPENDS_ON 引用了不存在的 SPDXID %q", r.From)
			continue
		}
		to, ok := name[r.To]
		if !ok {
			t.Errorf("SPDX 的 DEPENDS_ON 引用了不存在的 SPDXID %q", r.To)
			continue
		}
		edges = append(edges, from+" → "+to)
	}
	sort.Strings(edges)
	return edges
}

func sbomComponentCount(t *testing.T, entries []sbom.Entry, complete bool) int {
	t.Helper()
	var doc cdxDocForTest
	if err := json.Unmarshal(sbomBuild(t, entries, complete, sbom.FormatCycloneDX), &doc); err != nil {
		t.Fatal(err)
	}
	return len(doc.Components)
}

func equalEdges(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
