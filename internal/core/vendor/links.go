package vendor

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
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
	return lt.materialize(digest, canonicalPath, subPath, false)
}

// MaterializeModule 把模块作用域的 content store 条目落地到 vendor。
//
// 与 Materialize 的区别：模块 digest 的清单路径**已经相对模块根**，
// 不能再按 subPath 收窄；目标路径仍由 subPath 决定。
func (lt *LinkTree) MaterializeModule(moduleDigest, canonicalPath, subPath string) (LinkResult, error) {
	return lt.materialize(moduleDigest, canonicalPath, subPath, true)
}

func (lt *LinkTree) materialize(digest, canonicalPath, subPath string, relative bool) (LinkResult, error) {
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
	// v0.81：`nil` 条目集合与"这份依赖确实是空的"必须区分开。
	//
	// v2 走清单，零条目返回的是**非 nil 的空切片**；只有 v1 那条
	// "扫目录"的路径在没有内容时返回 `nil`。此前 nil 会一路走到
	// linkEntries(nil, dst)：先 RemoveAll 再建目录，最后返回一个
	// `Files: 0` 的 LinkResult，上层当作成功——用户得到一个空目录，
	// 而 install 报告一切正常。空 commit 是合法的（ADR-008 明确支持），
	// 所以这里只拦 nil，不拦空。
	if all == nil && (subPath == "" || relative) {
		return LinkResult{}, errs.New(errs.CodeConfigInvalid,
			"the content store entry for "+digest+" has no file listing",
			"run `ngm install` to rebuild the content store")
	}
	entries := all
	if !relative {
		entries = EntriesUnder(all, subPath)
	}
	if !relative {
		if sub := strings.Trim(filepath.ToSlash(subPath), "/"); sub != "" && len(entries) == 0 {
			return LinkResult{}, errs.New(
				errs.CodeConfigInvalid,
				"monorepo sub-path "+subPath+" does not exist in the content tree for "+digest,
				"check the `path` field of this dependency in ngm.json")
		}
	}

	dst := filepath.Join(lt.vendorRoot, filepath.FromSlash(rel))

	// v0.66：落地前安全预检——**必须在清理旧内容之前**，
	// 否则一次被拒绝的落地会先把已有的 vendor 树删掉。
	//
	// 背景（评估报告 P1#2）：此前 `os.Symlink(e.LinkTarget, target)` 原样重建
	// Git 仓库里的链接目标，且清单 path 不做逃逸校验。一个恶意/失误的上游可以
	// 让 vendor 里出现"看起来在包里、实际指向包外"的链接——这正是这个项目
	// 最忌讳的失败形态（"看起来正常但指向别处"）。
	if err := validateEntryPaths(entries); err != nil {
		return LinkResult{}, err
	}

	// 父目录先建出来：下面的**大小写探针**要在真实的目标文件系统上问，
	// 而不是按 GOOS 猜（Linux 也能挂 case-insensitive 的卷）。
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid, "create vendor parent dir", "", err)
	}

	// 大小写折叠的文件系统上，仅大小写不同的两个条目会落到**同一个路径**：
	// 第一次 hardlink 成功，第二次返回 EEXIST → auto 降级 copy → 以 O_TRUNC
	// 覆盖同一个文件 → 后写者胜，而 `files++` 照增、安装照报成功。
	//
	// v0.105 实测（Windows）：仓库里同时有 `Foo.txt` 与 `foo.txt` 时，
	// `ngm install` 打印 "installed 1 dependencies"，vendor 里只剩一个文件；
	// 紧接的 `ngm verify` 报 `critical` exit 2（"foo.txt: present in the content
	// store but missing from vendor"）——**工具自己造出一个自己拒绝的世界**，
	// 而中间那步看起来是成功的。
	//
	// 处置是 fail-closed，且必须在 `os.RemoveAll(dst)` **之前**：
	// 先删再拒绝等于用一个"拒绝"把用户原来的 vendor 也删掉了。
	if err := ensureNoCaseCollisions(entries, filepath.Dir(dst)); err != nil {
		return LinkResult{}, err
	}

	// vendor 是派生物：先清理，避免残留已删除的文件
	if err := os.RemoveAll(dst); err != nil {
		return LinkResult{}, errs.Wrap(errs.CodeConfigInvalid, "clean "+dst,
			"close any process holding files under ngm.vendor/", err)
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

// validateEntryPaths 在落地前拒绝逃逸条目（fail-closed）。
//
// 两类检查：
//
//  1. 条目自身的 path 必须落在依赖子树内（`..` 组件、绝对路径、驱动器前缀一律拒绝）。
//     当前内容来源（`git ls-tree` 的清单）在 Git 的路径模型下不会产生这类路径——
//     这是**纵深防御**：v1 的目录扫描与将来的新来源不该改变这条前提。
//  2. symlink 的**目标**必须在依赖子树内。目标字符串是文件内容（上游可控），
//     相对目标按"链接所在目录"解析、绝对目标直接逃逸——两者都拒绝。
//
// 为什么拒绝而不是"跳过并警告"：跳过会让 vendor 里少一个条目而用户不知情，
// 那正是"看起来正常但缺东西"的形态；而指向子树外的链接即使落地也是断链
// （目标不在 vendor 里），构建工具跟随它只会读到包外文件。
func validateEntryPaths(entries []ContentEntry) error {
	for _, e := range entries {
		p := path.Clean(filepath.ToSlash(e.Path))
		if p == "." || p == ".." || strings.HasPrefix(p, "../") || looksLikeAbsPath(p) {
			return errs.New(errs.CodeConfigInvalid,
				"refusing to materialize an entry whose path escapes the vendor tree: "+e.Path,
				"the manifest contains an unsafe path; re-run `ngm install` to rebuild the content store")
		}
		if e.Symlink && EscapesPackageTree(e.Path, e.LinkTarget) {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("refusing to materialize symlink %s → %q: the target escapes the dependency tree",
					e.Path, e.LinkTarget),
				"an escaping symlink would resolve against files that are not vendored; "+
					"fix it upstream, or pin a commit where the dependency does not contain it")
		}
	}
	return nil
}

// ensureNoCaseCollisions 在落地的目标文件系统上拒绝"仅大小写不同"的条目对。
//
// 判据跟随**磁盘**（isCaseInsensitiveDir），不跟随 GOOS：同一份清单在 Linux
// 上可以忠实落地，在 Windows 上不能。把两件事混为一谈会得到"要么误拒 Linux、
// 要么在 Windows 上静默丢文件"。
func ensureNoCaseCollisions(entries []ContentEntry, dir string) error {
	a, b, ok := caseCollision(entries)
	if !ok || !isCaseInsensitiveDir(dir) {
		return nil
	}
	return errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("refusing to materialize %s and %s: they differ only by case, "+
			"and this filesystem folds case (one would silently overwrite the other)", a, b),
		"the dependency cannot be materialized faithfully here; "+
			"pin a commit that renames one of them, or use a case-sensitive volume")
}

// caseCollision 返回第一对**仅大小写不同**的条目路径（按清单顺序）。
//
// 用 ToLower 做折叠：这是对 Windows / macOS 默认卷行为的近似，而不是完整的
// Unicode case folding——它比漏判更保守（会多拒几对罕见路径），而漏判的代价是
// 静默丢文件。
func caseCollision(entries []ContentEntry) (a, b string, ok bool) {
	seen := make(map[string]string, len(entries))
	for _, e := range entries {
		folded := strings.ToLower(e.Path)
		if prev, dup := seen[folded]; dup && prev != e.Path {
			return prev, e.Path, true
		}
		if _, dup := seen[folded]; !dup {
			seen[folded] = e.Path
		}
	}
	return "", "", false
}

// isCaseInsensitiveDir 在**真实的**目标目录上探测"这个文件系统折叠大小写吗"。
//
// 不按 runtime.GOOS 判断：Linux 可以挂 case-insensitive 卷（ext4 casefold、
// 容器里的 vfat），macOS 也可以格式化成 case-sensitive。判据必须是磁盘本身。
//
// 探测失败返回 false（= 按大小写敏感处理，保持既有行为）：
// 探针目录是落地目标本身，写不进去时后面每一步都会失败并报出真因，
// 这里不该用一条推测性的拒绝把那个真因盖掉。
func isCaseInsensitiveDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".ngm-caseprobe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(name) }()

	folded := strings.ToUpper(filepath.Base(name))
	if folded == filepath.Base(name) {
		return false
	}
	_, statErr := os.Stat(filepath.Join(dir, folded))
	return statErr == nil
}

// EscapesPackageTree 报告条目路径空间中的 symlink 目标是否逃逸依赖子树。
//
// 输入空间：entryPath 与 target 都是清单口径的相对路径（`/` 分隔）。
// 判定：
//
//	目标是绝对形态（POSIX `/…`、Windows 驱动器前缀或 UNC） → 逃逸
//	相对目标按"链接所在目录"解析后以 `..` 开头            → 逃逸
//
// 为什么由 vendor 包导出：落地侧的预检（validateEntryPaths）与判定侧的
// landing 检查（internal/verify）必须用**同一个**判据——两处各写一份，
// 迟早在某个边界上分叉，而那种分叉的表现是"安装拒绝、verify 放行"。
func EscapesPackageTree(entryPath, target string) bool {
	if target == "" {
		return false
	}
	// v0.81：目标字符串也必须先归到 `/` 口径。
	//
	// v0.66 只归一了 entryPath，于是 `..\..\..\Users\x\.ssh\id_rsa` 这类
	// **反斜杠相对目标**绕过了整个判定：`path.Clean`/`path.Join` 只把 `/`
	// 当分隔符，它既不以 `../` 开头、也不像绝对路径，判据返回 false。
	// 而 Windows 的 CreateSymbolicLinkW 把 `\` 当分隔符解析——
	// 落地后就是一个指向包外的链接。**这个洞只在 POSIX 口径下被封住了。**
	//
	// 代价说明（保守拒绝）：POSIX 上 `\` 是合法的文件名字符，因此
	// `..\..\x` 在那里是一个（极罕见的）合法文件名而非逃逸。这里统一按
	// 分隔符处理 = 宁可拒掉一个罕见合法名，也不放过一个真实逃逸。
	target = filepath.ToSlash(target)
	if looksLikeAbsPath(target) {
		return true
	}
	dir := path.Dir(path.Clean(filepath.ToSlash(entryPath)))
	joined := path.Clean(path.Join(dir, target))
	return joined == ".." || strings.HasPrefix(joined, "../")
}

// looksLikeAbsPath 跨平台判定"形态像绝对路径"。
//
// 不能只用 filepath.IsAbs：清单里的路径来自 Git（POSIX 风格），
// 一个 `/etc/passwd` 目标在 Windows 上不是 IsAbs，却同样是逃逸。
func looksLikeAbsPath(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	if len(p) >= 2 && p[1] == ':' {
		c := p[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
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
