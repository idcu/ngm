package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// 命令表：以 "name" 索引到实现。每个命令只负责一件事。
//
// 子命令各自的 Run 函数签名统一为 Run(ctx, args, stdout, stderr) int ——
// stdout/stderr 由 main 显式传入，便于测试与未来添加 `--sandbox` 等隔离模式。
type commandSpec struct {
	Name string
	Run  func(ctx context.Context, args []string, stdout, stderr io.Writer) int
	// Usage 是 `ngm <name> --help` 打印的文本。它**就是**参数错误时打印的那一份常量
	// （initUsage / verifyUsage / …），而不是另写的"帮助文本"。
	//
	// 复用而不是另写，是因为同一件事写在第二个地方就一定会有一处不一致——
	// 这个项目已经为这条纪律付过几次学费。顺带一句：`--help` 在 v0.11 之前
	// 打印的是 "help not yet implemented"（占位），而 help.go 的包注释声称
	// "--help 在所有子命令上下文可用"：注释说的是"能被解析"，用户看到的是另一回事。
	Usage string
}

// commands 是全部已实现的命令；表项顺序即 help 输出顺序（与 rootUsage 字符串顺序一致）。
// 每一项目前都有真实 Run——0.1 时代的 nil 占位与 notImplementedYet 已随 v0.3 移除。
var commands = []*commandSpec{
	{Name: "init", Run: runInit, Usage: initUsage},
	{Name: "add", Run: runAdd, Usage: addUsage},
	{Name: "install", Run: runInstall, Usage: installUsage},
	{Name: "update", Run: runUpdate, Usage: updateUsage},
	{Name: "remove", Run: runRemove, Usage: removeUsage},
	{Name: "verify", Run: runVerify, Usage: verifyUsage},
	{Name: "audit", Run: runAudit, Usage: auditUsage},
	{Name: "why", Run: runWhy, Usage: whyUsage},
	{Name: "tree", Run: runTree, Usage: treeUsage},
	{Name: "outdated", Run: runOutdated, Usage: outdatedUsage},
	{Name: "typecheck", Run: runTypecheck, Usage: typecheckUsage},
	{Name: "typedecl", Run: runTypeDecl, Usage: typedeclUsage},
	{Name: "build", Run: runBuild, Usage: buildUsage},
	{Name: "transform", Run: runTransform, Usage: transformUsage},
	{Name: "css", Run: runCSS, Usage: cssUsage},
	{Name: "mappings", Run: runMappings, Usage: mappingsUsage},
	{Name: "integrations", Run: runIntegrations, Usage: integrationsUsage},
	{Name: "cache", Run: runCache, Usage: cacheUsage},
	{Name: "store", Run: runStore, Usage: storeUsage},
	{Name: "config", Run: runConfig, Usage: configUsage},
	{Name: "engines", Run: runEngines, Usage: enginesUsage},
}

// 顶层分发：处理 --version / --help 后取首个非 flag 元素作为子命令。
//
// 退出码约定：见 errs 包与 architecture/observability.md。
func dispatch(args []string, stdout, stderr io.Writer) int {
	// 切分子命令
	sub := ""
	rest := args
	subIdx := -1
	for i, a := range args {
		if !looksLikeFlag(a) {
			sub = a
			subIdx = i
			rest = args[i+1:]
			break
		}
	}

	// 根级 --version / --help 只在**子命令之前**生效：
	//
	//	ngm --help           → 根帮助
	//	ngm verify --help    → verify 的帮助
	//
	// 此前这个循环扫描**整条** args（"避免子命令参数污染"），于是任何位置出现
	// `--help` 都返回根帮助 —— 下面那段"子命令自身的 --help"因此**永远走不到**，
	// 它打印的 "help not yet implemented" 也就没人见过。
	// 手工扫描而不是 flag.Parse：后续命令各自 flag.Parse 自己的 args。
	head := args
	if subIdx >= 0 {
		head = args[:subIdx]
	}
	for _, a := range head {
		switch a {
		case "--version":
			fmt.Fprintln(stdout, versionLine())
			return 0
		case "-h", "--help":
			fmt.Fprint(stdout, rootUsage)
			return 0
		}
	}
	if sub == "" {
		fmt.Fprint(stderr, rootUsage)
		return 3 // 缺子命令：CLI 用法错误，按"配置/用法错误"返回 3
	}

	spec := findCommand(sub)
	if spec == nil {
		fmt.Fprintf(stderr, "unknown command: %s\n\n%s\n", sub, rootUsage)
		return 3
	}

	// 处理子命令自身的 --version/help
	for _, a := range rest {
		switch a {
		case "--version":
			fmt.Fprintln(stdout, versionLine())
			return 0
		case "-h", "--help":
			// 打印该命令自己的用法常量：它与参数错误时看到的是同一份文本。
			// 走 stdout 且 exit 0 —— 用户主动要的，不是失败。
			fmt.Fprint(stdout, spec.Usage)
			return 0
		}
	}

	// 显式给了**空**目录 ⇒ 拒绝（v0.41 修）。
	//
	// Go 的 flag 把 `--dir=` 解析成空串，而空串与"没给这个 flag"在后续代码里无法区分——
	// 于是命令**静默地把空值当成 CWD**。实测后果（这条判据自己抓到的）：
	// `ngm add github:x/dep@v1 --dir=` 会去改**当前目录**的 ngm.json；
	// `ngm init github.com:x/app --dir=` 会在当前目录建出项目。
	//
	// 最可能的来源是变量展开失败（`--dir="$PROJ"` 而 `$PROJ` 未设），而用户看到的是"成功了"。
	// 用法文本写着 `default: .`——想用当前目录就别给这个 flag，给了空值一定是个错误。
	if emptyDirFlag(args) {
		fmt.Fprint(stderr, "ngm: `--dir=` was given an empty value — that is almost always an unset "+
			"variable (`--dir=\"$PROJ\"`); pass a real path, or drop the flag to use the current directory\n\n")
		fmt.Fprint(stderr, spec.Usage)
		return 3
	}

	// 参数顺序交由子命令用 normalizeArgs 处理（它知道自己的 flag 类型）。
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return spec.Run(ctx, rest, stdout, stderr)
}

// emptyDirFlag 报告参数里有没有"显式给了空目录"的写法。
//
// 三种拼法都要挡：`--dir=` · `-dir=` · `--dir ""`（两个 token，值是空串）。
// 只挡**显式给了空值**这一种情况；不给这个 flag 仍然是合法的（默认 `.`）。
func emptyDirFlag(args []string) bool {
	for i, a := range args {
		switch a {
		case "--dir=", "-dir=":
			return true
		case "--dir", "-dir":
			if i+1 < len(args) && args[i+1] == "" {
				return true
			}
		}
	}
	return false
}

// flagSpec 描述一个子命令 flag：名称与是否接受值。
//
// normalizeArgs 需要它来安全地重排参数——`--dir <path>` 必须作为**两个相邻 token**
// 一起移动，而 `--all`（布尔）绝不能吞掉紧随其后的 positional。
type flagSpec struct {
	Name string // 不含前缀，如 "dir"
	Bool bool   // true 表示布尔 flag（不接受值）
}

// normalizeArgs 把 flag 提到 positional 之前，让 Go 标准库 flag 能解析它们
// （flag.Parse 在遇到第一个 positional 时停止，因此 `ngm init <name> --dir=x` 需要重排）。
//
// 支持的形式：
//
//	--key=value / -key=value      单 token
//	--key value / -key value      两个相邻 token（仅当 key 声明为非 Bool）
//	--boolflag / -b               单 token
//	--                            之后一律视为 positional
//	位置参数                        保持相对顺序
//
// 未知的 `--xxx` 按"非布尔"处理（保守：把它与后一个 token 一起移动；
// 若后一个 token 也是 flag，则只移动自身）。
func normalizeArgs(args []string, specs []flagSpec) []string {
	valFlags := map[string]bool{}
	boolFlags := map[string]bool{}
	for _, s := range specs {
		if s.Bool {
			boolFlags["--"+s.Name] = true
			boolFlags["-"+s.Name] = true
			continue
		}
		valFlags["--"+s.Name] = true
		valFlags["-"+s.Name] = true
	}

	var flags, positional, rest []string
	afterDoubleDash := false
	for i := 0; i < len(args); i++ {
		a := args[i]

		if afterDoubleDash {
			// 单独攒着：它们**必须**留在 `--` 之后（见函数末尾的说明）。
			rest = append(rest, a)
			continue
		}
		if a == "--" {
			afterDoubleDash = true
			continue
		}

		if !looksLikeFlag(a) {
			positional = append(positional, a)
			continue
		}

		// --key=value 或 -key=value
		if strings.ContainsRune(a, '=') {
			flags = append(flags, a)
			continue
		}

		name := a
		if boolFlags[name] {
			flags = append(flags, a)
			continue
		}

		// --key value（值不形如 flag 时才成对移动）
		if valFlags[name] || !boolFlags[name] {
			if i+1 < len(args) && !looksLikeFlag(args[i+1]) {
				flags = append(flags, a, args[i+1])
				i++
				continue
			}
		}
		flags = append(flags, a)
	}
	// **把 `--` 还回去**（v0.40 修）。
	//
	// 分类本来就是对的：`--` 之后的 token 确实被归到了 positional。
	// 但**重排会把这个信息丢掉**——`--dir=<bad>` 被原样放到列表末尾，
	// 而 `flag.Parse` 的规则是「遇到第一个非 flag 才停」，于是它又被当成 flag 解析。
	// 实测：`ngm verify --dir=<好> -- --dir=<坏>` 用的是**坏**目录，
	// 与本函数开头承诺的「`--` 之后一律视为 positional」正好相反（10 个命令都这样）。
	//
	// 修法是在它们前面**重新插入一个 `--`**，让 flag.Parse 在那里停下；
	// `--` 自身会被它吃掉（不会出现在 `fs.Args()` 里），所以下游看到的东西不变。
	out := append(flags, positional...)
	if len(rest) > 0 {
		out = append(out, "--")
		out = append(out, rest...)
	}
	return out
}

func findCommand(name string) *commandSpec {
	for _, c := range commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// looksLikeFlag 判定字符串是否形如 `--x` 或 `-x`（含负数 `--name=-v1`）。
//
// 实现细节：保留任何带 `=` 的 `--xxx=valueX` 形式（含值为空），方便子命令传值。
func looksLikeFlag(s string) bool {
	return len(s) >= 2 && s[0] == '-'
}

// v0.3 起命令表里已没有占位项（`integrations` 是最后一个），
// 因此移除了 0.1 时代返回 exit 3 的 notImplementedYet。
//
// 保留一个"永不触发的占位函数"没有价值，反而会让 help 与错误信息里
// 继续出现"未实现"这种描述——那条信息是给尚未到位的命令用的，
// 现在写下它就是错的。将来若要新增命令，直接实现 Run 即可。

// runWithRecovery 捕获 Run 内部 panic，避免 CLI 留下非零退出但堆栈风暴。
// 任何 panic 都按 errs.CodeRefDrift (1) 退出。
func runWithRecovery(fn func() int) (exit int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "ngm: internal panic: %v\n", r)
			exit = 1
		}
	}()
	return fn()
}

// newFlagSet 是 flag.NewFlagSet 的本地化封装。当前包内容只以它替换直接调用，
// 保留位置以便未来添加 flag 共享（如全局 --no-color）。
// newFlagSet 造一个"解析失败不中断"的 flagset，并把它的**输出接到 stderr**。
//
// 为什么必须显式 SetOutput：`flag.ContinueOnError` 在解析失败时会做两件事——
// 把**错误行**写到 `fs.Output()`，再调用 `fs.Usage`（各命令已把它设成打印自己的用法）。
// 而 `fs.Output()` 默认是**进程的 os.Stderr**，不是调用方传进来的那个 writer。
// 不设它的后果实测过（v0.15）：冒烟测试里 21 个命令的 `flag provided but not defined: …`
// **直接打在测试进程的 stderr 上**（测试自己捕获的 buffer 里一个字都没有）。
// 在终端里它看起来"正常"（终端也是 os.Stderr），所以这类缺陷只在**嵌进来的调用方**
// （测试、将来的库用法、任何重定向了 writer 的地方）里现形——与 v0.12 的
// "hook 输出混进 stdout" 同族：**流是接口的一部分**。
//
// 顺带删掉了这里原本的 `errSilent` 哨兵：它的注释写着"让 ContinueOnError 静默"，
// 但它**从未被引用过**——一段描述着不存在的机制的注释，比没有注释更糟。
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}
