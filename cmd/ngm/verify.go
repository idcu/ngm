package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/verify"
)

const verifyUsage = `ngm verify — check ref drift and replay archiveDigest

USAGE:
  ngm verify [<dep>...] [--dir=<dir>] [--offline] [--deep] [--strict] [--allow-drift] [--json]

ARGS:
  <dep>   verify only these dependency slugs (default: everything in ngm.lock)

FLAGS:
  --dir          project directory containing ngm.json/ngm.lock (default: .)
  --offline      never touch the network; compare against the local mirror snapshot.
                 Results are marked stale, and a cold mirror fails with exit 4.
  --deep         also verify bytes: replay the digest from the installed content
                 tree and hash every vendor file against it
                 (default only compares structure: paths, symlink targets, sizes)
  --strict       treat expected updates (a branch that advanced) as failures
  --allow-drift  do not fail on unexpected drift (critical still returns 2)
  --json         write a machine-readable report to stdout (CI should use this)

CHECK LEVELS (architecture/observability.md):
  ref      re-resolve refType and compare with the commit pinned in ngm.lock
  digest   rebuild the manifest from the local mirror at that commit and replay
           archiveDigest (fully local, works offline)
  landing  content store metadata + vendor tree structure; --deep adds byte hashes

EXIT CODES:
  0  everything matches (or only expected updates, unless --strict)
  1  unexpected drift: a tag was moved, or a branch history was rewritten
  2  integrity failure: digest replay mismatch, or tampered content/vendor bytes
  3  configuration or lock error
  4  Git or network failure (including --offline with a cold mirror)

driftKind in --json: expected | unexpected | critical
`

// runVerify 处理 `ngm verify`。
//
// 与 install/update 的分工（architecture/locking.md §更新策略）：
//
//	install  尊重 lock：让内容落地
//	update   重新解析 ref：改写 lock
//	verify   只检查、绝不改写：报告 ref 漂移与 digest 是否可重放
func runVerify(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("verify")
	dirFlag := fs.String("dir", ".", "project directory")
	offline := fs.Bool("offline", false, "never touch the network")
	deep := fs.Bool("deep", false, "hash every vendor file against the content store")
	strict := fs.Bool("strict", false, "treat expected updates as failures")
	allowDrift := fs.Bool("allow-drift", false, "do not fail on unexpected drift")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, verifyUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"},
		{Name: "offline", Bool: true},
		{Name: "deep", Bool: true},
		{Name: "strict", Bool: true},
		{Name: "allow-drift", Bool: true},
		{Name: "json", Bool: true},
	})); err != nil {
		return 3
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"no ngm.lock found at "+env.LockPath(),
			"run `ngm install` to resolve dependencies and create the lock"))
	}

	if names := fs.Args(); len(names) > 0 {
		filtered, ferr := filterLock(lf, names)
		if ferr != nil {
			return runErr(ctx, stdout, stderr, ferr)
		}
		lf = filtered
	}

	opts := verify.Options{
		Offline:     *offline,
		Deep:        *deep,
		Strict:      *strict,
		AllowDrift:  *allowDrift,
		MirrorRoot:  env.Layout.MirrorRoot(),
		ContentRoot: env.Layout.ContentRoot(),
		VendorRoot:  env.VendorRoot(pf),
		GitOpts:     env.GitOpts,
		Protocol:    env.Protocol,
		// --offline 的"绝不触网"承诺在这里落地：换成一个只认既有 mirror 的实现。
		EnsureMirror: env.EnsureMirror,
	}
	if *offline {
		opts.EnsureMirror = offlineEnsureMirror(env)
	} else {
		// 在线：ref 问远端（一次 ls-remote，不传对象），对象按需才取（ADR-010）
		opts.ResolveRemoteRef = env.RemoteRefResolver()
	}

	rep, verr := verify.Run(ctx, lf, opts)
	if verr != nil {
		return runErr(ctx, stdout, stderr, verr)
	}

	if *jsonOut {
		data, merr := rep.Marshal()
		if merr != nil {
			return runErr(ctx, stdout, stderr, merr)
		}
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "write report: %v\n", werr)
			return 1
		}
		return rep.Summary.ExitCode
	}

	for i := range rep.Dependencies {
		renderVerifyDep(stdout, &rep.Dependencies[i])
	}
	fmt.Fprintf(stdout, "\n%s\n", verifySummaryLine(rep, *offline))
	return rep.Summary.ExitCode
}

// renderVerifyDep 渲染单个依赖的结果。
//
// 结构对齐 observability.md §ngm verify 的输出示例：
//
//	✓ <name>@<ref> (<refType>) → <short commit> — match
//	⚠ ... (now <short>) — expected update
//	  → driftKind: expected; <remediation>
//
// 符号语义（与文档一致）：✓ 通过、⚠ 预期更新、✗ 非预期漂移或完整性失败。
func renderVerifyDep(w io.Writer, d *verify.DepResult) {
	mark, headline := "✓", "match"
	switch {
	case d.Err != "":
		mark, headline = "✗", "check incomplete"
	case d.DriftKind == verify.DriftExpected:
		mark, headline = "⚠", "expected update"
	case d.DriftKind == verify.DriftUnexpected:
		mark, headline = "✗", "unexpected drift"
	case d.DriftKind == verify.DriftCritical:
		mark, headline = "✗", "integrity failure"
	}

	where := ""
	if d.SubPath != "" {
		where = "#" + d.SubPath
	}
	fmt.Fprintf(w, "%s %s%s@%s (%s) → %s", mark, d.Name, where, d.Ref, d.RefType, git.ShortSHA(d.Commit))
	if d.ResolvedCommit != "" && d.ResolvedCommit != d.Commit {
		fmt.Fprintf(w, " (now %s)", git.ShortSHA(d.ResolvedCommit))
	}
	if d.Stale {
		fmt.Fprintf(w, " [stale]")
	}
	fmt.Fprintf(w, " — %s\n", headline)

	// 只展开未通过的检查：通过的检查对用户没有信息量，失败细节才是行动依据
	for _, c := range d.Checks {
		if c.Status == verify.StatusOK {
			continue
		}
		fmt.Fprintf(w, "    %s: %s\n", c.Check, c.Detail)
	}
	if d.Err != "" {
		fmt.Fprintf(w, "    error: %s\n", d.Err)
	}
	if d.DriftKind != verify.DriftNone && d.Remediation != "" {
		fmt.Fprintf(w, "  → driftKind: %s; %s\n", d.DriftKind, d.Remediation)
	}
}

// verifySummaryLine 生成末行汇总。
func verifySummaryLine(rep verify.Report, offline bool) string {
	s := rep.Summary
	var parts []string
	for _, p := range []struct {
		n    int
		word string
	}{
		{s.OK, "ok"},
		{s.Expected, "expected"},
		{s.Unexpected, "unexpected"},
		{s.Critical, "critical"},
		{s.Operational, "incomplete"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.word))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing to verify")
	}

	out := fmt.Sprintf("verified %d dependency(ies): %s (exit %d)",
		s.Total, strings.Join(parts, ", "), s.ExitCode)
	if offline {
		out += "\nnote: --offline — refs were compared against the local mirror snapshot (stale)"
	}
	return out
}

// filterLock 把 lock 过滤为只含指定依赖的子集。
//
// 用于 `ngm verify <dep>...`：CI 可以只对变更的依赖做门禁。
// 未在 lock 中出现的名字按配置错误处理（exit 3）——静默忽略会让
// "我验过了"变成假象。
func filterLock(lf *lock.File, names []string) (*lock.File, error) {
	out := lock.NewFile()
	for _, name := range names {
		want, err := resolve.ParseSlug(name)
		if err != nil {
			return nil, err
		}
		found := false
		for i := range lf.Dependencies {
			d := lf.Dependencies[i]
			got, perr := resolve.ParseSlug(d.Name)
			if perr != nil {
				continue
			}
			if got.Equal(want) {
				out.Dependencies = append(out.Dependencies, d)
				found = true
			}
		}
		if !found {
			return nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("dependency %q is not present in ngm.lock", name),
				"run `ngm install` first, or omit the argument to verify every dependency")
		}
	}
	return out, nil
}
