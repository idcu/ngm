// Package attest 把 ngm.lock 导出为 in-toto Statement v1 + SLSA provenance v1 谓词（v0.70）。
//
// 定位（评估报告第 3 层第二条）：把 ngm 从"L2 的一半"推到"**内容侧**的 L2"。
// 做法不是自造签名体系（[ADR-014] 明确拒绝），也不是给不可信的自述加签名，
// 而是**接一层标准谓词**：ngm 产出未签名的语句，签名交给用户/CI 的标准工具。
//
// 三条纪律：
//
//  1. **确定性**：同一份 lock 导出两次**字节相同**。语句里没有时间戳，
//     invocationId 是输入的哈希而不是随机的运行 id——"这次运行是谁、在何时"
//     由**签名者**提供，那才是它的位置（可复现优先，见 ADR-027）。
//  2. **只声称锁里有的**：subject 是 archiveDigest（规范化清单哈希，ADR-008），
//     material 是 commit；**不声称**上游构建完整性、可复现构建、许可证——那是别的轴。
//  3. **不发明格式**：_type / predicateType 都是既定标准，buildType 是一个可解析的 URI；
//     注解键用 `ngm/` 前缀（不占用我们并不拥有的域名）。
//
// [ADR-014]: https://github.com/idcu/ngm/blob/main/docs/adr/adr-014-self-report-signatures.md
package attest

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/sbom"
)

const (
	// StatementType 是 in-toto Statement v1（已被 ITE-6 稳定，不是 ngm 的发明）。
	StatementType = "https://in-toto.io/Statement/v1"
	// PredicateType 是 SLSA provenance v1——标准谓词。
	PredicateType = "https://slsa.dev/provenance/v1"
	// BuildType 指向定义了这份谓词字段语义的那一页（ADR-027）。
	BuildType = "https://github.com/idcu/ngm/blob/main/docs/adr/adr-027-attestation.md"
	// BuilderID 是产出这份语句的构建者标识。
	BuilderID = "https://github.com/idcu/ngm"

	// ManifestURI / LockURI 是输入的名字（仓库相对路径，不是本机绝对路径——
	// 绝对路径既不稳定也不该被写进要公开的文档）。
	//
	// 取自定义那两个文件的包，不在这里重写一遍字面量：名字写在第二个地方，
	// 迟早会漂移（v0.70 顺手把 config 里的字面量提成了常量）。
	ManifestURI = config.FileName
	LockURI     = lock.FileName

	// annotationDigestScope 说明 subject 的 sha256 **是什么**的哈希。
	// 这一条注解是整份文档里最容易被误读的地方：archiveDigest 是规范化清单的哈希，
	// **不是 tarball 的哈希**，拿去和别处的 tarball 校验和对比会得出错误结论。
	annotationDigestScope = "ngm/digest-scope"
	// annotationDeclaredRef 记下 ngm.json 里声明的 ref / refType（人类溯源）。
	annotationDeclaredRef = "ngm/declared-ref"

	digestScopeValue = "sha256 over the canonical archive manifest (ADR-008); not the tarball bytes"
)

// Options 控制一次导出。
type Options struct {
	// ToolVersion 是 ngm 自身的版本（写进 builder.version）。
	ToolVersion string
	// ManifestDigest / LockDigest 是**裸 hex**（64 位）——对提交进 Git 的
	// 那份文件字节的 sha256（不是规范化后的重排字节）。
	ManifestDigest string
	LockDigest     string
}

// Build 生成语句字节流。
//
// 输出纪律与仓库其余产物一致：2 空格缩进、LF、末尾单个换行。
func Build(entries []sbom.Entry, opts Options) ([]byte, error) {
	if !isHex64(opts.ManifestDigest) || !isHex64(opts.LockDigest) {
		return nil, errs.New(errs.CodeConfigInvalid,
			"attestation requires the sha256 digests of "+ManifestURI+" and "+LockURI,
			"this is a wiring error in ngm, not in your project; please report it")
	}
	if strings.TrimSpace(opts.ToolVersion) == "" {
		return nil, errs.New(errs.CodeConfigInvalid,
			"attestation requires the ngm version",
			"this is a wiring error in ngm, not in your project; please report it")
	}

	sorted := sbom.Sort(entries)

	subjects := make([]subject, 0, len(sorted))
	materials := make([]material, 0, len(sorted))
	for _, e := range sorted {
		subjects = append(subjects, subject{
			Name:   e.Purl(),
			Digest: sha256Digest{SHA256: e.HexDigest()},
			Annotations: map[string]string{
				annotationDigestScope: digestScopeValue,
			},
		})
		materials = append(materials, material{
			URI:    "git+" + e.VCSURL() + "@" + e.Commit,
			Digest: gitCommitDigest{GitCommit: e.Commit},
			Annotations: map[string]string{
				annotationDeclaredRef: e.RefType + ":" + e.Ref,
			},
		})
	}

	doc := statement{
		Type:          StatementType,
		Subject:       subjects,
		PredicateType: PredicateType,
		Predicate: provenance{
			BuildDefinition: buildDefinition{
				BuildType: BuildType,
				ExternalParameters: externalParameters{
					Manifest: fileRef{
						URI:    ManifestURI,
						Digest: sha256Digest{SHA256: opts.ManifestDigest},
					},
					Lock: fileRef{
						URI:    LockURI,
						Digest: sha256Digest{SHA256: opts.LockDigest},
					},
				},
				ResolvedDependencies: materials,
			},
			RunDetails: runDetails{
				Builder: builder{
					ID:      BuilderID,
					Version: map[string]string{"ngm": opts.ToolVersion},
				},
				Metadata: metadata{InvocationID: opts.LockDigest},
			},
		},
	}

	// v0.82：关掉 HTML 转义（同 `internal/sbom` 与 `cmd/ngm/main.go` 的口径）。
	//
	// 这里的理由比 SBOM 更硬：**这份字节是要被签名的**。
	// `json.MarshalIndent` 默认把 `&` `<` `>` 写成 `\u0026` 等，
	// 于是"被签名的字节"与字段的原始字符串不是同一串东西。
	// 任何人拿原始字符串去复算、或换一个不转义的编码器重编，
	// 都会得到不同的字节 ⇒ 签名验不过，而没有任何一方做错了事。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "encode the attestation", "", err)
	}
	return buf.Bytes(), nil // Encode 自带末尾单个 LF
}

// isHex64 判断 s 是不是 64 位十六进制（sha256 的裸形态）。
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// —— 下面是 in-toto Statement v1 / SLSA provenance v1 的字段映射。 ——
//
// 刻意用结构体而不是 map：字段顺序即序列化顺序，而"同一输入 ⇒ 同字节"
// 要求顺序是代码的一部分（map 的键序虽然由 encoding/json 排定，
// 但结构体让必需字段在类型层面就可见）。

type statement struct {
	Type          string     `json:"_type"`
	Subject       []subject  `json:"subject"`
	PredicateType string     `json:"predicateType"`
	Predicate     provenance `json:"predicate"`
}

type subject struct {
	Name        string            `json:"name"`
	Digest      sha256Digest      `json:"digest"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// sha256Digest / gitCommitDigest 是 in-toto 的 digest 映射（算法名 → 值）。
//
// 刻意写成**单字段结构体**而不是 `map[string]string`：键是标准规定的固定集合，
// 静态声明让"我们到底用了哪些键"成为可被静态检查的事实（v0.76：散文里点名的
// `gitCommit` 此前只是运行时 map 的键，机器的反向判据看不见它——
// 而"文档里点名的字段"本来就是这个 machine interface 的一部分）。
type sha256Digest struct {
	SHA256 string `json:"sha256"`
}

type gitCommitDigest struct {
	GitCommit string `json:"gitCommit"`
}

type provenance struct {
	BuildDefinition buildDefinition `json:"buildDefinition"`
	RunDetails      runDetails      `json:"runDetails"`
}

type buildDefinition struct {
	BuildType            string             `json:"buildType"`
	ExternalParameters   externalParameters `json:"externalParameters"`
	ResolvedDependencies []material         `json:"resolvedDependencies"`
}

type externalParameters struct {
	Manifest fileRef `json:"manifest"`
	Lock     fileRef `json:"lock"`
}

type fileRef struct {
	URI    string       `json:"uri"`
	Digest sha256Digest `json:"digest"`
}

type material struct {
	URI         string            `json:"uri"`
	Digest      gitCommitDigest   `json:"digest"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type runDetails struct {
	Builder  builder  `json:"builder"`
	Metadata metadata `json:"metadata"`
}

type builder struct {
	ID      string            `json:"id"`
	Version map[string]string `json:"version"`
}

type metadata struct {
	InvocationID string `json:"invocationId"`
}
