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
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
)

// Format 是导出格式。
type Format string

const (
	// FormatCycloneDX 是 CycloneDX 1.5 的 JSON 形态。
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

// Puri 返回该依赖的 purl（purl-spec 的 git 类型）。
//
// 形如 `pkg:git/github.com/x/dep@<commit>`；带子路径时在其后加 `#<subPath>`
// （purl 的 subpath 段）——与 lock 的口径一致：子路径是**同一个包的不同切片**。
func (e Entry) Purl() string {
	p := "pkg:git/" + e.Canonical + "@" + e.Commit
	if e.SubPath != "" {
		// v0.82：subpath 段按 purl 规则百分号编码。
		//
		// subPath 是**上游可控**的字符串（它来自依赖自己的 ngm.json）。
		// 一个含空格、`?`、`#`、`%` 的子路径会让 purl 语法失效——
		// 而 purl 是 SBOM 与 attestation 共同的**引用键**：它一旦不合法，
		// CycloneDX 的 bom-ref 与 SPDX 的引用就无法被工具解析。
		// 空格在真实仓库的子目录名里并不罕见。
		// canonical 与 commit 不需要：前者由 ParseSlug 归一、后者是 40 位 hex
		// （且 lock.Validate 保证），都不是自由文本。
		p += "#" + purlSubPath(e.SubPath)
	}
	return p
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
	return "https://" + e.Canonical
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
