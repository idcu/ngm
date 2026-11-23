package pathutil

import (
	"path"
	"path/filepath"
	"strings"
)

// EscapesPackageTree 报告一条包内相对路径上的链接目标是否逃出包树。
//
// 这是 git 来源（vendor/links.go）与本地来源（localfs.CopyTree）共用的唯一判据：
// 两条落地路径必须对称，否则同一条 `x -> ../../etc` 在 git 来源被拒、在
// local-dir 来源却原样落进 vendor 且 verify 仍判 OK（2026-11-23 审计 P2）。
func EscapesPackageTree(entryPath, target string) bool {
	if target == "" {
		return false
	}
	// 目标字符串先归到 `/` 口径：Windows 的 `..\..\x` 在 POSIX 的
	// path.Clean 下不是逃逸，但 CreateSymbolicLinkW 会按分隔符解析。
	target = NormalizeSeparators(target)
	if LooksLikeAbsPath(target) {
		return true
	}
	dir := path.Dir(path.Clean(NormalizeSeparators(entryPath)))
	joined := path.Clean(path.Join(dir, target))
	return joined == ".." || strings.HasPrefix(joined, "../")
}

// LooksLikeAbsPath 跨平台判定"形态像绝对路径"：
// POSIX 绝对路径、Windows 盘符、UNC 与反斜杠根都算。
//
// 不能用 filepath.IsAbs 单独判断：清单里的路径多为 POSIX 风格，
// 一个 `/etc/passwd` 在 Windows 上不是 IsAbs，却同样是逃逸。
func LooksLikeAbsPath(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		c := p[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}
