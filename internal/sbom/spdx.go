package sbom

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// SPDX 2.3 的 JSON 形态。
//
// 与 CycloneDX 那份同一纪律：字段顺序即序列化顺序；只填必需 + 有信息的字段。
// 两处 SPDX 特有的取舍写在各自的注释里（documentNamespace 与 SPDXID 的派生）。

// spdxDocument 是 SPDX 文档的顶层。
type spdxDocument struct {
	SPDXVersion       string           `json:"spdxVersion"`
	DataLicense       string           `json:"dataLicense"`
	SPDXID            string           `json:"SPDXID"`
	Name              string           `json:"name"`
	DocumentNamespace string           `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo `json:"creationInfo"`
	Packages          []spdxPackage    `json:"packages"`
	Relationships     []spdxRelation   `json:"relationships"`
}

type spdxCreationInfo struct {
	// Created 是**唯一**的非确定字段（与 lock 的 resolvedAt 同一处理方式）。
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	SPDXID           string `json:"SPDXID"`
	Name             string `json:"name"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	// FilesAnalyzed=false 是**诚实**的：我们不逐个分析文件，
	// 而是对一个规范化清单求哈希（ADR-008）。声明 true 会要求 files 段，
	// 而"为了让 schema 通过而编一份文件清单"正是 SBOM 最常见的失真来源。
	FilesAnalyzed    bool         `json:"filesAnalyzed"`
	LicenseConcluded string       `json:"licenseConcluded"`
	LicenseDeclared  string       `json:"licenseDeclared"`
	CopyrightText    string       `json:"copyrightText"`
	Checksums        []spdxSum    `json:"checksums"`
	ExternalRefs     []spdxExtRef `json:"externalRefs,omitempty"`
	Comment          string       `json:"comment,omitempty"`
}

type spdxSum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxExtRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type spdxRelation struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
	RelationshipType   string `json:"relationshipType"`
}

func buildSPDX(project Project, entries []Entry, opts Options) spdxDocument {
	doc := spdxDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              project.Name,
		DocumentNamespace: spdxNamespace(project, entries),
		CreationInfo: spdxCreationInfo{
			Created:  opts.Now.UTC().Truncate(time.Second).Format(time.RFC3339),
			Creators: []string{strings.TrimSpace("Tool: ngm-" + opts.ToolVersion)},
		},
		Packages:      make([]spdxPackage, 0, len(entries)),
		Relationships: make([]spdxRelation, 0, len(entries)),
	}

	for _, e := range entries {
		id := spdxID(e)
		pkg := spdxPackage{
			SPDXID:           id,
			Name:             e.Slug,
			VersionInfo:      e.Commit,
			DownloadLocation: e.VCSURL(),
			FilesAnalyzed:    false,
			// 许可证与版权一律 NOASSERTION：ngm **不知道**上游的许可证，
			// 而 SBOM 的价值恰恰在于"不知道就写不知道"——
			// 猜一个 SPDX 标识符（或者抄一个流行的）是这类工具最坏的失败形态。
			LicenseConcluded: "NOASSERTION",
			LicenseDeclared:  "NOASSERTION",
			CopyrightText:    "NOASSERTION",
			Checksums: []spdxSum{{
				Algorithm:     "SHA256",
				ChecksumValue: e.HexDigest(),
			}},
			ExternalRefs: []spdxExtRef{
				{
					ReferenceCategory: "PACKAGE-MANAGER",
					ReferenceType:     "purl",
					ReferenceLocator:  e.Purl(),
				},
				{
					ReferenceCategory: "OTHER",
					ReferenceType:     "vcs",
					ReferenceLocator:  e.VCSURL(),
				},
			},
			Comment: fmt.Sprintf("ngm: ref=%s refType=%s; checksum is the archiveDigest (canonical file listing, ADR-008), not a tarball hash",
				e.Ref, e.RefType),
		}
		doc.Packages = append(doc.Packages, pkg)
		doc.Relationships = append(doc.Relationships, spdxRelation{
			SPDXElementID:      "SPDXRef-DOCUMENT",
			RelatedSPDXElement: id,
			RelationshipType:   "DESCRIBES",
		})
	}
	// v0.73（ADR-028）：DEPENDS_ON 才是"依赖图形"；DESCRIBES 只是"文档讲了谁"。
	//
	// 根边从 DOCUMENT 出发：项目本身不是一个 package 元素（它没有被 DESCRIBES
	// 之外的身份），SPDX 允许文档作为关系的主体。
	doc.Relationships = append(doc.Relationships, spdxDependencies(entries)...)
	return doc
}

// spdxDependencies 导出 DEPENDS_ON 关系（v0.73，ADR-028）。
//
// 与 CycloneDX 那份同源（edgeIndex）：两种格式的依赖图形必须一致，
// 否则"挑一个格式看"的人会得到两棵树。旧 lock ⇒ 不追加任何关系（不编图）。
func spdxDependencies(entries []Entry) []spdxRelation {
	root, children := edgeIndex(entries)
	if len(root) == 0 && len(children) == 0 {
		return nil
	}
	idByKey := make(map[string]string, len(entries))
	for _, e := range entries {
		idByKey[e.Key()] = spdxID(e)
	}
	var out []spdxRelation
	for _, k := range root {
		out = append(out, spdxRelation{
			SPDXElementID:      "SPDXRef-DOCUMENT",
			RelatedSPDXElement: idByKey[k],
			RelationshipType:   "DEPENDS_ON",
		})
	}
	for _, e := range entries {
		for _, k := range children[e.Key()] {
			out = append(out, spdxRelation{
				SPDXElementID:      idByKey[e.Key()],
				RelatedSPDXElement: idByKey[k],
				RelationshipType:   "DEPENDS_ON",
			})
		}
	}
	return out
}

// spdxID 派生一个确定且合法的 SPDXID。
//
// SPDXID 的合法字符是 `[A-Za-z0-9.-]`，而 slug 里会出现 `:` 与 `/`——
// 逐个替换为 `-`；末尾接 commit 的前 8 位，让"同一仓库的不同 commit"
// 与"同一 commit 的不同子路径"都得到不同的 ID（后者由 subPath 参与派生）。
func spdxID(e Entry) string {
	raw := e.Slug + "@" + e.Commit
	if e.SubPath != "" {
		raw += "#" + e.SubPath
	}
	var b strings.Builder
	b.WriteString("SPDXRef-Package-")
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// spdxNamespace 派生 documentNamespace。
//
// 用它而不是随机 UUID：**同一份 lock 必须导出同一个命名空间**
// （SPDX 要求命名空间在一次导出内唯一；而"每次导出都换一个新 UUID"
// 会让两份文档无法被工具比对——对"可复现"为卖点的项目，那是错误方向）。
// 输入是排序后的 purl 列表（`Build` 已经排好序），因此它只随 lock 变化。
func spdxNamespace(project Project, entries []Entry) string {
	h := sha256.New()
	io.WriteString(h, project.Name+"@"+project.Version+"\n")
	for _, e := range entries {
		io.WriteString(h, e.Purl()+"\n")
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	name := project.Name
	if name == "" {
		name = "project"
	}
	return "https://ngm.invalid/sbom/" + sanitizeForURI(name) + "-" + sum
}

// sanitizeForURI 把项目名里的非 URI 友好字符换成 `-`。
func sanitizeForURI(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
