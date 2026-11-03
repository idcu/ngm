package sbom

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
)

// v0.82：purl 的 subpath 编码，以及 SPDX 命名空间覆盖 digest。

// TestV82PurlSubPathIsEncoded 钉住 subpath 段的百分号编码。
//
// subPath 是**上游可控**的字符串（来自依赖自己的 ngm.json）。
// 一个含空格、`?`、`#`、`%` 的子路径会让 purl 语法失效——而 purl 是
// SBOM（CycloneDX 的 bom-ref 与 externalReferences）与 attestation
// （subject[].name）共同的**引用键**：它不合法，工具就解析不了这两份产物。
//
// 牙齿：把 Purl 里的 `purlSubPath(e.SubPath)` 换回 `e.SubPath` ⇒ 本测试红。
func TestV82PurlSubPathIsEncoded(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		sub  string
		want string
		why  string
	}{
		{"packages/core", "packages/core", "普通段名不编码（`/` 是分隔符，字母是未保留字符）"},
		{"my package/src", "my%20package/src", "空格必须编码"},
		{"a?b", "a%3Fb", "`?` 会截断 purl 的 subpath 段"},
		{"a#b", "a%23b", "`#` 会截断"},
		{"a%b", "a%25b", "`%` 必须自身编码，否则无法还原"},
		{"a/b c", "a/b%20c", "只编码段内，分隔符保留"},
	}
	for _, tc := range cases {
		e := Entry{Slug: "github:org/dep", Canonical: "github.com/org/dep", Commit: commit, SubPath: tc.sub}
		got := e.Purl()
		if !strings.HasSuffix(got, "#"+tc.want) {
			t.Errorf("Purl() subpath = %q, want %q（%s）", got, tc.want, tc.why)
		}
		if !strings.HasPrefix(got, "pkg:github/org/dep@"+commit) {
			t.Errorf("Purl() = %q：前缀（type/namespace/version）不应被编码影响", got)
		}
	}
}

// TestV82PurlEncodingIsInjective 编码必须可逆：不同子路径不得得到同一个 purl。
func TestV82PurlEncodingIsInjective(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	subs := []string{"a b", "a%20b", "a/b", "a b/c", "a%2Fb"}
	seen := map[string]string{}
	for _, s := range subs {
		e := Entry{Slug: "github:org/dep", Canonical: "github.com/org/dep", Commit: commit, SubPath: s}
		p := e.Purl()
		if prev, dup := seen[p]; dup {
			t.Errorf("purl 碰撞：%q 与 %q 都得到 %q", prev, s, p)
		}
		seen[p] = s
	}
}

// TestV102PurlUsesHostSpecificTypes 钉住 v0.102 的 purl 分流与 bom-ref 稳定性。
//
// 主流 SBOM 解析器认识 github / gitlab / gitee；自建 host 需要 generic +
// vcs_url。bom-ref 保留旧的 pkg:git 形态，避免已归档文档失去稳定组件 ID。
func TestV102PurlUsesHostSpecificTypes(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		host, canonical, wantPrefix string
	}{
		{"github.com", "github.com/org/dep", "pkg:github/org/dep@" + commit},
		{"gitlab.com", "gitlab.com/org/dep", "pkg:gitlab/org/dep@" + commit},
		{"gitee.com", "gitee.com/org/dep", "pkg:gitee/org/dep@" + commit},
		{"git.example.com", "git.example.com/org/dep", "pkg:generic/git.example.com/org/dep@" + commit + "?vcs_url="},
	}
	for _, tc := range cases {
		e := Entry{Slug: tc.host + ":org/dep", Canonical: tc.canonical, Commit: commit}
		if got := e.Purl(); !strings.HasPrefix(got, tc.wantPrefix) {
			t.Errorf("%s: Purl() = %q, want prefix %q", tc.host, got, tc.wantPrefix)
		}
		if got := e.BomRef(); got != "pkg:git/"+tc.canonical+"@"+commit {
			t.Errorf("%s: BomRef() = %q, want the stable pkg:git form", tc.host, got)
		}
	}
}

// TestV82SpdxNamespaceCoversTheDigest 命名空间必须随内容变化，而不只随包集合。
//
// 只有 purl 时，命名空间标识的是"**这批包**"而不是"**这份文档**"：
// 两份 purl 集合相同、archiveDigest 不同的文档会共享同一个
// documentNamespace，工具比对时会把它们误判成同一份。
//
// 牙齿：把 spdxNamespace 里的 `+e.Digest` 去掉 ⇒ 本测试红。
func TestV82SpdxNamespaceCoversTheDigest(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	const hexA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const hexB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	entries := func(dg string) []Entry {
		return []Entry{{
			Slug: "github:org/dep", Canonical: "github.com/org/dep",
			Ref: "v1", RefType: "tag", Commit: commit,
			Digest: "sha256:" + dg, RequiredBy: []string{lock.RootRequirer},
		}}
	}

	proj := Project{Name: "github:me/app", Version: "1.0.0"}
	a := spdxNamespace(proj, entries(hexA))
	b := spdxNamespace(proj, entries(hexB))
	if a == b {
		t.Error("内容不同（digest 不同）的两份文档不得共享 documentNamespace")
	}
	// 同一份输入必须仍然确定
	if a != spdxNamespace(proj, entries(hexA)) {
		t.Error("命名空间必须确定")
	}
}
