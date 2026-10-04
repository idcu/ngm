package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

// configUsage 是 `ngm config --help` 的简短说明。
const configUsage = `ngm config — inspect and validate configuration

USAGE:
  ngm config <subcommand> [--dir=<dir>]

SUBCOMMANDS:
  validate    Validate the project's ngm.json against the schema
  show        Print the merged effective configuration (builtin → global → project)

FLAGS:
  --dir       project directory (default: .) — resolved here, not by the caller's cwd

EXIT CODES:
  0  validated (or printed)
  3  configuration error: a schema violation, an unknown subcommand, or no ngm.json
     in the target directory (validate has nothing to validate → it is not a pass)
`

// runConfig 处理 `ngm config <subcommand>`。
//
// 只支持 validate 与 show；其他子命令打印用法并以 exit 3 结束（不假装成功）。
//
// v0.19 修的三件事（都属于"读数在说谎"那一族）：
//
//  1. **`--dir` 此前不存在**：本命令是唯一只认"进程 CWD"的项目命令，
//     因此想校验另一个目录只能 `chdir`（本项目自己的测试就是这么绕的）。
//  2. **多余位置参数此前被静默丢弃**：`fs.NArg()` 只检查了 `== 0`，
//     而 Go 的 flag 在第一个位置参数处停止解析——于是 `--dir=x`（或任何笔误）
//     变成多余位置参数、被无声吞掉，命令转头去校验**另一个**目录。
//  3. **没有 ngm.json 时照样打印 `ngm.json OK`**：那不是"通过"，是**假通过**
//     （CI 在最常见的错误用法——在错的目录里跑——会拿到绿色）。
func runConfig(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, configUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{{Name: "dir"}})); err != nil {
		return 3
	}
	// 恰好一个子命令：多给的输入宁可拒绝，也不静默丢掉（v0.15 的纪律）。
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, configUsage)
		return 3
	}
	switch fs.Arg(0) {
	case "validate":
		return runConfigValidate(ctx, stdout, stderr, *dirFlag)
	case "show":
		return runConfigShow(ctx, stdout, stderr, *dirFlag)
	default:
		fmt.Fprint(stderr, configUsage)
		return 3
	}
}

// runConfigValidate 校验 `--dir` 指向的项目清单（默认 `.`）。
//
// 行为：
//   - 目标目录里**没有 ngm.json** → exit 3（v0.19 修：此前打印 `ngm.json OK`，
//     那是**假通过**——CI 在错的目录里跑会拿到绿色）
//   - 加载 builtin → global → project 三级配置；任何 schema 错误 → exit 3
//   - 校验通过 → stdout 打印 "ngm.json OK"（供 CI 解析）
//   - 校验失败 → stdout 打印具体错误并 exit 3
func runConfigValidate(ctx context.Context, stdout, stderr io.Writer, dir string) int {
	abs, aerr := filepath.Abs(dir)
	if aerr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir", "", aerr))
	}
	if _, serr := os.Stat(filepath.Join(abs, "ngm.json")); serr != nil {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"no ngm.json in "+abs,
			"validate has nothing to validate there; pass --dir=<path> to point at a project, "+
				"or use `ngm config show` to inspect the merged config without gating"))
	}
	home, _ := os.UserHomeDir()
	r, err := config.Load(abs, home)
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "validate ngm.json", "fix the reported field or remove unknown fields", err))
	}
	fmt.Fprintln(stdout, "ngm.json OK")

	// 权限列表也要校验。`config.Load` 不会看它的内容（字段类型是 []string），
	// 而**一条**写错的命名空间会让整份列表都不被采用——那正是用户检查配置时
	// 最先跑这个命令的原因。
	pol, perr := newPermissionPolicy(r.GlobalEffective.Permissions)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	if conflicts := pol.Conflicts(); len(conflicts) > 0 {
		fmt.Fprintf(stderr, "warning: in both `allow` and `deny`: %s\n", strings.Join(conflicts, ", "))
		fmt.Fprintln(stderr, "         deny wins, so these are currently refused; remove them from one list")
	}
	return 0
}

// runConfigShow 打印三级合并后的生效配置。
//
// 输出结构：
//
//	# builtin defaults
//	<JSON of BuiltinDefaults>
//	# global (~/.ngm/config.json)  [absent if missing]
//	<JSON of GlobalEffective>
//	# effective project (builtin → global → project merged)
//	<JSON of Effective>
//
// 每段以 # 注释行分隔，便于人眼阅读；机器解析请关注 JSON 段（连续两行 JSON）。
//
// 与 validate 的分工：**show 是查看，不设门禁**——目标目录没有 ngm.json 时它照常
// 打印合并结果（并在标签里写明"没有项目清单"），exit 0。
func runConfigShow(ctx context.Context, stdout, stderr io.Writer, dir string) int {
	abs, aerr := filepath.Abs(dir)
	if aerr != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir", "", aerr))
	}
	home, _ := os.UserHomeDir()
	r, err := config.Load(abs, home)
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "load config", "see `ngm config validate` for details", err))
	}

	// 一段一段输出。每段都是有效 JSON；用稳定缩进。
	enc := func(label string, v any) error {
		fmt.Fprintf(stdout, "# %s\n", label)
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(out))
		return nil
	}

	if err := enc("builtin defaults", r.Builtin); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if err := enc("global (~/.ngm/config.json)", r.GlobalEffective); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	label := "effective project (builtin → global → project merged)"
	if r.Project == nil {
		// 没有项目清单时也要说出来：否则这段默认值会被读成"某份 ngm.json 的合并结果"。
		label += "  [no ngm.json in " + abs + " — builtin+global only]"
	}
	if err := enc(label, r.Effective); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	return 0
}
