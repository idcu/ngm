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
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
)

// Mirror 管理本地裸仓库镜像（层 1）。
//
// 并发安全：Ensure 对同一 repo 串行化（**进程内**按路径加锁），避免并发 clone 同一目标。
//
// **跨进程并发没有锁**（v0.66 更正，评估报告 P2#10）：此前这里写着"跨进程并发由
// .lock 文件（M3 引入）防护"，而实现里只有进程内 `sync.Mutex`，全仓库既没有
// `O_EXCL` 也没有 flock——注释描述的是意图，不是现状。
//
// v0.101 起这句话的后半段变了（[ADR-031]）：**没有锁，但不是没有防护**——
//
//   - 克隆**原子落地**（`CloneLandingInfix`）：任何进程都不会在最终路径上看到半个
//     mirror，因此"两个进程同时 clone 同一个 repo"的结局是"一个赢、另一个复用它"，
//     而不是"互相踩"；
//   - 每个 mirror 的施工**只在它自己的目录里**发生（原地 fetch 是 git 支持的并发场景：
//     对象写入是内容寻址且幂等的，ref 更新有 git 自己的 per-ref 锁）；
//   - 剩下的窗口只产生**可见的失败**（"另一个进程正在操作这个仓库"），不产生坏数据；
//   - 清理类操作全部带**年龄门槛**（mirror_maintain.go 的 MirrorResidueGrace）。
//
// 跨进程文件锁被**明确拒绝**（ADR-031）：它要平台相关代码 + 一套陈旧锁策略
// （"进程死了"与"进程很慢"在文件系统上不可区分，git 自己的 index.lock 就是被这个
// 坑咬过的经典案例）+ 一个能同时跑两个 ngm 进程的测试基建，而它买到的只是
// **更早的报错**——不是更多的正确性。
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

// Exists 报告该仓库的 mirror 是否**已就绪**。
//
// 判据是 [git.IsMirror]（形状对**且**有镜像 refspec），不是 `IsBareMirror`（只看形状）：
// 一次被中断的克隆是一个形状完好的裸仓库，而它解析不出任何 commit，
// 因此"已就绪"对它来说是假话（v0.101 更正；此前这个函数与它的文档不一致）。
func (m *Mirror) Exists(repo resolve.Canonical) bool {
	return git.IsMirror(m.PathFor(repo))
}

// CloneLandingInfix 是"**正在落地**的克隆"目录名里那一段：
// 最终名是 `<repo>.git`，落地中的名字是 `<repo>.git.clone-<hex>`。
//
// 为什么要有它（v0.101 / [ADR-031]）：`git clone --mirror` 直接落在最终路径上，
// 于是一次被中断的克隆会在**那个位置**留下一个形状完好的裸仓库——v0.100 实测到一个
// **66.82 MiB** 的（对象库为空、`packed-refs` 不存在，而 `HEAD`/`objects/`/`refs/` 全齐，
// 所以它"看起来像个能用的 mirror"）。
//
// 层 2 的清单发布（content_v2.go 的 `publishManifest`）早就是用"临时目录 + rename"
// 解决同一件事的：**任何时刻都不会出现半个可被引用的内容树**。层 1 从此同理。
//
// 名字里带随机后缀而不是只带 pid：同一个目录里可能同时有多个进程在落地，
// 而 pid 重用（或用户手工复制目录）会让两条路径撞在一起。
const CloneLandingInfix = ".clone-"

// landingPathFor 返回本次克隆的落地路径。
//
// **必须与最终路径同目录**：`os.Rename` 的原子性只在同一个文件系统内成立，
// 而 mirror 根下的跨目录 rename 在某些挂载布局下会退化成 copy+delete。
func (m *Mirror) landingPathFor(final string) string {
	return final + CloneLandingInfix + randHex(4)
}

// randHex 返回 n 字节随机数的十六进制（落地路径的后缀）。
//
// 取不到随机数时退回 pid：那仍然比一个常量好（同目录下不同进程不会撞），
// 而这条路径失败得过于不可能，不值得为它设计第二种机制。
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strconv.Itoa(os.Getpid())
	}
	return hex.EncodeToString(b)
}

// removeLanding 删掉一个**我们自己刚建的**落地目录（best-effort）。
//
// 为什么忽略错误：它只在"克隆已经失败"或"对手已经赢了"这两条路上被调用，
// 而那时用户要看的错误是原来那个。清不掉也不丢东西——那个目录是残骸，
// `ngm store prune` 会按年龄门槛（MirrorResidueGrace）收掉它。
func removeLanding(tmp string) { _ = os.RemoveAll(tmp) }

// unusablePathError 说清"这个路径为什么不能用"，并给出路（v0.101）。
//
// 两种形状必须分开报，因为**处置的前提不同**：不是裸仓库的路径可能是用户放在那里的
// 别的东西（说法子必须保守），而"是裸仓库但没走完"是可以精确核对的
// （config 里没有 [git.MirrorRefspec]）。此前两者共用一个说法
// "is not a valid bare repository"，而第二种形状**通不过**那个说法——
// 它是合法裸仓库，只是里面没有镜像。
func unusablePathError(path string) error {
	if git.IsBareMirror(path) {
		return errs.New(errs.CodeGitFetch,
			"mirror clone never finished (no "+git.MirrorRefspec+" in its config): "+path,
			"delete the directory, then re-run — ngm will clone it again")
	}
	return errs.New(errs.CodeGitFetch,
		"mirror path exists but is not a valid bare repository: "+path,
		"remove the directory manually, then re-run")
}

// cloneInto 克隆到一个**落地中**的路径，成功后原子 rename 就位（v0.101 / [ADR-031]）。
//
// 这是层 1 的"看不到半个 mirror"：在 rename 成功之前，最终路径上什么都没有；
// 而 rename 本身是原子的，所以另一个进程要么看到"没有"，要么看到"完整的"。
func (m *Mirror) cloneInto(ctx context.Context, remoteURL, final string) (EnsureResult, error) {
	tmp := m.landingPathFor(final)
	if err := git.CloneMirror(ctx, m.opts, remoteURL, tmp); err != nil {
		// **失败必须收自己的临时目录**：中断留下的那 66.82 MiB 正是"没收"的代价。
		// 进程被强杀时这一步不会执行——那一路由 `ngm store prune` 按年龄门槛兜底。
		removeLanding(tmp)
		return EnsureResult{Path: final}, err
	}
	created, err := landClone(tmp, final)
	if err != nil {
		removeLanding(tmp)
		return EnsureResult{Path: final}, err
	}
	if !created {
		// 对手先落地了：我们这一份是多余的一次下载，清掉它。
		// 删它没有争议——名字是我们独有的，因此它一定是我们自己建的。
		removeLanding(tmp)
	}
	return EnsureResult{Path: final, Created: created}, nil
}

// landClone 把落地中的克隆原子地移到最终路径，并回答"这一份是我们落地的吗"。
//
// rename 失败**不一定是错误**：另一个进程可能刚好先我们一步把它的克隆落地了。
// 那时正确答案是"复用它那份"（并让调用方清掉我们多下的一份），
// 而把这种情形报成失败会让并发的两个 `ngm install` 之一莫名其妙地失败。
func landClone(tmp, final string) (bool, error) {
	if err := os.Rename(tmp, final); err == nil {
		return true, nil
	}
	// 对手赢了 ⇒ 最终路径上应当已经有**一个可用的** mirror。若那里其实不可用，
	// 那就不是"竞争"，而是真的失败了（权限、跨设备 rename、目标被别的东西占住）。
	if git.IsMirror(final) {
		return false, nil
	}
	return false, errs.New(errs.CodeGitFetch,
		"could not land the clone at "+final,
		"check filesystem permissions, then re-run; `ngm store usage` lists the leftovers")
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
		// 目标路径已被占着、却又不是可用的 mirror → 明确报错而不是静默删除用户数据
		// （v0.101 起两种形状分开报，见 unusablePathError）
		if _, err := os.Stat(path); err == nil {
			return EnsureResult{Path: path}, unusablePathError(path)
		}
		return m.cloneInto(ctx, remoteURL, path)
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
	if _, err := os.Stat(path); err == nil {
		// 与 Ensure 同一处置：不删、不覆盖，报出来 + 给出路。
		// （此前这里会走到 CloneMirror 的 "clone target already exists"，
		// 那句话把"目标被占"与"目标是个废墟"说成了同一件事。）
		return EnsureResult{Path: path}, unusablePathError(path)
	}
	return m.cloneInto(ctx, source, path)
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
