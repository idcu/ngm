package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/errs"
)

const typedeclUsage = `ngm typedecl — emit .d.ts declarations with an external engine

USAGE:
  ngm typedecl [<entry>] --outdir=<dir> [--engine=<name>] [--dry-run] [--dir=<dir>]

ARGS:
  <entry>   entry file (default: ` + "`main`" + ` from ngm.json)

FLAGS:
  --outdir=<dir>    where the declarations go (**required**)
  --engine=<name>   engine to use
  --dry-run         print the resolved command and exit without running anything
  --dir=<dir>       project directory (default: .)

WHY --outdir IS REQUIRED:
  This capability's product is files in a directory, not text on stdout. Without a
  destination ngm cannot report what was produced — and "the engine exited 0" would
  look exactly like success whether or not anything was written.

EXIT CODES:
  0  ran (also for --dry-run); the report lists the files that actually appeared
  1  the engine ran and failed
  3  configuration error (missing --outdir, unknown engine name)
  5  no usable engine (none declared, or not installed)
`

// runTypeDecl 处理 `ngm typedecl`。
//
// 它补上的是 v0.4 复核发现的一个缺口：`typeDecl` 能力在 adapter 层早已实现、也有
// 单测（tsc 的 `--emitDeclarationOnly`），**但没有任何命令驱动它**——于是
// `engines.typeDecl` 这个配置键对用户没有任何效果（配置接线检查曾为此记了一条豁免，
// 本命令让那条豁免可以被删掉）。
func runTypeDecl(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("typedecl")
	outdir := fs.String("outdir", "", "directory for the declarations")
	engineFlag := fs.String("engine", "", "engine name")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, typedeclUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "outdir"},
		{Name: "engine"},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, typedeclUsage)
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

	// 缺目的地是**用法错误**，不是"取个默认值"：这个能力的产物是目录里的文件，
	// 猜一个默认目录等于替用户决定把文件写到哪里。
	dir := strings.TrimSpace(*outdir)
	if dir == "" {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no output directory",
			"pass --outdir=<dir>; without a destination `ngm typedecl` cannot report what it produced"))
	}

	sel, err := ec.selectionFor(adapter.KindTypeDecl, *engineFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	opts := adapter.TypeDeclOptions{OutDir: dir}

	if *dryRun {
		plans, perr := ec.runner.Plans(adapter.KindTypeDecl, sel, opts, entry)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		what := entry
		if strings.TrimSpace(what) == "" {
			what = "the project"
		}
		fmt.Fprintf(stdout, "would emit declarations for %s → %s\n", what, dir)
		printPlans(stdout, plans)
		return 0
	}

	res, cerr := ec.runner.GenerateTypeDecl(ctx, sel, entry, opts)
	if cerr != nil {
		return runErr(ctx, stdout, stderr, cerr)
	}

	// 报告**实际出现的文件**，而不是引擎声称会产出的：命名规则是引擎自己的事。
	if len(res.Files) == 0 {
		// 引擎退出 0 却什么都没写，是合法结果（项目里可能没有可导出的类型），
		// 但它与"命令悄悄什么都没做"长得一模一样，因此必须明说。
		fmt.Fprintf(stderr, "note: the engine exited 0 but no files appeared under %s\n", dir)
		return 0
	}
	fmt.Fprintf(stdout, "emitted %d declaration file(s) → %s\n", len(res.Files), dir)
	for _, f := range res.Files {
		fmt.Fprintf(stdout, "  %s\n", f)
	}
	return 0
}
