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
	// Trees 是**v1 遗留**的内容树清单，按**体积降序、同体积按 digest 升序**排列——
	// 顺序必须是确定的：依赖目录遍历顺序的输出会让人误以为"内容变了"。
	// （v2 布局下没有"树"可列，见下面的 V2Trees / Blobs。）
	Trees []ContentStoreEntry
	// TreeBytes 是 v1 遗留内容树的字节合计。
	TreeBytes int64
	// V2Trees / V2TreeBytes 是 v2 布局下的**清单**数与体积（ADR-019）。它很小——
	// 真正的内容在 blob 池里。
	V2Trees     int
	V2TreeBytes int64
	// V2Entries 是 v2 的逐清单明细（digest + **逻辑体积** + 来自哪个 repo@commit）。
	//
	// 逻辑体积 = 该树各条目字节之和；多棵树共享 blob 时它会重复计算，
	// 因此**真实占用看 BlobBytes**——两个数都报，免得把逻辑体积读成磁盘占用。
	V2Entries []ContentStoreEntry
	// Blobs / BlobBytes 是 blob 池的文件数与体积，即**去重后的真实内容占用**。
	Blobs     int
	BlobBytes int64
	// TempCount / TempBytes 是解包残骸（`.unpack-*`）的数量与体积。
	// 它们不是内容树，是**中断留下的垃圾**，也是 `prune` 唯一会清的东西。
	TempCount int
	TempBytes int64
}

// Usage 只读地统计 content store 的占用。
func (s *ContentStore) Usage() (ContentStoreUsage, error) {
	algDir := filepath.Join(s.root, digest.Algorithm)
	out := ContentStoreUsage{Root: s.root}

	entries, err := os.ReadDir(algDir)
	if err != nil && !os.IsNotExist(err) {
		return out, errs.Wrap(errs.CodeConfigInvalid, "read content store", "", err)
	}
	// v2：清单与 blob 池
	if tdir, terr := os.ReadDir(s.treesRoot()); terr == nil {
		out.Exists = true
		for _, e := range tdir {
			// `trees/` 下也可能有 `.unpack-*` 残骸（Put 的临时清单目录就建在它下面）——
			// 它属于 TempCount，不是一棵树；漏掉它会让 `usage` 少报一块占用。
			if strings.HasPrefix(e.Name(), unpackPrefix) {
				bytes, berr := dirBytes(filepath.Join(s.treesRoot(), e.Name()))
				if berr != nil {
					return out, berr
				}
				out.TempCount++
				out.TempBytes += bytes
				continue
			}
			bytes, berr := dirBytes(filepath.Join(s.treesRoot(), e.Name()))
			if berr != nil {
				return out, berr
			}
			out.V2Trees++
			out.V2TreeBytes += bytes

			entry := ContentStoreEntry{Digest: digest.Algorithm + ":" + e.Name()}
			if m, merr := s.readManifest(entry.Digest); merr == nil && m != nil {
				var logical int64
				for _, me := range m.Entries {
					logical += me.Size
				}
				entry.Bytes = logical
				if meta, err := s.readMetaV2(entry.Digest); err == nil && meta != nil {
					entry.MetaReadable = true
					entry.Repo = meta.Repo
					entry.Commit = meta.Commit
				}
			}
			out.V2Entries = append(out.V2Entries, entry)
		}
		sort.Slice(out.V2Entries, func(i, j int) bool {
			if out.V2Entries[i].Bytes != out.V2Entries[j].Bytes {
				return out.V2Entries[i].Bytes > out.V2Entries[j].Bytes
			}
			return out.V2Entries[i].Digest < out.V2Entries[j].Digest
		})
	}
	if bdir, berr := os.ReadDir(s.blobsRoot()); berr == nil {
		out.Exists = true
		for _, shard := range bdir {
			if !shard.IsDir() {
				continue
			}
			files, ferr := os.ReadDir(filepath.Join(s.blobsRoot(), shard.Name()))
			if ferr != nil {
				return out, errs.Wrap(errs.CodeConfigInvalid, "read blob shard", "", ferr)
			}
			for _, f := range files {
				info, ierr := f.Info()
				if ierr != nil {
					return out, ierr
				}
				out.Blobs++
				out.BlobBytes += info.Size()
			}
		}
	}

	if entries != nil {
		out.Exists = true
	}

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
		// 这里循环的就是 `sha256/` 下的目录，因此**只读 v1 的 meta**——
		// 用双布局的 `ReadMeta` 会让"v1 条目缺 meta"被 v2 的 meta 悄悄补上。
		if m, merr := s.readMetaV1(entry.Digest); merr == nil && m != nil {
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
	out := PruneResult{Root: s.root, DryRun: dryRun}

	// 残骸可能出现在**两个**目录：`Put` 的解包临时目录建在 `sha256/` 下
	// （v2 也是——它解包时还没有布局可言），而 `trees/` 下也可能留下**发布失败**的
	// 半个目录。而"什么是残骸"仍然只有一处定义（`unpackPrefix`）——
	// 布局变了，规则不能跟着分叉，否则后果是删错东西。
	dirs := []string{filepath.Join(s.root, digest.Algorithm), s.treesRoot()}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return out, errs.Wrap(errs.CodeConfigInvalid, "read content store", "", err)
		}
		for _, e := range entries {
			name := e.Name()
			path := filepath.Join(dir, name)
			if !strings.HasPrefix(name, unpackPrefix) {
				out.KeptTrees++
				continue
			}
			bytes, berr := dirBytes(path)
			if berr != nil {
				return out, berr
			}
			out.BytesFreed += bytes
			out.Removed = append(out.Removed, filepath.Join(filepath.Base(dir), name))
			if dryRun {
				continue
			}
			if rerr := os.RemoveAll(path); rerr != nil {
				return out, errs.Wrap(errs.CodeConfigInvalid, "remove unpack residue "+name, "", rerr)
			}
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
