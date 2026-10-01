package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/errs"
)

const typecheckUsage = `ngm typecheck — type-check a project with an external engine

USAGE:
  ngm typecheck [<entry>] [--engine=<name>] [--tsconfig=<path>] [--dry-run] [--dir=<dir>]

ARGS:
  <entry>   entry file (default: ` + "`main`" + ` from ngm.json)

FLAGS:
  --engine=<name>     engine to use
  --tsconfig=<path>   tsconfig to hand to the engine (default: let the engine find it)
  --dry-run           print the resolved command and exit without running anything
  --dir=<dir>         project directory (default: .)

NOTE:
  This build adapts no type-checking engine. esbuild is NOT a substitute —
  it strips type annotations without checking them, so ` + "`ngm build`" + ` passing
  says nothing about types. Declare a real checker in ngm.engines.json:

    {"version": 1, "engines": [
      {"name": "typescript", "kind": "typeCheck", "adapter": "subprocess",
       "command": "tsc --noEmit"}
    ]}

EXIT CODES:
  0  no diagnostics
  1  the engine ran and reported problems
  3  configuration error
  5  no usable engine (none declared, or not installed)
`

// runTypecheck 处理 `ngm typecheck`。
func runTypecheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("typecheck")
	engineFlag := fs.String("engine", "", "engine name")
	tsconfig := fs.String("tsconfig", "", "tsconfig path")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, typecheckUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "engine"},
		{Name: "tsconfig"},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, typecheckUsage)
		return 3
	}

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 类型检查的入口可以省略（很多检查器只吃 tsconfig）；bundle 则必须给
	entry := fs.Arg(0)
	if entry == "" {
		entry = ec.pf.Main
	}

	sel, err := ec.selectionFor(adapter.KindTypeCheck, *engineFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	opts := adapter.TypeCheckOptions{TSConfig: *tsconfig}

	if *dryRun {
		plans, perr := ec.runner.Plans(adapter.KindTypeCheck, sel, opts, entry)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		if entry == "" {
			fmt.Fprintln(stdout, "would type-check the project")
		} else {
			fmt.Fprintf(stdout, "would type-check %s\n", entry)
		}
		printPlans(stdout, plans)
		return 0
	}

	res, cerr := ec.runner.Check(ctx, sel, entry, opts)
	if cerr != nil {
		// 引擎非零退出即"发现类型问题"，退出码 1（见 runChain）。
		//
		// 但这是**有内容**的失败：诊断必须展示在与成功路径相同的位置。
		// 只报"引擎退出 1"等于把类型检查的产物丢掉——用户拿不到该改哪一行。
		// （tsc 把 `error TS…` 写在 stdout，所以两边都要看。）
		var ee *adapter.EngineError
		if errors.As(cerr, &ee) {
			for _, line := range ee.Diagnostics() {
				fmt.Fprintln(stdout, line)
			}
		}
		return runErr(ctx, stdout, stderr, cerr)
	}

	// 引擎退出码 0 就是"没有类型错误"——这来自 P4 的协议
	// （"退出码 0 成功，非 0 失败"），不能因为引擎往 stderr 写了日志
	// 就改判为失败。诊断原样展示，判定只看退出码。
	for _, line := range res.Diagnostics {
		fmt.Fprintln(stdout, line)
	}
	if len(res.Diagnostics) == 0 {
		fmt.Fprintln(stdout, "no type errors")
	} else {
		fmt.Fprintln(stdout, "type check passed (engine exit 0)")
	}
	return 0
}

const cssUsage = `ngm css — compile CSS with an external engine

USAGE:
  ngm css <input.css> [--engine=<name>] [--outfile=<path>] [--minify] [--dry-run] [--dir=<dir>]

ARGS:
  <input.css>       input file (required)

FLAGS:
  --engine=<name>   engine to use
  --outfile=<path>  write the result there (default: stdout)
  --minify          minify the output
  --dry-run         print the resolved command and exit without running anything
  --dir=<dir>       project directory (default: .)

EXIT CODES:
  0  compiled (also for --dry-run)
  1  the engine ran and failed
  3  configuration error (missing input, unknown engine name)
  5  no usable engine
`

// runCSS 处理 `ngm css`。
func runCSS(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("css")
	engineFlag := fs.String("engine", "", "engine name")
	outfile := fs.String("outfile", "", "output file")
	minify := fs.Bool("minify", false, "minify the output")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, cssUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "engine"},
		{Name: "outfile"},
		{Name: "minify", Bool: true},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
	})); err != nil {
		return 3
	}

	input := fs.Arg(0)
	if strings.TrimSpace(input) == "" {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no input file",
			"usage: ngm css <input.css>"))
	}

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	sel, err := ec.selectionFor(adapter.KindCSS, *engineFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	opts := adapter.CSSOptions{Outfile: *outfile, Minify: *minify}

	if *dryRun {
		plans, perr := ec.runner.Plans(adapter.KindCSS, sel, opts, "")
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}
		fmt.Fprintf(stdout, "would compile %s\n", input)
		printPlans(stdout, plans)
		return 0
	}

	body, err := readInputFile(ec.env.ProjectDir, input)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	res, cerr := ec.runner.Compile(ctx, sel, body, opts)
	if cerr != nil {
		return runErr(ctx, stdout, stderr, cerr)
	}
	// 两条通道都要转：**引擎的 stderr** 与 **ngm 自己的说明**。
	//
	// 后者不能省：postcss 没有内建压缩，ngm 用它会明说"--minify 被忽略了"，
	// 吞掉它等于替用户决定"不必知道"。前者此前被整个丢掉——引擎写到 stderr 的
	// 警告（弃用提示、插件的非致命报错）根本到不了用户，与"诊断被吞掉"同型。
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, w)
	}
	for _, n := range res.Notes {
		fmt.Fprintf(stderr, "note: %s\n", n)
	}
	if res.Outfile != "" {
		fmt.Fprintf(stdout, "compiled %s → %s\n", input, res.Outfile)
		return 0
	}
	if _, werr := stdout.Write(res.Code); werr != nil {
		fmt.Fprintf(stderr, "write css: %v\n", werr)
		return 1
	}
	return 0
}
