package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/errs"
)

const buildUsage = `ngm build — bundle a project with an external engine

USAGE:
  ngm build [<entry>] [--engine=<name>] [--outfile=<path>] [--production] [--dry-run] [--dir=<dir>]

ARGS:
  <entry>   entry file (default: ` + "`main`" + ` from ngm.json)

FLAGS:
  --engine=<name>   engine to use (default: engines.bundle in ngm.json, else esbuild)
  --outfile=<path>  write the bundle there (default: stdout, exactly like running esbuild)
  --production      minify, and define process.env.NODE_ENV="production"
  --dry-run         print the resolved command and exit without running anything
  --dir=<dir>       project directory (default: .)

WHAT NGM ADDS:
  Every entry in ngm.mappings.json becomes --alias:<from>=<to>, so
  ` + "`import x from \"github:org/repo\"`" + ` resolves into ngm.vendor — the bytes
  that ngm proved. Other than that this is a thin wrapper around the engine.

EXIT CODES:
  0  built (also for --dry-run)
  1  the engine ran and failed (its own stderr is preserved)
  3  configuration error (no entry, unknown engine name)
  5  no usable engine (not installed, or a dry-run stub)
`

// runBuild 处理 `ngm build`。
func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("build")
	engineFlag := fs.String("engine", "", "engine name")
	outfile := fs.String("outfile", "", "output file")
	production := fs.Bool("production", false, "production build")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, buildUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "engine"},
		{Name: "outfile"},
		{Name: "production", Bool: true},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, buildUsage)
		return 3
	}

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	entry := fs.Arg(0)
	if entry == "" {
		entry = ec.pf.Main
	}
	if strings.TrimSpace(entry) == "" {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no entry file",
			"pass one (`ngm build src/index.ts`) or set `main` in ngm.json"))
	}

	sel, err := ec.selectionFor(adapter.KindBundle, *engineFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	alias, err := ec.mappingsAlias()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	opts := adapter.BundleOptions{
		Outfile:    *outfile,
		Alias:      alias,
		Production: *production,
	}

	if *dryRun {
		plans, perr := ec.runner.Plans(adapter.KindBundle, sel, opts, entry)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		fmt.Fprintf(stdout, "would bundle %s\n", entry)
		printPlans(stdout, plans)
		return 0
	}

	res, berr := ec.runner.Bundle(ctx, sel, entry, opts)
	if berr != nil {
		return runErr(ctx, stdout, stderr, berr)
	}

	// 引擎的诊断原样转发到 stderr（不转述、不吞掉）
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, w)
	}
	// ngm 自己的说明另起一行并加前缀：用户要能分清"引擎在报警"与"ngm 在解释自己做的事"。
	// 这条通道此前是**断的**：`deno bundle` 的实验性提示写好了却没人读——
	// 而那句提示恰恰是"我们没说过它稳定"的全部依据。
	for _, n := range res.Notes {
		fmt.Fprintf(stderr, "note: %s\n", n)
	}
	if res.Outfile != "" {
		fmt.Fprintf(stdout, "bundled %s → %s\n", entry, res.Outfile)
	} else {
		if _, werr := stdout.Write(res.Code); werr != nil {
			fmt.Fprintf(stderr, "write bundle: %v\n", werr)
			return 1
		}
	}
	return 0
}
