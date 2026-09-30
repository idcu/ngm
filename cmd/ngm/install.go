package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

const installUsage = `ngm install — resolve dependencies and produce ngm.lock

USAGE:
  ngm install [--dir=<dir>] [--digest] [--frozen-lockfile] [--offline]

FLAGS:
  --dir               project directory containing ngm.json (default: .)
  --digest            also print each dependency's archiveDigest
  --frozen-lockfile   never resolve a new ref and never write ngm.lock
  --offline           never touch the network

BEHAVIOR (architecture/locking.md §更新策略):
  no ngm.lock   resolve from ngm.json → write lock → populate content store
  has ngm.lock  trust the lock (do NOT re-resolve refs); verify it still matches
                ngm.json, and make sure the content store is populated

CI MODES (v0.2 F):
  --frozen-lockfile         fail (exit 3) if ngm.lock is missing or does not
                            match ngm.json. Downloading is still allowed: a cold
                            content store may fetch. What it forbids is deciding
                            a new commit - that must come from the committed lock.
  --offline                 no network at all. A cold content store and a cold
                            mirror both fail (exit 4): "offline" must never mean
                            "installed something slightly different".
  --frozen-lockfile --offline
                            fully reproducible offline install - the CI default.
                            Needs ngm.lock committed and a warm content store.

EXIT CODES:
  0  installed
  3  --frozen-lockfile and the lock is missing or out of sync with ngm.json
  4  --offline and a required resource is not available locally
`

// runInstall 处理 `ngm install`。
//
// 与 `ngm update` 的分工（locking.md §更新策略）：
//
//	install  尊重既有 lock：不重新解析 ref，只确保内容落地
//	update   重新解析 ref → 新 commit → 新 digest → 刷新 lock
func runInstall(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("install")
	dirFlag := fs.String("dir", ".", "project directory")
	digestFlag := fs.Bool("digest", false, "also print each archiveDigest")
	frozen := fs.Bool("frozen-lockfile", false, "never resolve a new ref or write ngm.lock")
	offline := fs.Bool("offline", false, "never touch the network")
	fs.Usage = func() { fmt.Fprint(stderr, installUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "digest", Bool: true},
		{Name: "frozen-lockfile", Bool: true}, {Name: "offline", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, installUsage)
		return 3
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	roots := toDepSpecs(pf.Dependencies)

	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// --frozen-lockfile 是 CI 的核心承诺："装出来的东西必须与提交的 lock 一致"。
	// 因此缺 lock、或 lock 与声明不一致，都必须**失败**（exit 3）而不是退化成
	// 重新解析——后者会让 CI 在没人改 lock 的情况下装出别的内容，
	// 而"lock 已提交、安装即可复现"这个前提也就被悄悄绕过了。
	if *frozen {
		if lf == nil {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeConfigInvalid,
				"--frozen-lockfile: no ngm.lock at "+env.LockPath(),
				"commit ngm.lock to the repository, or run `ngm install` without --frozen-lockfile to create it"))
		}
		if missing := lockManifestMismatch(lf, roots); len(missing) > 0 {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeConfigInvalid,
				"--frozen-lockfile: ngm.lock does not match ngm.json ("+strings.Join(missing, ", ")+")",
				"run `ngm update` and commit the lock, or drop --frozen-lockfile"))
		}
	}
	// 离线且没有 lock：无从解析（读上游 manifest 需要网络），也不能凭空造一个
	if *offline && lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeGitFetch,
			"--offline: no ngm.lock to install from",
			"commit ngm.lock; an offline install can only materialise what the lock already pins"))
	}

	// 决策：lock 是否仍与 ngm.json 一致？
	if lf != nil {
		missing := lockManifestMismatch(lf, roots)
		if len(missing) == 0 {
			return installFromLock(ctx, env, pf, lf, *digestFlag, *offline, stdout, stderr)
		}
		fmt.Fprintf(stdout,
			"ngm.lock does not match ngm.json (%s); re-resolving\n",
			strings.Join(missing, ", "))
	}

	return installFresh(ctx, env, pf, roots, *digestFlag, *offline, stdout, stderr)
}

// materializeVendor 落地 vendor 树并写出 ngm.mappings.json。
//
// vendor 与 mappings 由同一份 content 子树派生，因此"映射指向的路径"
// 与"vendor 里真实的文件"必然一致。
func materializeVendor(env *projectEnv, pf *config.ProjectFile, items []digestNode, stderr io.Writer) error {
	mr, err := env.MaterializeVendorAndMappings(pf, items)
	if err != nil {
		return err
	}
	for _, w := range mr.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	return mappings.Write(mappings.Find(env.ProjectDir), mr.Mappings)
}

// lockManifestMismatch 返回 ngm.json 中无法由现有 lock 满足的声明。
//
// 判定口径：根声明必须在 lock 中存在，且 ref / refType 一致——
// 否则说明 ngm.json 被改动过，lock 已过期。
//
// 只检查**根声明**：lock 还包含传递依赖，它们的增减由重新解析决定。
func lockManifestMismatch(lf *lock.File, roots []resolve.DepSpec) []string {
	var out []string
	for _, r := range roots {
		d, ok := lf.Find(r.Name, r.SubPath)
		if !ok {
			out = append(out, r.Name+" (missing from lock)")
			continue
		}
		if d.Ref != r.Ref || resolve.RefType(d.RefType) != r.RefType {
			out = append(out, r.Name+" ("+d.Ref+" ("+d.RefType+") → "+r.Ref+" ("+string(r.RefType)+"))")
		}
	}
	return out
}

// installFresh 从 ngm.json 完整解析并生成 lock。
func installFresh(ctx context.Context, env *projectEnv, pf *config.ProjectFile, roots []resolve.DepSpec, showDigest, offline bool, stdout, stderr io.Writer) int {
	// 供应链策略：白名单在解析阶段生效（ADR-009），命中即 exit 3 并附来源链
	pol, perr := projectPolicy(pf)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	opts := env.GraphOptions()
	opts.CheckRepo = pol.CheckRepo
	if offline {
		// 解析要读上游 manifest，离线时只能来自本地 mirror（与 verify 同一语义）
		opts.EnsureMirror = offlineEnsureMirror(env)
	}

	g, err := resolve.ResolveGraph(ctx, roots, opts)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	// 晾晒期只能在解析**之后**判定：时间源是 commit 的 committer date
	if err := checkReleaseAges(ctx, env, g, pol); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 上游把 Git 依赖写在 package.json 等情况：提示但不阻断
	for _, w := range g.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	lf := lock.NewFile()
	now := time.Now()
	items := make([]digestNode, 0, len(g.Nodes))

	for _, n := range g.Nodes {
		dg, derr := git.BuildArchiveDigest(ctx, env.GitOpts, n.MirrorPath, n.Commit)
		if derr != nil {
			return runErr(ctx, stdout, stderr, derr)
		}

		if _, perr := env.Store.Put(ctx, env.GitOpts, n.MirrorPath, vendor.Meta{
			Repo:    n.Name,
			Commit:  n.Commit,
			Digest:  dg,
			SubPath: n.SubPath,
		}); perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		lf.Dependencies = append(lf.Dependencies, lock.Dependency{
			Name:          n.Name,
			Ref:           n.Ref,
			RefType:       string(n.RefType),
			Commit:        n.Commit,
			ArchiveDigest: dg,
			ResolvedAt:    lock.FormatResolvedAt(now),
			VendorPath:    n.Repo.MirrorRelPath(),
			SubPath:       n.SubPath,
		})
		items = append(items, digestNodeFromResolved(n, dg))
		printResolved(stdout, n, dg, showDigest)
	}

	// 因 root wins 被忽略的传递声明：必须在整张图展开完成后输出
	// （IgnoredRefs 在展开过程中逐步填充，循环内打印会漏掉后填充的）
	for _, n := range g.Nodes {
		for _, ig := range n.IgnoredRefs {
			fmt.Fprintf(stderr, "note: %s\n", ig)
		}
	}

	// vendor 落地 + mappings（从同一份 content 派生，保证指向一致）
	if err := materializeVendor(env, pf, items, stderr); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	lf.Sort()
	if err := lock.Write(env.LockPath(), lf); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	fmt.Fprintf(stdout, "\ninstalled %d dependencies; wrote %s and %s\n",
		len(lf.Dependencies), env.LockPath(), mappings.FileName)
	return 0
}

// installFromLock 尊重既有 lock：不重新解析 ref，只确保内容就绪。
//
// 这是 v0.1 的可复现性核心——同一 lock 在不同机器上必须安装出相同内容。
func installFromLock(ctx context.Context, env *projectEnv, pf *config.ProjectFile, lf *lock.File, showDigest, offline bool, stdout, stderr io.Writer) int {
	items := make([]digestNode, 0, len(lf.Dependencies))
	fromStore := 0

	// --offline 的"绝不触网"在这里落地：换成一个只认既有 mirror 的实现，
	// mirror 冷就直接 exit 4（而不是退化成 clone）。
	ensure := env.EnsureMirror
	if offline {
		ensure = offlineEnsureMirror(env)
	}

	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]

		repo, perr := resolve.ParseSlug(d.Name)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		// content store 命中即直接落地 vendor：**不需要 mirror，也就不需要网络**。
		//
		// 这不是性能优化，而是承诺问题。locking.md 明确写着"配合 content store 或
		// vendor，环境无网络也能安装与构建"——若命中时仍然 fetch，无网络的 CI 会
		// 在**什么都没变**的情况下失败，而"什么都没变"正是 CI 最常见的场景。
		if env.Store.Has(d.ArchiveDigest) {
			fromStore++
			items = append(items, digestNodeFromLock(d, repo))
			fmt.Fprintf(stdout, "%s@%s (%s) → %s\n", d.Name, d.Ref, d.RefType, d.Commit)
			if showDigest {
				fmt.Fprintf(stdout, "  archiveDigest: %s\n", d.ArchiveDigest)
			}
			continue
		}

		// 未命中：需要 mirror（已就绪时只做增量 fetch，缺失时才 clone）
		mirrorPath, merr := ensure(ctx, repo)
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}

		// lock 的 commit 必须在 mirror 中可解析
		ok, cerr := git.CommitExists(ctx, env.GitOpts, mirrorPath, d.Commit)
		if cerr != nil {
			return runErr(ctx, stdout, stderr, cerr)
		}
		if !ok {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeGitFetch,
				d.Name+": commit "+git.ShortSHA(d.Commit)+" from ngm.lock is not present in the local mirror",
				"the mirror is incomplete or the lock points at a removed commit; "+
					"re-fetch (delete ~/.ngm/mirror/<host>/<org>/<repo>.git) or run `ngm update`"))
		}

		// Put 会重新计算 digest 并与 lock 比对，因此"lock 与内容不符"在这里被拒绝（exit 2）
		if _, perr := env.Store.Put(ctx, env.GitOpts, mirrorPath, vendor.Meta{
			Repo:    d.Name,
			Commit:  d.Commit,
			Digest:  d.ArchiveDigest,
			SubPath: d.SubPath,
		}); perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		items = append(items, digestNodeFromLock(d, repo))
		fmt.Fprintf(stdout, "%s@%s (%s) → %s\n", d.Name, d.Ref, d.RefType, d.Commit)
		if showDigest {
			fmt.Fprintf(stdout, "  archiveDigest: %s\n", d.ArchiveDigest)
		}
	}

	// vendor 落地 + mappings（沿用 lock 的 digest，不重新解析）
	if err := materializeVendor(env, pf, items, stderr); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 报告命中来源：用户需要一眼看出这次安装是不是完全离线的
	source := fmt.Sprintf("all %d served from the content store (no git access)", len(lf.Dependencies))
	if fromStore < len(lf.Dependencies) {
		source = fmt.Sprintf("%d from the content store, %d fetched",
			fromStore, len(lf.Dependencies)-fromStore)
	}
	fmt.Fprintf(stdout, "\ninstalled %d dependencies from %s (lock respected; %s); wrote %s\n",
		len(lf.Dependencies), env.LockPath(), source, mappings.FileName)
	return 0
}

// printResolved 打印一条解析结果。
func printResolved(w io.Writer, n *resolve.Node, dg string, showDigest bool) {
	pathNote := ""
	if n.SubPath != "" {
		pathNote = "#" + n.SubPath
	}
	fmt.Fprintf(w, "%s%s@%s (%s) → %s\n", n.Name, pathNote, n.Ref, n.RefType, n.Commit)
	if showDigest {
		fmt.Fprintf(w, "  archiveDigest: %s\n", dg)
	}
}
