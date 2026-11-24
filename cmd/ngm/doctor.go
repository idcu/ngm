package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
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
  scripts   install-script declarations in vendor / package-lock.json (preview only)
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
	// Scripts 是脚本审批**预演**：只列声明与会被默认策略拒绝的条目，从不执行。
	Scripts *scriptApprovalReport `json:"scripts,omitempty"`
}

// scriptApprovalEntry 是一条"会被审批"的声明。
type scriptApprovalEntry struct {
	Dependency string `json:"dependency"`
	Kind       string `json:"kind"`   // postinstall.js / package.json:scripts.postinstall / package.json:scripts.install / package-lock.json:hasInstallScript
	Source     string `json:"source"` // vendor | package-lock.json
	Detail     string `json:"detail,omitempty"`
}

// scriptApprovalReport 的形状：status ∈ {none, pending, declared}。
// pending = 有声明且当前策略不是 allow（默认 deny / prompt 都会拒绝执行）。
type scriptApprovalReport struct {
	Status  string                `json:"status"`
	Policy  string                `json:"policy"`
	Entries []scriptApprovalEntry `json:"entries,omitempty"`
	// Allow / Deny 是可直接粘进 ngm.json 的 supplyChain 片段（全局策略，非逐包白名单）。
	Allow string `json:"allow,omitempty"`
	Deny  string `json:"deny,omitempty"`
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("doctor", stderr)
	dir := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable report")

	c := newCmdContext(ctx, args, stdout, stderr, fs)
	c.usage(doctorUsage)
	if !c.parse([]flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true},
	}, jsonOut) {
		// v0.105：`doctor` 是 v0.104 新增的命令，没被 v0.54 那张网接管——
		// 实测 `ngm doctor --json --bogus` 的 stdout 是 **0 字节**，
		// 而用户明明提了 `--json`（要机器可读，却只拿到人读的 usage）。
		return 3
	}
	// 注意：doctor 用 `c.parse(..., jsonOut)` 记下 --json，因此这里起的所有失败
	// 都经 c.fail 出机器可读信封（此前这条分支只写 stderr，--json 拿到空 stdout）。
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, doctorUsage)
		return c.fail(errs.New(errs.CodeConfigInvalid,
			"unexpected argument(s): "+strings.Join(fs.Args(), " "),
			"doctor takes no positional arguments"))
	}

	env, code := c.env(*dir)
	if code != 0 {
		return code
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
	// ADR-053：新项目由 init 写入 P1D；老项目如果完全没有 supplyChain 段，
	// doctor 必须说出来——否则"全局默认"只存在于 API 里，用户永远看不到。
	if pf != nil && pf.SupplyChain == nil {
		add("supplyChain", "warn",
			"no supplyChain section; add `\"supplyChain\":{\"minimumReleaseAge\":\"P1D\"}` "+
				"to enable the 24h release-age cooldown (explicit PT0S opts out)")
	} else if pf != nil && pf.SupplyChain != nil && pf.SupplyChain.MinimumReleaseAge == "" {
		add("supplyChain", "warn",
			"supplyChain section has no minimumReleaseAge; 24h default applies")
	} else {
		add("supplyChain", "ok", "release-age policy present")
	}
	vendorStatus, vendorDetail := doctorVendorReadout(ctx, env, pf)
	add("vendor", vendorStatus, vendorDetail)

	scripts := scanScriptApprovals(*dir, env.VendorRoot(pf), doctorPostInstallPolicy(pf))
	rep.Scripts = &scripts
	switch scripts.Status {
	case "none":
		add("scripts", "ok", "no install-script declarations; no approval needed (doctor executed nothing)")
	case "declared":
		add("scripts", "ok", fmt.Sprintf("%d declaration(s); postInstallPolicy=%s would allow them "+
			"in a sandbox (doctor executed nothing)", len(scripts.Entries), scripts.Policy))
	default:
		add("scripts", "warn", fmt.Sprintf("%d dependency(ies) declare install scripts; "+
			"postInstallPolicy=%s would deny them; see the scripts section below",
			len(scripts.Entries), scripts.Policy))
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
		return writeJSONDocument(stdout, stderr, rep, 0)
	}
	for _, c := range rep.Checks {
		fmt.Fprintf(stdout, "%-8s %-5s %s\n", c.Name, c.Status, c.Detail)
	}
	writeScriptApprovals(stdout, &scripts)
	if rep.OK {
		fmt.Fprintln(stdout, "\nOK")
	} else {
		fmt.Fprintln(stdout, "\nFAIL")
	}
	return 0
}

// doctorPostInstallPolicy 读取**声明值**；空值即默认 deny。
//
// 不调用 policy 层是因为这里只做预演，不判定、不执行；"prompt" 在非交互工具里
// 同样不执行，因此除 "allow" 外都按"会被拒绝"呈现。
func doctorPostInstallPolicy(pf *config.ProjectFile) string {
	if pf != nil && pf.SupplyChain != nil && pf.SupplyChain.PostInstallPolicy != "" {
		return pf.SupplyChain.PostInstallPolicy
	}
	return "deny"
}

// scanScriptApprovals 只读扫描 vendor 树与 package-lock.json 里**声明**的安装脚本。
//
// 来源仅两类：
//   - vendor 中的 `postinstall.js`（ngm 唯一可沙箱执行的钩子）与 package.json 的
//     `scripts.postinstall` / `scripts.install`（ngm 从不执行 shell 钩子）；
//   - package-lock.json v2/v3 的 `hasInstallScript` 标记（明确的声明，不是猜）。
//
// pnpm/yarn/bun/deno lock 不携带脚本声明，ngm.lock 也没有脚本字段——
// 这些边界在 ADR-054 里写明。函数**不执行任何脚本**，也不改任何文件。
func scanScriptApprovals(projectDir, vendorRoot, policy string) scriptApprovalReport {
	if policy == "" {
		policy = "deny"
	}
	rep := scriptApprovalReport{Policy: policy}
	var entries []scriptApprovalEntry

	if vendorRoot != "" {
		_ = filepath.WalkDir(vendorRoot, func(p string, d os.DirEntry, werr error) error {
			if werr != nil {
				return nil
			}
			if d.IsDir() {
				if p != vendorRoot && (d.Name() == "node_modules" || d.Name() == ".git") {
					return filepath.SkipDir
				}
				return nil
			}
			switch d.Name() {
			case PostInstallHookName:
				rel, err := filepath.Rel(vendorRoot, p)
				if err != nil {
					rel = p
				}
				entries = append(entries, scriptApprovalEntry{
					Dependency: doctorVendorDepName(vendorRoot, filepath.Dir(p)),
					Kind:       PostInstallHookName,
					Source:     "vendor",
					Detail:     filepath.ToSlash(rel),
				})
			case "package.json":
				data, rerr := os.ReadFile(p)
				if rerr != nil {
					return nil
				}
				var pj struct {
					Name    string            `json:"name"`
					Scripts map[string]string `json:"scripts"`
				}
				if json.Unmarshal(data, &pj) != nil {
					return nil
				}
				dep := pj.Name
				if dep == "" {
					dep = doctorVendorDepName(vendorRoot, filepath.Dir(p))
				}
				for _, s := range []struct{ key, kind string }{
					{"postinstall", "package.json:scripts.postinstall"},
					{"install", "package.json:scripts.install"},
				} {
					if cmd := strings.TrimSpace(pj.Scripts[s.key]); cmd != "" {
						entries = append(entries, scriptApprovalEntry{
							Dependency: dep, Kind: s.kind, Source: "vendor", Detail: truncateCmd(cmd),
						})
					}
				}
			}
			return nil
		})
	}

	if raw, err := os.ReadFile(filepath.Join(projectDir, "package-lock.json")); err == nil {
		var l struct {
			Packages map[string]struct {
				HasInstallScript bool `json:"hasInstallScript"`
			} `json:"packages"`
		}
		if json.Unmarshal(raw, &l) == nil {
			for path, e := range l.Packages {
				if !e.HasInstallScript {
					continue
				}
				dep := path
				if i := strings.LastIndex(path, "node_modules/"); i >= 0 {
					dep = path[i+len("node_modules/"):]
				}
				entries = append(entries, scriptApprovalEntry{
					Dependency: dep, Kind: "package-lock.json:hasInstallScript",
					Source: "package-lock.json", Detail: path,
				})
			}
		}
	}

	// 排序 + 去重：同一次扫描（以及不同文件系统枚举顺序）必须给出同一份报告。
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Dependency != b.Dependency {
			return a.Dependency < b.Dependency
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Detail < b.Detail
	})
	uniq := entries[:0]
	for _, e := range entries {
		if len(uniq) == 0 || uniq[len(uniq)-1] != e {
			uniq = append(uniq, e)
		}
	}
	rep.Entries = uniq

	switch {
	case len(rep.Entries) == 0:
		rep.Status = "none"
	case policy == "allow":
		rep.Status = "declared"
	default:
		rep.Status = "pending"
		rep.Allow = `"supplyChain": {"postInstallPolicy": "allow"}`
		rep.Deny = `"supplyChain": {"postInstallPolicy": "deny"}`
	}
	return rep
}

func doctorVendorDepName(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}

// writeScriptApprovals 把预演报告写成人读的一节；永不执行被列出的东西。
func writeScriptApprovals(w io.Writer, r *scriptApprovalReport) {
	if r == nil {
		return
	}
	fmt.Fprintln(w, "\nscript approval preview (read-only; doctor ran no script):")
	if r.Status == "none" {
		fmt.Fprintln(w, "  no install-script declarations; no approval needed (无需审批)")
		return
	}
	fmt.Fprintf(w, "  postInstallPolicy=%s; %d declaration(s)\n", r.Policy, len(r.Entries))
	if r.Status == "pending" {
		fmt.Fprintf(w, "  %d would be denied by the default policy (nothing was executed)\n", len(r.Entries))
	}
	for _, e := range r.Entries {
		fmt.Fprintf(w, "  - %s  %s  [%s]  %s\n", e.Dependency, e.Kind, e.Source, e.Detail)
	}
	if r.Status == "pending" {
		fmt.Fprintf(w, "  allow (sandbox only): %s\n", r.Allow)
		fmt.Fprintf(w, "  keep denying:         %s\n", r.Deny)
	}
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
