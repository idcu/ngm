package localfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/pathutil"
)

// 默认排除集（ADR-052 §2.3 / spec §10 已拍板）：
//   - `.git/`：git 元数据不是源内容；local-dir 来源的意义就是"不依赖 git"。
//   - `node_modules/`：第三方安装产物，不属于本模块内容身份。
//   - `ngm.vendor/`：ngm 自己落地的依赖，属于下游产物。
//   - `.tmp*` 前缀：测试/工具的临时目录。
//   - `ngm.json` 自身：来源清单由消费方 lock 记录，不纳入自身摘要（避免自指）。
//
// `test/` 与 `docs/` **包含**——它们是内容身份的一部分（spec §10 决策 3）。
var defaultExcludes = []string{".git", "node_modules", "ngm.vendor"}

func excluded(rel string) bool {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if p == "ngm.json" && i == len(parts)-1 {
			return true
		}
		for _, x := range defaultExcludes {
			if p == x {
				return true
			}
		}
		if strings.HasPrefix(p, ".tmp") {
			return true
		}
	}
	return false
}

// modeOf 把本地文件系统条目映射成 digest 规范里的 Git mode 串。
//
// 规则与 git 一致（ADR-052 §2.3）：
//   - 符号链接 → 120000，内容 = 链接目标字符串
//   - 可执行文件（owner/group/other 任一执行位，或 Windows 上的脚本/可执行扩展名）→ 100755
//   - 其他普通文件 → 100644
//
// Windows 注意：NTFS 不保留 Unix 执行位，os.WriteFile(..., 0755) 在 Windows 上
// 仍得到 100644。为让同一份目录在 Unix 与 Windows 得到相同 digest，
// 用"可执行扩展名"作为跨平台一致的第二判据（.sh/.bash/.cmd/.bat/.ps1/.exe）。
// 这与 git 在 core.fileMode=false 下的行为不同，但 ngm 证明的是"磁盘字节 + 可执行意图"，
// 扩展名是唯一跨平台稳定的意图信号。
func modeOf(info fs.FileInfo) string {
	m := info.Mode()
	if m&os.ModeSymlink != 0 {
		return digest.ModeSymlink
	}
	if m&0111 != 0 || executableExt(filepath.Ext(info.Name())) {
		return digest.ModeExecutable
	}
	return digest.ModeRegular
}

func executableExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".sh", ".bash", ".cmd", ".bat", ".ps1", ".exe":
		return true
	}
	return false
}

// CollectRecords 遍历 root，产出规范化清单记录（未排序，BuildDirDigest 内部排序）。
//
// 只产出 digest 规范允许的三类 mode；其他类型（设备、socket、gitlink 等）一律报错——
// 宁可拒绝一个奇怪的目录，也不产出一个"装得进去验不过"的清单。
func CollectRecords(root string) ([]digest.Record, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("source path %q is not a directory", root)
	}

	var records []digest.Record
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		mode := modeOf(info)

		var content []byte
		if mode == digest.ModeSymlink {
			target, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", rel, err)
			}
			// digest 边界也拒绝逃逸链接：不能靠"install 拦住"作为唯一防线，
			// 否则旧 lock / 手工改过的源目录仍能把逃逸链接带进 verify。
			if pathutil.EscapesPackageTree(rel, target) {
				return fmt.Errorf("symlink %q escapes the local source tree (target %q)", rel, target)
			}
			content = []byte(target)
		} else {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported file type at %q (mode %s)", rel, info.Mode())
			}
			content, err = os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", rel, err)
			}
		}
		sum := sha256Hex(content)
		r := digest.Record{Path: rel, Mode: mode, BlobSHA256: sum}
		if err := digest.ValidateRecord(r); err != nil {
			return fmt.Errorf("invalid record for %s: %w", rel, err)
		}
		records = append(records, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := digest.ValidateUniquePaths(records); err != nil {
		return nil, err
	}
	return records, nil
}

// BuildDirDigest 为本地目录生成内容锚点：
//
//	dirDigest = digest.Digest(digest.BuildManifestWithVersion(LocalManifestVersion, records))
//
// 形如 `sha256:<64hex>`，与 archiveDigest/moduleDigest 前缀一致，但 header 域分离。
func BuildDirDigest(root string) (string, error) {
	records, err := CollectRecords(root)
	if err != nil {
		return "", err
	}
	return digest.Digest(digest.BuildManifestWithVersion(LocalManifestVersion, records)), nil
}
