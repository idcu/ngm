package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/runscript"
)

const runUsage = `ngm run — run project scripts without npm

USAGE:
  ngm run <script> [--dir=<path>] [--if-present] [-- <extra args>]
  ngm run --list [--json] [--dir=<path>]

SOURCES:
  package.json.scripts is authoritative. ngm.json.scripts is the fallback.
  A script defined in BOTH is a configuration error (exit 3): ngm names both
  definitions instead of silently preferring one.

FLAGS:
  --dir         project directory (default: .)
  --if-present  exit 0 when the script does not exist (like npm run --if-present)
  --list        list script names, their source, and whether pre/post exist
  --json        with --list: machine-readable report (same data as the human text)

EXECUTION:
  POSIX uses "sh -c"; Windows uses "cmd.exe /d /s /c". ngm does no extra quoting:
  the platform shell owns quote, glob and "&&" semantics, exactly as in npm.

  pre/post follow the npm convention: "pre<name>" and "post<name>". They run in
  that order; the first non-zero step stops the chain, and its exit code is
  returned. Each step sees its own npm_lifecycle_event.

  node_modules/.bin directories are collected from the project directory and its
  ancestors (nearest first) and prepended to the child PATH.

ENVIRONMENT (contract, not "npm-compatible"):
  INIT_CWD, npm_lifecycle_event, npm_lifecycle_script, npm_package_name,
  npm_package_version. Nothing else is promised: npm_execpath is NOT set and
  npm_config_* is not simulated. Scripts that need them should still use npm.

PERMISSION:
  One gate, "run:script", denied by default. Project scripts run OUTSIDE ngm's
  sandbox — that is why this gate is separate from supplyChain.postInstallPolicy
  (which governs install-time dependency scripts). Enable it with:
    {"permissions":{"allow":["run:script"]}}

EXIT CODES:
  0  the script (and its pre/post steps) exited 0; also --if-present when absent
  3  usage or configuration error: no such script, the same name in both sources,
     permission denied, or unreadable scripts
  5  the platform shell is unavailable (POSIX "sh" / Windows "cmd.exe")
  Any other code belongs to the script itself and is passed through verbatim:
  ngm run never remaps it to ngm's own 0-6 codes. A child killed by SIGINT is
  reported as 130, by SIGTERM as 143.
`

type runScriptRow struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source"`
	Pre     string `json:"pre,omitempty"`
	Post    string `json:"post,omitempty"`
}

type runListReport struct {
	Version int            `json:"version"`
	Project string         `json:"project"`
	Scripts []runScriptRow `json:"scripts"`
}

func runRun(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	head, extra := splitDoubleDash(args)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".", "")
	ifPresent := fs.Bool("if-present", false, "")
	list := fs.Bool("list", false, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(normalizeArgs(head, []flagSpec{
		{Name: "dir"},
		{Name: "if-present", Bool: true},
		{Name: "list", Bool: true},
		{Name: "json", Bool: true},
	})); err != nil {
		fmt.Fprint(stderr, runUsage)
		return runArgError(ctx, args, stdout, stderr,
			"invalid arguments for `ngm run`", "see `ngm run --help`")
	}
	ctx = markJSONIfRequested(ctx, *asJSON)

	listMode := *list || *asJSON
	positional := fs.Args()
	if listMode && len(positional) > 0 {
		fmt.Fprint(stderr, runUsage)
		return runArgError(ctx, args, stdout, stderr, "`--list`/`--json` take no script name",
			"run `ngm run --list` to list scripts, or `ngm run <script>` to execute one")
	}
	if !listMode {
		if len(positional) == 0 {
			fmt.Fprint(stderr, runUsage)
			return runArgError(ctx, args, stdout, stderr, "`ngm run` needs a script name",
				"run `ngm run --list` to see the available scripts")
		}
		if len(positional) > 1 {
			return runArgError(ctx, args, stdout, stderr,
				"unexpected extra argument(s): "+strings.Join(positional[1:], " "),
				"extra arguments must follow `--`: write `ngm run <script> -- <args>` "+
					"so ngm passes them to the script verbatim")
		}
	}

	env, err := newProjectEnv(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	set, err := runscript.Load(env.ProjectDir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	if listMode {
		return runList(ctx, set, *asJSON, stdout, stderr)
	}

	name := positional[0]
	script, ok := set.Lookup(name)
	if !ok {
		if *ifPresent {
			fmt.Fprintf(stderr, "note: script %q is not defined; --if-present makes this a success\n", name)
			return 0
		}
		hint := "available: " + availableScripts(set, 10)
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"no such script: "+name, hint+" (run `ngm run --list` for all of them)"))
	}

	// 单一闸门：在**任何一步**执行前判定一次。pre/post 是同一个脚本链，
	// 不能因为分步执行而重复要求授权。
	if perr := env.Policy.CheckRun("script"); perr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"permission denied: run:script",
			"project scripts run OUTSIDE ngm's sandbox, which is why this gate is separate from "+
				"install-time dependency scripts (`supplyChain.postInstallPolicy` in ngm.json). "+
				"Enable it by pasting "+`{"permissions":{"allow":["run:script"]}}`+
				" into "+env.Policy.Source(), perr))
	}

	binDirs := runscript.BinDirs(env.ProjectDir)

	type runStep struct {
		event   string
		command string
	}
	steps := make([]runStep, 0, 3)
	if script.Pre != "" {
		if pre, ok := set.Lookup(script.Pre); ok {
			steps = append(steps, runStep{pre.Name, pre.Command})
		}
	}
	steps = append(steps, runStep{script.Name, script.Command})
	if script.Post != "" {
		if post, ok := set.Lookup(script.Post); ok {
			steps = append(steps, runStep{post.Name, post.Command})
		}
	}

	for _, step := range steps {
		// `--` 之后的参数只追加到**主脚本**：npm 的 pre/post 步骤不接收用户参数，
		// 拷贝“全都追加”会让 pre 脚本收到它不认识的开关。
		var stepArgs []string
		if step.event == script.Name {
			stepArgs = extra
		}
		code, err := runscript.Exec(ctx, runscript.ExecSpec{
			Dir:            env.ProjectDir,
			Command:        step.command,
			Event:          step.event,
			PackageName:    set.PackageName,
			PackageVersion: set.PackageVersion,
			ExtraArgs:      stepArgs,
			BinDirs:        binDirs,
		}, stdout, stderr)
		if err != nil {
			return runErr(ctx, stdout, stderr, err)
		}
		if code != 0 {
			if len(binDirs) == 0 {
				fmt.Fprintf(stderr, "note: no node_modules/.bin found under %s; "+
					"if the script needs a dependency-provided binary, install dependencies first\n",
					env.ProjectDir)
			}
			return code
		}
	}
	return 0
}

// runArgError 把“早期用法错误”写到用户看得见的地方：`--json` 模式走错误信封，
// 人读模式用与 `runErr` 相同的渲染器（错误码名 + hint）。`failEarly` 只在
// JSON 模式生效，单靠它会让人读模式的失败只剩 usage 而看不到怎么改（v0.54 的同一族教训）。
func runArgError(ctx context.Context, args []string, stdout, stderr io.Writer, msg, hint string) int {
	ne := errs.New(errs.CodeConfigInvalid, msg, hint)
	if wantsJSON(ctx) {
		failEarly(args, stdout, msg, hint)
	} else {
		// 与 `runErr` 同一个渲染器：错误行必须以**错误码名**开头，否则
		// 参数维度那张网（TestV39）认不出这是"错误通道"，会把它算成未分类输出。
		fmt.Fprintln(stderr, errs.FormatHuman(ne))
	}
	return errs.CodeConfigInvalid.ExitCode()
}

// splitDoubleDash 把参数分成"第一个 `--` 之前"与"之后"。
//
// 之后的部分**不做任何 flag 解析**，原样交给脚本——这正是 npm 的契约。
// 之前的部分才交给 flag 包，因此 `ngm run build -- --flag` 不会把 `--flag`
// 当成 ngm 的 flag。
func splitDoubleDash(args []string) (head, extra []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func runList(ctx context.Context, set *runscript.Set, asJSON bool, stdout, stderr io.Writer) int {
	report := runListReport{Version: 1, Project: set.Project}
	for _, sc := range set.Scripts {
		report.Scripts = append(report.Scripts, runScriptRow{
			Name: sc.Name, Command: sc.Command, Source: string(sc.Source),
			Pre: sc.Pre, Post: sc.Post,
		})
	}
	if asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeInternal, "encode run report",
				"the report could not be encoded as JSON; re-run without --json", err))
		}
		if _, err := fmt.Fprintln(stdout, string(data)); err != nil {
			return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeInternal, "write run report",
				"the report could not be written; check that stdout is writable", err))
		}
		return 0
	}

	fmt.Fprintf(stdout, "scripts in %s (%d):\n", set.Project, len(report.Scripts))
	for _, sc := range report.Scripts {
		var marks []string
		if sc.Pre != "" {
			marks = append(marks, "pre: "+sc.Pre)
		}
		if sc.Post != "" {
			marks = append(marks, "post: "+sc.Post)
		}
		suffix := ""
		if len(marks) > 0 {
			suffix = "  [" + strings.Join(marks, ", ") + "]"
		}
		fmt.Fprintf(stdout, "  %-20s %-12s %s%s\n", sc.Name, sc.Source, sc.Command, suffix)
	}
	return 0
}

// availableScripts 返回可执行脚本名的逗号列表（最多 limit 个）。
func availableScripts(set *runscript.Set, limit int) string {
	names := set.Names()
	if len(names) == 0 {
		return "(none)"
	}
	if len(names) > limit {
		names = append(names[:limit], "…")
	}
	return strings.Join(names, ", ")
}
