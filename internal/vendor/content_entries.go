package vendor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// ContentEntry 是内容树里的**一个条目**，与存储布局无关。
//
// 为什么需要这一层（v0.8 A 组）：层 2 换布局后（[ADR-019](../docs/adr/adr-019-content-addressed-blobs.md)）
// 不再有一棵已物化的树，而消费方（verify / LinkTree）真正需要的只是
// "有哪些条目、各是什么内容"。把消费方改成吃条目，换布局就只是改 `Entries` 的实现。
//
// **本阶段数据仍来自 v1 的树目录**——行为与原来完全一致；它的价值是让下一阶段
// （写 v2）不必再动安全关键的 verify 路径。
type ContentEntry struct {
	// Path 是仓库内的相对路径，统一用 `/` 分隔（与清单口径一致，跨平台可比）。
	Path string
	// Size 是普通文件的字节数；symlink 时是链接目标字符串的长度（不参与比对）。
	Size int64
	// Symlink 为 true 表示这是一个符号链接，其"内容"是 LinkTarget。
	Symlink bool
	// LinkTarget 是 symlink 的目标字符串（非 symlink 时为空）。
	LinkTarget string
	// Full 是该条目**字节当前的存放位置**（v1：树目录里的文件；v2：blob 文件）。
	// symlink 时为空——它的内容就是 LinkTarget。
	//
	// 这个字段是给深校验用的读取位置；它随布局变化，因此**只**用于读字节，
	// 不参与任何判定（判定只用 Path / Size / LinkTarget / 内容哈希）。
	Full string
}

// Entries 返回某个 digest 的条目集合，按 Path 升序（结果确定，便于比对与快照）。
//
// 目录不存在时返回空集合而不是错误：与 `integrity.scanTree` 的口径一致
// （"不存在的树视为空树"），由调用方的路径集合比对去报告差异。
func (s *ContentStore) Entries(digestID string) ([]ContentEntry, error) {
	root := s.TreePath(digestID)

	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "stat "+root, "", err)
	}
	if !info.IsDir() {
		return nil, errs.New(errs.CodeConfigInvalid, root+" is not a directory", "")
	}

	var out []ContentEntry
	err = filepath.Walk(root, func(path string, fi os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if fi.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		e := ContentEntry{Path: filepath.ToSlash(rel), Size: fi.Size(), Full: path}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			e.Symlink = true
			e.LinkTarget = target
			e.Full = ""
		}
		out = append(out, e)
		return nil
	})
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "walk "+root, "", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// EntriesUnder 把条目按 monorepo 子路径收窄，并**去掉该前缀**。
//
// 与 `contentTreeFor`（旧口径：把子目录拼到根路径上）等价，只是作用在条目上：
// vendor 里只落子目录，因此比对用的条目也必须是"相对子目录"的。
func EntriesUnder(entries []ContentEntry, subPath string) []ContentEntry {
	sub := strings.Trim(filepath.ToSlash(subPath), "/")
	if sub == "" {
		return entries
	}
	prefix := sub + "/"
	out := make([]ContentEntry, 0, len(entries))
	for _, e := range entries {
		if len(e.Path) <= len(prefix) || e.Path[:len(prefix)] != prefix {
			continue
		}
		e.Path = e.Path[len(prefix):]
		out = append(out, e)
	}
	return out
}
