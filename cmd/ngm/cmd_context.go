package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/core/errs"
)

// cmdContext 是子命令开头那段的唯一实现（P2-3）。
//
// 为什么要有它：命令之间逐字重复同一段
//
//	造 flagset → 设 usage → parse + failEarly → markJSON → newProjectEnv + runErr
//	→ JSON 报告序列化
//
// 重复本身不是问题，问题是**每条通道都可能各自漂移**：v0.54 修过一条 parse
// 失败的信封，v0.105 又发现 doctor 漏了，v0.108 又发现 why 的位置参数分支漏了。
// 抽成一处之后，新命令只有一条路可走。
//
// 它**不**接管 flag 的声明：`newFlagSet("name")` 与 `fs.Bool(...)` 留在各自文件里，
// 因为 v0.45 的派生网按这两个字面量切块（见 subcommand_flag_semantics_test.go）。
// 也不接管 `fs.NArg()` 那类**各命令不同**的位置参数判据——那些是命令自己的语义，
// 不是骨架。
type cmdContext struct {
	ctx    context.Context
	args   []string
	stdout io.Writer
	stderr io.Writer
	fs     *flag.FlagSet
	// jsonAware 表示这条命令**接受** `--json`（默认 true）。
	//
	// 只有那些 stdout 本身就是机器可读文档的命令会关掉它（`sbom` / `attest`：
	// 它们的输出是 CycloneDX / in-toto 语句，再加一个 `--json` 没有意义）。
	// 关掉之后解析失败**不写**错误信封——依据 v0.16 规则④："命令不接受 `--json`
	// 时 stdout 仍为空"是正确行为，给一个它从未承诺的信封反而是一条新契约。
	// 判据：`early_failure_envelope_test.go` 的对照二（`add --json` 的 stdout 为空）。
	jsonAware bool
}

// newCmdContext 绑定一次调用的四个流/参数，以及命令自己已经造好的 flagset。
//
// 默认这条命令接受 `--json`：解析失败时按 v0.54 从**原始参数**里问一次，
// 该给信封就给。
func newCmdContext(ctx context.Context, args []string, stdout, stderr io.Writer, fs *flag.FlagSet) *cmdContext {
	return &cmdContext{ctx: ctx, args: args, stdout: stdout, stderr: stderr, fs: fs, jsonAware: true}
}

// newCmdContextNoJSON 同 newCmdContext，但这条命令**不接受** `--json`。
//
// 用于 stdout 本身即文档的命令（`sbom` / `attest`）。语义差别只有一处：
// `--json` 出现在参数里时，解析失败**不写**错误信封（见 jsonAware）。
func newCmdContextNoJSON(ctx context.Context, args []string, stdout, stderr io.Writer, fs *flag.FlagSet) *cmdContext {
	return &cmdContext{ctx: ctx, args: args, stdout: stdout, stderr: stderr, fs: fs}
}

// usage 把命令的用法文本接到 stderr（flag 包在解析失败时会调用它）。
func (c *cmdContext) usage(text string) {
	c.fs.Usage = func() { fmt.Fprint(c.stderr, text) }
}

// parse 解析参数并把 `--json` 记进 `c.ctx`（随后调用方 `ctx = c.ctx`）。
//
// 解析失败时：给 `--json` 信封（看的是**原始参数**，因为此刻命令还不知道自己有没有
// 该 flag），返回 false；调用方统一 `return 3`。命令不接受 `--json` 时不写信封。
//
// jsonOut 为 nil 表示不把 flag 的值记进 ctx——注意这与"命令没有 `--json`"**不是**
// 一回事：`verify` 就是先用 nil 调（它要把 `--format` 的校验放在 `--json` 生效之前），
// 但它接受 `--json`，所以解析失败时照样给信封。是否需要信封看 jsonAware。
func (c *cmdContext) parse(specs []flagSpec, jsonOut *bool) bool {
	if err := c.fs.Parse(normalizeArgs(c.args, specs)); err != nil {
		if c.jsonAware {
			failEarly(c.args, c.stdout, err.Error(),
				"run `ngm "+c.fs.Name()+" --help` for the flags it accepts")
		}
		return false
	}
	if jsonOut != nil && *jsonOut {
		c.ctx = withJSON(c.ctx)
	}
	return true
}

// markJSON 记录一次 `--json` 请求并返回带标记的 ctx。
//
// 多数命令在 parse 时就把这件事做了（parse 的 jsonOut 参数）；少数命令需要把
// 校验（如 verify 的 `--level` / `--format`）放在 **`--json` 生效之前**，于是
// 它们用 nil 调 parse，再在合适的位置调这个方法。两条路径写的是同一份状态。
func (c *cmdContext) markJSON(requested bool) context.Context {
	if requested {
		c.ctx = withJSON(c.ctx)
	}
	return c.ctx
}

// env 打开项目运行环境；失败时按统一通道（`--json` 模式下含错误信封）写出并返回退出码。
//
// 返回码为 0 表示成功；调用方 `if code != 0 { return code }`。
func (c *cmdContext) env(dir string) (*projectEnv, int) {
	env, err := newProjectEnv(dir)
	if err != nil {
		return nil, c.fail(err)
	}
	return env, 0
}

// fail 是 runErr 的 ctx 绑定版：命令不必每次把三个流再传一遍。
func (c *cmdContext) fail(err error) int {
	return runErr(c.ctx, c.stdout, c.stderr, err)
}

// jsonMarshaler 是一份报告的机器可读形态。outdated / tree / verify / why 的报告
// 直接用它序列化；audit 与 install 在报告之后还有 hook / 附加层，仍走各自的收尾
// （install 用的是本包既有的 emitWriteReport）。
type jsonMarshaler interface {
	Marshal() ([]byte, error)
}

// writeReport 把报告序列化并写到 stdout，返回进程退出码。
//
// 成功时返回 okCode（各命令自己的结论码）；序列化失败走 c.fail（错误码由错误本身
// 决定）；写出失败固定 6——"结论已经得出但送不出去"（ADR-026）。
func (c *cmdContext) writeReport(rep jsonMarshaler, okCode int) int {
	data, err := rep.Marshal()
	if err != nil {
		return c.fail(err)
	}
	if _, werr := c.stdout.Write(data); werr != nil {
		fmt.Fprintf(c.stderr, "write report: %v\n", werr)
		return errs.CodeInternal.ExitCode()
	}
	return okCode
}
