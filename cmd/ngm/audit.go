package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/supplychain"
	"github.com/idcu/ngm/internal/vendor"
)

const auditUsage = `ngm audit — check locked dependencies against known vulnerabilities (OSV.dev)

USAGE:
  ngm audit [<dep>...] [--dir=<dir>] [--offline] [--no-cache] [--json]

ARGS:
  <dep>   audit only these dependency slugs (default: everything in ngm.lock)

FLAGS:
  --dir        project directory containing ngm.json/ngm.lock (default: .)
  --offline    never touch the network; use only the local OSV cache.
               A dependency with no cached result fails with exit 4.
  --no-cache   ignore the cached result and re-query OSV.dev
  --json       write a machine-readable report to stdout (CI should use this)
  --hook=<path>  run this script in a Deno sandbox after the audit. It receives the
               report as JSON on stdin; exit 0 to accept, non-zero to reject (exit 1).
               This is how a team adds its own policy on top of OSV - it needs Deno,
               and ngm will not run it unsandboxed

EXIT CODES:
  0  no vulnerabilities above the configured threshold
  1  at least one vulnerability above the threshold, or --hook rejected the report
  3  configuration or lock error (including a --hook path that does not exist)
  4  OSV network failure and no usable cache
  5  --hook was given (or the sandbox was requested) and Deno is missing

IMPORTANT — read the coverage note in every report. A clean result means
"no known entry for this commit in OSV.dev", NOT "proven safe": zero-day
issues are absent from the database, and most advisories are recorded
against semver versions rather than Git commits.

CACHE:
  Results are cached under <NGM_HOME>/cache/osv/<commit>.json for 24 hours.
  --offline reads the cache only; --no-cache bypasses it.
`

// runAudit 处理 `ngm audit`。
//
// 与 verify 的分工：
//
//	verify  证明"落地的内容与锁定的一致"（完整性）
//	audit   报告"锁定的 commit 是否有已知漏洞"（已知风险）
func runAudit(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("audit")
	dirFlag := fs.String("dir", ".", "project directory")
	offline := fs.Bool("offline", false, "never touch the network")
	noCache := fs.Bool("no-cache", false, "bypass the OSV cache and re-query")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	hook := fs.String("hook", "", "script to run in the sandbox with the report on stdin")
	fs.Usage = func() { fmt.Fprint(stderr, auditUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"},
		{Name: "offline", Bool: true},
		{Name: "no-cache", Bool: true},
		{Name: "json", Bool: true},
		{Name: "hook"},
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

	// 策略只提供"忽略哪些严重级别"这一项输入；其余门禁不参与 audit
	pol, perr := projectPolicy(pf)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}

	rep, aerr := supplychain.Audit(ctx, lf, supplychain.AuditOptions{
		OSV: supplychain.OSVConfig{
			CacheDir: vendor.NewCache(env.Layout.CacheRoot()).OSVRoot(),
			// NGM_OSV_URL 让测试能指向 httptest 服务器。这不是"后门"：
			// 全项目纪律是禁止测试依赖公网，而没有它 audit 的测试就只能连真实 OSV.dev。
			BaseURL: os.Getenv("NGM_OSV_URL"),
			Offline: *offline,
			NoCache: *noCache,
		},
		IgnoreSeverities: pol.IgnoredSeverities(),
	})
	if aerr != nil {
		return runErr(ctx, stdout, stderr, aerr)
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
		return runAuditHook(ctx, env, rep, *hook, rep.ExitCode, stdout, stderr)
	}

	rep.Render(stdout)
	return runAuditHook(ctx, env, rep, *hook, rep.ExitCode, stdout, stderr)
}
