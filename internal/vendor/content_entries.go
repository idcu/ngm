package vendor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
)

// ContentEntry 是内容树里的**一个条目**，与存储布局无关。
//
// 为什么需要这一层（v0.8 A 组）：层 2 换布局后（[ADR-019](../docs/adr/adr-019-content-addressed-blobs.md)）
// 不再有一棵已物化的树，而消费方（verify / LinkTree）真正需要的只是
// "有哪些条目、各是什么内容"。把消费方改成吃条目，换布局就只是改 `Entries` 的实现。
//
// 来源随布局变化（v2 读 `manifest.json`、v1 扫树目录），但**形状不变**——
// 因此换布局不必再动安全关键的 verify 路径。
type ContentEntry struct {
	// Path 是仓库内的相对路径，统一用 `/` 分隔（与清单口径一致，跨平台可比）。
	Path string
	// Size 是普通文件的字节数；symlink 时是链接目标字符串的长度（不参与比对）。
	Size int64
	// Symlink 为 true 表示这是一个符号链接，其"内容"是 LinkTarget。
	Symlink bool
	// LinkTarget 是 symlink 的目标字符串（非 symlink 时为空）。
	LinkTarget string
	// Mode 是 Git 的 mode（100644 / 100755 / 120000）。
	//
	// 落地层需要它来还原可执行位：v2 的 blob 是按 mode 落盘的（见 `blobKey`），
	// 而 v1 只能从文件权限反推。判定层（verify）**不用**它——那与 ADR-008 的
	// 口径一致（mode 不能以文件系统为准）。
	Mode string
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
	// v2：清单直接给出条目（blob 池形态下没有"树目录"可扫）。
	//
	// 判据是**清单存在性**，不是"读清单是否成功"：否则清单损坏会被静默当成
	// "这是 v1"，从而回落到一棵可能已不存在（收敛时被删）的树目录，报出误导性的差异。
	if s.hasManifest(digestID) {
		m, err := s.readManifest(digestID)
		if err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid,
				"read the content manifest for "+digestID,
				"delete the entry and re-run `ngm install`", err)
		}
		return s.entriesFromManifest(m), nil
	}

	// v1：扫描已物化的树目录（与升级前的行为一致）。
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
		e := ContentEntry{Path: filepath.ToSlash(rel), Size: fi.Size(), Full: path, Mode: modeForPerm(fi)}
		if fi.Mode()&os.ModeSymlink != 0 {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			e.Symlink = true
			e.LinkTarget = target
			e.Full = ""
			e.Mode = digest.ModeSymlink
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

// ReadFile 按树内相对路径读取内容，**与布局无关**（v2 读 blob、v1 读树目录）。
//
// rel 用 `/` 分隔、相对于该 digest 的内容树根；monorepo 由调用方拼上子路径。
// 第二个返回值表示"该路径存在且可读"；不存在时返回 (nil, false, nil)——
// 与 `mappings.FileReader` 的口径一致（缺文件不是错误，由调用方决定怎么办）。
//
// 为什么要有这个方法（而不是让调用方自己拼 `Full`）：`Full` 是布局细节，
// 调用方一旦自己拼路径，换布局就得再改一遍——v0.8 A 阶段把消费方与布局解耦时，
// `env.ContentReader` 正是这么一处（它原先拼的是 v1 的 `TreePath`）。
func (s *ContentStore) ReadFile(digestID, rel string) ([]byte, bool, error) {
	entries, err := s.Entries(digestID)
	if err != nil {
		return nil, false, err
	}
	want := strings.Trim(filepath.ToSlash(rel), "/")
	for _, e := range entries {
		if e.Path != want {
			continue
		}
		if e.Symlink {
			// symlink 的"内容"就是目标字符串（与 digest 层口径一致）
			return []byte(e.LinkTarget), true, nil
		}
		if e.Full == "" {
			return nil, false, errs.New(errs.CodeConfigInvalid,
				"content entry "+want+" has no byte location in the store",
				"re-run `ngm install` to rebuild the content store")
		}
		data, rerr := os.ReadFile(e.Full)
		if rerr != nil {
			return nil, false, errs.Wrap(errs.CodeConfigInvalid,
				"read "+want+" from the content store",
				"re-run `ngm install` to rebuild the content store", rerr)
		}
		return data, true, nil
	}
	return nil, false, nil
}

// modeForPerm 从文件权限反推 Git 的 mode，**只用于 v1 的遗留树**。
//
// v2 的 mode 直接来自 Git 的清单，不需要猜；v1 只剩一棵已物化的树，
// 可执行位是唯一可用的信号。Windows 上可执行位不可表示，因此这里会一律得到
// 100644——与 v1 时代一致（那时落地层读的也是文件权限）。
func modeForPerm(fi os.FileInfo) string {
	if fi.Mode().Perm()&0o111 != 0 {
		return digest.ModeExecutable
	}
	return digest.ModeRegular
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
