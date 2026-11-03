package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/idcu/ngm/internal/config"
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
  vendor    vendor tree readout: present / ignored-by-git / absent (read-only)
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

	pf, perr := env.ReadManifest()
	if perr != nil {
		add("project", "fail", "cannot read ngm.json; run `ngm config validate`")
		pf = nil
	} else {
		add("project", "ok", pf.Name+" "+pf.Version)
	}
	vendorStatus, vendorDetail := doctorVendorReadout(ctx, env, pf)
	add("vendor", vendorStatus, vendorDetail)

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

// doctorVendorReadout 报告 `ngm.vendor/` 的存在性与 Git 可见性。
//
// 同一个 vendor 目录，有的项目随仓库分发（部署机不装 npm、不联网），有的项目
// 由 `.gitignore` 排除并靠 `install` 重建，两种都对。错的只有一种：**打算分发、
// 却被 Git 忽略**——冷克隆的 CI 会在 `install --frozen-lockfile --offline` 处以
// exit 4 失败，而错误信息指向"缺内容"，指不到"你的 .gitignore 排除了整棵树"。
//
// 因此这里只**报告**、不判定：present / ignored-by-git / absent 三种读数都不改变
// 退出码语义（doctor 恒 0），也不执行任何 Git 写操作——ADR-038 规定
// internal/core/git 是唯一网关，这里只用 `check-ignore` 这个只读探测。
func doctorVendorReadout(ctx context.Context, env *projectEnv, pf *config.ProjectFile) (string, string) {
	root := env.VendorRoot(pf)
	if pf != nil && pf.Vendor != nil && pf.Vendor.Mode == "global" {
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			return "warn", "global vendor absent at " + root + "; run `ngm install`"
		}
		return "ok", "global vendor at " + root + " (outside the project work tree; Git cannot track it)"
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "warn", "absent at ./" + vendor.VendorDirName + "; run `ngm install`"
	}
	count := 0
	if entries, rerr := os.ReadDir(root); rerr == nil {
		count = len(entries)
	}

	opts := env.GitOpts
	opts.Dir = env.ProjectDir
	res, gerr := git.RunAllowFailure(ctx, opts, "check-ignore", "-q", "--", vendor.VendorDirName)
	if gerr != nil {
		return "ok", fmt.Sprintf("present (%d entries); Git state unknown: %v", count, gerr)
	}
	switch res.ExitCode {
	case 0:
		return "warn", fmt.Sprintf(
			"present (%d entries) but ignored by Git — if you intend to ship it, check .gitignore and `git add %s/`",
			count, vendor.VendorDirName)
	case 1:
		return "ok", fmt.Sprintf("present (%d entries); Git does not ignore it", count)
	default:
		return "ok", fmt.Sprintf("present (%d entries); Git could not answer (not a work tree?)", count)
	}
}

func oneLine(s string) string {
	for i, r := range s {
		if r == '\n' || r == '\r' {
			return s[:i]
		}
	}
	return s
}
