package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
)

const mappingsUsage = `ngm mappings — inspect and validate ngm.mappings.json

USAGE:
  ngm mappings <subcommand> [--dir=<dir>]

SUBCOMMANDS:
  validate   check that every mapping is consistent with ngm.lock and the vendor tree

FLAGS:
  --dir      project directory (default: .)
`

// runMappings 处理 `ngm mappings <subcommand>`。
func runMappings(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("mappings")
	dirFlag := fs.String("dir", ".", "project directory")
	fs.Usage = func() { fmt.Fprint(stderr, mappingsUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{{Name: "dir"}})); err != nil {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, mappingsUsage)
		return 3
	}
	switch fs.Arg(0) {
	case "validate":
		return runMappingsValidate(ctx, *dirFlag, stdout, stderr)
	default:
		fmt.Fprint(stderr, mappingsUsage)
		return 3
	}
}

// runMappingsValidate 校验 mappings 与 lock / vendor 的一致性。
//
// 检查项（modules/p4-ecosystem.md §校验）：
//
//	from 是否在 ngm.lock 中
//	to 路径是否存在
//	main / types 文件是否存在
//
// 退出码：0 通过（允许仅有警告）；3 存在致命问题（配置类）。
func runMappingsValidate(ctx context.Context, dirFlag string, stdout, stderr io.Writer) int {
	env, err := newProjectEnv(dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	f, err := mappings.Read(mappings.Find(env.ProjectDir))
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if f == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			mappings.FileName+" not found in "+env.ProjectDir,
			"run `ngm install` to generate it"))
	}

	// 从 lock 构造"已锁定依赖集合"（不强制要求 lock 存在：
	// 缺 lock 时跳过该维度，仍能校验 to / main / types）
	var lockNames map[string]bool
	lf, lerr := lock.Read(env.LockPath())
	if lerr != nil {
		return runErr(ctx, stdout, stderr, lerr)
	}
	if lf != nil {
		lockNames = make(map[string]bool, len(lf.Dependencies))
		for i := range lf.Dependencies {
			lockNames[lf.Dependencies[i].Name] = true
		}
	}

	findings, err := mappings.Validate(f, mappings.ValidateEnvironment{
		ProjectDir: env.ProjectDir,
		LockNames:  lockNames,
	})
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	if len(findings) == 0 {
		fmt.Fprintf(stdout, "%s OK (%d mappings)\n", mappings.FileName, len(f.Mappings))
		return 0
	}

	text, fatal := mappings.FormatFindings(findings)
	fmt.Fprint(stderr, text)

	if fatal > 0 {
		return runErr(ctx, stdout, stderr, mappings.ErrInvalid(fmt.Sprintf(
			"mappings validation failed: %d error(s)", fatal)))
	}

	// 只有警告：不阻断，但明确告知
	fmt.Fprintf(stdout, "%s OK with %d warning(s)\n", mappings.FileName, len(findings))
	return 0
}
