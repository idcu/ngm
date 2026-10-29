package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
)

const doctorUsage = `ngm doctor — diagnose the local ngm environment and project

USAGE:
  ngm doctor [--dir=<dir>] [--json]

FLAGS:
  --dir   project directory (default: .)
  --json  machine-readable report

CHECKS:
  git       git executable/version
  project   ngm.json can be read and parsed
  lock      ngm.lock can be read (missing lock is a warning, not a failure)
  layout    ~/.ngm root exists (missing is a warning; install creates it)
  store     mirror / module-cache / content-store usage
  modules   nested module manifests can be scanned

EXIT CODES:
  0  checks completed (inspect "ok"/warnings; doctor is diagnostic)
  3  usage error or the environment cannot be opened at all
`

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok / warn / fail
	Detail string `json:"detail"`
}

type doctorReport struct {
	Version int           `json:"version"`
	OK      bool          `json:"ok"`
	Checks  []doctorCheck `json:"checks"`
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("doctor", stderr)
	dir := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, doctorUsage) }
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true},
	})); err != nil {
		// v0.105：`doctor` 是 v0.104 新增的命令，没被 v0.54 那张网接管——
		// 实测 `ngm doctor --json --bogus` 的 stdout 是 **0 字节**，
		// 而用户明明提了 `--json`（要机器可读，却只拿到人读的 usage）。
		failEarly(args, stdout, err.Error(),
			"run `ngm doctor --help` for the flags it accepts")
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, doctorUsage)
		return 3
	}

	env, err := newProjectEnv(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	rep := doctorReport{Version: 1, OK: true}
	add := func(name, status, detail string) {
		if status == "fail" {
			rep.OK = false
		}
		rep.Checks = append(rep.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	if res, gerr := git.Run(ctx, env.GitOpts, "--version"); gerr != nil {
		add("git", "fail", "cannot run git: "+gerr.Error())
	} else {
		add("git", "ok", oneLine(string(res.Stdout)))
	}

	if pf, perr := env.ReadManifest(); perr != nil {
		add("project", "fail", "cannot read ngm.json; run `ngm config validate`")
	} else {
		add("project", "ok", pf.Name+" "+pf.Version)
	}

	lf, lerr := lock.Read(env.LockPath())
	switch {
	case lerr != nil:
		add("lock", "fail", "cannot read ngm.lock; re-run `ngm install`")
	case lf == nil:
		add("lock", "warn", "no ngm.lock yet; run `ngm install`")
	default:
		add("lock", "ok", fmt.Sprintf("%d dependencies", len(lf.Dependencies)))
	}

	if _, derr := os.Stat(env.Layout.Home); derr != nil {
		add("layout", "warn", "~/.ngm does not exist yet; install creates it")
	} else {
		add("layout", "ok", "~/.ngm")
	}

	mirror, merr := vendor.NewMirror(env.Layout.MirrorRoot(), env.GitOpts).Usage()
	mc, mcerr := vendor.NewModuleCache(env.Layout.ModuleCacheRoot(), env.GitOpts).Usage()
	store, serr := vendor.NewContentStore(env.Layout.ContentRoot()).Usage()
	if merr != nil || mcerr != nil || serr != nil {
		add("store", "fail", fmt.Sprintf("usage failed: mirror=%v module-cache=%v content=%v", merr, mcerr, serr))
	} else {
		add("store", "ok", fmt.Sprintf("mirror %d, module-cache %d, content %d",
			mirror.Repos, mc.Repos, store.V2Trees))
	}

	mods, modErr := scanModules(*dir)
	if modErr != nil {
		add("modules", "warn", "cannot scan module manifests under --dir")
	} else {
		add("modules", "ok", fmt.Sprintf("%d module(s)", len(mods)))
	}

	if *jsonOut {
		return writeModulesJSON(stdout, stderr, rep)
	}
	for _, c := range rep.Checks {
		fmt.Fprintf(stdout, "%-8s %-5s %s\n", c.Name, c.Status, c.Detail)
	}
	if rep.OK {
		fmt.Fprintln(stdout, "\nOK")
	} else {
		fmt.Fprintln(stdout, "\nFAIL")
	}
	return 0
}

func oneLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
