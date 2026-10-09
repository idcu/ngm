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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
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
	// Digest 是 `sha256:<hex>` 形态的 archiveDigest。
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
		p += "#" + e.SubPath
	}
	return p
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

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "encode the SBOM", "", err)
	}
	return append(data, '\n'), nil
}

func (f Format) valid() bool {
	for _, v := range Formats() {
		if f == v {
			return true
		}
	}
	return false
}
