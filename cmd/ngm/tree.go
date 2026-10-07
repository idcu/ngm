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
  ngm tree [--dir=<dir>] [--osv] [--offline] [--json] [--all]

FLAGS:
  --dir       project directory containing ngm.json (default: .)
  --osv       also mark known vulnerabilities (✗) by querying OSV.dev
  --offline   never touch the network (OSV results come from the 24h cache only)
  --json      write a machine-readable report to stdout (CI should use this)
  --all       expand every entry, lifting the default budget (see below)

HOW IT WORKS:
  ngm.lock is a flat list and carries no topology, so "who pulled in whom"
  only exists while resolving. tree rebuilds the graph from ngm.json by
  reading each dependency's manifest from the local mirror - with a warm
  mirror this needs no network at all.

  The tree is expanded PER PATH, so the same dependency appears once for every
  route that reaches it - and the entry count can be EXPONENTIAL in a wide graph
  (the same measurement as ngm why: 41 nodes produced over a million paths).
  The graph's shape comes from the upstream manifests. So at most 4096 entries
  are expanded by default, and the report SAYS SO when it stops ("树不完整" in
  text, entriesTruncated in --json) - truncation is never silent. --all lifts
  the budget.

MARKERS:
  ⚠  the ref no longer points at the commit pinned in ngm.lock
  ↺  a cycle was found and is not expanded
  …  the entry budget ran out here; children are not listed
  ✗  known vulnerabilities (only with --osv; without it nothing is claimed)

EXIT CODES:
  0  tree printed
  1  a vulnerability was found (only with --osv)
  3  configuration or lock error
  4  Git or network failure (including --offline with a cold mirror)
`

// runTree 处理 `ngm tree`。
func runTree(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("tree", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	osv := fs.Bool("osv", false, "mark known vulnerabilities from OSV.dev")
	offline := fs.Bool("offline", false, "never touch the network")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	allEntries := fs.Bool("all", false, "expand every entry (lifts the default budget)")
	fs.Usage = func() { fmt.Fprint(stderr, treeUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "osv", Bool: true}, {Name: "offline", Bool: true},
		{Name: "json", Bool: true}, {Name: "all", Bool: true},
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
	//
	// `--offline` 必须**穿进图解析**：解析要读上游 manifest，而离线时它只能来自
	// 本地 mirror。不穿它的后果实测过（v0.21）：解析仍会发起取数意图，权限层于是
	// 把它判成"权限被拒"（exit 3）——而 tree 自己的 EXIT CODES 承诺的是 4
	// （"Git or network failure，含 --offline 且冷 mirror"）。
	// 于是 `ngm tree --offline` 在最该用它的场景（CI 里禁网）反而退了错的码。
	opts := env.GraphOptions()
	if *offline {
		opts.EnsureMirror = offlineEnsureMirror(env)
	}
	g, err := resolve.ResolveGraph(ctx, toDepSpecs(pf.Dependencies), opts)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	for _, w := range g.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	// 展开默认有预算（见 MaxTreeEntries 与 ADR-024）：树是**按路径展开**的，
	// 条目数随图"宽"指数增长，而图的形状来自上游清单。`--all` 显式解除；
	// 达到预算时报告会**说出来**（EntriesTruncated / 条目上的 Truncated）。
	limit := observability.MaxTreeEntries
	if *allEntries {
		limit = 0
	}
	entries, truncated := observability.BuildTree(g, lf, limit)
	rep := &observability.TreeReport{
		Project:          projectLabel(pf),
		Entries:          entries,
		Dependencies:     g.Len(),
		Drifted:          observability.CountDrifted(entries),
		EntriesTruncated: truncated,
		EntriesLimit:     limit,
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
		// 机器可读那一侧也要有"下一步"（v0.50）：人读那侧从 v0.32 起就有
		// 一行 `→ N known vulnerability(ies); run ngm audit …`，而 JSON 里此前没有——
		// 读 `--json` 的脚本只能看到漏洞数组，看不到该做什么。同一条句子同源。
		if n := observability.VulnCount(entries); n > 0 {
			rep.Remediation = observability.VulnRemediation(n)
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
