package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/observability"
	"github.com/idcu/ngm/internal/resolve"
)

const whyUsage = `ngm why — why is this dependency here?

USAGE:
  ngm why <dep> [--dir=<dir>] [--json] [--all]

ARGS:
  <dep>   dependency slug, e.g. github:org/utils (or github:org/repo#sub for a
          monorepo sub-path)

FLAGS:
  --dir   project directory containing ngm.json (default: .)
  --json  write a machine-readable report to stdout (CI should use this)
  --all   list every path, lifting the default cap (see below)

OUTPUT:
  The paths from ngm.json to the dependency. A dependency pulled in by several
  parents shows several paths - that is the point of the question.

  Since v0.73 the graph is stored in ngm.lock, so this command reads the lock and
  answers WITHOUT touching git (no sub-processes, works offline). If the lock was
  written by an older ngm (no graph yet), it falls back to resolving upstream
  manifests - and the report's last line says which of the two it used.

  The path count can be EXPONENTIAL in a wide graph (41 nodes produced over a
  million paths in a measurement), and the graph's shape comes from the upstream
  manifests, not from this project. So at most 64 paths are enumerated by
  default, and the report SAYS SO when it stops ("还有更多未列出" in text,
  pathsTruncated in --json) - truncation is never silent. --all lifts the cap.

EXIT CODES:
  0  the dependency is in the graph
  3  configuration error, or the dependency is not in the graph
	6  internal failure: the conclusion was reached but could not be delivered (writing stdout, encoding JSON, reading stdin), or ngm panicked
`

// runWhy 处理 `ngm why`。
func runWhy(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("why", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	allPaths := fs.Bool("all", false, "list every path (lifts the default cap)")
	fs.Usage = func() { fmt.Fprint(stderr, whyUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true}, {Name: "all", Bool: true},
	})); err != nil {
		// v0.54：解析失败发生在「命令还不知道 --json」之前——从**原始参数**里问一次。
		failEarly(args, stdout, err.Error(),
			"run `ngm "+fs.Name()+" --help` for the flags it accepts")
		return 3
	}
	ctx = markJSONIfRequested(ctx, *jsonOut)
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, whyUsage)
		return 3
	}
	target := fs.Arg(0)

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

	// v0.73（ADR-028）：lock 里带着依赖图时**不重新解析**——"它从哪来"的答案
	// 早就写进 lock 了，为此再跑一串 git 子进程（且离线不可用）是纯浪费。
	// 两条路径的代价差一个量级，因此来源必须写进报告（WhyReport.Source），
	// 绝不静默回退：旧 lock 没有图时，用户会看到"重新解析"那一行。
	var g *resolve.Graph
	source := observability.WhySourceResolved
	if lf != nil && lf.HasGraph() {
		g = observability.GraphFromLock(lf)
		source = observability.WhySourceLock
	} else {
		var rerr error
		g, rerr = resolve.ResolveGraph(ctx, toDepSpecs(pf.Dependencies), env.GraphOptions())
		if rerr != nil {
			return runErr(ctx, stdout, stderr, rerr)
		}
	}
	node, terr := observability.ResolveTarget(g, target)
	if terr != nil {
		// 不在图里是"问错了对象"，属用法/配置错误：exit 3 而不是静默成功
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			terr.Error(), "run `ngm tree` to see what is actually in the graph", terr))
	}

	// 路径枚举默认有上界（见 MaxWhyPaths 与 ADR-024）：路径数是**指数**的，
	// 而图的形状来自上游清单。`--all` 是显式解除，不是默认行为；
	// 达到上限时报告会**说出来**（PathsTruncated），绝不静默截断。
	limit := observability.MaxWhyPaths
	if *allPaths {
		limit = 0
	}
	paths, truncated := observability.FindPaths(g, node.Key, limit)

	rep := &observability.WhyReport{
		Name:           node.Name,
		Ref:            node.Ref,
		RefType:        string(node.RefType),
		Commit:         node.Commit,
		SubPath:        node.SubPath,
		RootDeclared:   node.RootDeclared,
		Paths:          paths,
		PathsTruncated: truncated,
		PathsLimit:     limit,
		Locked:         observability.LockedFrom(lf, node.Name, node.SubPath),
		Source:         source,
	}

	if *jsonOut {
		data, merr := rep.Marshal()
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "write report: %v\n", werr)
			return errs.CodeInternal.ExitCode()
		}
		return 0
	}

	rep.Render(stdout)
	return 0
}
