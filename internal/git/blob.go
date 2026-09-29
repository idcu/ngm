package git

import (
	"context"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
)

// ReadFileAtCommit 读取 commit 中某个路径的文件内容。
//
// 返回值：
//   - content, true,  nil  文件存在
//   - nil,     false, nil  文件不存在（或该路径是目录）
//   - nil,     false, err  其他失败（mirror 损坏、git 错误）
//
// 实现选择：先用 `git ls-tree -z <commit> -- <path>` 精确定位条目，再按 blob sha
// 取内容。相比 `git show <commit>:<path>`，这样可以**区分"不存在"与"是目录"**，
// 并且不会被 `git show` 的格式化行为影响（对二进制内容不安全）。
//
// 用途：读取上游依赖根目录的 `ngm.json` 以展开传递依赖
// （dependency-resolution.md §传递性依赖：唯一来源是 ngm.json）。
func ReadFileAtCommit(ctx context.Context, opts Options, repoPath, commit, path string) ([]byte, bool, error) {
	o := opts
	o.Dir = repoPath

	res, err := Run(ctx, o, "ls-tree", "-z", commit, "--", path)
	if err != nil {
		return nil, false, err
	}
	entries := parseListTreeZ(res.Stdout)
	if len(entries) == 0 {
		return nil, false, nil // 路径不存在
	}

	e := entries[0]
	switch e.Mode {
	case digest.ModeRegular, digest.ModeExecutable, digest.ModeSymlink:
		content, gerr := GetBlob(ctx, opts, repoPath, e.SHA)
		if gerr != nil {
			return nil, false, gerr
		}
		return content, true, nil
	case digest.ModeTree:
		return nil, false, nil // 是目录，不是文件
	default:
		return nil, false, errs.New(
			errs.CodeConfigInvalid,
			"path "+path+" at commit "+ShortSHA(commit)+" has unsupported mode "+e.Mode,
			"only regular files, executables and symlinks can be read as file content")
	}
}

// CommitExists 报告某个对象是否为该仓库中可解析的 commit。
//
// 用途：commit 类型依赖的"存在性验证"（M1 只做了格式校验，M3 起有 mirror 可验证）。
func CommitExists(ctx context.Context, opts Options, repoPath, commit string) (bool, error) {
	o := opts
	o.Dir = repoPath

	// `git cat-file -e <rev>^{commit}` 在存在且可解析为 commit 时退出 0。
	res, err := RunAllowFailure(ctx, o, "cat-file", "-e", commit+"^{commit}")
	if err != nil {
		return false, err
	}
	return res.ExitCode == 0, nil
}
