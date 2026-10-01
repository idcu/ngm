package git

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
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
func BuildArchive(ctx context.Context, opts Options, repoPath, commit string) ([]byte, error) {
	entries, err := ListTree(ctx, opts, repoPath, commit)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		// 空 commit（无文件）是合法的：清单只有头部
		return digest.BuildManifest(nil), nil
	}

	// 2) 预检：拒绝不支持的模式，并收集需要读取的 blob
	blobSHAs := make([]string, 0, len(entries))
	for _, e := range entries {
		if err := validateTreeEntry(e, commit); err != nil {
			return nil, err
		}
		if e.Mode == digest.ModeTree {
			continue // -r 不应产生 tree 条目；保险起见跳过
		}
		blobSHAs = append(blobSHAs, e.SHA)
	}

	// 3) 批量读取内容
	contents, err := CatFileBatch(ctx, opts, repoPath, blobSHAs)
	if err != nil {
		return nil, err
	}

	// 4)+5) 检测 LFS 并计算每条记录的 sha256
	//
	// **这一步在一个依赖内部并行**（v0.2 复盘 §2.3 起挂账项）。
	// 并行**不改变任何字节，也不改变顺序**：每个结果按索引写回，最后按索引合并。
	//
	// 收益有多大，实测说了算（`internal/git/archive_bench_test.go`，2000 × 8 KiB，
	// 用 `GOMAXPROCS=1` 作串行对照）：278.9ms → 244.1ms，**1.14×**。
	//
	// 这个数字**低于我事前的预期**，原因值得写下来：本函数的成本大头是
	// `git ls-tree` + `git cat-file --batch` 两个子进程（16 MiB 的 sha256 在这台机器上
	// 只要几十毫秒），因此把纯 CPU 段并行化，最多也只能省下那段。
	// 换句话说：**这条路径的下一个优化是"少开子进程 / 一次取更多"，不是"开更多线程"**。
	// 保留这个并行是因为它几乎零成本（无额外分配，代码只多一层循环），
	// 但若将来有人以为它解决了 digest 的慢，那会是误读——慢在 git，不在这里。
	//
	// LFS 指针的报错也因此要保持确定：串行版本报的是**第一条**（entries 顺序）命中。
	// 并行时"谁先返回"取决于调度，所以这里先把所有命中记下来，再按索引取最小的一条——
	// 否则同一个仓库在不同机器上会给出不同的报错路径（本项目对确定性有明确纪律）。
	type lfsHit struct{ path, oid string }

	metas := make([]digest.Record, 0, len(blobSHAs))
	for _, e := range entries {
		if e.Mode == digest.ModeTree {
			continue
		}
		metas = append(metas, digest.Record{Path: e.Path, Mode: e.Mode})
	}

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

	return digest.BuildManifest(metas), nil
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
