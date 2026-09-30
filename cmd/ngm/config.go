package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

// configUsage 是 `ngm config --help` 的简短说明。
const configUsage = `ngm config — inspect and validate configuration

USAGE:
  ngm config <subcommand>

SUBCOMMANDS:
  validate    Validate ngm.json against schema; exit 3 on failure
  show         Print the merged effective configuration (builtin → global → project)
`

// runConfig 处理 `ngm config <subcommand>`。
//
// v0.1 阶段只支持 validate 与 show；其他子命令返回 "未实现"。
func runConfig(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("config")
	fs.Usage = func() { fmt.Fprint(stderr, configUsage) }
	if err := fs.Parse(args); err != nil {
		return 3
	}
	if fs.NArg() == 0 {
		fmt.Fprint(stderr, configUsage)
		return 3
	}
	sub := fs.Arg(0)
	switch sub {
	case "validate":
		return runConfigValidate(ctx, stdout, stderr)
	case "show":
		return runConfigShow(ctx, stdout, stderr)
	default:
		fmt.Fprint(stderr, configUsage)
		return 3
	}
}

// runConfigValidate 校验当前工作目录的 ngm.json。
//
// 行为：
//   - 加载 builtin → global → project 三级配置；任何 schema 错误 → exit 3
//   - 校验通过 → stdout 打印 "ngm.json OK"（供 CI 解析）
//   - 校验失败 → stdout 打印具体错误并 exit 3
func runConfigValidate(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "get cwd", "", err))
	}
	home, _ := os.UserHomeDir()
	r, err := config.Load(cwd, home)
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
//	# project (./ngm.json)
//	<JSON of Effective>
//
// 每段以 # 注释行分隔，便于人眼阅读；机器解析请关注 JSON 段（连续两行 JSON）。
func runConfigShow(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "get cwd", "", err))
	}
	home, _ := os.UserHomeDir()
	r, err := config.Load(cwd, home)
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
	if err := enc("effective project (builtin → global → project merged)", r.Effective); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	return 0
}
