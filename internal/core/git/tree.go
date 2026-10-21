package git

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
)

// TreeEntry 是 commit tree 中的一个非目录条目。
type TreeEntry struct {
	// Mode 是 Git 模式串，如 "100644" / "100755" / "120000" / "160000"。
	Mode string
	// Type 是对象类型："blob" 或 "commit"（gitlink）。
	Type string
	// SHA 是对象 ID（blob 的 git sha1，或 gitlink 指向的外部 commit）。
	SHA string
	// Path 是仓库根相对路径，以 `/` 分隔。
	Path string
}

// ListTree 递归列出 commit tree 中所有非目录条目。
//
// 实现：`git ls-tree -r -z <commit>`。
//   - `-r` 递归展开 subtree，因此结果中**不含**目录（tree）条目——
//     这正是 ADR-008 "目录不单独成条" 所需
//   - `-z` 用 NUL 分隔记录并禁用路径转义，使含空格 / Unicode / 换行的路径
//     以原始字节返回（普通模式下 git 会加引号并转义）
//
// 输出顺序与 git 内部一致（tree 序），调用方不应依赖该顺序——
// 清单排序由 digest.BuildManifest 负责。
func ListTree(ctx context.Context, opts Options, repoPath, commit string) ([]TreeEntry, error) {
	o := opts
	o.Dir = repoPath
	res, err := Run(ctx, o, "ls-tree", "-r", "-z", commit)
	if err != nil {
		return nil, err
	}
	return parseListTreeZ(res.Stdout), nil
}

// parseListTreeZ 解析 `git ls-tree -r -z` 的输出。
//
// 记录格式（-z 模式）：`<mode> SP <type> SP <object> TAB <path> NUL`
func parseListTreeZ(out []byte) []TreeEntry {
	var entries []TreeEntry
	for len(out) > 0 {
		nul := indexByte(out, 0)
		if nul < 0 {
			// 末尾缺 NUL：丢弃残余（不应发生）
			break
		}
		rec := out[:nul]
		out = out[nul+1:]
		if len(rec) == 0 {
			continue
		}

		tab := indexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta := string(rec[:tab])
		path := string(rec[tab+1:])

		// 空路径无意义，且会在清单里产生不可定位的记录——丢弃
		if path == "" {
			continue
		}

		// meta = "<mode> <type> <object>"
		parts := strings.SplitN(meta, " ", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			continue
		}
		entries = append(entries, TreeEntry{
			Mode: parts[0],
			Type: parts[1],
			SHA:  parts[2],
			Path: path,
		})
	}
	return entries
}

func indexByte(b []byte, c byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// ArchiveMaterial 是一次 digest 计算的**中间产物**（v0.68）。
//
// 为什么需要它（评估报告 P1#4 量出的路径）：重放一次 digest 的成本是两个 git
// 子进程（`ls-tree` + `cat-file --batch`），而 install 此前把它跑了**两遍**——
// 自己算一遍（为了写 lock），`Put` 内部再算一遍（为了校验）——再加上 `Put`
// 自己又要一遍条目与内容：**每个依赖 6 个 spawn，其中 4 个是重复的**。
// 把"一次算出的条目、内容与清单记录"带下去，重复的那部分就消失了。
//
// 安全边界（把话说清楚）：本结构是**进程内**的传递物，不是可持久化的数据。
// 消费方（`vendor.Put`）仍然核对两个身份字段（Commit / Digest）——
// 拼错的 Material 会被**拒绝**，而不是被静默接受
// （"把 A 的内容写进 B 的槽位"是这个 store 最危险的失效模式）。
type ArchiveMaterial struct {
	// Commit 是这份材料来自的 commit（身份字段之一）。
	Commit string
	// ManifestVersion 是这份材料使用的清单 scope（空表示仓库级 v1）。
	ManifestVersion string
	// SubtreeOid 是模块材料的 Git tree 绑定锚（根材料为空）。
	SubtreeOid string
	// Digest 是它的内容 digest（`sha256:<hex>`）。
	Digest string
	// Entries 是**已过滤且已验证**的条目（不含 tree 条目），与 Contents / Records
	// 一一对应、顺序相同。
	Entries []TreeEntry
	// Contents[i] 是 Entries[i] 的原始字节。
	Contents [][]byte
	// Records 是清单记录（Path / Mode / BlobSHA256）——哈希已经算好，
	// 消费方不需要也不应该重算（重算就是重复劳动；而"少算一次"不会降低任何保证，
	// 因为 digest 本身由 Records 决定）。
	Records []digest.Record
}

// Manifest 返回这份材料的规范清单字节流。
func (m *ArchiveMaterial) Manifest() []byte {
	return digest.BuildManifestWithVersion(m.ManifestVersion, m.Records)
}

// BuildArchive 从 mirror 的 repoPath 中读取 commit，产出规范化清单。
//
// 流程：
//  1. `git ls-tree -r -z <commit>` 递归列出条目
//  2. 拒绝 v0.1 不支持的条目：
//     - gitlink（mode 160000）→ 子模块不支持（ADR-008 §已知限制）
//     - 未知 mode → 报错而非静默哈希
//  3. `git cat-file --batch` 批量取 blob 原始字节
//  4. 检测 LFS 指针并报错（不得对指针求哈希）
//  5. 逐条计算 blob sha256，交给 digest.BuildManifest 排序并拼装
//
// 返回值是清单字节流；其 digest 用 digest.Digest 计算。
//
// v0.68：本函数是 `BuildArchiveMaterial` 的薄包装——两条路径的字节级行为
// 由同一段代码保证（不允许"优化过的路径"与"老路径"各算各的）。
func BuildArchive(ctx context.Context, opts Options, repoPath, commit string) ([]byte, error) {
	m, err := BuildArchiveMaterial(ctx, opts, repoPath, commit)
	if err != nil {
		return nil, err
	}
	return m.Manifest(), nil
}

// BuildArchiveMaterial 一次算出条目、内容、清单记录与 digest（见 ArchiveMaterial）。
//
// 步骤与 BuildArchive 的文档完全一致；区别只是**把中间产物留下来**供调用方复用。
func BuildArchiveMaterial(ctx context.Context, opts Options, repoPath, commit string) (*ArchiveMaterial, error) {
	entries, err := ListTree(ctx, opts, repoPath, commit)
	if err != nil {
		return nil, err
	}
	return materialFromEntries(ctx, opts, repoPath, commit, entries, digest.ManifestVersion)
}

// BuildModuleMaterial 计算某个 commit 下 `subPath` 子树的模块级材料（ADR-032）。
//
// 路径是**相对模块根**的；`moduleDigest` 使用 ngm-module-digest/v1 头部。
// 兄弟目录的条目不会被读取，也不会进入清单。
func BuildModuleMaterial(ctx context.Context, opts Options, repoPath, commit, subPath string) (*ArchiveMaterial, error) {
	sub := strings.Trim(strings.TrimSpace(subPath), "/")
	if sub == "" {
		return BuildArchiveMaterial(ctx, opts, repoPath, commit)
	}
	treeish := commit + ":" + sub
	entries, err := ListTree(ctx, opts, repoPath, treeish)
	if err != nil {
		return nil, err
	}
	return materialFromEntries(ctx, opts, repoPath, commit, entries, digest.ModuleManifestVersion)
}

// materialFromEntries 是根 scope 与模块 scope 共用的唯一实现。
//
// entries 必须已经收窄到目标 scope；本函数只负责拒绝不支持的条目、读 blob、
// 检测 LFS、逐条哈希并拼装清单。
func materialFromEntries(ctx context.Context, opts Options, repoPath, commit string,
	entries []TreeEntry, manifestVersion string) (*ArchiveMaterial, error) {
	if len(entries) == 0 {
		// 空 commit / 空模块是合法的：清单只有头部
		mf := digest.BuildManifestWithVersion(manifestVersion, nil)
		return &ArchiveMaterial{
			Commit: commit, ManifestVersion: manifestVersion, Digest: digest.Digest(mf),
			Entries: []TreeEntry{}, Contents: [][]byte{}, Records: []digest.Record{},
		}, nil
	}

	// 2) 预检：拒绝不支持的模式，并收集需要读取的 blob 与清单记录。
	//
	// v0.66：blobSHAs 与 metas 在**同一个循环**里产出（两者一一对应）——
	// 此前"跳过 tree 条目"这个过滤条件写在两个循环里各一份，改一处漏一处
	// 会**静默产生错误的 digest**（评估报告 P3）。过滤现在只有一处定义。
	kept := make([]TreeEntry, 0, len(entries))
	blobSHAs := make([]string, 0, len(entries))
	metas := make([]digest.Record, 0, len(entries))
	for _, e := range entries {
		if err := validateTreeEntry(e, commit); err != nil {
			return nil, err
		}
		if e.Mode == digest.ModeTree {
			continue // -r 不应产生 tree 条目；保险起见跳过
		}
		kept = append(kept, e)
		blobSHAs = append(blobSHAs, e.SHA)
		metas = append(metas, digest.Record{Path: e.Path, Mode: e.Mode})
	}

	// 3) 批量读取内容
	contents, err := CatFileBatch(ctx, opts, repoPath, blobSHAs)
	if err != nil {
		return nil, err
	}
	return finishMaterial(commit, kept, metas, contents, manifestVersion)
}

// finishMaterial 在内容已经读好之后完成 LFS 检查、逐条哈希与清单拼装。
//
// entries/contents 必须一一对应且顺序相同；批量路径也复用它，避免第二套
// 哈希逻辑。
func finishMaterial(commit string, kept []TreeEntry, metas []digest.Record,
	contents [][]byte, manifestVersion string) (*ArchiveMaterial, error) {
	if len(metas) != len(contents) || len(metas) != len(kept) {
		return nil, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("internal error: %d entries, %d records, %d contents", len(kept), len(metas), len(contents)),
			"report this as an ngm bug")
	}

	// 4)+5) 检测 LFS 并计算每条记录的 sha256
	//
	// **这一步在一个依赖内部并行**（v0.2 复盘 §2.3 起挂账项）。
	// 并行**不改变任何字节，也不改变顺序**：每个结果按索引写回，最后按索引合并。
	type lfsHit struct{ path, oid string }

	hashes := make([]string, len(metas))
	hits := make([]*lfsHit, len(metas))

	workers := runtime.GOMAXPROCS(0)
	if workers > len(metas) {
		workers = len(metas)
	}
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for i := start; i < len(metas); i += workers {
				content := contents[i]
				if ptr, ok := digest.DetectLFSPointer(content); ok {
					hits[i] = &lfsHit{path: metas[i].Path, oid: ptr.OID}
					continue
				}
				hashes[i] = digest.HashBytes(content)
			}
		}(w)
	}
	wg.Wait()

	for i := range metas {
		if h := hits[i]; h != nil {
			return nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("commit %s contains a Git LFS pointer at %q (oid sha256:%s)", ShortSHA(commit), h.path, h.oid),
				"ngm v0.1 cannot prove LFS content: the pointer is not the file. "+
					"Remove LFS from this dependency, or pin a commit whose files are stored as regular Git objects")
		}
		metas[i].BlobSHA256 = hashes[i]
	}

	mf := digest.BuildManifestWithVersion(manifestVersion, metas)
	return &ArchiveMaterial{
		Commit:          commit,
		ManifestVersion: manifestVersion,
		Digest:          digest.Digest(mf),
		Entries:         kept,
		Contents:        contents,
		Records:         metas,
	}, nil
}

// TreeOIDAtPath 返回 `<commit>:<subPath>` 的 tree 对象 ID（根 scope 为 commit 的根树）。
//
// 这不是内容 digest；它是 Git 的 Merkle 绑定锚（ADR-032）。`blob:none` 的
// partial clone 保留 tree，因此本函数不需要 blob，可离线执行。
func TreeOIDAtPath(ctx context.Context, opts Options, repoPath, commit, subPath string) (string, error) {
	spec := commit + "^{tree}"
	sub := strings.Trim(strings.TrimSpace(subPath), "/")
	if sub != "" {
		for _, part := range strings.Split(sub, "/") {
			if part == "" || part == "." || part == ".." {
				return "", errs.New(errs.CodeConfigInvalid,
					fmt.Sprintf("invalid module sub-path %q", subPath),
					"use a repository-relative path without `.` or `..` components")
			}
		}
		spec = commit + ":" + sub
	}

	o := opts
	o.Dir = repoPath
	res, err := Run(ctx, o, "rev-parse", "--verify", spec)
	if err != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid,
			fmt.Sprintf("resolve tree for %q", spec),
			"check that the sub-path exists at the locked commit", err)
	}
	oid := strings.TrimSpace(string(res.Stdout))
	if oid == "" {
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("resolve tree for %q returned an empty object id", spec),
			"check that the commit and sub-path exist in the local mirror")
	}

	typ, terr := Run(ctx, o, "cat-file", "-t", oid)
	if terr != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "read object type of "+oid,
			"check that the local mirror is readable; re-fetch it if the object is missing", terr)
	}
	if got := strings.TrimSpace(string(typ.Stdout)); got != "tree" {
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("%q is a %s, not a tree", spec, got),
			"the module path must point at a directory, not a file")
	}

	// lock 里的锚带算法前缀，避免 SHA-1 → SHA-256 迁移时同一个字段有两种解释。
	return objectIDWithAlgo(oid, spec)
}

// objectIDWithAlgo 给 Git 对象 ID 加算法前缀。
func objectIDWithAlgo(oid, spec string) (string, error) {
	switch len(oid) {
	case 40:
		return "sha1:" + oid, nil
	case 64:
		return "sha256:" + oid, nil
	default:
		return "", errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unexpected Git object id length %d for %q", len(oid), spec),
			"this build understands 40-hex (sha1) and 64-hex (sha256) object ids")
	}
}

// listTreeWithTrees 与 ListTree 类似，但包含 tree 条目（`-t`），并可限定 pathspec。
//
// 批量模块材料需要 tree 条目来拿 subtreeOid，同时只对目标 pathspec 展开，
// 避免兄弟模块进入结果。
func listTreeWithTrees(ctx context.Context, opts Options, repoPath, treeish string, paths ...string) ([]TreeEntry, error) {
	args := []string{"ls-tree", "-r", "-t", "-z", treeish}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	o := opts
	o.Dir = repoPath
	res, err := Run(ctx, o, args...)
	if err != nil {
		return nil, err
	}
	return parseListTreeZ(res.Stdout), nil
}

// isTreeMode 兼容 `git ls-tree -t` 的 "040000" 与 digest.ModeTree 的 "40000"。
func isTreeMode(mode string) bool {
	return strings.TrimLeft(mode, "0") == digest.ModeTree
}

type moduleAcc struct {
	oid     string
	entries []TreeEntry
}

// BuildModuleMaterials 在一次 git 调用里计算多个模块的材料（ADR-032）。
//
// 复杂度目标：一次 `ls-tree -r -t` + 一次 `cat-file --batch`，而不是
// 每个模块各跑一遍整树。返回 map 的 key 是规范化后的 subPath。
func BuildModuleMaterials(ctx context.Context, opts Options, repoPath, commit string, subPaths []string) (map[string]*ArchiveMaterial, error) {
	if len(subPaths) == 0 {
		return map[string]*ArchiveMaterial{}, nil
	}

	mods := make([]string, 0, len(subPaths))
	seen := make(map[string]bool, len(subPaths))
	for _, raw := range subPaths {
		sub, err := normalizeModulePath(raw)
		if err != nil {
			return nil, err
		}
		if seen[sub] {
			return nil, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("duplicate module sub-path %q", sub),
				"give each module a distinct path")
		}
		seen[sub] = true
		mods = append(mods, sub)
	}
	sort.Strings(mods)
	for i := range mods {
		for j := i + 1; j < len(mods); j++ {
			if strings.HasPrefix(mods[j], mods[i]+"/") {
				return nil, errs.New(errs.CodeConfigInvalid,
					fmt.Sprintf("module paths overlap: %q contains %q", mods[i], mods[j]),
					"modules must not be nested; choose distinct paths")
			}
		}
	}

	entries, err := listTreeWithTrees(ctx, opts, repoPath, commit, mods...)
	if err != nil {
		return nil, err
	}

	acc := make(map[string]*moduleAcc, len(mods))
	for _, m := range mods {
		acc[m] = &moduleAcc{}
	}
	for _, e := range entries {
		if a, ok := acc[e.Path]; ok {
			if !isTreeMode(e.Mode) {
				return nil, errs.New(errs.CodeConfigInvalid,
					fmt.Sprintf("module path %q is a %s, not a directory", e.Path, e.Type),
					"a module must be a directory containing ngm.json")
			}
			a.oid = e.SHA
			continue
		}
		// 找到该条目所属的最长模块前缀；模块路径已保证不嵌套。
		parts := strings.Split(e.Path, "/")
		for i := len(parts) - 1; i >= 1; i-- {
			p := strings.Join(parts[:i], "/")
			a, ok := acc[p]
			if !ok {
				continue
			}
			if isTreeMode(e.Mode) {
				break // 子目录本身不进入清单
			}
			e.Path = strings.TrimPrefix(e.Path, p+"/")
			a.entries = append(a.entries, e)
			break
		}
	}

	// 一次读取所有模块的 blob；按 SHA 去重。
	var blobSHAs []string
	blobSeen := make(map[string]bool)
	for _, m := range mods {
		for _, e := range acc[m].entries {
			if !blobSeen[e.SHA] {
				blobSeen[e.SHA] = true
				blobSHAs = append(blobSHAs, e.SHA)
			}
		}
	}
	contents, err := CatFileBatch(ctx, opts, repoPath, blobSHAs)
	if err != nil {
		return nil, err
	}
	bySHA := make(map[string][]byte, len(blobSHAs))
	for i, sha := range blobSHAs {
		bySHA[sha] = contents[i]
	}

	out := make(map[string]*ArchiveMaterial, len(mods))
	for _, m := range mods {
		a := acc[m]
		if a.oid == "" {
			return nil, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("module path %q was not found at commit %s", m, ShortSHA(commit)),
				"check the sub-path at the locked commit")
		}
		metas := make([]digest.Record, 0, len(a.entries))
		blobs := make([][]byte, 0, len(a.entries))
		for _, e := range a.entries {
			metas = append(metas, digest.Record{Path: e.Path, Mode: e.Mode})
			blobs = append(blobs, bySHA[e.SHA])
		}
		mat, merr := finishMaterial(commit, a.entries, metas, blobs, digest.ModuleManifestVersion)
		if merr != nil {
			return nil, merr
		}
		mat.SubtreeOid, merr = objectIDWithAlgo(a.oid, commit+":"+m)
		if merr != nil {
			return nil, merr
		}
		out[m] = mat
	}
	return out, nil
}

// normalizeModulePath 校验并规范化一个模块子路径。
func normalizeModulePath(raw string) (string, error) {
	sub := strings.Trim(strings.TrimSpace(raw), "/")
	if sub == "" {
		return "", errs.New(errs.CodeConfigInvalid,
			"module sub-path is empty",
			"use a repository-relative path such as `packages/utils`")
	}
	for _, part := range strings.Split(sub, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("invalid module sub-path %q", raw),
				"use a repository-relative path without `.` or `..` components")
		}
	}
	return sub, nil
}

// BuildArchiveDigest 是 BuildArchive 的便捷封装，直接返回 `sha256:<hex>`。
func BuildArchiveDigest(ctx context.Context, opts Options, repoPath, commit string) (string, error) {
	manifest, err := BuildArchive(ctx, opts, repoPath, commit)
	if err != nil {
		return "", err
	}
	return digest.Digest(manifest), nil
}

// validateTreeEntry 检查单个 tree 条目是否受 ADR-008 支持。
//
// 支持：`100644`（普通）/ `100755`（可执行）/ `120000`（symlink）/ `40000`（目录，调用方跳过）。
// 拒绝：`160000`（gitlink → 子模块不支持）；其他（如历史仓库的 `100664`）→ 报错而非静默哈希。
//
// 为什么抽成独立函数：`git update-index --cacheinfo` 会把非标准 mode 规范化为
// `100644`，因此无法端到端构造出非法 mode 的 tree —— 只能靠单元测试覆盖。
func validateTreeEntry(e TreeEntry, commit string) error {
	switch e.Mode {
	case digest.ModeRegular, digest.ModeExecutable, digest.ModeSymlink, digest.ModeTree:
		return nil
	case digest.ModeGitlink:
		return errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("commit %s contains a gitlink (submodule) at %q", ShortSHA(commit), e.Path),
			"ngm v0.1 cannot hash submodules: the referenced commit lives in another repository. "+
				"Vendor the submodule into this repository, or pin a commit that does not use one")
	default:
		return errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("commit %s has unsupported entry mode %q at %q", ShortSHA(commit), e.Mode, e.Path),
			"only regular files (100644), executables (100755) and symlinks (120000) are supported")
	}
}

// ShortSHA 把 commit 缩短到 7 位用于错误消息（完整值在 log 中可查）。
//
// 导出供上层包（如 vendor）在诊断消息中复用同一缩写口径。
func ShortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
