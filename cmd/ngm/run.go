package main

import (
	"context"
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
  ngm run --explain <script> [--json] [--dir=<path>]

SOURCES:
  package.json.scripts is authoritative. ngm.json.scripts is the fallback.
  A script defined in BOTH is a configuration error (exit 3): ngm names both
  definitions instead of silently preferring one.

FLAGS:
  --dir         project directory (default: .)
  --if-present  exit 0 when the script does not exist (like npm run --if-present)
  --list        list script names, their source, and whether pre/post exist
  --json        with --list or --explain: machine-readable report (same data as the human text)
  --explain     read-only plan for one script: pre/main/post order, source files,
                injected PATH segments and the current run:script decision.
                Executes nothing, creates nothing, grants nothing (未执行).

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
  npm_package_version. Nothing else is promised: npm_execpath is NOT set, and
  the npm_config_* namespace is neither simulated NOR inherited (a script must
  not behave differently depending on whether you launched ngm from a shell or
  from an npm script). Scripts that need npm's own variables should use npm.

  The parent environment is passed through MINUS the names denied by
  permissions.deny: ["env:<NAME>"] — the same list that keeps git, engine and
  sandbox subprocesses from seeing them. The five names above and the injected
  PATH are ngm's own contract and are always set.

PERMISSION:
  One gate, "run:script", denied by default. Project scripts run OUTSIDE ngm's
  sandbox — that is why this gate is separate from supplyChain.postInstallPolicy
  (which governs install-time dependency scripts). Enable it with:
    {"permissions":{"allow":["run:script"]}}

EXPLAIN (--explain):
  A read-only plan, not a dry run of the gate: it never evaluates the gate as an
  error, never executes a script, never writes the permission config, never creates
  files and never starts an engine. It only prints what an actual run would do and
  which decision the current policy would make (当前策略会拒绝/放行（未执行）).
  The pre/main/post order comes from the same plan builder as real execution.

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
	explain := fs.Bool("explain", false, "")
	if err := fs.Parse(normalizeArgs(head, []flagSpec{
		{Name: "dir"},
		{Name: "if-present", Bool: true},
		{Name: "list", Bool: true},
		{Name: "json", Bool: true},
		{Name: "explain", Bool: true},
	})); err != nil {
		fmt.Fprint(stderr, runUsage)
		// 信封里的 message 必须说清**是哪个 flag**（V54 的第③条），而 flag 包
		// 已经把那条具体的错误打到 stderr 了——人读侧再说一遍是重复，机器侧
		// 说"invalid arguments"又太含糊。两边各拿自己该拿的那一句。
		if wantsJSON(ctx) || jsonAskedEarly(args) {
			failEarly(args, stdout, err.Error(), "run `ngm run --help` for the flags it accepts")
			return errs.CodeConfigInvalid.ExitCode()
		}
		return runArgError(ctx, args, stdout, stderr,
			"invalid arguments for `ngm run`", "see `ngm run --help`")
	}
	ctx = markJSONIfRequested(ctx, *asJSON)

	// --explain 需要恰好一个脚本名，所以它走"取脚本"那支，而不是 --list/--json 的列表支。
	// 因此解释模式必须把 *asJSON 从 listMode 里摘出去，否则 `--explain --json` 会被
	// 误判成"列表且不给脚本名"。
	explainMode := *explain
	listMode := (*list || *asJSON) && !explainMode
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

	// --explain 在**权限闸门与执行之前**返回：它只呈现计划，绝不把 deny 变成错误，
	// 也绝不进入下面任何会启动子进程、创建文件或写配置的路径（spec §3.6 / E10）。
	if explainMode {
		return runExplain(env, set, script, extra, *asJSON, stdout, stderr)
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
	// 与 --explain 共用同一个计划构造器：顺序/来源若在这里分叉，
	// "explain 说的"就不是"run 做的"（E10 判据的"顺序同源"）。
	steps := runPlanSteps(set, script)

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
			// 与 git / 引擎 / 沙箱共用同一个名单（`Policy.DeniedEnvVars`）：
			// 被拒绝的名字从**透传**给脚本的那一份环境里剔除（2026-11-19）。
			DeniedEnv: env.Policy.DeniedEnvVars(),
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

// runStep 是一步执行计划。run（真实执行）与 run --explain（只读预演）共用，
// phase/source 只用于 explain 的展示，不参与执行。
type runStep struct {
	event   string
	command string
	phase   string // pre / main / post
	source  string // package.json / ngm.json
}

// runPlanSteps 把一条脚本展开成 pre→main→post 的顺序。
// 这是"顺序同源"的唯一来源：explain 与真实 run 都调用它，谁都不能另写一份。
func runPlanSteps(set *runscript.Set, script runscript.Script) []runStep {
	steps := make([]runStep, 0, 3)
	if script.Pre != "" {
		if pre, ok := set.Lookup(script.Pre); ok {
			steps = append(steps, runStep{event: pre.Name, command: pre.Command,
				phase: "pre", source: string(pre.Source)})
		}
	}
	steps = append(steps, runStep{event: script.Name, command: script.Command,
		phase: "main", source: string(script.Source)})
	if script.Post != "" {
		if post, ok := set.Lookup(script.Post); ok {
			steps = append(steps, runStep{event: post.Name, command: post.Command,
				phase: "post", source: string(post.Source)})
		}
	}
	return steps
}

// ---- run --explain 的报告形状（--json 形状表由 cli.md 单独同步）----

type runExplainStep struct {
	Phase   string `json:"phase"`
	Event   string `json:"event"`
	Source  string `json:"source"`
	Command string `json:"command"`
}

type runExplainAuth struct {
	Permission string `json:"permission"` // 恒为 run:script
	Allowed    bool   `json:"allowed"`
	Decision   string `json:"decision"` // 当前策略会拒绝/放行（未执行）
	Source     string `json:"source"`   // 策略配置文件
}

type runExplainReport struct {
	Version  int              `json:"version"`
	Project  string           `json:"project"`
	Script   string           `json:"script"`
	Auth     runExplainAuth   `json:"auth"`
	PathDirs []string         `json:"pathDirs"`
	Steps    []runExplainStep `json:"steps"`
	Note     string           `json:"note"`
}

// runExplain 输出只读计划。整个函数**只做读**：
//   - runPlanSteps 只查内存里的 set；
//   - runscript.BinDirs 只 os.Stat；
//   - Policy.CheckRun 只查 allow/deny map（与真实 run 同一个判定）；
//   - 不调用 runscript.Exec、不建目录、不写文件、不写权限配置、不启引擎。
func runExplain(env *projectEnv, set *runscript.Set, script runscript.Script, extra []string, asJSON bool, stdout, stderr io.Writer) int {
	// 与真实 run 的闸门调用同一个 CheckRun，只是这里把结果**呈现**出来而不是
	// 转成 exit 3：deny 与 allow 都会照常出计划（spec §3.6 的"当前授权状态"）。
	allowed := env.Policy.CheckRun("script") == nil
	decision := approvalDecisionLine(allowed)

	rep := runExplainReport{
		Version: 1,
		Project: env.ProjectDir,
		Script:  script.Name,
		Auth: runExplainAuth{
			Permission: "run:script",
			Allowed:    allowed,
			Decision:   decision,
			Source:     env.Policy.Source(),
		},
		PathDirs: runscript.BinDirs(env.ProjectDir),
		Note:     "read-only plan; nothing was executed (未执行)",
	}
	for _, st := range runPlanSteps(set, script) {
		cmd := st.command
		if st.phase == "main" && len(extra) > 0 {
			// 与真实 run 一致：`--` 之后的参数只追加到主脚本。
			cmd = runscript.AppendArgs(cmd, extra)
		}
		rep.Steps = append(rep.Steps, runExplainStep{
			Phase: st.phase, Event: st.event, Source: st.source, Command: cmd,
		})
	}
	if asJSON {
		return writeJSONDocument(stdout, stderr, rep, 0)
	}

	fmt.Fprintf(stdout, "run --explain (read-only; 未执行)\n")
	fmt.Fprintf(stdout, "project: %s\n", rep.Project)
	fmt.Fprintf(stdout, "script:  %s\n", rep.Script)
	fmt.Fprintf(stdout, "run:script authorization: %s\n", rep.Auth.Decision)
	fmt.Fprintf(stdout, "  permission: %s  source: %s\n", rep.Auth.Permission, rep.Auth.Source)
	fmt.Fprintln(stdout, "PATH injection (nearest first):")
	if len(rep.PathDirs) == 0 {
		fmt.Fprintln(stdout, "  (none: no node_modules/.bin found under the project)")
	}
	for _, d := range rep.PathDirs {
		fmt.Fprintf(stdout, "  - %s\n", d)
	}
	fmt.Fprintln(stdout, "plan (pre -> main -> post):")
	for i, st := range rep.Steps {
		fmt.Fprintf(stdout, "  %d. %-5s %-20s [%s] %s\n",
			i+1, st.Phase, st.Event, st.Source, st.Command)
	}
	fmt.Fprintln(stdout, rep.Note)
	return 0
}

// runArgError 把“早期用法错误”写到用户看得见的地方：`--json` 模式走错误信封，
// 人读模式用与 `runErr` 相同的渲染器（错误码名 + hint）。`failEarly` 只在
// JSON 模式生效，单靠它会让人读模式的失败只剩 usage 而看不到怎么改（v0.54 的同一族教训）。
func runArgError(ctx context.Context, args []string, stdout, stderr io.Writer, msg, hint string) int {
	ne := errs.New(errs.CodeConfigInvalid, msg, hint)
	// 是否给信封按**原始参数**判定，不只按 ctx（v0.136）。
	//
	// 解析失败发生在 `--json` 生效之前——那时 ctx 还没被标记，于是
	// `ngm run --json --bogus` 的 stdout 是空的，而同一台机器上
	// `ngm verify --json --bogus` 有信封。这条不一致一直藏着，因为派生的
	// flag 表只认 `newFlagSet`，看不见 run 这个手写骨架（`flag.NewFlagSet`）。
	if wantsJSON(ctx) || jsonAskedEarly(args) {
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
		// 统一写出点（ADR-045）：四份实现曾经各写各的错误文案与转义策略。
		return writeJSONDocument(stdout, stderr, report, 0)
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
