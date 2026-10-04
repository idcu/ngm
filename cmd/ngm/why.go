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

  The path count can be EXPONENTIAL in a wide graph (41 nodes produced over a
  million paths in a measurement), and the graph's shape comes from the upstream
  manifests, not from this project. So at most 64 paths are enumerated by
  default, and the report SAYS SO when it stops ("还有更多未列出" in text,
  pathsTruncated in --json) - truncation is never silent. --all lifts the cap.

EXIT CODES:
  0  the dependency is in the graph
  3  configuration error, or the dependency is not in the graph
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
		return 3
	}
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

	g, err := resolve.ResolveGraph(ctx, toDepSpecs(pf.Dependencies), env.GraphOptions())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
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
		return 0
	}

	rep.Render(stdout)
	return 0
}
