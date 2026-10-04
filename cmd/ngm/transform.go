package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/errs"
)

const transformUsage = `ngm transform — transform a single file (or stdin) with an external engine

USAGE:
  ngm transform [<file>] [--engine=<name>] [--outfile=<path>] [--loader=<name>]
                [--target=<es20xx>] [--format=<esm|cjs|iife>] [--minify] [--sourcemap]
                [--dry-run] [--dir=<dir>]

ARGS:
  <file>    input file, resolved relative to the project directory.
            Default: **stdin** (this is the pipeline-shaped capability).

FLAGS:
  --engine=<name>   engine to use (default: engines.transform in ngm.json, else esbuild)
  --outfile=<path>  write the result there, relative to the project directory
                    (default: stdout)
  --loader=<name>   input syntax: ts / tsx / js / jsx. An engine cannot infer syntax
                    from a pipe, so a loader must come from somewhere. Three sources,
                    in this order: --loader, the file extension (announced on stderr),
                    engines.transform.options.loader in ngm.json.
                    Only if none of them applies does ngm refuse — it never guesses.
  --target=<es20xx> output target (e.g. es2020)
  --format=<fmt>    module format: esm / cjs / iife
  --minify          minify the output
  --sourcemap       inline source map
  --dry-run         print the resolved command and exit without running anything
  --dir=<dir>       project directory (default: .)

BUNDLE OR TRANSFORM:
  ` + "`ngm build`" + ` bundles a project and resolves imports; transform touches **one
  file** and resolves nothing. It exists for pipelines that want an engine, not a
  bundler — test runners, dev servers, custom steps.

WHAT NGM ADDS:
  Engine selection, exit-code discipline and the report. The engine still decides the
  syntax; ngm never edits your code.

EXIT CODES:
  0  transformed (also for --dry-run)
  1  the engine ran and failed (its own stderr is preserved)
  3  configuration error (no loader for stdin, unreadable input, unknown engine name)
  5  no usable engine (not installed, or a dry-run stub)
`

// stdinReader 是子命令读取 stdin 的入口。
//
// 为什么不直接用 `os.Stdin`：验收测试在**进程内**调用 dispatch，
// 进程的 stdin 属于测试运行器（`go test` 下通常不是可读的管道）。
// 留一个可替换的变量，"从 stdin 读"这条路径才可能被断言——
// 与 testutils 里 WriteUserConfig 用的手法相同：把不可控的外部依赖留一个接缝。
// 生产路径上它就是 os.Stdin。
var stdinReader io.Reader = os.Stdin

// runTransform 处理 `ngm transform`。
//
// 它补上的是同一类缺口：`transform` 能力在 adapter 层早已实现（esbuild 的
// `TransformEngine`，有单测），`engines.transform` 这个配置键也一直在被读——
// **但没有命令驱动它**，于是那个键对用户没有任何可观察的效果
// （v0.1 复盘 §6 起就登记为"有实现、无入口"；v0.4 给 `typeDecl` 补了入口，
// 本条是它的同型项）。
//
// 顺带让另一件事成真：adapter 在"stdin 却没给 loader"时的提示写着
// "pass --loader=<ext>"，而在本命令出现之前，**没有任何命令有 --loader**。
//
// 第一版在这里自己判"没 loader 就报错"，结果把清单里已声明的
// `engines.transform.options.loader` 挡在门外（adapter 本来会用它）——
// 也就是说，本命令的第一版**自己**制造了一次"声明了、不生效"。
// 现在那个判断交给 adapter（单一事实源），CLI 只负责预检与推断。
func runTransform(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("transform", stderr)
	engineFlag := fs.String("engine", "", "engine name")
	outfile := fs.String("outfile", "", "output file")
	loaderFlag := fs.String("loader", "", "input syntax (ts / tsx / js / jsx)")
	target := fs.String("target", "", "output target")
	format := fs.String("format", "", "module format")
	minify := fs.Bool("minify", false, "minify the output")
	sourcemap := fs.Bool("sourcemap", false, "inline source map")
	dryRun := fs.Bool("dry-run", false, "print the resolved command and exit")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, transformUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "engine"},
		{Name: "outfile"},
		{Name: "loader"},
		{Name: "target"},
		{Name: "format"},
		{Name: "minify", Bool: true},
		{Name: "sourcemap", Bool: true},
		{Name: "dry-run", Bool: true},
		{Name: "dir"},
	})); err != nil {
		return 3
	}
	if fs.NArg() > 1 {
		fmt.Fprint(stderr, transformUsage)
		return 3
	}

	ec, err := newEngineContext(*dirFlag, stderr)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	input := fs.Arg(0)
	loader := strings.TrimSpace(*loaderFlag)

	// loader 的推断只在**有文件名**时可能：管道里没有扩展名可看。
	// 推断结果必须说出来——"我替你选了一个"和"你选的那个生效了"是两件事，
	// 而本项目已经因为"默默按另一套规则办事"栽过（相对路径按了 CWD）。
	if loader == "" && input != "" {
		loader = loaderForExtension(input)
		if loader != "" {
			fmt.Fprintf(stderr, "note: loader %q inferred from %s (pass --loader to override)\n",
				loader, filepath.Ext(input))
		}
	}

	sel, err := ec.selectionFor(adapter.KindTransform, *engineFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	opts := adapter.TransformOptions{
		Loader:     loader,
		Target:     *target,
		Format:     *format,
		Minify:     *minify,
		SourceMaps: *sourcemap,
	}

	// **预检**：把"这条调用成立吗"交给 adapter 回答，而不是在这里复刻它的规则。
	//
	// 这里曾经自己判"没 loader 就报错"，于是目录里已经声明的
	// `engines.transform.options.loader` 被挡在门外——adapter 明明会用它。
	// 那是本项目反复登记的同一类错误的又一个实例：**在第二个地方重写了一遍规则**，
	// 两处一旦不一致，用户看到的是"配置写了却不生效"。
	// adapter 的那条错误还更完整（它同时给出 `--loader=` 与清单里怎么写）。
	//
	// 预检必须在**读 stdin 之前**：否则 `ngm transform` 会先等一个永远不会来的输入，
	// 才告诉用户参数不对。
	plans, perr := ec.runner.Plans(adapter.KindTransform, sel, opts, "")
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}

	if *dryRun {
		what := "stdin"
		if input != "" {
			what = input
		}
		fmt.Fprintf(stdout, "would transform %s\n", what)
		printPlans(stdout, plans)
		return 0
	}

	var body []byte
	if input == "" {
		body, err = io.ReadAll(stdinReader)
		if err != nil {
			fmt.Fprintf(stderr, "read stdin: %v\n", err)
			return 1
		}
	} else {
		body, err = readInputFile(ec.env.ProjectDir, input)
		if err != nil {
			return runErr(ctx, stdout, stderr, err)
		}
	}

	res, cerr := ec.runner.Transform(ctx, sel, body, opts)
	if cerr != nil {
		return runErr(ctx, stdout, stderr, cerr)
	}
	// 引擎的说明原样转发（与 build / css 一致）：吞掉它等于替用户决定"不必知道"。
	for _, w := range res.Warnings {
		fmt.Fprintln(stderr, w)
	}

	// transform 的引擎接口只有 stdout 一条出口（TransformResult 没有 Outfile），
	// 因此 --outfile 由 ngm 自己落盘——不是"引擎答应写的"。
	if strings.TrimSpace(*outfile) == "" {
		if _, werr := stdout.Write(res.Code); werr != nil {
			fmt.Fprintf(stderr, "write output: %v\n", werr)
			return 1
		}
		return 0
	}

	dest := *outfile
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(ec.env.ProjectDir, dest)
	}
	if derr := os.MkdirAll(filepath.Dir(dest), 0o755); derr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "create output directory", "", derr))
	}
	if werr := os.WriteFile(dest, res.Code, 0o644); werr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "write "+*outfile,
			"the path is resolved relative to the project directory ("+ec.env.ProjectDir+")", werr))
	}
	src := "stdin"
	if input != "" {
		src = input
	}
	fmt.Fprintf(stdout, "transformed %s → %s\n", src, *outfile)
	return 0
}

// loaderForExtension 把文件扩展名映射到引擎的 loader 名。
//
// 只认**明确无误**的那几个：推断不出来时返回空串，由调用方要求用户显式给出，
// 而不是猜一个（猜错的后果是语法错误或更糟的静默转换）。
func loaderForExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".mts", ".cts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".jsx":
		return "jsx"
	default:
		return ""
	}
}
