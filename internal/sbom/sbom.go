// Package sbom 把 ngm.lock 导出为 SBOM（CycloneDX / SPDX）。
//
// 为什么是"几乎免费"的（评估报告第 3 层的判断）：lock 里已经有
// commit + archiveDigest + 来源链，而 SBOM 要的正是这三样。
//
// 确定性纪律（与 lock / mappings / 各报告同一套）：
//
//   - 组件顺序**必须确定**（按 name + subPath 排序，与 lock 同口径），
//     否则"同一份 lock 导两次字节不同"会让 diff / 快照全部失效；
//   - 时间戳是**唯一**的非确定字段，且如实写（`timestamp` / `created`）——
//     与 lock 的 `resolvedAt` 同一处理方式；
//   - 每个组件必须带 `archiveDigest` 的哈希与一个 purl：
//     **没有哈希的 SBOM 只是清单**，而有哈希的才是"可核对的清单"。
//
// 边界（诚实记）：本包只导出**直接 + 传递依赖的清单**，
// 不声称构建可复现、不声称来源可信——那是 SLSA provenance 的事（正交轴）。
package sbom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/resolve"
)

// Format 是导出格式。
type Format string

const (
	// FormatCycloneDX 是 CycloneDX 1.7 的 JSON 形态。
	FormatCycloneDX Format = "cyclonedx"
	// FormatSPDX 是 SPDX 2.3 的 JSON 形态。
	FormatSPDX Format = "spdx"
)

// Formats 列出全部合法值（顺序稳定，便于帮助文本与错误提示）。
func Formats() []Format { return []Format{FormatCycloneDX, FormatSPDX} }

// ParseFormat 解析配置/命令行里的格式名。
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatCycloneDX:
		return FormatCycloneDX, nil
	case FormatSPDX:
		return FormatSPDX, nil
	default:
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unknown SBOM format %q", s),
			"supported formats: "+formatList())
	}
}

func formatList() string {
	names := make([]string, 0, len(Formats()))
	for _, f := range Formats() {
		names = append(names, string(f))
	}
	return strings.Join(names, ", ")
}

// Project 是被描述的项目自身（写进 metadata / name）。
type Project struct {
	// Name 是项目名（ngm.json 的 name）。
	Name string
	// Version 是项目版本（可空）。
	Version string
}

// Entry 是一条依赖——从 ngm.lock 映射而来。
//
// 刻意不 import lock 包：导出层只吃"已归一化的最小字段"，
// 于是 lock 的结构变化不会牵连两种 SBOM 格式的生成逻辑。
type Entry struct {
	// Slug 是依赖标识（`github:x/dep`）。
	Slug string
	// Ref / RefType 是人类声明（tag / branch / commit）。
	Ref     string
	RefType string
	// Commit 是锁定的完整 commit（不可变锚点）。
	Commit string
	// Digest 是 `sha256:<hex>` 形态的内容 digest：根条目为 archiveDigest，
	// 模块条目为 moduleDigest（ADR-032）。
	Digest string
	// Canonical 是 `host/path` 形态（purl 与 vcs URL 的基）。
	Canonical string
	// SubPath 是 monorepo 子路径（可空）。
	SubPath string
	// SubtreeOID 是模块条目的 Git tree 绑定锚（`sha1:`/`sha256:`，可空）。
	SubtreeOID string
	// Version 是模块清单的 version（可空；根条目通常为空）。
	Version string
	// RequiredBy 是**直接引用者**的身份（v0.73，ADR-028）：`name` 或 `name#subPath`，
	// 项目直接声明时为 `lock.RootRequirer`。空 = 这份 lock 里没有图（导出端据此省略依赖段）。
	RequiredBy []string
}

// Key 是该条目的身份（`name` 或 `name#subPath`），与 lock / resolve 同一口径。
func (e Entry) Key() string {
	if e.SubPath != "" {
		return e.Slug + "#" + e.SubPath
	}
	return e.Slug
}

// IsLocal 报告该条目来自 ADR-052 的本地来源（local-git / local-dir）。
// local 没有可解析的仓库 URL，产物里不能伪造 `https://local/...`。
func (e Entry) IsLocal() bool {
	return strings.HasPrefix(strings.TrimSpace(e.Slug), resolve.LocalNamespace)
}

// ExportVersion 返回产物里的版本字段。
// Git 来源用 commit；local 来源的 commit 位放的是内容 digest，
// 写成 `sha256:...` 不是合法版本形态，改用裸 hex。
func (e Entry) ExportVersion() string {
	if e.IsLocal() {
		return e.HexDigest()
	}
	return e.Commit
}

// HashKind 说明哈希字段是哪一种证明（产物里必须写清楚，不能混称 archiveDigest）。
func (e Entry) HashKind() string {
	if e.RefType == string(resolve.RefTypeLocalDir) {
		return "dirDigest (ngm-local-digest/v1, ADR-052)"
	}
	return "archiveDigest (canonical file listing, ADR-008)"
}

// Purl 返回该依赖的 purl（purl-spec 的包类型，按 host 分流）。
//
// v0.102 起不再一律使用 `pkg:git`：主流 SBOM 工具认识的是
// `pkg:github/<owner>/<repo>`、`pkg:gitlab/...`、`pkg:gitee/...`；
// 自建 host 没有注册类型，使用 `pkg:generic` 并带 `vcs_url` 限定符。
// 形状：`pkg:<type>/<namespace>/<name>@<commit>[?vcs_url=...][#<subPath>]`。
//
// `bom-ref` **不跟着变**：见 BomRef。
func (e Entry) Purl() string {
	if e.IsLocal() {
		// 本地来源不是可解析的 registry/git 包：用 generic 类型显式标注
		// `local/<name>` 命名空间，版本用内容 digest，且不伪造 vcs_url。
		p := "pkg:generic/local/" + e.CanonicalPath() + "@" + e.HexDigest()
		if e.SubPath != "" {
			p += subPathFragment(e.SubPath)
		}
		return p
	}
	host, path := e.hostAndPath()
	var p string
	switch host {
	case "github.com":
		p = "pkg:github/" + path + "@" + e.Commit
	case "gitlab.com":
		p = "pkg:gitlab/" + path + "@" + e.Commit
	case "gitee.com":
		p = "pkg:gitee/" + path + "@" + e.Commit
	default:
		// generic 的 namespace/name 仍保留 host/path，vcs_url 是权威来源。
		p = "pkg:generic/" + host + "/" + path + "@" + e.Commit +
			"?vcs_url=" + url.QueryEscape("git+"+e.VCSURL())
	}
	if e.SubPath != "" {
		p += subPathFragment(e.SubPath)
	}
	return p
}

// BomRef 返回该依赖在 SBOM 文档内的稳定引用键。
//
// 为什么与 purl 分开：v0.102 把 purl 改成标准类型后，`bom-ref` 若也换掉，
// 所有已归档的 SBOM 会失去可比的组件 ID。这里保留 v0.82 起的旧形态
// `pkg:git/<canonical>@<commit>[#sub]`——它是文档内的标识，不负责对外解析。
func (e Entry) BomRef() string {
	version := e.Commit
	if e.IsLocal() {
		version = e.HexDigest()
	}
	p := "pkg:git/" + e.Canonical + "@" + version
	if e.SubPath != "" {
		p += subPathFragment(e.SubPath)
	}
	return p
}

// CanonicalPath 返回 canonical 的 path 段；local 伪 Canonical 即 `local:<name>` 的后缀。
func (e Entry) CanonicalPath() string {
	_, path := e.hostAndPath()
	return path
}

func (e Entry) hostAndPath() (host, path string) {
	canonical := strings.TrimSpace(e.Canonical)
	if i := strings.IndexByte(canonical, '/'); i >= 0 {
		return canonical[:i], canonical[i+1:]
	}
	return canonical, ""
}

// subPathFragment 渲染 purl 的 subpath 段（含前导 `#`）。
func subPathFragment(sub string) string {
	return "#" + purlSubPath(sub)
}

// purlSubPath 编码 purl 的 subpath 段：段内百分号编码，`/` 作为分隔符保留。
func purlSubPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = percentEncode(s)
	}
	return strings.Join(segs, "/")
}

// percentEncode 保留 RFC 3986 的未保留字符，其余逐字节百分号编码
// （UTF-8 多字节字符因此按字节编码——这正是 URL 编码的定义）。
func percentEncode(s string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789-._~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return b.String()
}

// VCSURL 返回该依赖的仓库地址（由 canonical 推导，永远 https——
// 它只是"去哪儿能找到"，不参与任何判定）。
func (e Entry) VCSURL() string {
	if e.IsLocal() {
		// 没有可访问的仓库地址；产物侧据此省略 vcs 引用，而不是造一个假 URL。
		return ""
	}
	return "https://" + e.Canonical
}

// downloadLocation 返回 SPDX 的下载位置；本地来源没有可下载位置，按 SPDX 惯例写 NOASSERTION。
func downloadLocation(e Entry) string {
	if vcs := e.VCSURL(); vcs != "" {
		return vcs
	}
	return "NOASSERTION"
}

// HexDigest 返回 digest 的裸 hex（SBOM 的 hash 字段不带 `sha256:` 前缀）。
func (e Entry) HexDigest() string {
	return strings.TrimPrefix(e.Digest, digest.Algorithm+":")
}

// Options 控制一次导出。
type Options struct {
	Format Format
	// Now 是时间戳（测试注入；零值时调用方填 time.Now）。
	Now time.Time
	// ToolVersion 是 ngm 自身的版本（写进工具段）。
	ToolVersion string
	// GraphComplete 报告 lock 是否携带**完整**的依赖图形（v0.81）。
	//
	// 为 false 时两种格式都**整段省略**依赖边，而不是导出一份半张图。
	// 半张图的危害在于它看起来是完整的：一份 lock 里只要有一条 `requiredBy`
	// 缺失，`edgeIndex` 仍会为其余条目导出边，`dependencies` / `DEPENDS_ON`
	// 照常出现且不带任何"不完整"的标记，合规读者会把它读成完整的依赖树。
	// 这与 `why` 的口径一致（它显式报告 `Source`），也是本项目一贯的纪律：
	// **不知道就不能说成没有，更不能说成完整**。
	GraphComplete bool
}

// Build 把文档导出为指定格式的 JSON 字节流。
//
// 输出纪律：2 空格缩进、LF、末尾单个换行——与仓库其余产物一致。
func Build(project Project, entries []Entry, opts Options) ([]byte, error) {
	if !opts.Format.valid() {
		return nil, errs.New(errs.CodeConfigInvalid,
			"unsupported SBOM format "+string(opts.Format),
			"supported formats: "+formatList())
	}
	// 排序必须在这里做，而不是信任调用方：两种格式的**所有** ID/引用
	// 都派生自顺序，顺序不确定 ⇒ 整份文档不确定（实现在 Sort，v0.70 起两处共用）。
	sorted := Sort(entries)

	var doc any
	switch opts.Format {
	case FormatCycloneDX:
		doc = buildCycloneDX(project, sorted, opts)
	case FormatSPDX:
		doc = buildSPDX(project, sorted, opts)
	}

	// v0.82：用 Encoder 并关掉 HTML 转义，与仓库其余 JSON 出口同口径
	// （`cmd/ngm/main.go` 的错误信封：路径里的 `<` 不该变成 `\u003c`）。
	//
	// `json.MarshalIndent` 默认 `SetEscapeHTML(true)`，于是 `&` `<` `>`
	// 会被写成 `\u0026` 等。确定性不受影响（所以这不是"错误"），
	// 但 SBOM 是要给**别的工具**读的，而它的字段里有上游可控的字符串
	// （ref、slug、子路径）。写出去的字节与字符串内容不一致，
	// 会让下游按原文比对的实现对不上。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "encode the SBOM", "", err)
	}
	return buf.Bytes(), nil // Encode 自带末尾单个 LF，与其余出口一致
}

func (f Format) valid() bool {
	for _, v := range Formats() {
		if f == v {
			return true
		}
	}
	return false
}
