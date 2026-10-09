package sbom

import "time"

// CycloneDX 1.5 的 JSON 形态（只用**必需 + 有价值**的字段，
// 不填的字段一律省略——SBOM 里的空字段比没有字段更容易被读成"确认过"）。
//
// 结构体的字段顺序即序列化顺序：它决定导出字节的顺序，
// 因此**调整顺序等同于改变输出**（与 lock / mappings 同一条纪律）。

// cdxDocument 是 CycloneDX 文档的顶层。
type cdxDocument struct {
	BomFormat   string         `json:"bomFormat"`
	SpecVersion string         `json:"specVersion"`
	Version     int            `json:"version"`
	Metadata    cdxMetadata    `json:"metadata"`
	Components  []cdxComponent `json:"components"`
}

type cdxMetadata struct {
	// Timestamp 是**唯一**的非确定字段（与 lock 的 resolvedAt 同一处理方式）。
	Timestamp string    `json:"timestamp"`
	Tools     []cdxTool `json:"tools"`
	Component cdxTarget `json:"component"`
}

type cdxTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// cdxTarget 描述"这份 SBOM 讲的是谁"（metadata.component）。
type cdxTarget struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type cdxComponent struct {
	Type string `json:"type"`
	// BomRef 是组件在本文档内的引用键——用 purl（全局唯一、可解析）。
	BomRef string `json:"bom-ref"`
	// Name 是 ngm 的依赖标识（slug），不是包名——Git 依赖没有"包名"这回事，
	// 而 slug 正是用户在自己的 ngm.json 里写的那个字符串（可回溯）。
	Name string `json:"name"`
	// Version 用**commit**：SBOM 的版本字段在这里回答的是"内容是哪一份"，
	// 而人类声明（ref / refType）放进 properties（它们是可变的命名）。
	Version string `json:"version"`
	Purl    string `json:"purl"`
	// Hashes 是 archiveDigest（**规范化清单哈希**，ADR-008）——
	// 它不是文件的字节哈希，因此 properties 里同时写明它的来源，
	// 免得有人拿它去和 tarball 的 sha256 对不上时以为导错了。
	Hashes             []cdxHash      `json:"hashes"`
	ExternalReferences []cdxReference `json:"externalReferences,omitempty"`
	Properties         []cdxProperty  `json:"properties,omitempty"`
}

type cdxHash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

type cdxReference struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type cdxProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func buildCycloneDX(project Project, entries []Entry, opts Options) cdxDocument {
	doc := cdxDocument{
		BomFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: cdxMetadata{
			Timestamp: opts.Now.UTC().Truncate(time.Second).Format(time.RFC3339),
			Tools: []cdxTool{{
				Vendor:  "ngm",
				Name:    "ngm",
				Version: opts.ToolVersion,
			}},
			Component: cdxTarget{
				Type:    "application",
				Name:    project.Name,
				Version: project.Version,
			},
		},
		Components: make([]cdxComponent, 0, len(entries)),
	}

	for _, e := range entries {
		c := cdxComponent{
			Type:    "library",
			BomRef:  e.Purl(),
			Name:    e.Slug,
			Version: e.Commit,
			Purl:    e.Purl(),
			Hashes: []cdxHash{{
				Alg:     "SHA-256",
				Content: e.HexDigest(),
			}},
			ExternalReferences: []cdxReference{{
				Type: "vcs",
				URL:  e.VCSURL(),
			}},
			Properties: []cdxProperty{
				{Name: "ngm:ref", Value: e.Ref},
				{Name: "ngm:refType", Value: e.RefType},
				{Name: "ngm:hashKind", Value: "archiveDigest (canonical file listing, ADR-008)"},
			},
		}
		if e.SubPath != "" {
			c.Properties = append(c.Properties, cdxProperty{Name: "ngm:subPath", Value: e.SubPath})
		}
		doc.Components = append(doc.Components, c)
	}
	return doc
}
