package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// CloneMirror 目标位置已存在且是有效裸仓库时返回的哨兵错误文案片段。
// 调用方应先检查 ExistsMirror，或直接调用 EnsureMirror 处理幂等。
//
// 本文件提供三个层次：
//
//	CloneMirror    --mirror 克隆一个裸仓库
//	FetchMirror    在既有裸仓库上增量 fetch（--prune，保持 mirror 语义）
//	IsBareMirror   校验一个目录是否为可用裸仓库
//
// vendor 层的 Mirror 类型（internal/vendor/mirror.go）在此之上管理布局与生命周期。

// CloneMirror 执行 `git clone --mirror <remoteURL> <localPath>`。
//
// 语义：--mirror 会创建裸仓库并把 remote.origin.fetch 设为 `+refs/*:refs/*`，
// 使后续 `git fetch --prune` 具备完整的镜像更新能力（含 tag 与分支删除）。
//
// 前置条件：localPath 的父目录会被创建；localPath 本身必须不存在。
// 若需要"存在即更新"的幂等语义，使用 vendor.Mirror.Ensure。
func CloneMirror(ctx context.Context, opts Options, remoteURL, localPath string) error {
	if strings.TrimSpace(remoteURL) == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty remote URL", "check the dependency declaration")
	}
	if localPath == "" {
		return errs.New(errs.CodeGitFetch, "clone: empty local path", "")
	}
	if exists(localPath) {
		return errs.New(errs.CodeGitFetch,
			"clone target already exists: "+localPath,
			"remove it or use the mirror update path")
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return errs.Wrap(errs.CodeGitFetch,
			"create mirror parent directory", "check filesystem permissions", err)
	}

	_, err := Run(ctx, opts, "clone", "--mirror", "--quiet", remoteURL, localPath)
	return err
}

// FetchMirror 在既有裸仓库上执行增量更新。
//
// 命令：`git -C <localPath> fetch --prune`
// 因该仓库由 --mirror 创建，其 refspec 覆盖全部 refs，`--prune` 会同步删除上游已删除的 ref。
func FetchMirror(ctx context.Context, opts Options, localPath string) error {
	if !IsBareMirror(localPath) {
		return errs.New(errs.CodeGitFetch,
			"not a valid bare mirror: "+localPath,
			"delete the directory and re-run to re-create the mirror")
	}
	o := opts
	o.Dir = localPath
	_, err := Run(ctx, o, "fetch", "--prune", "--quiet")
	return err
}

// FetchMirrorTags 额外强制同步 tags（部分 host 对 tag 更新需要显式 --tags）。
func FetchMirrorTags(ctx context.Context, opts Options, localPath string) error {
	if !IsBareMirror(localPath) {
		return errs.New(errs.CodeGitFetch, "not a valid bare mirror: "+localPath, "")
	}
	o := opts
	o.Dir = localPath
	_, err := Run(ctx, o, "fetch", "--prune", "--tags", "--quiet")
	return err
}

// IsBareMirror 校验 path 是否为一个可用的裸仓库。
//
// 判定：存在 `HEAD` 文件、`objects/` 与 `refs/` 目录，且 `config` 中 core.bare=true。
// 不调用 git 子进程（快速路径，供 Ensure 在热路径上使用）。
func IsBareMirror(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	for _, f := range []string{"HEAD", "objects", "refs"} {
		if !exists(filepath.Join(path, f)) {
			return false
		}
	}
	cfg, err := os.ReadFile(filepath.Join(path, "config"))
	if err != nil {
		return false
	}
	// 宽松匹配：core.bare = true（允许空白变体）
	for _, line := range strings.Split(string(cfg), "\n") {
		l := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line), " ", ""))
		if l == "bare=true" {
			return true
		}
	}
	return false
}

// MirrorRemoteURL 读取裸仓库的 origin URL（用于诊断，不含写入路径）。
func MirrorRemoteURL(ctx context.Context, opts Options, localPath string) (string, error) {
	o := opts
	o.Dir = localPath
	res, err := Run(ctx, o, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
