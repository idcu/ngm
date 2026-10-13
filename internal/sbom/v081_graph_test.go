package sbom

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/lock"
)

// v0.81：半张图不得被导出成一张看起来完整的图。
//
// 背景：edgeIndex 只看每条 `RequiredBy`，缺少 HasGraph 的等价判定。
// 一份 lock 里只要有一条 `requiredBy` 缺失（旧 lock、手改），其余条目的边
// 照常导出，`dependencies` / `DEPENDS_ON` 出现且不带任何"不完整"标记。
// 合规读者会把它读成完整的依赖树——而它少了一条边。
//
// 牙齿：把 Options.GraphComplete 的判据去掉（恒为 true），本测试红。

func v081Entries() []Entry {
	const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return []Entry{
		{
			Slug: "github:org/a", Canonical: "github.com/org/a",
			Ref: "v1", RefType: "tag",
			Commit:     "0123456789abcdef0123456789abcdef01234567",
			Digest:     "sha256:" + hex64,
			RequiredBy: []string{lock.RootRequirer},
		},
		{
			Slug: "github:org/b", Canonical: "github.com/org/b",
			Ref: "v2", RefType: "tag",
			Commit:     "89abcdef0123456789abcdef0123456789abcdef",
			Digest:     "sha256:" + hex64,
			RequiredBy: []string{"github:org/a"},
		},
	}
}

func TestV81PartialGraphIsNotExported(t *testing.T) {
	entries := v081Entries()

	complete, err := Build(Project{Name: "app", Version: "1.0.0"}, entries,
		Options{Format: FormatCycloneDX, GraphComplete: true})
	if err != nil {
		t.Fatalf("Build(complete): %v", err)
	}
	if !strings.Contains(string(complete), `"dependencies"`) {
		t.Fatal("完整图必须导出 dependencies 段")
	}

	// 同一份条目，但声明"图不完整"⇒ 整段省略。
	partial, err := Build(Project{Name: "app", Version: "1.0.0"}, entries,
		Options{Format: FormatCycloneDX, GraphComplete: false})
	if err != nil {
		t.Fatalf("Build(partial): %v", err)
	}
	if strings.Contains(string(partial), `"dependencies"`) {
		t.Error("图不完整时不得导出 dependencies —— 半张图看起来就是完整的")
	}
	if !strings.Contains(string(partial), `"components"`) {
		t.Error("组件段仍应导出：不完整的只是图形，不是清单")
	}
}

func TestV81PartialGraphSpdxOmitsDependsOn(t *testing.T) {
	entries := v081Entries()

	partial, err := Build(Project{Name: "app", Version: "1.0.0"}, entries,
		Options{Format: FormatSPDX, GraphComplete: false})
	if err != nil {
		t.Fatalf("Build(spdx, partial): %v", err)
	}
	if strings.Contains(string(partial), "DEPENDS_ON") {
		t.Error("图不完整时不得出现 DEPENDS_ON")
	}
	if !strings.Contains(string(partial), "DESCRIBES") {
		t.Error("DESCRIBES 与图形完整性无关，应保留")
	}
}

// TestV81SpdxIDsAreDistinct 钉住 SPDXID 派生的**单射性**。
//
// v0.81 之前所有非法字节一律压成单个 `-`，于是 `x/y`、`x-y`、`x:y`
// 派生出同一个 ID——两个不同组件共用一份标识，relationships 无法消歧，
// 而文档结构完全合法，没有任何东西会报错。
//
// 牙齿：把逐字节转义换回 `default: b.WriteByte('-')`，本测试红。
func TestV81SpdxIDsAreDistinct(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	variants := []Entry{
		{Slug: "github:org/dep", SubPath: "x/y", Commit: commit},
		{Slug: "github:org/dep", SubPath: "x-y", Commit: commit},
		{Slug: "github:org/dep", SubPath: "x:y", Commit: commit},
		{Slug: "github:org/dep", SubPath: "x.y", Commit: commit},
	}
	seen := map[string]string{}
	for _, e := range variants {
		id := spdxID(e)
		if prev, dup := seen[id]; dup {
			t.Errorf("SPDXID 碰撞：%q 与 %q 都派生为 %q", prev, e.SubPath, id)
		}
		seen[id] = e.SubPath
		if strings.ContainsAny(strings.TrimPrefix(id, "SPDXRef-Package-"),
			" !\"#$%&'()*+,/:;<=>?@[\\]^_`{|}~") {
			t.Errorf("SPDXID 含非法字符: %q", id)
		}
	}
}

// TestV81SpdxIDIsStable 同一条目两次派生必须相同（确定性）。
func TestV81SpdxIDIsStable(t *testing.T) {
	e := v081Entries()[0]
	if spdxID(e) != spdxID(e) {
		t.Error("SPDXID 必须确定")
	}
}

// TestV81ExportRefusesUnusableDigest 导出层必须拒掉空哈希（v0.81）。
//
// lock 的 Validate 只校验 `sha256:` 前缀（被篡改的 lock 要留到内容比对层报 exit 2），
// 但**导出层**要把 digest 写进产物：一个空哈希会让 CycloneDX 输出 `content: ""`、
// attestation 写出 `sha256: ""`——结构完全合法，内容却是空的。
// 而本包开头写着"没有哈希的 SBOM 只是清单"。
//
// 位置在 FromLock：SBOM 与 attestation 共用这一处，所以一份守卫覆盖两个出口。
//
// 牙齿：去掉 FromLock 里那道 IsArchiveDigest 判定，本测试红。
func TestV81ExportRefusesUnusableDigest(t *testing.T) {
	bad := []string{
		"sha256:",
		"sha256:abc",
		"sha256:" + strings.Repeat("0", 128),
	}
	for _, dg := range bad {
		lf := &lock.File{
			Version: 1, LockfileVersion: "1.0.0",
			Dependencies: []lock.Dependency{{
				Name:          "github:org/dep",
				Ref:           "v1",
				RefType:       "tag",
				Commit:        "0123456789abcdef0123456789abcdef01234567",
				ArchiveDigest: dg,
				VendorPath:    "github.com/org/dep",
				RequiredBy:    []string{lock.RootRequirer},
			}},
		}
		if _, err := FromLock(lf); err == nil {
			t.Errorf("digest %q 必须被导出层拒绝（它会产出 content 为空的假证据）", dg)
		} else if !strings.Contains(err.Error(), "no usable") {
			t.Errorf("拒绝时要说清成因，got: %v", err)
		}
	}
}

// TestV81BuildIsDeterministic 两种格式各导出两次必须字节相同。
func TestV81BuildIsDeterministic(t *testing.T) {
	entries := v081Entries()
	for _, f := range []Format{FormatCycloneDX, FormatSPDX} {
		a, err := Build(Project{Name: "app", Version: "1.0.0"}, entries,
			Options{Format: f, GraphComplete: true})
		if err != nil {
			t.Fatalf("Build(%s): %v", f, err)
		}
		b, err := Build(Project{Name: "app", Version: "1.0.0"}, entries,
			Options{Format: f, GraphComplete: true})
		if err != nil {
			t.Fatalf("Build(%s) x2: %v", f, err)
		}
		if string(a) != string(b) {
			t.Errorf("%s 导出不确定", f)
		}
		var v any
		if err := json.Unmarshal(a, &v); err != nil {
			t.Errorf("%s 输出不是合法 JSON: %v", f, err)
		}
	}
}
