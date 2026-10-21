package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/resolve"
)

const addUsage = `ngm add — add a Git dependency to ngm.json

USAGE:
  ngm add <git-url>[@<ref>] [--ref-type=<type>] [--infer-ref-type] [--path=<sub/path>] [--dir=<dir>]

ARGS:
  <git-url>   one of: https://host/org/repo[.git] | git@host:org/repo |
              git://host/org/repo | host:org/repo (shorthand)
  @<ref>      tag / branch / commit hash (optional)

FLAGS:
  --ref-type  tag | branch | commit; optional only when <ref> is a full 40-char commit
              (otherwise ngm never silently infers it; see guides/configuration.md)
  --infer-ref-type
              use one ls-remote to disambiguate a tag/branch ref (network; only when
              --ref-type is missing). If the ref is both a tag and a branch, ngm fails.
  --path      monorepo sub-path; equivalent to the "#path=" URL fragment
  --dir       project directory containing ngm.json (default: .)
  --dry-run   print the resulting ngm.json without writing

EXAMPLES:
  ngm add github:my-org/utils@v1.2.3 --ref-type tag
  ngm add github:my-org/logger@main   --ref-type branch
  ngm add github:my-org/utils@v1.2.3 --infer-ref-type
  ngm add github:my-org/monorepo#path=packages/core@v2.0.0 --ref-type tag

EXIT CODES:
  0  the declaration was written (or printed, with --dry-run)
  3  configuration error: --ref-type missing, the git address could not be parsed,
     or ngm.json could not be read or written
`

// runAdd 处理 `ngm add <git-url>[@<ref>] --ref-type <type>`。
//
// M1 范围：解析规格、校验 refType、写 ngm.json。
// ref → commit 的解析与 lock 写入属于 install（M3）；默认本命令不触网，
// 只有显式 `--infer-ref-type` 才会做一次远端 ls-remote。
func runAdd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("add", stderr)
	refType := fs.String("ref-type", "", "tag | branch | commit (optional for a full 40-char commit)")
	infer := fs.Bool("infer-ref-type", false, "use the remote to disambiguate tag/branch")
	pathFlag := fs.String("path", "", "monorepo sub-path")
	dirFlag := fs.String("dir", ".", "project directory")
	dryRun := fs.Bool("dry-run", false, "print the resulting ngm.json without writing")
	fs.Usage = func() { fmt.Fprint(stderr, addUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "ref-type"}, {Name: "infer-ref-type", Bool: true},
		{Name: "path"}, {Name: "dir"}, {Name: "dry-run", Bool: true},
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

	// 2) refType：完整 40 位 hash 可窄默认 commit；tag/branch 仍必须显式，
	// 除非用户显式要求用远端证据推断（--infer-ref-type）。
	if *refType == "" && resolve.IsFullCommit(spec.Ref) {
		*refType = string(resolve.RefTypeCommit)
	}
	if *refType == "" && *infer {
		env, ierr := newProjectEnv(*dirFlag)
		if ierr != nil {
			return runErr(ctx, stdout, stderr, ierr)
		}
		inferred, ierr := resolve.InferRefTypeFromRemote(ctx, spec.Repo, spec.Ref, resolve.ResolveOptions{
			GitURL:   spec.Repo.CloneURL(env.Protocol),
			Protocol: env.Protocol,
			Secrets:  env.Secrets,
			Policy:   env.Policy,
		})
		if ierr != nil {
			return runErr(ctx, stdout, stderr, ierr)
		}
		*refType = string(inferred)
		fmt.Fprintf(stderr, "inferred refType %s from the remote for %s@%s\n",
			inferred, spec.Repo.Slug(), spec.Ref)
	}
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
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir",
			"pass an absolute --dir, or run ngm from the directory you mean", err))
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
		// 兜底建议（v0.48 引入、v0.49 统一成 WrapUnlessHinted）：它讲的是**仓库标识**的
		// 形状，只在**内层没话说**时才用它——否则用户会为 path 挨骂、却被指去查仓库标识。
		// v0.49 起 `dep.Validate()` 的每个字段分支都自己带建议，于是这句兜底
		// 只在"说不出是哪个字段"时出场。
		return runErr(ctx, stdout, stderr, errs.WrapUnlessHinted(errs.CodeConfigInvalid,
			"invalid dependency",
			"the accepted shape is `<host>:<org>/<repo>[@<ref>]` with --ref-type=tag|branch|commit; "+
				"see `ngm add --help`", err))
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
	hint := "add one of: --ref-type tag | --ref-type branch | --ref-type commit, or pass --infer-ref-type"
	if spec.Ref != "" {
		if inferred, ok := resolve.InferRefType(spec.Ref); ok {
			hint = fmt.Sprintf(
				"`%s` looks like a %s, but inference can be wrong (a branch may be named `v1.2.3`); pass --ref-type %s explicitly",
				spec.Ref, inferred, inferred)
		}
	}
	return errs.New(errs.CodeConfigInvalid, msg, hint)
}
