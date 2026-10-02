package vendor

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
)

// unpackPrefix 是 `Put` 解包时用的临时目录名前缀；`Prune` 只认这个前缀。
//
// 前缀**单独定义**，是因为它此前只出现在 `Put` 那一行字符串里。若 `prune` 再写一个
// 一模一样的字面量，两处就会各改各的——而这两处一旦不一致，后果不是"没清干净"，
// 而是"清掉了不该清的东西"（那正是 [ADR-018](../docs/adr/adr-018-store-reclaim.md)
// 判定"不做按可达性删除"时最担心的形态）。
const unpackPrefix = ".unpack-"

// ContentStoreEntry 是 `ngm store usage` 报告里的一行（一份内容树）。
type ContentStoreEntry struct {
	// Digest 是 `sha256:<hex>` 形态的标识（目录名是它的 hex 部分）。
	Digest string
	// Bytes 是该内容树占用的字节数（含 `tree/` 与 `meta.json`）。
	Bytes int64
	// Repo / Commit 来自 meta.json；MetaReadable 为 false 时两者为空。
	Repo         string
	Commit       string
	MetaReadable bool
}

// ContentStoreUsage 是 `ngm store usage` 的只读报告。
//
// **它不修改任何东西**（[ADR-018](../docs/adr/adr-018-store-reclaim.md) 决策 2）：
// 存在的意义是让"磁盘被什么占了"第一次可以被回答——在此之前，层 2 只有"越用越大"这个印象。
type ContentStoreUsage struct {
	// Root 是 content store 根目录（通常是 `~/.ngm/content`）。
	Root string
	// Exists 为 false 表示 store 还不存在（没有可报告的东西，这不是错误）。
	Exists bool
	// Trees 是内容树清单，按**体积降序、同体积按 digest 升序**排列——
	// 顺序必须是确定的：依赖目录遍历顺序的输出会让人误以为"内容变了"。
	Trees []ContentStoreEntry
	// TreeBytes 是所有内容树的字节合计。
	TreeBytes int64
	// TempCount / TempBytes 是解包残骸（`.unpack-*`）的数量与体积。
	// 它们不是内容树，是**中断留下的垃圾**，也是 `prune` 唯一会清的东西。
	TempCount int
	TempBytes int64
}

// Usage 只读地统计 content store 的占用。
func (s *ContentStore) Usage() (ContentStoreUsage, error) {
	algDir := filepath.Join(s.root, digest.Algorithm)
	out := ContentStoreUsage{Root: algDir}

	entries, err := os.ReadDir(algDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // 还没有 store：没有东西可报告，不是错误
		}
		return out, errs.Wrap(errs.CodeConfigInvalid, "read content store", "", err)
	}
	out.Exists = true

	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(algDir, name)
		bytes, berr := dirBytes(path)
		if berr != nil {
			return out, berr
		}
		if strings.HasPrefix(name, unpackPrefix) {
			out.TempCount++
			out.TempBytes += bytes
			continue
		}
		entry := ContentStoreEntry{Digest: digest.Algorithm + ":" + name, Bytes: bytes}
		if m, merr := s.ReadMeta(entry.Digest); merr == nil && m != nil {
			entry.MetaReadable = true
			entry.Repo = m.Repo
			entry.Commit = m.Commit
		}
		out.Trees = append(out.Trees, entry)
		out.TreeBytes += bytes
	}

	sort.Slice(out.Trees, func(i, j int) bool {
		if out.Trees[i].Bytes != out.Trees[j].Bytes {
			return out.Trees[i].Bytes > out.Trees[j].Bytes
		}
		return out.Trees[i].Digest < out.Trees[j].Digest
	})
	return out, nil
}

// PruneResult 描述一次 `ngm store prune` 的结果。
type PruneResult struct {
	Root string
	// Removed 是被删掉的目录名（只可能是 `.unpack-*`）。
	Removed []string
	// BytesFreed 是释放的字节数。
	BytesFreed int64
	// KeptTrees 是**没被动过的内容树数量**。
	//
	// 为什么要把"没动"也报出来：一个删除类命令的输出里只有"删了什么"，
	// 用户就得自己去猜"它是不是顺手删了别的东西"。把留下来的数量写出来，
	// 就是把安全声明放在它该在的地方。
	KeptTrees int
	// DryRun 为 true 表示本次没有真的删除。
	DryRun bool
}

// Prune 只清 `Put` 的解包残骸（`.unpack-*`）。
//
// **它绝不碰任何内容树**（`sha256/<digest>/`）。这不是"暂时没做"，是
// [ADR-018](../docs/adr/adr-018-store-reclaim.md) 决策 1/5 的边界：
// 按可达性删除需要一个 ngm 没有的项目注册表，而误删会让别的项目在某天突然验不过。
//
// 幂等：没有残骸时 0 删除、不报错。
func (s *ContentStore) Prune(dryRun bool) (PruneResult, error) {
	algDir := filepath.Join(s.root, digest.Algorithm)
	out := PruneResult{Root: algDir, DryRun: dryRun}

	entries, err := os.ReadDir(algDir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, errs.Wrap(errs.CodeConfigInvalid, "read content store", "", err)
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, unpackPrefix) {
			out.KeptTrees++
			continue
		}
		path := filepath.Join(algDir, name)
		bytes, berr := dirBytes(path)
		if berr != nil {
			return out, berr
		}
		out.BytesFreed += bytes
		out.Removed = append(out.Removed, name)
		if dryRun {
			continue
		}
		if rerr := os.RemoveAll(path); rerr != nil {
			return out, errs.Wrap(errs.CodeConfigInvalid, "remove unpack residue "+name, "", rerr)
		}
	}
	return out, nil
}

// dirBytes 递归求和目录里所有文件的字节数（目录自身不计）。
//
// 符号链接按其**链接本身**计（`DirEntry.Info` 是 Lstat 语义）：内容树里可以有 symlink，
// 而它的目标未必在 store 里——按目标算会把 store 之外的体积算进来。
func dirBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, errs.Wrap(errs.CodeConfigInvalid, "measure "+root, "", err)
	}
	return total, nil
}
