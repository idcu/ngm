package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

const removeUsage = `ngm remove — remove a dependency from ngm.json

USAGE:
  ngm remove <dep> [--dir=<dir>]

ARGS:
  <dep>   dependency slug (e.g. github:org/repo)

FLAGS:
  --dir   project directory containing ngm.json (default: .)

NOTES:
  This command edits the *declaration* only. ngm.lock records what was resolved,
  so it is refreshed by "ngm install" — that keeps a single writer for the lock
  and avoids a half-updated state when the graph cannot be re-resolved.
`

// runRemove 处理 `ngm remove <dep>`。
//
// 职责边界：只改 ngm.json（声明源）。lock 的刷新交给 `ngm install`——
// 因为移除一个直接依赖可能连带移除若干传递依赖，那需要重新解析整张图，
// 而"谁改 lock"只应该有一个入口。
func runRemove(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("remove")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, removeUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{{Name: "dir"}})); err != nil {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, removeUsage)
		return 3
	}
	name := fs.Arg(0)

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	removed, err := pf.RemoveDependency(name)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if !removed {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("dependency %q is not declared in ngm.json", name),
			"check the slug (e.g. `github:org/repo`), or run `ngm config show` to list declarations"))
	}

	if err := config.WriteProjectFile(env.ManifestPath(), pf); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	fmt.Fprintf(stdout, "removed %s from %s\n", name, env.ManifestPath())
	fmt.Fprintln(stdout, "next: run `ngm install` to refresh ngm.lock")
	return 0
}
