// Package digest 实现 archiveDigest 的规范定义（ADR-008）。
//
// 核心约定：digest **不对归档文件（tar/zip）字节流计算**，而对由 commit tree
// 直接生成的「规范化内容清单」计算。因此 digest 完全可在本地重放，不依赖
// Git host 的 archive API，也不受 tar/gzip 实现差异影响。
//
//	archiveDigest = "sha256:" + sha256(清单字节流)
//
// 清单字节流的精确格式见 ManifestVersion 的文档注释。
package digest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

// ManifestVersion 是清单规范版本，随 lockfileVersion 绑定（1.x 固定使用 v1）。
//
// ⚠️ 该字符串是**格式契约的一部分**：它是清单字节流的头部，
// 一旦发布，任何改动都会改变所有 digest。变更必须递增版本号并走 lock 迁移。
const ManifestVersion = "ngm-archive-digest/v1"

// Algorithm 是 digest 算法前缀（v1 固定 sha256；见 ADR-008 §已知限制）。
const Algorithm = "sha256"

// 受支持的 Git 文件模式（ADR-008 §清单格式）。
//
// 目录（40000）不单独成条，因此不在此列；gitlink（160000）与 subtree
// 由 BuildArchive 在读取阶段显式报错，不会进入清单。
const (
	// ModeRegular 普通文件。
	ModeRegular = "100644"
	// ModeExecutable 可执行文件。
	ModeExecutable = "100755"
	// ModeSymlink 符号链接；其「内容」是链接目标字符串。
	ModeSymlink = "120000"
	// ModeGitlink 子模块引用（tree 中的 commit 条目）——v0.1 不支持。
	ModeGitlink = "160000"
	// ModeTree 目录条目。
	ModeTree = "40000"
)

// Record 是清单中的一条记录。
type Record struct {
	// Path 是仓库根相对路径，以 `/` 分隔，无前导 `./`。
	Path string
	// Mode 是 Git 模式串（100644 / 100755 / 120000）。
	Mode string
	// BlobSHA256 是内容**原始字节**的 sha256 小写十六进制。
	//
	// 对 symlink 而言，内容即链接目标字符串（Git 把目标存在 blob 里）。
	// 不做任何换行转换——ngm 证明的是"仓库里的字节"（ADR-008 §关键说明 3）。
	BlobSHA256 string
}

// BuildManifest 生成规范清单字节流。
//
// 格式（ADR-008 §清单格式）：
//
//	"ngm-archive-digest/v1" NUL
//	<path> NUL <mode> NUL <blob-sha256> NUL
//	<path> NUL <mode> NUL <blob-sha256> NUL
//	...
//
// 重要实现细节：
//   - 字段之间**仅以单个 NUL（0x00）分隔**，没有空格、换行或其他分隔字节。
//     ADR 文档中的 `\0` 两侧空格是排版可读性，不是字节流的一部分。
//   - 记录按 Path 的 **UTF-8 字节序**升序排序。Go 的字符串 `<` 正是逐字节比较，
//     因此不受本地化（collation）影响——这是跨平台一致性的关键。
//   - 输入切片不被修改（内部先复制再排序）。
//   - 输出以 NUL 结尾（最后一条记录的 blob-sha256 之后也有 NUL）。
func BuildManifest(records []Record) []byte {
	sorted := make([]Record, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		// 字节序；Path 唯一（Git tree 不允许同路径重复），无需二级排序键
		return sorted[i].Path < sorted[j].Path
	})

	var buf bytes.Buffer
	// 估算容量，避免大仓库反复扩容
	total := len(ManifestVersion) + 1
	for _, r := range sorted {
		total += len(r.Path) + len(r.Mode) + len(r.BlobSHA256) + 3
	}
	buf.Grow(total)

	buf.WriteString(ManifestVersion)
	buf.WriteByte(0)
	for _, r := range sorted {
		buf.WriteString(r.Path)
		buf.WriteByte(0)
		buf.WriteString(r.Mode)
		buf.WriteByte(0)
		buf.WriteString(r.BlobSHA256)
		buf.WriteByte(0)
	}
	return buf.Bytes()
}

// Digest 计算清单字节流的 archiveDigest，形如 `sha256:<64 hex>`。
//
// 对应 ADR-008：`archiveDigest = "sha256:" + sha256(清单字节流)`。
func Digest(manifest []byte) string {
	sum := sha256.Sum256(manifest)
	return Algorithm + ":" + hex.EncodeToString(sum[:])
}

// HashBytes 返回内容原始字节的 sha256 小写十六进制（即 Record.BlobSHA256 的取值）。
func HashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// RenderManifest 把清单字节流渲染为可读文本，用于诊断输出与 golden 审查。
//
// 格式（每行一条记录，TAB 分隔——path 可能含空格，故不能用空格分隔）：
//
//	ngm-archive-digest/v1
//	<path>\t<mode>\t<blob-sha256>
//	...
//
// 注意：**渲染结果不是 digest 的输入**。digest 永远在原始 NUL 分隔字节流上计算
// （见 BuildManifest）。本函数只服务于人眼与 diff。
//
// 对残缺清单（段数不是 3 的倍数）保持容错：尽力渲染已解析部分，便于排查损坏。
func RenderManifest(manifest []byte) string {
	parts := bytes.Split(manifest, []byte{0})
	// 末尾 NUL 会产生一个空段，去掉
	if n := len(parts); n > 0 && len(parts[n-1]) == 0 {
		parts = parts[:n-1]
	}
	if len(parts) == 0 {
		return ""
	}

	var sb bytes.Buffer
	sb.Write(parts[0])
	sb.WriteByte('\n')

	body := parts[1:]
	for i := 0; i+3 <= len(body); i += 3 {
		sb.Write(body[i])
		sb.WriteByte('\t')
		sb.Write(body[i+1])
		sb.WriteByte('\t')
		sb.Write(body[i+2])
		sb.WriteByte('\n')
	}
	// 残缺尾段（诊断用）
	if rem := len(body) % 3; rem != 0 {
		start := len(body) - rem
		sb.WriteString("(truncated record) ")
		for _, p := range body[start:] {
			sb.Write(p)
			sb.WriteByte('\t')
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
