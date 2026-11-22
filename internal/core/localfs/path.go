// Package localfs 为 `refType: local-dir` 依赖提供两项能力：
//
//  1. 源路径安全校验（ValidateSourcePath）——本地来源不得逃逸项目根；
//  2. 目录内容锚点（BuildDirDigest）——把任意本地目录变成可离线重放的摘要。
//
// 边界（ADR-052 §3）：
//   - local-git 来源**不**用本包算 digest，它仍走 internal/core/git 的 commit 证明链；
//     本包只帮它校验 sourcePath 的形状。
//   - 本包不读 git、不访问网络、不写文件——只读目录并产出规范清单字节。
package localfs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// LocalManifestVersion 是本地目录清单规范版本（ADR-052 §2.3）。
//
// 与 digest.ManifestVersion / digest.ModuleManifestVersion 域分离：
// 同样的记录集合，不同 header 得到不同 digest，避免三类来源的证明被混用。
// 该字符串是格式契约的一部分——任何改动都会改变所有 local-dir digest，
// 必须升 lockfileVersion MAJOR（ADR-052 §3 边界 4）。
const LocalManifestVersion = "ngm-local-digest/v1"

// ValidateSourcePath 校验 ngm.json 里的 sourcePath 声明。
//
// 规则（spec §0.1 已拍板：拒绝逃逸，不默认 HEAD，不做跨仓）：
//   - 非空；
//   - 相对路径（拒绝绝对路径、盘符、UNC、URL scheme）；
//   - 不含 `..` 段（拒绝逃逸项目根）；
//   - 分隔符只接受 `/` 或 `\`（Windows 声明常见），统一在 ResolveSourcePath 里归一化。
//
// 注意：这里只做**语法**校验；"解析后仍在项目根内"由 ResolveSourcePath 保证。
func ValidateSourcePath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("sourcePath is empty")
	}
	if strings.ContainsAny(p, "\x00\r\n") {
		return fmt.Errorf("sourcePath %q contains a NUL or newline", p)
	}
	// scheme（file://、https:// 等）——本地来源不是 URL
	if i := strings.Index(p, "://"); i >= 0 {
		return fmt.Errorf("sourcePath %q must be a relative filesystem path, not a URL", p)
	}
	// Windows 盘符 / UNC
	if len(p) >= 2 && p[1] == ':' {
		return fmt.Errorf("sourcePath %q must not be an absolute drive path", p)
	}
	if strings.HasPrefix(p, "//") || strings.HasPrefix(p, `\\`) {
		return fmt.Errorf("sourcePath %q must not be a UNC path", p)
	}
	// POSIX 绝对路径必须显式拒绝：Windows 上 filepath.IsAbs("/x") 返回 false，
	// 仅靠它会在跨平台时漏判。
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return fmt.Errorf("sourcePath %q must be relative", p)
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return fmt.Errorf("sourcePath %q contains a `..` component: escaping the project root is not allowed", p)
		}
	}
	return nil
}

// ResolveSourcePath 把已通过 ValidateSourcePath 的相对声明解析为绝对路径。
//
// projectRoot 必须是绝对路径；结果用 filepath.Clean 归一化。额外的 "仍在根内"
// 检查是纵深防御：即便将来 ValidateSourcePath 漏了某种逃逸写法，这里也拦得住。
func ResolveSourcePath(projectRoot, sourcePath string) (string, error) {
	if !filepath.IsAbs(projectRoot) {
		return "", fmt.Errorf("projectRoot must be absolute: %q", projectRoot)
	}
	if err := ValidateSourcePath(sourcePath); err != nil {
		return "", err
	}
	rel := filepath.FromSlash(strings.ReplaceAll(sourcePath, "\\", "/"))
	abs := filepath.Clean(filepath.Join(projectRoot, rel))
	root := filepath.Clean(projectRoot)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("sourcePath %q resolves outside project root %q", sourcePath, projectRoot)
	}
	return abs, nil
}
