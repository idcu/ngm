package digest

import (
	"fmt"
	"strings"
)

// ValidateRecord 报告一条记录能否被**安全地**编码进清单（v0.107）。
//
// 存在理由：清单的字节流是 `path NUL mode NUL sha256 NUL`——**路径本身就是分隔符的
// 载体**。此前 `BuildManifest` 直接拼接，不做任何字段校验，于是只要有一处把
// "别人给的字符串"当路径传进来，就能伪造记录边界：
//
//	Path = "a\x00100644\x00<64hex>\x00b"   ⇒ 一条记录变成两条
//
// 生产路径目前只从 `git ls-tree -z` 取路径（Git 的路径不含 NUL），所以这是个
// **尚未被利用**的洞；但读回来的路径不只有这一个来源——content store 的清单 JSON
// 是可改写的，而 `verify --deep` 会把它的 Path/Mode 直接拿去重算摘要。
// 那意味着 `--deep`（本产品最强的那个承诺）可以被一份改过的清单骗过。
//
// 校验规则全部是"语法上不该出现"的东西，不做任何规范化：
//
//   - 路径非空，且不含 NUL / CR / LF（记录与行的分隔符）
//   - 路径不用 `\`（Git 的路径口径是 POSIX；反斜杠会让两台机器得到不同清单）
//   - 路径不是绝对路径，不以 `./` 开头，不含空段 / `.` / `..` 段
//   - mode 只能是 100644 / 100755 / 120000（gitlink 与非法 mode 一律拒绝）
//   - BlobSHA256 必须是 64 位小写十六进制
func ValidateRecord(r Record) error {
	if r.Path == "" {
		return fmt.Errorf("the record has an empty path")
	}
	if strings.ContainsAny(r.Path, "\x00\r\n") {
		return fmt.Errorf("path %q contains a NUL or newline: "+
			"the manifest separates fields with NUL, so such a path can forge record boundaries",
			r.Path)
	}
	if strings.Contains(r.Path, "\\") {
		return fmt.Errorf("path %q contains a backslash: manifest paths are POSIX", r.Path)
	}
	if strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("path %q is absolute", r.Path)
	}
	if strings.HasPrefix(r.Path, "./") {
		return fmt.Errorf("path %q starts with `./`: manifest paths are repo-relative", r.Path)
	}
	for _, part := range strings.Split(r.Path, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("path %q has an empty, `.` or `..` component", r.Path)
		}
	}
	switch r.Mode {
	case ModeRegular, ModeExecutable, ModeSymlink:
	default:
		return fmt.Errorf("mode %q is not one of %s / %s / %s",
			r.Mode, ModeRegular, ModeExecutable, ModeSymlink)
	}
	if !isLowerHex(r.BlobSHA256) || len(r.BlobSHA256) != 64 {
		return fmt.Errorf("blob sha256 %q is not 64 lowercase hex characters", r.BlobSHA256)
	}
	return nil
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
