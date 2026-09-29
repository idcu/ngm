package vendor

import (
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/errs"
)

// Cache 是 vendor 层 4：可丢弃的加速层。
//
// 契约（architecture/vendor-layers.md §层 4）：
//
//   - cache **可以在任何时刻整层删除**——可证明性由 mirror + content + lock 保证，
//     与 cache 无关。因此本层不存放任何"唯一事实"。
//   - 布局：`<root>/metadata`（Git ref 信息、tag 列表）、`<root>/osv`（漏洞查询结果）、
//     `<root>/tmp`（临时目录）
//
// 注意与层 1/2 的区别：mirror 与 content store 是不可丢弃的（它们承担可证明性）；
// cache 是纯加速，删掉只会变慢。
type Cache struct {
	root string
}

// NewCache 创建 cache 管理器。root 通常是 `~/.ngm/cache`。
func NewCache(root string) *Cache { return &Cache{root: root} }

// Root 返回缓存根目录。
func (c *Cache) Root() string { return c.root }

// MetadataRoot 返回 Git ref / tag 列表缓存目录。
func (c *Cache) MetadataRoot() string { return filepath.Join(c.root, "metadata") }

// OSVRoot 返回 OSV.dev 查询结果缓存目录（v0.2 使用）。
func (c *Cache) OSVRoot() string { return filepath.Join(c.root, "osv") }

// TmpRoot 返回临时目录（解包、下载中转等）。
func (c *Cache) TmpRoot() string { return filepath.Join(c.root, "tmp") }

// EnsureDirs 创建缓存目录（幂等）。
func (c *Cache) EnsureDirs() error {
	for _, d := range []string{c.root, c.MetadataRoot(), c.OSVRoot(), c.TmpRoot()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return errs.Wrap(errs.CodeConfigInvalid, "create cache dir "+d,
				"check directory permissions", err)
		}
	}
	return nil
}

// CleanResult 描述一次清理的结果（可观测性：用户应能确认删掉了什么）。
type CleanResult struct {
	// Root 是被清理的缓存根目录。
	Root string
	// RemovedEntries 是被删除的顶层条目名（排序稳定）。
	RemovedEntries []string
	// Existed 为 false 表示缓存目录本来就不存在（无需清理）。
	Existed bool
}

// Clean 清空缓存层（保留根目录本身）。
//
// 语义：删除 `<root>` 下的所有内容但保留 `<root>`；
// 若 `<root>` 不存在则视为成功（幂等）。
//
// 安全性：本层可整层丢弃（见类型文档），因此不做任何备份。
func (c *Cache) Clean() (CleanResult, error) {
	res := CleanResult{Root: c.root}

	entries, err := os.ReadDir(c.root)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil // 本来就没有：幂等成功
		}
		return res, errs.Wrap(errs.CodeConfigInvalid, "read cache dir "+c.root, "", err)
	}
	res.Existed = true

	for _, e := range entries {
		res.RemovedEntries = append(res.RemovedEntries, e.Name())
		if err := os.RemoveAll(filepath.Join(c.root, e.Name())); err != nil {
			return res, errs.Wrap(errs.CodeConfigInvalid,
				"remove cache entry "+e.Name(), "close any process using it", err)
		}
	}

	// 重建三个标准子目录，让后续命令无需各自 EnsureDirs
	if err := c.EnsureDirs(); err != nil {
		return res, err
	}
	return res, nil
}
