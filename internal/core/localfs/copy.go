package localfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CopyTree 把 src 目录复制到 dst，排除集与 BuildDirDigest 完全一致（关键不变量：
// vendor 里的字节集合必须正好是 dirDigest 覆盖的集合，否则 verify 的重放没有意义）。
//
// 符号链接按链接目标重建（不跟随），mode 保留（可执行位尽力而为）。
func CopyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %q is not a directory", src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if excluded(slash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0111 != 0 || executableExt(filepath.Ext(info.Name())) {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
}

// RelName 从 local:<name> 提取 name，并归一为安全的相对目录名。
// 拒绝空名与分隔符逃逸——vendor 路径在落地前最后一道检查。
func RelName(name string) (string, error) {
	const prefix = "local:"
	if !strings.HasPrefix(name, prefix) {
		return "", fmt.Errorf("%q is not a local dependency name", name)
	}
	n := strings.TrimPrefix(name, prefix)
	if n == "" || strings.ContainsAny(n, `/\`) || n == "." || n == ".." {
		return "", fmt.Errorf("invalid local dependency name %q", name)
	}
	return n, nil
}
