package vendor

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
)

// ModuleCache 是层 1.5：按 repo 维护的 partial clone（`--filter=blob:none`）。
//
// 它与 Mirror 的边界（ADR-034）：
//   - Mirror 承诺“任意 ref 可离线解析、整树可重放”；
//   - ModuleCache 只承诺“lock 钉住的那个 commit 可解析，模块子树可重放”；
//   - 它可丢弃、可重建，不冒充 mirror。
type ModuleCache struct {
	root string
	opts git.Options

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewModuleCache 创建 module cache 管理器。root 通常是 `~/.ngm/module-cache`。
func NewModuleCache(root string, opts git.Options) *ModuleCache {
	return &ModuleCache{
		root:  root,
		opts:  opts,
		locks: make(map[string]*sync.Mutex),
	}
}

// Root 返回 module cache 根目录。
func (c *ModuleCache) Root() string { return c.root }

// PathFor 返回该仓库在 module cache 中的绝对路径（裸仓库目录）。
func (c *ModuleCache) PathFor(repo resolve.Canonical) string {
	rel := filepath.FromSlash(repo.MirrorRelPath() + ".git")
	return filepath.Join(c.root, rel)
}

// Exists 报告该 repo 的 module cache 是否**可用**（filter 真的生效）。
func (c *ModuleCache) Exists(repo resolve.Canonical) bool {
	return git.IsPartialClone(c.PathFor(repo))
}

// HasCommit 报告缓存里是否已能解析某个 commit（离线可判定）。
func (c *ModuleCache) HasCommit(ctx context.Context, repo resolve.Canonical, commit string) (bool, error) {
	if !c.Exists(repo) {
		return false, nil
	}
	return git.CommitExists(ctx, c.opts, c.PathFor(repo), commit)
}

// Ensure 确保缓存存在且包含 lock 钉住的那个 commit。
//
// remoteURL 为空时由 repo.CloneURL(protocol) 生成；ref/refType 用于在服务端
// 不允许按 SHA fetch 时回退到 tag/branch。最终仍会校验 commit 可解析。
func (c *ModuleCache) Ensure(ctx context.Context, repo resolve.Canonical, remoteURL string,
	protocol resolve.Protocol, commit, ref, refType string) (EnsureResult, error) {
	path := c.PathFor(repo)
	unlock := c.lockPath(path)
	defer unlock()

	if remoteURL == "" {
		p := protocol
		if !p.IsValid() {
			p = resolve.ProtocolHTTPS
		}
		remoteURL = repo.CloneURL(p)
	}
	// 顺序与 Mirror 一致：先 run:git，再 net。读配置/起子进程本身就要 git。
	if err := git.CheckRunAccess(c.opts.Policy, "git"); err != nil {
		return EnsureResult{Path: path}, err
	}

	if !c.Exists(repo) {
		if _, err := os.Stat(path); err == nil {
			return EnsureResult{Path: path}, errs.New(errs.CodeGitFetch,
				"module cache path exists but is not a partial clone: "+path,
				"delete it and re-run; ngm will re-create the module cache")
		}
		if err := git.ClonePartial(ctx, c.opts, remoteURL, path); err != nil {
			return EnsureResult{Path: path}, err
		}
		return EnsureResult{Path: path, Created: true}, nil
	}

	if ok, err := c.HasCommit(ctx, repo, commit); err != nil {
		return EnsureResult{Path: path}, err
	} else if ok {
		return EnsureResult{Path: path}, nil
	}

	if err := git.CheckNetAccess(c.opts.Policy, remoteURL); err != nil {
		return EnsureResult{Path: path}, err
	}
	if err := git.FetchPartialCommit(ctx, c.opts, path, commit, ref, refType); err != nil {
		return EnsureResult{Path: path}, err
	}
	return EnsureResult{Path: path}, nil
}

// ModuleCacheEntry 是 `ngm store usage` 里的一行（一个 partial clone 缓存仓库）。
type ModuleCacheEntry struct {
	// Repo 是相对 module cache 根的路径（斜杠分隔），如 `github.com/org/repo.git`。
	Repo string
	// Bytes 是该目录占用的字节合计（真实占用）。
	Bytes int64
	// PackBytes 是 `objects/pack/*.pack` 的字节和——**已收成包的历史**。
	//
	// partial clone 的意义主要是“blob 按需取”，所以 tree/commit 往往比 pack 更受关注；
	// 报 pack 是为了与层 1 同一口径，让“缓存里到底有多少历史本体”可被回答。
	PackBytes int64
	// NotPartial 为 true 表示这个目录**不是可用的 partial clone**
	// （`git.IsPartialClone` 为假）：它有体积，却可能Filter 没生效/被删过配置。
	NotPartial bool
}

// ModuleCacheUsage 是层 1.5 的只读报告（`ngm store usage` 的 module cache 段）。
//
// 与 Mirror/ContentStore 同一条纪律：**不修改任何东西**，不跑 git、不触网。
type ModuleCacheUsage struct {
	Root   string
	Exists bool
	// Entries 按 Repo 升序——顺序必须确定（与 MirrorUsage 同一条理由）。
	Entries []ModuleCacheEntry
	// Repos / Bytes / PackBytes / NotPartialCount 是逐行小计。
	Repos           int
	Bytes           int64
	PackBytes       int64
	NotPartialCount int
}

// Usage 只读地统计 module cache 的占用。
func (c *ModuleCache) Usage() (ModuleCacheUsage, error) {
	out := ModuleCacheUsage{Root: c.root}
	if _, err := os.Stat(c.root); err == nil {
		out.Exists = true
	} else if !os.IsNotExist(err) {
		return out, errs.Wrap(errs.CodeConfigInvalid, "read the module cache root",
			"check directory permissions on the module cache", err)
	}
	if !out.Exists {
		return out, nil
	}

	var repos []string
	werr := filepath.WalkDir(c.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == c.root {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".git") {
			return nil
		}
		rel, rerr := filepath.Rel(c.root, p)
		if rerr != nil {
			return rerr
		}
		repos = append(repos, filepath.ToSlash(rel))
		return fs.SkipDir
	})
	if werr != nil {
		return out, errs.Wrap(errs.CodeConfigInvalid, "walk the module cache root",
			"check directory permissions on the module cache", werr)
	}
	sort.Strings(repos)
	out.Repos = len(repos)

	for _, rel := range repos {
		dir := filepath.Join(c.root, filepath.FromSlash(rel))
		e := ModuleCacheEntry{Repo: rel, NotPartial: !git.IsPartialClone(dir)}
		var err error
		if e.Bytes, err = dirBytes(dir); err != nil {
			return out, err
		}
		if e.PackBytes, err = packFileBytes(dir); err != nil {
			return out, err
		}
		if e.NotPartial {
			out.NotPartialCount++
		}
		out.Bytes += e.Bytes
		out.PackBytes += e.PackBytes
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

func (c *ModuleCache) lockPath(path string) func() {
	key := strings.ToLower(filepath.Clean(path))
	c.mu.Lock()
	defer c.mu.Unlock()
	mu, ok := c.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		c.locks[key] = mu
	}
	mu.Lock()
	return mu.Unlock
}
