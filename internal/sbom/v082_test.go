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
		if !strings.HasPrefix(got, "pkg:git/github.com/org/dep@"+commit) {
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

// TestV82PurlMatchesTheRegisteredGitType 把 purl 的形状钉在**规范**上。
//
// v0.82 之前这一条一直挂在"已知未做"里，理由是"未核对 purl-spec"。
// 现已核对（来源：purl-spec 的 purl-types-index.json 与 types/git 的定义）：
//
//   - `git` 是**已注册**的 purl type（与 github / gitlab / bitbucket 并列）；
//   - 语法 `pkg:git/<namespace>/<name>@<version>?<qualifiers>#<subpath>`；
//   - namespace **必填** = 源码宿主；name **必填** = 仓库名（含 owner）；
//   - version 可选，定义写明"理想情况是 commit 或 tag"——ngm 给的正是 commit；
//   - subpath 受支持（v0.82 起按 RFC 3986 编码）。
//
// 唯一需要知道的歧义：规范说 name 是"repository name with owner"（自身含 `/`），
// 而通用解析器按**最后一段**切分 name。于是
// `pkg:git/github.com/org/repo@<commit>` 可能被读成
// namespace=`github.com/org`、name=`repo`。**这不影响往返**：
// namespace + "/" + name 恒等于 canonical。记录在此，免得有人以为它是 bug。
func TestV82PurlMatchesTheRegisteredGitType(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	e := Entry{Slug: "github:org/dep", Canonical: "github.com/org/dep", Commit: commit}
	p := e.Purl()

	if !strings.HasPrefix(p, "pkg:git/") {
		t.Fatalf("type 必须是已注册的 git：%q", p)
	}
	// 去掉 type，剩下的形状必须是 <namespace>/<name>@<version>
	rest := strings.TrimPrefix(p, "pkg:git/")
	at := strings.Index(rest, "@")
	if at <= 0 {
		t.Fatalf("缺少 version 段（规范要求 version 是 git ref，ngm 给 commit）：%q", p)
	}
	version := rest[at+1:]
	if version != commit {
		t.Errorf("version = %q, want commit %q", version, commit)
	}
	nsName := rest[:at]
	if strings.Count(nsName, "/") < 2 {
		t.Errorf("namespace/name = %q：至少要含 host 与 repo 两段（namespace 与 name 都必填）", nsName)
	}
	// 往返：namespace + name 必须能还原 canonical
	if nsName != e.Canonical {
		t.Errorf("namespace+name = %q, want canonical %q（往返必须成立）", nsName, e.Canonical)
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
