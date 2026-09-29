package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

const updateUsage = `ngm update — re-resolve dependency refs to commits

USAGE:
  ngm update [<dep>] [--all] [--dir=<dir>] [--offline] [--digest] [--store]

ARGS:
  <dep>       dependency slug (e.g. github:org/repo); repeatable

FLAGS:
  --all       update every dependency in ngm.json
  --dir       project directory containing ngm.json (default: .)
  --offline   never touch the network; require a warm local mirror (exit 4 if missing)
  --digest    also compute and print the archiveDigest (ADR-008)
  --store     populate the content store with the resolved content (implies --digest)

NOTES:
  M1 阶段本命令完成"refType → commit"的重新解析并输出结果；
  ngm.lock 的写入与 root-wins 冲突裁决属于 M3。
`

// runUpdate 处理 `ngm update [<dep>|--all]`。
//
// 流程：
//  1. 读取 ngm.json 的依赖声明
//  2. 选定目标（显式 <dep> 列表或 --all）
//  3. 确保 mirror 就绪（非 --offline 时允许 clone/fetch；--offline 时要求已存在）
//  4. 对每个目标执行 refType → commit 解析
//  5. 输出解析结果（M3 起会同时写入 ngm.lock）
func runUpdate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("update")
	all := fs.Bool("all", false, "update every dependency")
	dirFlag := fs.String("dir", ".", "project directory")
	offline := fs.Bool("offline", false, "never touch the network")
	digestFlag := fs.Bool("digest", false, "also compute and print archiveDigest")
	storeFlag := fs.Bool("store", false, "populate the content store (implies --digest)")
	maxConc := fs.Int("concurrency", 4, "max parallel resolutions")
	fs.Usage = func() { fmt.Fprint(stderr, updateUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "all", Bool: true}, {Name: "dir"}, {Name: "offline", Bool: true},
		{Name: "digest", Bool: true}, {Name: "store", Bool: true}, {Name: "concurrency"},
	})); err != nil {
		return 3
	}
	if !*all && fs.NArg() == 0 {
		fmt.Fprint(stderr, updateUsage)
		return 3
	}
	if *all && fs.NArg() > 0 {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"--all cannot be combined with explicit dependency names",
			"pass either `--all` or a list of dependency slugs"))
	}

	// 1) 运行环境 + 项目声明
	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 2) 选定目标（用于**输出过滤**；解析总是覆盖完整图）
	selected, err := selectTargets(pf, fs.Args(), *all)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	selectedKeys := make(map[string]bool, len(selected))
	for _, d := range selected {
		selectedKeys[resolve.DepSpec{Name: d.Name, SubPath: d.Path}.Key()] = true
	}

	// 3) 完整图解析。
	//
	// 为什么 update 也解析整张图：lock 必须与 ngm.json 的**声明闭包**一致。
	// 若只刷新被点名的条目，`ngm remove` 留下的孤儿会永远留在 lock 里。
	// update 的语义就是"重新计算锁定"，因此它以图结果**重建** lock。
	opts := env.GraphOptions()
	if *offline {
		opts.EnsureMirror = offlineEnsureMirror(env)
	}
	// 供应链策略在解析阶段生效（ADR-009）。它与 --offline 无关：
	// 策略判定是纯本地的，不该因为断网就被跳过——那会让离线成为绕过策略的通道。
	check, perr := policyChecker(pf)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	opts.CheckRepo = check
	g, rerr := resolve.ResolveGraph(ctx, toDepSpecs(pf.Dependencies), opts)
	if rerr != nil {
		return runErr(ctx, stdout, stderr, rerr)
	}
	for _, w := range g.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	if len(g.Nodes) == 0 {
		lf := lock.NewFile()
		if werr := lock.Write(env.LockPath(), lf); werr != nil {
			return runErr(ctx, stdout, stderr, werr)
		}
		fmt.Fprintf(stdout, "no dependencies to update; wrote empty %s\n", env.LockPath())
		return 0
	}

	// 4) 计算 digest、落地 content store、重建 lock
	lf := lock.NewFile()
	now := time.Now()
	printed := 0
	items := make([]digestNode, 0, len(g.Nodes))

	for _, n := range g.Nodes {
		dg, derr := git.BuildArchiveDigest(ctx, env.GitOpts, n.MirrorPath, n.Commit)
		if derr != nil {
			return runErr(ctx, stdout, stderr, derr)
		}

		putRes, perr := env.Store.Put(ctx, env.GitOpts, n.MirrorPath, vendor.Meta{
			Repo:    n.Name,
			Commit:  n.Commit,
			Digest:  dg,
			SubPath: n.SubPath,
		})
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		items = append(items, digestNodeFromResolved(n, dg))

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

		// 输出只聚焦选中项（--all 时即全部）
		if !*all && !selectedKeys[n.Key] {
			continue
		}
		printed++
		printResolved(stdout, n, dg, *digestFlag || *storeFlag)
		if *storeFlag {
			status := "stored"
			if putRes.AlreadyPresent {
				status = "already present"
			}
			fmt.Fprintf(stdout, "  content store: %s (%s)\n", putRes.Path, status)
		}
	}

	// 5) 被根声明覆盖的传递声明：必须在**整张图展开完成后**才能完整输出
	// （IgnoredRefs 是在展开过程中逐步填充的）
	for _, n := range g.Nodes {
		for _, ig := range n.IgnoredRefs {
			fmt.Fprintf(stderr, "note: %s\n", ig)
		}
	}

	// vendor 落地 + mappings
	if merr := materializeVendor(env, pf, items, stderr); merr != nil {
		return runErr(ctx, stdout, stderr, merr)
	}

	lf.Sort()
	if werr := lock.Write(env.LockPath(), lf); werr != nil {
		return runErr(ctx, stdout, stderr, werr)
	}

	fmt.Fprintf(stdout, "\nupdated %d dependency(ies); wrote %s and %s\n",
		printed, env.LockPath(), mappings.FileName)
	_ = maxConc // 层内并发已由 resolve.ResolveGraph 提供；此 flag 预留给未来的解析阶段
	return 0
}

// offlineEnsureMirror 返回一个"只在 mirror 已存在时才可用"的 EnsureMirror 实现。
//
// `--offline` 的契约是"禁止一切网络访问"：mirror 缺失时立即失败（exit 4），
// 而不是退化为 clone。
func offlineEnsureMirror(env *projectEnv) func(context.Context, resolve.Canonical) (string, error) {
	return func(_ context.Context, repo resolve.Canonical) (string, error) {
		if !env.Mirror.Exists(repo) {
			return "", errs.New(
				errs.CodeGitFetch,
				fmt.Sprintf("--offline: no local mirror for %s", repo.Slug()),
				"run once without --offline to populate the mirror, or use `ngm install`")
		}
		return env.Mirror.PathFor(repo), nil
	}
}

// selectTargets 依据参数选定要更新的依赖。
//
// 匹配逻辑：显式 <dep> 参数按归一化后的仓库地址比较（name 大小写与 host 折叠不敏感），
// 因此 `github.com:org/repo` 与 `github:org/repo` 都能命中同一条声明。
func selectTargets(pf *config.ProjectFile, names []string, all bool) ([]config.Dependency, error) {
	if all {
		out := make([]config.Dependency, len(pf.Dependencies))
		copy(out, pf.Dependencies)
		return out, nil
	}

	var out []config.Dependency
	for _, name := range names {
		want, err := resolve.ParseSlug(name)
		if err != nil {
			return nil, err
		}
		found := false
		for _, dep := range pf.Dependencies {
			got, perr := resolve.ParseSlug(dep.Name)
			if perr != nil {
				continue
			}
			if got.Equal(want) {
				out = append(out, dep)
				found = true
				break
			}
		}
		if !found {
			return nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("dependency %q is not declared in ngm.json", name),
				"run `ngm add` first, or use `ngm update --all`")
		}
	}
	return out, nil
}
