package attest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/sbom"
)

// v0.70：attestation 的判据。
//
// 四条性质最重要：
//   - **确定性**：同一输入两次导出逐字节相同（没有时间戳、没有随机 id）；
//   - **顺序无关**：输入顺序不改变输出（排序由导出器做）；
//   - **哈希在场且被限定**：subject 必须带 sha256，且注解说明它是**清单**的哈希；
//   - **不越界**：不声称上游构建、不写时间、不带 ngm 自造的格式。

const (
	testCommit  = "abc123def4567890abcdef1234567890abcdef12"
	testDigest  = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	testHex     = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	testLockHex = "1111111111111111111111111111111111111111111111111111111111111111"
	testManHex  = "2222222222222222222222222222222222222222222222222222222222222222"
)

func testEntries() []sbom.Entry {
	return []sbom.Entry{
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

func testOptions() Options {
	return Options{
		ToolVersion:    "0.70.0-test",
		ManifestDigest: testManHex,
		LockDigest:     testLockHex,
	}
}

// statement 是测试侧对文档的独立映射（刻意不 import 包内结构体：
// 断言读的是**字节里的字段**，这样字段名/嵌套形态的变化会被测试看见）。
type testStatement struct {
	Type          string `json:"_type"`
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Name        string            `json:"name"`
		Digest      map[string]string `json:"digest"`
		Annotations map[string]string `json:"annotations"`
	} `json:"subject"`
	Predicate struct {
		BuildDefinition struct {
			BuildType          string `json:"buildType"`
			ExternalParameters struct {
				Manifest struct {
					URI    string            `json:"uri"`
					Digest map[string]string `json:"digest"`
				} `json:"manifest"`
				Lock struct {
					URI    string            `json:"uri"`
					Digest map[string]string `json:"digest"`
				} `json:"lock"`
			} `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI         string            `json:"uri"`
				Digest      map[string]string `json:"digest"`
				Annotations map[string]string `json:"annotations"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID      string            `json:"id"`
				Version map[string]string `json:"version"`
			} `json:"builder"`
			Metadata map[string]any `json:"metadata"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

func buildAndParse(t *testing.T, entries []sbom.Entry, opts Options) (string, testStatement) {
	t.Helper()
	data, err := Build(entries, opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var doc testStatement
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	return string(data), doc
}

func TestV70StatementShape(t *testing.T) {
	raw, doc := buildAndParse(t, testEntries(), testOptions())

	if doc.Type != StatementType || doc.PredicateType != PredicateType {
		t.Errorf("信封不对: %q / %q", doc.Type, doc.PredicateType)
	}
	if doc.Predicate.BuildDefinition.BuildType != BuildType {
		t.Errorf("buildType = %q", doc.Predicate.BuildDefinition.BuildType)
	}
	if len(doc.Subject) != 2 || len(doc.Predicate.BuildDefinition.ResolvedDependencies) != 2 {
		t.Fatalf("subject=%d materials=%d, want 2/2", len(doc.Subject), len(doc.Predicate.BuildDefinition.ResolvedDependencies))
	}

	// subject：purl + 清单哈希 + "这个哈希是什么"的注解。
	for _, s := range doc.Subject {
		if !strings.HasPrefix(s.Name, "pkg:git/") || !strings.Contains(s.Name, "@"+testCommit) {
			t.Errorf("subject 名不是 purl: %q", s.Name)
		}
		if s.Digest["sha256"] != testHex {
			t.Errorf("%s：digest 不是裸 hex 的 sha256: %+v", s.Name, s.Digest)
		}
		scope := s.Annotations[annotationDigestScope]
		if !strings.Contains(scope, "manifest") || !strings.Contains(scope, "not the tarball") {
			t.Errorf("%s：缺少/写错了 digest-scope 注解: %q", s.Name, scope)
		}
	}
	// 排序：mono 在 utils 之前（字节序），且子路径进 purl 的 subpath 段。
	if !strings.HasSuffix(doc.Subject[0].Name, "#packages/core") {
		t.Errorf("monorepo 子路径应当进 purl 的 subpath 段，且 mono 应排在 utils 前: %q", doc.Subject[0].Name)
	}

	// materials：上游 commit 是 gitCommit 形态，URI 是可解析的 git+https。
	for _, m := range doc.Predicate.BuildDefinition.ResolvedDependencies {
		if m.Digest["gitCommit"] != testCommit {
			t.Errorf("material 缺 gitCommit: %+v", m)
		}
		if !strings.HasPrefix(m.URI, "git+https://") || !strings.HasSuffix(m.URI, "@"+testCommit) {
			t.Errorf("material uri 形态不对: %q", m.URI)
		}
		if m.Annotations[annotationDeclaredRef] == "" {
			t.Errorf("material 应当记下声明的 ref: %+v", m)
		}
	}
	var refs []string
	for _, m := range doc.Predicate.BuildDefinition.ResolvedDependencies {
		refs = append(refs, m.Annotations[annotationDeclaredRef])
	}
	// materials 与 subject 同序：mono（branch:main）在前，utils（tag:v1.2.3）在后。
	if strings.Join(refs, " ") != "branch:main tag:v1.2.3" {
		t.Errorf("declared-ref 序列 = %v, want [branch:main tag:v1.2.3]", refs)
	}

	// 输入：两个文件的 sha256（提交进 Git 的那份字节）。
	ep := doc.Predicate.BuildDefinition.ExternalParameters
	if ep.Manifest.URI != ManifestURI || ep.Manifest.Digest["sha256"] != testManHex {
		t.Errorf("manifest 外部参数不对: %+v", ep.Manifest)
	}
	if ep.Lock.URI != LockURI || ep.Lock.Digest["sha256"] != testLockHex {
		t.Errorf("lock 外部参数不对: %+v", ep.Lock)
	}

	// builder 与 invocationId。
	rd := doc.Predicate.RunDetails
	if rd.Builder.ID != BuilderID || rd.Builder.Version["ngm"] != "0.70.0-test" {
		t.Errorf("builder 不对: %+v", rd.Builder)
	}
	if rd.Metadata["invocationId"] != testLockHex {
		t.Errorf("invocationId 应当是 lock 的哈希（确定性要求）: %+v", rd.Metadata)
	}

	// 时间戳：一个都不许有。
	for _, key := range []string{"timestamp", "startedOn", "finishedOn", "created"} {
		if strings.Contains(raw, `"`+key+`"`) {
			t.Errorf("语句里不许有时间字段（确定性）：出现了 %q", key)
		}
	}
	if !strings.HasSuffix(raw, "}\n") {
		t.Error("输出必须以单个换行收尾")
	}
}

func TestV70Deterministic(t *testing.T) {
	a, err := Build(testEntries(), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(testEntries(), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("同一输入两次导出字节不同——diff / 快照 / 签名比对全部失效")
	}
}

// 组件顺序由导出器决定：输入顺序不改变字节。
func TestV70OrderIndependentOfInput(t *testing.T) {
	entries := testEntries()
	reversed := []sbom.Entry{entries[1], entries[0]}

	a, err := Build(entries, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(reversed, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("输入顺序改变了输出——排序没有在导出器内部做")
	}
}

// 空依赖集合仍是一份合法语句（"没有依赖"是有信息的声明，不是错误）。
func TestV70EmptyDependencySet(t *testing.T) {
	raw, doc := buildAndParse(t, nil, testOptions())
	if len(doc.Subject) != 0 || len(doc.Predicate.BuildDefinition.ResolvedDependencies) != 0 {
		t.Errorf("空集合应当导出空数组: %+v", doc)
	}
	// 必须是 `[]` 而不是 `null`（消费方的解析器会感谢这一点）。
	if !strings.Contains(raw, `"subject": []`) {
		t.Errorf("空的 subject 应当是 []: %s", raw)
	}
}

// 缺输入（哈希/版本）必须报错，而不是导出一份"看起来完整"的语句。
func TestV70RequiresInputs(t *testing.T) {
	cases := []struct {
		name string
		opts Options
	}{
		{"缺 manifest 哈希", Options{ToolVersion: "x", LockDigest: testLockHex}},
		{"缺 lock 哈希", Options{ToolVersion: "x", ManifestDigest: testManHex}},
		{"哈希不是 64 位 hex", Options{ToolVersion: "x", ManifestDigest: "abc", LockDigest: testLockHex}},
		{"缺版本", Options{ManifestDigest: testManHex, LockDigest: testLockHex}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Build(testEntries(), c.opts); err == nil {
				t.Fatal("必须报错——一份缺输入的语句会被下游当成完整的证据")
			} else if hint := errs.Hint(err); !strings.Contains(hint, "wiring error") {
				t.Errorf("错误要指明这是 ngm 的接线问题（不是用户的配置问题）: %q", hint)
			}
		})
	}
}
