package vendor

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// VendorDirName 是 link tree（层 3）在项目中的固定目录名。
//
// 依据（三处一致）：
//   - architecture/vendor-layers.md 的目录树与 §层 3
//   - architecture/security-model.md 的权限表 `write:$PROJECT/ngm.vendor`
//   - modules/p4-ecosystem.md 的 mappings 示例 `to: "./ngm.vendor/..."`
//
// （guides/configuration.md 里的 "vendor/" 是对该模式的泛称。）
const VendorDirName = "ngm.vendor"

// LinkMode 是层 3 的落地方式（guides/configuration.md §linkMode）。
type LinkMode string

const (
	// LinkAuto：hardlink 优先；跨卷/不支持时**整树降级**为复制。
	LinkAuto LinkMode = "auto"
	// LinkHardlink：强制 hardlink，失败即报错（要求零重复磁盘）。
	LinkHardlink LinkMode = "hardlink"
	// LinkCopy：普通复制（网络文件系统、要提交 vendor 的保守场景）。
	LinkCopy LinkMode = "copy"
	// LinkSymlink：依赖目录**整体** symlink 指向 content 树（不逐文件链接）。
	//
	// v2 布局下没有"一棵已物化的树"可指（ADR-019：内容在 blob 池里），因此
	// **该模式退化为逐条目 hardlink**，并在结果里如实报告实际使用的 mode
	// （vendor-layers.md 要求"所有链接失败路径必须可观测"）。
	// 磁盘收益不受影响——hardlink 与目录 symlink 同样不复制内容。
	// 已有的 v1 digest 仍按老语义整目录 symlink（读路径两种布局都认）。
	LinkSymlink LinkMode = "symlink"
)

// ValidLinkModes 列出全部合法值（顺序稳定，便于错误提示）。
func ValidLinkModes() []LinkMode {
	return []LinkMode{LinkAuto, LinkHardlink, LinkCopy, LinkSymlink}
}

// ParseLinkMode 解析配置中的 linkMode 字符串；空值或非法值回退到 LinkAuto。
func ParseLinkMode(s string) LinkMode {
	switch LinkMode(strings.ToLower(strings.TrimSpace(s))) {
	case LinkHardlink:
		return LinkHardlink
	case LinkCopy:
		return LinkCopy
	case LinkSymlink:
		return LinkSymlink
	default:
		return LinkAuto
	}
}

// IsValid 报告 mode 是否为受支持的值。
func (m LinkMode) IsValid() bool {
	for _, v := range ValidLinkModes() {
		if m == v {
			return true
		}
	}
	return false
}

// LinkTree 把 content store（层 2）的内容树落地到项目的 ngm.vendor/（层 3）。
//
// 设计要点：
//   - **目录不能 hardlink**，所以落地方式是"真实目录结构 + 逐文件 hardlink"
//   - symlink 条目按目标字符串原样重建为 symlink（不 hardlink）
//   - 目标是**派生物**：每次落地前清理旧内容，因此不存在"半更新"状态
//   - 实际使用的 mode 会被返回，供调用方输出（vendor-layers.md 要求
//     "所有链接失败路径必须可观测（输出使用的 linkMode）"）
type LinkTree struct {
	vendorRoot string
	store      *ContentStore
	mode       LinkMode
}

// NewLinkTree 创建 link tree 管理器。vendorRoot 通常是 `<project>/ngm.vendor`。
func NewLinkTree(vendorRoot string, store *ContentStore, mode LinkMode) *LinkTree {
	if !mode.IsValid() {
		mode = LinkAuto
	}
	return &LinkTree{vendorRoot: vendorRoot, store: store, mode: mode}
}

// Root 返回 vendor 根目录。
func (lt *LinkTree) Root() string { return lt.vendorRoot }

// Mode 返回配置的 linkMode。
func (lt *LinkTree) Mode() LinkMode { return lt.mode }

// LinkResult 描述一次落地的结果。
type LinkResult struct {
	// Path 是依赖在 vendor 中的路径（`<vendorRoot>/<relPath>`）。
	Path string
	// Mode 是**实际使用**的 mode。auto 模式下若 hardlink 不可用会降级为 copy，
	// 这里如实反映结果（用户需要知道磁盘上究竟是链接还是副本）。
	Mode LinkMode
	// Degraded 表示 auto 模式发生了降级（hardlink → copy）。
	Degraded bool
	// Files 是落地的文件数（symlink 模式下为 0——整目录链接不逐文件计数）。
	Files int
}

// Materialize 把某个 digest 的内容树落地到 vendor 下。
//
// 参数语义：
//
//	canonicalPath  归一化仓库坐标，如 `github.com/org/repo`
//	subPath        monorepo 子路径（可空）。**vendor 里只放该子目录**
//	               （dependency-resolution.md §monorepo 子路径）
//
// 目标路径 = `<vendorRoot>/<canonicalPath>[/<subPath>]`，即 mappings 的 `to`。
//
// 重复调用是幂等的：先清理目标目录再重建。清理范围严格限定在目标目录内，
// 因此同一仓库的多个子包（`packages/core`、`packages/web`）互不影响。
func (lt *LinkTree) Materialize(digest, canonicalPath, subPath string) (LinkResult, error) {
	rel := VendorPathFor(canonicalPath, subPath)
	if rel == "" {
		return LinkResult{}, errs.New(errs.CodeConfigInvalid, "empty vendor path", "")
	}
	if !lt.store.Has(digest) {
		return LinkResult{}, errs.New(
			errs.CodeDigestMismatch,
			"content store has no entry for "+digest+"; cannot build the vendor tree",
			"run `ngm install` to populate the content store first")
	}

	// 源：**条目集合**（v2 清单 / v1 扫描），不再假定 store 里有一棵目录树（ADR-019）。
	all, eerr := lt.store.Entries(digest)
	if eerr != nil {
		return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid,
			"read the content store entry list for "+digest, "", eerr)
	}
	entries := EntriesUnder(all, subPath)
	if sub := strings.Trim(filepath.ToSlash(subPath), "/"); sub != "" && len(entries) == 0 {
		return LinkResult{}, errs.New(
			errs.CodeConfigInvalid,
			"monorepo sub-path "+subPath+" does not exist in the content tree for "+digest,
			"check the `path` field of this dependency in ngm.json")
	}

	dst := filepath.Join(lt.vendorRoot, filepath.FromSlash(rel))

	// vendor 是派生物：先清理，避免残留已删除的文件
	if err := os.RemoveAll(dst); err != nil {
		return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid, "clean "+dst,
			"close any process holding files under ngm.vendor/", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid, "create vendor parent dir", "", err)
	}

	// 整目录 symlink 只在**还有一棵树可链**时成立（v1 布局）。
	// v2 下没有这样的目录，于是逐条目落地，并在结果里如实报告实际使用的 mode
	// （vendor-layers.md 要求"所有链接失败路径必须可观测"）。
	if lt.mode == LinkSymlink {
		srcTree := lt.store.TreePath(digest)
		if sub := strings.Trim(filepath.ToSlash(subPath), "/"); sub != "" {
			srcTree = filepath.Join(srcTree, filepath.FromSlash(sub))
		}
		if st, serr := os.Stat(srcTree); serr == nil && st.IsDir() {
			if err := os.Symlink(srcTree, dst); err != nil {
				return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid,
					"cannot symlink "+dst+" → "+srcTree,
					symlinkHint(), err)
			}
			return LinkResult{Path: dst, Mode: LinkSymlink}, nil
		}
	}

	used, files, degraded, err := lt.linkEntries(entries, dst)
	if err != nil {
		return LinkResult{}, err
	}
	return LinkResult{Path: dst, Mode: used, Degraded: degraded, Files: files}, nil
}

// linkEntries 按条目把内容落地到 dst，返回实际使用的 mode 与文件数。
//
// 与旧 `linkTree(src, dst)` 的差别只是**来源**：过去扫一个目录，现在吃条目集合——
// 因为 v2 布局下那份"目录"并不存在。落地规则（hardlink → copy 降级、symlink 原样重建、
// 权限位保留）与过去完全一致。
func (lt *LinkTree) linkEntries(entries []ContentEntry, dst string) (used LinkMode, files int, degraded bool, err error) {
	// auto 模式从 hardlink 起步；首次失败即整树降级为 copy
	current := lt.mode
	if current == LinkAuto || current == LinkSymlink {
		current = LinkHardlink
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", 0, false, err
	}

	for _, e := range entries {
		target := filepath.Join(dst, filepath.FromSlash(e.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", files, degraded, err
		}

		if e.Symlink {
			// symlink 条目原样重建（不 hardlink）
			if rerr := os.RemoveAll(target); rerr != nil {
				return "", files, degraded, rerr
			}
			if serr := os.Symlink(e.LinkTarget, target); serr != nil {
				return "", files, degraded, serr
			}
			files++
			continue
		}

		src := e.Full
		if _, serr := os.Stat(src); serr != nil {
			return "", files, degraded, errs.Wrap(errs.CodeConfigInvalid,
				"content blob for "+e.Path+" is missing", "re-run `ngm install` to rebuild the content store", serr)
		}

		if current == LinkHardlink {
			if lerr := os.Link(src, target); lerr == nil {
				files++
				continue
			} else if lt.mode == LinkHardlink {
				// 强制 hardlink：不得降级，明确报错
				return "", files, degraded, errs.Wrap(errs.CodeConfigInvalid,
					"hardlink failed for "+e.Path+" (linkMode=hardlink forbids falling back to copy)",
					"set vendor.linkMode to \"auto\" or \"copy\" to allow a copy fallback", lerr)
			}
			// auto：降级为 copy
			current = LinkCopy
			degraded = true
		}
		// copy 的权限取自**条目**而不是源文件的权限：v2 的 blob 本身就按 mode 落盘，
		// 但 v1 的树在某些平台（Windows）表达不了可执行位，而清单里的 mode 是权威的。
		if cerr := copyFile(src, target, permForMode(e.Mode)); cerr != nil {
			return "", files, degraded, cerr
		}
		files++
	}

	return current, files, degraded, nil
}

// copyFile 复制单个文件并保留权限位（可执行位必须带过去）。
func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// symlinkHint 给出与平台相关的可操作建议。
func symlinkHint() string {
	if runtime.GOOS == "windows" {
		return "on Windows, enable Developer Mode (or run as Administrator) to create symlinks; " +
			"alternatively use vendor.linkMode \"auto\" or \"copy\""
	}
	return "check that the filesystem supports symlinks and the target path is writable"
}
