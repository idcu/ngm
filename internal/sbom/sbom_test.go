package sbom

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.69：SBOM 导出的判据。
//
// 三条性质最重要（其余都是它们的具体化）：
//   - **确定性**：同一份 lock 导两次，除时间戳外逐字节相同；
//   - **哈希在场**：每个组件都必须带 archiveDigest 的哈希——没有哈希的 SBOM 只是清单；
//   - **不猜**：许可证/版权一律 NOASSERTION，不编造。

const (
	testCommit = "abc123def4567890abcdef1234567890abcdef12"
	testDigest = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	testHex    = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
)

func testEntries() []Entry {
	return []Entry{
		{
			Slug: "github:org/utils", Ref: "v1.2.3", RefType: "tag",
			Commit: testCommit, Digest: testDigest,
			Canonical: "github.com/org/utils",
		},
		{
			Slug: "github:org/mono", Ref: "main", RefType: "branch",
			Commit: testCommit, Digest: testDigest,
			Canonical: "github.com/org/mono", SubPath: "packages/core",
		},
	}
}

func fixedOptions(f Format) Options {
	return Options{
		Format:      f,
		Now:         time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		ToolVersion: "0.69.0-test",
	}
}

func TestV69ParseFormat(t *testing.T) {
	for _, in := range []string{"cyclonedx", "CycloneDX", " spdx ", "SPDX"} {
		if _, err := ParseFormat(in); err != nil {
			t.Errorf("ParseFormat(%q): %v", in, err)
		}
	}
	if _, err := ParseFormat("sarif"); err == nil {
		t.Error("未知格式必须报错（并给出支持列表）")
	} else if hint := errs.Hint(err); !strings.Contains(hint, "cyclonedx") || !strings.Contains(hint, "spdx") {
		t.Errorf("hint 要给出支持列表，got: %q", hint)
	}
}

// TestV69OutputIsDeterministic 固定"同输入 ⇒ 同字节"（时间戳注入为同一值）。
func TestV69OutputIsDeterministic(t *testing.T) {
	for _, f := range Formats() {
		a, err := Build(Project{Name: "app", Version: "1.0.0"}, testEntries(), fixedOptions(f))
		if err != nil {
			t.Fatalf("Build(%s): %v", f, err)
		}
		b, err := Build(Project{Name: "app", Version: "1.0.0"}, testEntries(), fixedOptions(f))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s：同一输入两次导出字节不同——diff / 快照全部失效", f)
		}
		if !strings.HasSuffix(string(a), "}\n") {
			t.Errorf("%s：输出必须以单个换行收尾（与仓库其余产物一致）", f)
		}
	}
}

// TestV69OrderIsIndependentOfInput 固定"组件顺序由导出器决定，不信任调用方"。
func TestV69OrderIsIndependentOfInput(t *testing.T) {
	entries := testEntries()
	reversed := []Entry{entries[1], entries[0]}

	for _, f := range Formats() {
		a, err := Build(Project{Name: "app"}, entries, fixedOptions(f))
		if err != nil {
			t.Fatal(err)
		}
		b, err := Build(Project{Name: "app"}, reversed, fixedOptions(f))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Errorf("%s：输入顺序改变了输出——排序没有在导出器内部做", f)
		}
	}
}

// TestV69TimestampIsTheOnlyNonDeterministicField：换一个 Now，
// 只有时间戳字段变，其余逐字节相同。
func TestV69TimestampIsTheOnlyNonDeterministicField(t *testing.T) {
	optsA := fixedOptions(FormatCycloneDX)
	optsB := optsA
	optsB.Now = optsA.Now.Add(24 * time.Hour)

	a, err := Build(Project{Name: "app"}, testEntries(), optsA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(Project{Name: "app"}, testEntries(), optsB)
	if err != nil {
		t.Fatal(err)
	}

	strip := func(s string) string {
		var doc map[string]any
		if err := json.Unmarshal([]byte(s), &doc); err != nil {
			t.Fatal(err)
		}
		if md, ok := doc["metadata"].(map[string]any); ok {
			delete(md, "timestamp")
		}
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if strip(string(a)) != strip(string(b)) {
		t.Error("除时间戳之外还有字段随 Now 变化——那会让导出的 diff 里出现假差异")
	}
}

func TestV69CycloneDXCarriesHashesAndPurls(t *testing.T) {
	data, err := Build(Project{Name: "app", Version: "1.0.0"}, testEntries(), fixedOptions(FormatCycloneDX))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		BomFormat   string `json:"bomFormat"`
		SpecVersion string `json:"specVersion"`
		Version     int    `json:"version"`
		Metadata    struct {
			Timestamp string `json:"timestamp"`
			Tools     []struct {
				Vendor string `json:"vendor"`
				Name   string `json:"name"`
			} `json:"tools"`
			Component struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"component"`
		} `json:"metadata"`
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
			ExternalReferences []struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"externalReferences"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}

	if doc.BomFormat != "CycloneDX" || doc.SpecVersion != "1.5" || doc.Version != 1 {
		t.Errorf("文档头不对: %+v", doc)
	}
	if doc.Metadata.Timestamp != "2026-10-09T12:00:00Z" {
		t.Errorf("timestamp = %q", doc.Metadata.Timestamp)
	}
	if len(doc.Metadata.Tools) != 1 || doc.Metadata.Tools[0].Vendor != "ngm" {
		t.Errorf("tools 段不对: %+v", doc.Metadata.Tools)
	}
	if doc.Metadata.Component.Name != "app" {
		t.Errorf("metadata.component.name = %q", doc.Metadata.Component.Name)
	}
	if len(doc.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(doc.Components))
	}

	for _, c := range doc.Components {
		if len(c.Hashes) != 1 || c.Hashes[0].Alg != "SHA-256" || c.Hashes[0].Content != testHex {
			t.Errorf("%s：哈希段不对: %+v（SBOM 的价值就在这一行）", c.Name, c.Hashes)
		}
		if !strings.HasPrefix(c.Purl, "pkg:git/") || !strings.Contains(c.Purl, "@"+testCommit) {
			t.Errorf("%s：purl 形态不对: %q", c.Name, c.Purl)
		}
		if c.BomRef != c.Purl {
			t.Errorf("%s：bom-ref 应当就是 purl（可解析、唯一）", c.Name)
		}
		if c.Version != testCommit {
			t.Errorf("%s：version 应当是 commit（内容锚点），got %q", c.Name, c.Version)
		}
		var hasVCS bool
		for _, r := range c.ExternalReferences {
			if r.Type == "vcs" && strings.HasPrefix(r.URL, "https://") {
				hasVCS = true
			}
		}
		if !hasVCS {
			t.Errorf("%s：缺 vcs externalReference", c.Name)
		}
	}

	// 排序：github:org/mono 在 github:org/utils 之前（字节序）。
	if doc.Components[0].Name != "github:org/mono" {
		t.Errorf("排序不对：第一个是 %q", doc.Components[0].Name)
	}
	// subPath 进 purl 的 subpath 段。
	if !strings.HasSuffix(doc.Components[0].Purl, "#packages/core") {
		t.Errorf("monorepo 子路径应当进 purl 的 subpath 段: %q", doc.Components[0].Purl)
	}
}

func TestV69SPDXCarriesHashesPurlsAndNoAssertion(t *testing.T) {
	data, err := Build(Project{Name: "app", Version: "1.0.0"}, testEntries(), fixedOptions(FormatSPDX))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SPDXVersion       string `json:"spdxVersion"`
		DataLicense       string `json:"dataLicense"`
		SPDXID            string `json:"SPDXID"`
		DocumentNamespace string `json:"documentNamespace"`
		CreationInfo      struct {
			Created  string   `json:"created"`
			Creators []string `json:"creators"`
		} `json:"creationInfo"`
		Packages []struct {
			SPDXID           string `json:"SPDXID"`
			Name             string `json:"name"`
			VersionInfo      string `json:"versionInfo"`
			DownloadLocation string `json:"downloadLocation"`
			FilesAnalyzed    bool   `json:"filesAnalyzed"`
			LicenseConcluded string `json:"licenseConcluded"`
			LicenseDeclared  string `json:"licenseDeclared"`
			CopyrightText    string `json:"copyrightText"`
			Checksums        []struct {
				Algorithm     string `json:"algorithm"`
				ChecksumValue string `json:"checksumValue"`
			} `json:"checksums"`
			ExternalRefs []struct {
				ReferenceCategory string `json:"referenceCategory"`
				ReferenceType     string `json:"referenceType"`
				ReferenceLocator  string `json:"referenceLocator"`
			} `json:"externalRefs"`
		} `json:"packages"`
		Relationships []struct {
			SPDXElementID      string `json:"spdxElementId"`
			RelatedSPDXElement string `json:"relatedSpdxElement"`
			RelationshipType   string `json:"relationshipType"`
		} `json:"relationships"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}

	if doc.SPDXVersion != "SPDX-2.3" || doc.DataLicense != "CC0-1.0" || doc.SPDXID != "SPDXRef-DOCUMENT" {
		t.Errorf("文档头不对: %+v", doc)
	}
	if !strings.HasPrefix(doc.DocumentNamespace, "https://ngm.invalid/sbom/") {
		t.Errorf("documentNamespace = %q（应当是确定的 URI，不是随机 UUID）", doc.DocumentNamespace)
	}
	if len(doc.Packages) != 2 || len(doc.Relationships) != 2 {
		t.Fatalf("packages=%d relationships=%d, want 2/2", len(doc.Packages), len(doc.Relationships))
	}

	ids := map[string]bool{}
	for _, p := range doc.Packages {
		if p.FilesAnalyzed {
			t.Errorf("%s：filesAnalyzed 必须为 false（我们不逐个分析文件）", p.Name)
		}
		if p.LicenseConcluded != "NOASSERTION" || p.LicenseDeclared != "NOASSERTION" || p.CopyrightText != "NOASSERTION" {
			t.Errorf("%s：许可证/版权必须写 NOASSERTION（不知道就写不知道），got %+v", p.Name, p)
		}
		if len(p.Checksums) != 1 || p.Checksums[0].Algorithm != "SHA256" || p.Checksums[0].ChecksumValue != testHex {
			t.Errorf("%s：checksums 不对: %+v", p.Name, p.Checksums)
		}
		if p.VersionInfo != testCommit {
			t.Errorf("%s：versionInfo 应当是 commit", p.Name)
		}
		if !strings.HasPrefix(p.DownloadLocation, "https://") {
			t.Errorf("%s：downloadLocation 应当是 URL: %q", p.Name, p.DownloadLocation)
		}
		var hasPurl bool
		for _, r := range p.ExternalRefs {
			if r.ReferenceType == "purl" && strings.HasPrefix(r.ReferenceLocator, "pkg:git/") {
				hasPurl = true
			}
		}
		if !hasPurl {
			t.Errorf("%s：缺 purl externalRef", p.Name)
		}
		if !strings.HasPrefix(p.SPDXID, "SPDXRef-Package-") {
			t.Errorf("%s：SPDXID 形态不对: %q", p.Name, p.SPDXID)
		}
		if ids[p.SPDXID] {
			t.Errorf("SPDXID 重复: %q", p.SPDXID)
		}
		ids[p.SPDXID] = true
	}

	for _, r := range doc.Relationships {
		if r.RelationshipType != "DESCRIBES" || r.SPDXElementID != "SPDXRef-DOCUMENT" {
			t.Errorf("relationship 不对: %+v", r)
		}
	}
}

// 空依赖集合也要导出合法文档（"没有依赖"是一份**有信息**的 SBOM，
// 不是错误——CI 在最小项目上也要能拿到文档）。
func TestV69EmptyDependencySetIsAValidDocument(t *testing.T) {
	for _, f := range Formats() {
		data, err := Build(Project{Name: "empty"}, nil, fixedOptions(f))
		if err != nil {
			t.Fatalf("Build(%s): %v", f, err)
		}
		var any map[string]any
		if err := json.Unmarshal(data, &any); err != nil {
			t.Fatalf("%s: 输出不是合法 JSON: %v", f, err)
		}
	}
}
