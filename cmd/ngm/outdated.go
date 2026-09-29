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

const outdatedUsage = `ngm outdated — which dependencies have newer versions?

USAGE:
  ngm outdated [--dir=<dir>] [--offline] [--json]

FLAGS:
  --dir       project directory containing ngm.json (default: .)
  --offline   never touch the network
  --json      write a machine-readable report to stdout (CI should use this)

HOW REFS ARE CHECKED:
  tag     lists the tags in the local mirror; newest wins (semver first,
          otherwise by tag creation date) - works offline with a warm mirror
  branch  needs a fetch to learn the tip; with --offline it is reported as
          unknown, never as "up to date"
  commit  pinned to a commit, so there is nothing to compare against

EXIT CODES:
  0  report written (this command reports; it does not gate)
  3  configuration or lock error

A dependency that could not be checked is reported as ` + "`unknown`" + `.
That is a different conclusion from "no update found", and the report keeps
them apart on purpose.
`

// runOutdated 处理 `ngm outdated`。
func runOutdated(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("outdated")
	dirFlag := fs.String("dir", ".", "project directory")
	offline := fs.Bool("offline", false, "never touch the network")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, outdatedUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "offline", Bool: true}, {Name: "json", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, outdatedUsage)
		return 3
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no ngm.lock found at "+env.LockPath(),
			"run `ngm install` to resolve dependencies and create the lock"))
	}

	rep, err := observability.CheckOutdated(ctx, lf, observability.OutdatedOptions{
		GitOpts: env.GitOpts,
		Offline: *offline,
		EnsureMirror: func(ctx context.Context, slug string) (string, error) {
			repo, perr := resolve.ParseSlug(slug)
			if perr != nil {
				return "", perr
			}
			return env.EnsureMirror(ctx, repo)
		},
	})
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
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
