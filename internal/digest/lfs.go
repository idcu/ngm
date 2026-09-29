package digest

import (
	"bytes"
	"regexp"
)

// LFS 指针（Git Large File Storage）在仓库里是一个普通文本文件，形如：
//
//	version https://git-lfs.github.com/spec/v1
//	oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393
//	size 12345
//
// 实体内容存放在 LFS 服务器，不在 Git 对象库里。若对指针文件求哈希，
// digest 证明的是"指针"而不是依赖的真实内容——**那是错误的安全性证据**。
//
// ADR-008 §已知限制明确要求："实现检测到 LFS 指针必须报错，不得静默哈希"。

// lfsVersionLine 是 LFS 指针的固定首行。
const lfsVersionLine = "version https://git-lfs.github.com/spec/v1"

// lfsOIDRe 匹配 `oid sha256:<64 hex>`。
var lfsOIDRe = regexp.MustCompile(`(?m)^oid sha256:([0-9a-f]{64})[ \t]*$`)

// lfsSizeRe 匹配 `size <非负整数>`。
var lfsSizeRe = regexp.MustCompile(`(?m)^size [0-9]+[ \t]*$`)

// LFSPointer 描述一个被识别出的 LFS 指针。
type LFSPointer struct {
	// OID 是实体内容的 sha256（LFS 服务器上的标识）。
	OID string
}

// DetectLFSPointer 判断内容是否为 LFS 指针。
//
// 判定策略（保守但精确，避免把普通文本误判）：
//  1. 首行必须是 `version https://git-lfs.github.com/spec/v1`
//  2. 且必须能找到 `oid sha256:<64hex>` 行
//  3. 且必须能找到 `size <n>` 行
//
// 三个条件同时满足才算指针。缺任一条件时按普通内容处理——
// 宁可漏报（由用户用别的手段发现）也不误报（把正常文件判为不支持）。
//
// 关于 CRLF：LFS 规范要求 LF，但现实中存在 CRLF 指针。检测时把 CRLF 归一为 LF
// **仅用于识别**；这不影响哈希——Record.BlobSHA256 始终基于原始字节
// （ADR-008 §关键说明 3：不做换行转换）。
//
// 关于大小上限：真实指针只有几百字节，这里限制 1 KiB 以避免对超大文件做正则扫描。
func DetectLFSPointer(content []byte) (LFSPointer, bool) {
	const maxPointerSize = 1024
	if len(content) > maxPointerSize {
		return LFSPointer{}, false
	}

	// 归一 CRLF（仅用于识别）
	normalized := content
	if bytes.IndexByte(content, '\r') >= 0 {
		normalized = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	}

	firstLine := normalized
	if i := bytes.IndexByte(normalized, '\n'); i >= 0 {
		firstLine = normalized[:i]
	}
	if !bytes.Equal(firstLine, []byte(lfsVersionLine)) {
		return LFSPointer{}, false
	}

	oidMatch := lfsOIDRe.FindSubmatch(normalized)
	if oidMatch == nil {
		return LFSPointer{}, false
	}
	if !lfsSizeRe.Match(normalized) {
		return LFSPointer{}, false
	}
	return LFSPointer{OID: string(oidMatch[1])}, true
}
