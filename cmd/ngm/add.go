package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/resolve"
)

const addUsage = `ngm add — add a Git dependency to ngm.json

USAGE:
  ngm add <git-url>[@<ref>] --ref-type <type> [--path=<sub/path>] [--dir=<dir>]

ARGS:
  <git-url>   one of: https://host/org/repo[.git] | git@host:org/repo |
              git://host/org/repo | host:org/repo (shorthand)
  @<ref>      tag / branch / commit hash (optional; --ref-type is still required)

FLAGS:
  --ref-type  REQUIRED: tag | branch | commit
              (ngm never silently infers it; see guides/configuration.md)
  --path      monorepo sub-path; equivalent to the "#path=" URL fragment
  --dir       project directory containing ngm.json (default: .)
  --dry-run   print the resulting ngm.json without writing

EXAMPLES:
  ngm add github:my-org/utils@v1.2.3 --ref-type tag
  ngm add github:my-org/logger@main   --ref-type branch
  ngm add github:my-org/monorepo#path=packages/core@v2.0.0 --ref-type tag

EXIT CODES:
  0  the declaration was written (or printed, with --dry-run)
  3  configuration error: --ref-type missing, the git address could not be parsed,
     or ngm.json could not be read or written
`

// runAdd 处理 `ngm add <git-url>[@<ref>] --ref-type <type>`。
//
// M1 范围：解析规格、校验 refType 必填、写 ngm.json。
// ref → commit 的解析与 lock 写入属于 install（M3）；本命令不触网。
func runAdd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("add", stderr)
	refType := fs.String("ref-type", "", "tag | branch | commit (required)")
	pathFlag := fs.String("path", "", "monorepo sub-path")
	dirFlag := fs.String("dir", ".", "project directory")
	dryRun := fs.Bool("dry-run", false, "print the resulting ngm.json without writing")
	fs.Usage = func() { fmt.Fprint(stderr, addUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "ref-type"}, {Name: "path"}, {Name: "dir"}, {Name: "dry-run", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() != 1 {
		fmt.Fprint(stderr, addUsage)
		return 3
	}
	specArg := fs.Arg(0)

	// 1) 解析规格
	spec, err := resolve.ParseAddSpec(specArg)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// --path 覆盖 `#path=`（显式 flag 优先）
	if *pathFlag != "" {
		spec.Path = *pathFlag
	}

	// 2) refType 必填（推断仅用于给出建议）
	if *refType == "" {
		return runErr(ctx, stdout, stderr, missingRefTypeError(spec))
	}
	rt := resolve.RefType(*refType)
	if !rt.IsValid() {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("invalid --ref-type %q", *refType),
			fmt.Sprintf("must be one of %v", resolve.ValidRefTypes())))
	}

	// 3) ref 处理：commit 需要显式 hash；tag/branch 需要名字
	if spec.Ref == "" {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			fmt.Sprintf("missing ref in %q", specArg),
			"append `@<ref>` (e.g. `github:org/repo@v1.2.3`) or pass it explicitly"))
	}

	// 4) 读取并更新 ngm.json
	projectDir, err := filepath.Abs(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir", "", err))
	}
	cfgPath := filepath.Join(projectDir, "ngm.json")

	pf, err := config.ReadProjectFile(cfgPath)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	dep := config.Dependency{
		Name:    spec.Repo.Slug(),
		Ref:     spec.Ref,
		RefType: rt,
		Path:    spec.Path,
	}
	if err := dep.Validate(); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "invalid dependency", "", err))
	}

	added, err := pf.UpsertDependency(dep)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	if *dryRun {
		out, merr := config.MarshalProjectFile(pf)
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}
		fmt.Fprint(stdout, string(out))
		return 0
	}

	if err := config.WriteProjectFile(cfgPath, pf); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	verb := "updated"
	if added {
		verb = "added"
	}
	fmt.Fprintf(stdout, "%s %s@%s (%s) in %s\n", verb, dep.Name, dep.Ref, dep.RefType, cfgPath)
	if added {
		fmt.Fprintln(stdout, "next: run `ngm install` to resolve and lock")
	}
	return 0
}

// missingRefTypeError 在用户漏写 --ref-type 时给出含推断建议的错误。
//
// 设计理由：refType 必填是 ngm 的声明自描述原则（ADR-004）。
// 推断可能出错（branch 叫 v1.2.3、tag 叫 main），因此只作为 Hint 呈现，
// 绝不静默写入。
func missingRefTypeError(spec resolve.AddSpec) error {
	msg := fmt.Sprintf("--ref-type is required for %q", spec.Repo.Slug())
	hint := "add one of: --ref-type tag | --ref-type branch | --ref-type commit"
	if spec.Ref != "" {
		if inferred, ok := resolve.InferRefType(spec.Ref); ok {
			hint = fmt.Sprintf(
				"`%s` looks like a %s, but inference can be wrong (a branch may be named `v1.2.3`); pass --ref-type %s explicitly",
				spec.Ref, inferred, inferred)
		}
	}
	return errs.New(errs.CodeConfigInvalid, msg, hint)
}
