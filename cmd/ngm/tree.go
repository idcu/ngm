package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/observability"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/supplychain"
	"github.com/idcu/ngm/internal/vendor"
)

const treeUsage = `ngm tree — print the dependency tree

USAGE:
  ngm tree [--dir=<dir>] [--osv] [--offline] [--json]

FLAGS:
  --dir       project directory containing ngm.json (default: .)
  --osv       also mark known vulnerabilities (✗) by querying OSV.dev
  --offline   never touch the network (OSV results come from the 24h cache only)
  --json      write a machine-readable report to stdout (CI should use this)

HOW IT WORKS:
  ngm.lock is a flat list and carries no topology, so "who pulled in whom"
  only exists while resolving. tree rebuilds the graph from ngm.json by
  reading each dependency's manifest from the local mirror - with a warm
  mirror this needs no network at all.

MARKERS:
  ⚠  the ref no longer points at the commit pinned in ngm.lock
  ↺  a cycle was found and is not expanded
  ✗  known vulnerabilities (only with --osv; without it nothing is claimed)

EXIT CODES:
  0  tree printed
  1  a vulnerability was found (only with --osv)
  3  configuration or lock error
  4  Git or network failure (including --offline with a cold mirror)
`

// runTree 处理 `ngm tree`。
func runTree(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("tree")
	dirFlag := fs.String("dir", ".", "project directory")
	osv := fs.Bool("osv", false, "mark known vulnerabilities from OSV.dev")
	offline := fs.Bool("offline", false, "never touch the network")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, treeUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "osv", Bool: true}, {Name: "offline", Bool: true}, {Name: "json", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, treeUsage)
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
	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 重建依赖图：tree 要的是"谁引入了谁"，lock 里没有这个信息
	g, err := resolve.ResolveGraph(ctx, toDepSpecs(pf.Dependencies), env.GraphOptions())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	for _, w := range g.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	entries := observability.BuildTree(g, lf)
	rep := &observability.TreeReport{
		Project:      projectLabel(pf),
		Entries:      entries,
		Dependencies: g.Len(),
		Drifted:      observability.CountDrifted(entries),
	}

	exit := 0
	if *osv {
		pol, perr := projectPolicy(pf)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		if err := observability.MarkOSV(ctx, entries, supplychain.OSVConfig{
			CacheDir: vendor.NewCache(env.Layout.CacheRoot()).OSVRoot(),
			BaseURL:  os.Getenv("NGM_OSV_URL"),
			Offline:  *offline,
			// 与 audit 同一条取数路径，因此也是同一个 net 门禁（v0.5 C 组）——
			// 用权限策略，不是上面那份供应链策略
			CheckNet: env.Policy.CheckNet,
		}, pol.IgnoredSeverities()); err != nil {
			return runErr(ctx, stdout, stderr, err)
		}
		rep.OSVChecked = true
		if countVulns(entries) > 0 {
			exit = 1
		}
	}

	if *jsonOut {
		data, merr := rep.Marshal()
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "write report: %v\n", werr)
			return 1
		}
		return exit
	}

	rep.Render(stdout)
	return exit
}

func countVulns(entries []*observability.TreeEntry) int {
	n := 0
	for _, e := range entries {
		n += len(e.Vulns)
		n += countVulns(e.Children)
	}
	return n
}

// projectLabel 取树的根节点名：项目名优先，缺失时退回通用名。
func projectLabel(pf *config.ProjectFile) string {
	if pf != nil && pf.Name != "" {
		return pf.Name
	}
	return "project"
}
