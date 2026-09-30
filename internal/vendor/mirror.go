// Package vendor 实现 vendor 4 层：mirror（裸仓库镜像）、content store、
// link tree（逐文件 hardlink）、cache。
//
// 本文件是**层 1：mirror**。布局（architecture/vendor-layers.md 与
// modules/p0-core.md）：
//
//	~/.ngm/mirror/<host>/<org>/<repo>.git
//
// mirror 是 Git 依赖可证明性的本地根基：archiveDigest 从 mirror 的 commit tree
// 生成，verify 的 digest 重放完全在本地进行（见 ADR-008）。
package vendor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
)

// Mirror 管理本地裸仓库镜像（层 1）。
//
// 并发安全：Ensure 对同一 repo 串行化（进程内按路径加锁），避免并发 clone 同一目标。
// 跨进程并发由 .lock 文件（M3 引入）防护。
type Mirror struct {
	root string
	opts git.Options

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewMirror 创建 mirror 管理器。root 通常是 `~/.ngm/mirror`。
func NewMirror(root string, opts git.Options) *Mirror {
	return &Mirror{
		root:  root,
		opts:  opts,
		locks: make(map[string]*sync.Mutex),
	}
}

// Root 返回 mirror 根目录。
func (m *Mirror) Root() string { return m.root }

// PathFor 返回该仓库在 mirror 中的绝对路径（以 `.git` 结尾的裸仓库目录）。
//
// 布局与 resolve.Canonical.MirrorRelPath 保持一致（斜杠分隔 → 本地文件分隔符）。
func (m *Mirror) PathFor(repo resolve.Canonical) string {
	rel := filepath.FromSlash(repo.MirrorRelPath() + ".git")
	return filepath.Join(m.root, rel)
}

// Exists 报告该仓库的 mirror 是否已就绪。
func (m *Mirror) Exists(repo resolve.Canonical) bool {
	return git.IsBareMirror(m.PathFor(repo))
}

// EnsureResult 描述一次 Ensure 的结果。
type EnsureResult struct {
	// Path 是 mirror 路径。
	Path string
	// Created 为 true 表示本次新建（clone）；false 表示复用既有 mirror 并做了增量 fetch。
	Created bool
}

// Ensure 确保 mirror 存在且已更新。
//
// 行为：
//   - 不存在 → `git clone --mirror`
//   - 已存在 → `git fetch --prune`（增量）
//
// remoteURL 为空时由 repo.CloneURL(protocol) 生成（protocol 默认 https）。
func (m *Mirror) Ensure(ctx context.Context, repo resolve.Canonical, remoteURL string, protocol resolve.Protocol) (EnsureResult, error) {
	path := m.PathFor(repo)
	unlock := m.lockPath(path)
	defer unlock()

	if remoteURL == "" {
		p := protocol
		if !p.IsValid() {
			p = resolve.ProtocolHTTPS
		}
		remoteURL = repo.CloneURL(p)
	}

	// 权限门禁放在**这里**而不是调用方：只有本函数知道实际要访问的那个 URL。
	//
	// 调用方只看得到 slug（`github:org/repo`），而 mirror 可能来自本地路径
	// （测试、CI 预置镜像、本地裸仓库）——按 slug 的主机名判定会把纯本地操作
	// 当成网络访问拦下。
	//
	// 先判 run:git，再判 net：读已存在 mirror 的 origin URL 本身就要跑 git，
	// 若顺序反了，被拒绝的 run:git 会被报成"net 权限不足"（见 CheckRunAccess）。
	if err := git.CheckRunAccess(m.opts.Policy, "git"); err != nil {
		return EnsureResult{Path: path}, err
	}

	netURL := remoteURL
	if m.Exists(repo) {
		// 已存在的 mirror 从它记录的 origin 取（本地读配置，不触网）。
		// 读不到时退回克隆 URL 判定：宁可多要求一次 net 权限，
		// 也不让一次网络访问绕过门禁。这是 fail-closed 的取舍——
		// 它确实可能在极端情形下多拦一次本地 fetch，但不会漏放一次网络访问。
		u, uerr := git.MirrorRemoteURL(ctx, m.opts, path)
		if uerr == nil && strings.TrimSpace(u) != "" {
			netURL = u
		}
	}
	if err := git.CheckNetAccess(m.opts.Policy, netURL); err != nil {
		return EnsureResult{Path: path}, err
	}

	if !m.Exists(repo) {
		// 目标路径存在但不是有效裸仓库 → 明确报错而不是静默删除用户数据
		if _, err := os.Stat(path); err == nil {
			return EnsureResult{Path: path}, errs.New(
				errs.CodeGitFetch,
				"mirror path exists but is not a valid bare repository: "+path,
				"remove the directory manually, then re-run")
		}
		if err := git.CloneMirror(ctx, m.opts, remoteURL, path); err != nil {
			return EnsureResult{Path: path}, err
		}
		return EnsureResult{Path: path, Created: true}, nil
	}

	if err := git.FetchMirror(ctx, m.opts, path); err != nil {
		return EnsureResult{Path: path}, err
	}
	return EnsureResult{Path: path, Created: false}, nil
}

// EnsureLocal 用于把外部已存在的裸仓库登记到 mirror 布局下（测试与本地演练用）。
//
// 若目标不存在且 source 是有效裸仓库，则用 clone --mirror 复制到布局位置。
func (m *Mirror) EnsureLocal(ctx context.Context, repo resolve.Canonical, source string) (EnsureResult, error) {
	path := m.PathFor(repo)
	unlock := m.lockPath(path)
	defer unlock()

	if m.Exists(repo) {
		return EnsureResult{Path: path, Created: false}, nil
	}
	if err := git.CloneMirror(ctx, m.opts, source, path); err != nil {
		return EnsureResult{Path: path}, err
	}
	return EnsureResult{Path: path, Created: true}, nil
}

// lockPath 返回按路径串行化的解锁函数。
func (m *Mirror) lockPath(path string) func() {
	key := strings.ToLower(filepath.Clean(path))
	m.mu.Lock()
	mu, ok := m.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		m.locks[key] = mu
	}
	m.mu.Unlock()

	mu.Lock()
	return mu.Unlock
}
