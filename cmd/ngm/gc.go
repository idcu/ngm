package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/core/vendor"
)

const gcUsage = `ngm gc — reclaim objects that the current project's lock does not reference

USAGE:
  ngm gc [--dir=<dir>] [--apply] [--json]

FLAGS:
  --dir     project directory containing ngm.json/ngm.lock (default: .)
  --apply   actually delete the candidates. **Without it this command is a dry run**
            (the default direction is deliberate: gc can delete bytes, so it must be
            asked twice).
  --dry-run explicit spelling of the default (mutually exclusive with --apply)
  --json    machine-readable report on stdout (same numbers as the human text)

REACHABILITY:
  A candidate is an object that the **current** ngm.lock does not name. Everything the
  lock names is kept, unconditionally:

    store    every archiveDigest / moduleDigest in the lock (layer 2 entries)
    mirror   every repository named by a dependency (layer 1 bare clones)
    module-cache the same repositories as mirror (layer 1.5 partial clones)
    vendor   every vendorPath in the lock, including monorepo subPath directories
             (a parent directory that contains a reachable path is preserved)

  The graph stored in the lock (requiredBy) is not consulted object by object: every
  dependency in the lock is reachable by definition, so indirect selections are kept.

  Layer 2 also reclaims blobs that only the deleted trees referenced. Blobs newer
  than one minute are left alone: "ngm install" writes blobs before it publishes the
  manifest that references them, so a fresh blob may belong to a concurrent install.

SAFETY:
  Layers 1 and 2 are **shared by every project on this machine**, and ngm has no
  project registry. Reclaiming a mirror or digest that this project no longer names
  can make another checkout fetch or re-install again (bytes are recoverable: layer 2
  is derived from layer 1, and layer 1 can be re-cloned). Do not run gc while another
  install/update is running.

  The global vendor (vendor.mode=global) is different: entries there may be used by
  any project, and ngm cannot prove they are not. Such entries are reported as
  "kept" and are never deleted by this command — remove them by hand if you are sure.

  Deletion failures are collected per object and never abort the rest of the run.

EXIT CODES:
  0  scanned (dry run), or applied with no failures
  3  configuration/lock error (missing ngm.json or ngm.lock, unreadable store/manifest)
  4  some object could not be deleted (the rest were still removed; see the report)
`

// gcItem 是一个候选对象（文件或目录）的最小描述。
type gcItem struct {
	// ID 是层内标识：digest / mirror 相对路径 / vendor 相对路径。
	ID string `json:"id"`
	// Bytes 是可回收字节（dry-run 时是"将会回收"）。
	Bytes int64 `json:"bytes"`
	// Layout 只对 store 有意义："v2" 或 "v1"。
	Layout string `json:"layout,omitempty"`
}

// gcClass 是一层的报告。
type gcClass struct {
	Root       string   `json:"root"`
	Exists     bool     `json:"exists"`
	Mode       string   `json:"mode,omitempty"`
	Candidates []gcItem `json:"candidates"`
	Objects    int      `json:"objects"`
	Bytes      int64    `json:"bytes"`
	Removed    int      `json:"removed"`
	BytesFreed int64    `json:"bytesFreed"`
	// Kept 是"是候选、但按安全规则不删"的对象（目前只有 global vendor）。
	Kept           []gcItem `json:"kept,omitempty"`
	KeptYoung      int      `json:"keptYoungBlobs,omitempty"`
	KeptYoungBytes int64    `json:"keptYoungBytes,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

type gcTotal struct {
	Objects int   `json:"objects"`
	Bytes   int64 `json:"bytes"`
}

// gcReport 是 `ngm gc --json` 的顶层结构，同时是人工输出的唯一数据源。
type gcReport struct {
	Version     int     `json:"version"`
	DryRun      bool    `json:"dryRun"`
	Applied     bool    `json:"applied"`
	Project     string  `json:"project"`
	Lock        string  `json:"lock"`
	Store       gcClass `json:"store"`
	Mirror      gcClass `json:"mirror"`
	ModuleCache gcClass `json:"moduleCache"`
	Vendor      gcClass `json:"vendor"`
	Total       gcTotal `json:"total"`
	// Warnings 是"本次没有删、但你需要知道"的事实。
	Warnings []string `json:"warnings,omitempty"`
}

// runGC 处理 `ngm gc`。
func runGC(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("gc", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	apply := fs.Bool("apply", false, "delete the candidates (default: dry run)")
	dryRun := fs.Bool("dry-run", false, "explicit spelling of the default dry run")
	jsonOut := fs.Bool("json", false, "machine-readable report")

	c := newCmdContext(ctx, args, stdout, stderr, fs)
	c.usage(gcUsage)
	if !c.parse([]flagSpec{
		{Name: "dir"},
		{Name: "apply", Bool: true},
		{Name: "dry-run", Bool: true},
		{Name: "json", Bool: true},
	}, jsonOut) {
		// 与其他命令同一条通道：提了 --json 的早期失败也要给一份错误信封。
		return 3
	}
	ctx = c.ctx
	// 从这里起，失败路径走 runErr：`--json` 时它会把一份完整的错误信封
	// 写到 stdout（配置文件/lock 读不出来也是一种"没有报告可给"的失败）。
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, gcUsage)
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"unexpected argument(s): "+strings.Join(fs.Args(), " "),
			"gc takes no positional arguments"))
	}
	if *apply && *dryRun {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"--apply and --dry-run ask for different things",
			"keep one: omit both for a dry run, or pass --apply to delete"))
	}

	env, code := c.env(*dirFlag)
	if code != 0 {
		return code
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
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"no ngm.lock in "+env.ProjectDir,
			"gc needs the lock to decide what is reachable; run `ngm install` first"))
	}

	reach, err := gcReachability(lf)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	now := time.Now()
	rep := gcReport{
		Version: 1,
		DryRun:  !*apply,
		Applied: *apply,
		Project: env.ProjectDir,
		Lock:    env.LockPath(),
	}

	// ---- 先把三层的计划都算出来，再动手。
	//
	// 顺序很重要：任何一层的扫描失败都必须在**删除任何东西之前**退出——
	// 否则一条读不出清单的 store 会让命令删掉 mirror，然后才报"无法判定"。
	storePlan, err := env.Store.PlanStoreReclaim(reach.digests, now)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	mu, err := env.Mirror.Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	mcu, err := vendor.NewModuleCache(env.Layout.ModuleCacheRoot(), env.GitOpts).Usage()
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	vendorRoot := env.VendorRoot(pf)
	mode := "local"
	if pf != nil && pf.Vendor != nil && strings.EqualFold(strings.TrimSpace(pf.Vendor.Mode), "global") {
		mode = "global"
	}
	vplan, err := vendor.PlanVendorReclaim(vendorRoot, reach.vendorPaths)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// ---- 层 2：content store
	rep.Store = gcClass{Root: storePlan.Root, Exists: storePlan.Exists}
	for _, it := range storePlan.Items {
		rep.Store.Candidates = append(rep.Store.Candidates, gcItem{ID: it.Digest, Bytes: it.Bytes, Layout: it.Layout})
	}
	rep.Store.Objects = len(rep.Store.Candidates)
	rep.Store.Bytes = storePlan.TotalBytes
	rep.Store.KeptYoung = storePlan.YoungBlobs
	rep.Store.KeptYoungBytes = storePlan.YoungBlobBytes
	if *apply && !storePlan.Empty() {
		res := env.Store.ApplyStoreReclaim(storePlan)
		rep.Store.Removed = len(res.Removed)
		rep.Store.BytesFreed = res.BytesFreed
		rep.Store.Errors = append(rep.Store.Errors, res.Errors...)
	}

	// ---- 层 1：mirror
	rep.Mirror = gcClass{Root: mu.Root, Exists: mu.Exists}
	for _, e := range mu.Entries {
		if reach.mirrors[e.Repo] {
			continue
		}
		rep.Mirror.Candidates = append(rep.Mirror.Candidates,
			gcItem{ID: e.Repo, Bytes: e.Bytes})
	}
	sort.Slice(rep.Mirror.Candidates, func(i, j int) bool {
		return rep.Mirror.Candidates[i].ID < rep.Mirror.Candidates[j].ID
	})
	rep.Mirror.Objects = len(rep.Mirror.Candidates)
	for _, it := range rep.Mirror.Candidates {
		rep.Mirror.Bytes += it.Bytes
	}
	if *apply {
		for _, it := range rep.Mirror.Candidates {
			path := filepath.Join(mu.Root, filepath.FromSlash(it.ID))
			if rerr := os.RemoveAll(path); rerr != nil {
				rep.Mirror.Errors = append(rep.Mirror.Errors, path+": "+rerr.Error())
				continue
			}
			rep.Mirror.Removed++
			rep.Mirror.BytesFreed += it.Bytes
		}
	}

	// ---- 层 1.5：module cache（partial clone）
	//
	// 与 mirror 同一把钥匙：lock 里的每个仓库都按 `repo.git` 可达。
	// 一个仓库同时有 mirror 与 module cache 是正常的（模块条目用后者）；
	// 因此这里只回收"连 mirror 都不再引用"的仓库，避免删掉仍被别的
	// 形态使用的缓存。被删的 partial clone 可由 mirror/远端重建。
	rep.ModuleCache = gcClass{Root: mcu.Root, Exists: mcu.Exists}
	for _, e := range mcu.Entries {
		if reach.mirrors[e.Repo] {
			continue
		}
		rep.ModuleCache.Candidates = append(rep.ModuleCache.Candidates,
			gcItem{ID: e.Repo, Bytes: e.Bytes})
	}
	sort.Slice(rep.ModuleCache.Candidates, func(i, j int) bool {
		return rep.ModuleCache.Candidates[i].ID < rep.ModuleCache.Candidates[j].ID
	})
	rep.ModuleCache.Objects = len(rep.ModuleCache.Candidates)
	for _, it := range rep.ModuleCache.Candidates {
		rep.ModuleCache.Bytes += it.Bytes
	}
	if *apply {
		for _, it := range rep.ModuleCache.Candidates {
			path := filepath.Join(mcu.Root, filepath.FromSlash(it.ID))
			if rerr := os.RemoveAll(path); rerr != nil {
				rep.ModuleCache.Errors = append(rep.ModuleCache.Errors, path+": "+rerr.Error())
				continue
			}
			rep.ModuleCache.Removed++
			rep.ModuleCache.BytesFreed += it.Bytes
		}
	}

	// ---- 层 3：vendor
	rep.Vendor = gcClass{Root: vplan.Root, Exists: vplan.Exists, Mode: mode}
	if mode == "global" {
		// 无法判定别的项目是否引用：保留并 warning，不猜（用户决策）。
		for _, it := range vplan.Items {
			rep.Vendor.Kept = append(rep.Vendor.Kept, gcItem{ID: it.Path, Bytes: it.Bytes})
		}
		if len(vplan.Items) > 0 {
			var names []string
			for _, it := range vplan.Items {
				names = append(names, it.Path)
			}
			rep.Warnings = append(rep.Warnings, fmt.Sprintf(
				"vendor.mode=global: kept %d unreachable entr(ies) (%s) — ngm cannot prove "+
					"another project does not reference them: %s",
				len(vplan.Items), storeBytes(vplan.TotalBytes), strings.Join(names, ", ")))
		}
	} else {
		for _, it := range vplan.Items {
			rep.Vendor.Candidates = append(rep.Vendor.Candidates,
				gcItem{ID: it.Path, Bytes: it.Bytes})
		}
		rep.Vendor.Objects = len(rep.Vendor.Candidates)
		rep.Vendor.Bytes = vplan.TotalBytes
		if *apply {
			for _, it := range rep.Vendor.Candidates {
				path := filepath.Join(vplan.Root, filepath.FromSlash(it.ID))
				if rerr := os.RemoveAll(path); rerr != nil {
					rep.Vendor.Errors = append(rep.Vendor.Errors, path+": "+rerr.Error())
					continue
				}
				rep.Vendor.Removed++
				rep.Vendor.BytesFreed += it.Bytes
			}
		}
	}

	rep.Total.Objects = rep.Store.Objects + rep.Mirror.Objects + rep.ModuleCache.Objects + rep.Vendor.Objects
	rep.Total.Bytes = rep.Store.Bytes + rep.Mirror.Bytes + rep.ModuleCache.Bytes + rep.Vendor.Bytes

	if *jsonOut {
		// 统一写出点（ADR-045）。此前这里用 `SetEscapeHTML(false)`，与另外三份
		// 实现不同——差别没有任何文档说得清，而**解码后完全等价**。
		// 退出码仍是本命令自己的判定（回收失败要退 4），写出点只负责送出去。
		return writeJSONDocument(stdout, stderr, rep, gcExitCode(rep))
	}
	renderGCHuman(stdout, rep)
	return gcExitCode(rep)
}

// gcReach 是"当前 lock 还要什么"的三张集合。
type gcReach struct {
	digests     map[string]bool
	mirrors     map[string]bool
	vendorPaths []string
}

// gcReachability 从 lock 推导可达对象。
//
// 图（requiredBy）不需要单独遍历：lock 里的每一条都在图上，因此"选择集"
// （间接依赖、monorepo 模块）天然在其中。
func gcReachability(lf *lock.File) (gcReach, error) {
	out := gcReach{
		digests: map[string]bool{},
		mirrors: map[string]bool{},
	}
	if lf == nil {
		return out, nil
	}
	seenVendor := map[string]bool{}
	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]
		if dg := d.ContentDigest(); dg != "" {
			out.digests[dg] = true
		}
		repo, err := resolve.ParseSlug(d.Name)
		if err != nil {
			return out, errs.Wrap(errs.CodeConfigInvalid,
				"lock dependency "+d.Name+" is not a valid slug",
				"regenerate the lock with `ngm install` (a slug looks like `github:org/repo`)", err)
		}
		out.mirrors[repo.MirrorRelPath()+".git"] = true
		vp := strings.Trim(filepath.ToSlash(vendor.VendorPathFor(d.VendorPath, d.SubPath)), "/")
		if vp != "" && !seenVendor[vp] {
			seenVendor[vp] = true
			out.vendorPaths = append(out.vendorPaths, vp)
		}
	}
	sort.Strings(out.vendorPaths)
	return out, nil
}

// gcExitCode 把逐对象删除失败折成退出码 4。
//
// v0.136：ModuleCache 是层 1.5，同样会逐对象删除（:277 写 Errors、:417 渲染），
// 漏算它会让“报告里有 error 行、退出码却是 0”，CI 因此误判成功。
func gcExitCode(rep gcReport) int {
	if len(rep.Store.Errors)+len(rep.Mirror.Errors)+len(rep.ModuleCache.Errors)+len(rep.Vendor.Errors) > 0 {
		return 4
	}
	return 0
}

// renderGCHuman 打印与 --json 同源的人工报告。
func renderGCHuman(w io.Writer, rep gcReport) {
	head := "dry run (nothing was deleted)"
	if rep.Applied {
		head = "applied"
	}
	fmt.Fprintf(w, "ngm gc — %s\n", head)
	fmt.Fprintf(w, "project: %s\n", rep.Project)
	fmt.Fprintf(w, "lock:    %s\n", rep.Lock)
	fmt.Fprintln(w)

	if rep.Total.Objects == 0 {
		fmt.Fprintf(w, "nothing to reclaim: store, mirror, module-cache and vendor hold no object outside %s\n", rep.Lock)
	} else {
		renderGCClass(w, "store (layer 2)", rep.Store, rep.DryRun)
		renderGCClass(w, "mirror (layer 1)", rep.Mirror, rep.DryRun)
		renderGCClass(w, "module-cache (layer 1.5)", rep.ModuleCache, rep.DryRun)
		renderGCClass(w, "vendor (layer 3)", rep.Vendor, rep.DryRun)
		fmt.Fprintf(w, "\ntotal: %d object(s), %s\n", rep.Total.Objects, storeBytes(rep.Total.Bytes))
	}

	if rep.Store.KeptYoung > 0 {
		fmt.Fprintf(w,
			"store: kept %d blob(s) (%s) younger than %s — an install may be writing them; re-run later\n",
			rep.Store.KeptYoung, storeBytes(rep.Store.KeptYoungBytes), vendor.UnpackResidueGrace)
	}
	for _, ke := range rep.Vendor.Kept {
		fmt.Fprintf(w, "vendor: kept %s (%s)\n", ke.ID, storeBytes(ke.Bytes))
	}
	for _, warning := range rep.Warnings {
		fmt.Fprintf(w, "warning: %s\n", warning)
	}
	for _, e := range append(append(append(append([]string{}, rep.Store.Errors...), rep.Mirror.Errors...), rep.ModuleCache.Errors...), rep.Vendor.Errors...) {
		fmt.Fprintf(w, "error: %s\n", e)
	}
	if rep.Total.Objects > 0 {
		fmt.Fprintln(w,
			"note: layers 1 and 2 are shared by every project on this machine; a candidate may\n"+
				"      still be wanted elsewhere. Layer 2 rebuilds from layer 1, and a deleted\n"+
				"      mirror is re-cloned on the next install that needs it.")
	}
	if rep.DryRun && rep.Total.Objects > 0 {
		fmt.Fprintln(w, "dry run: re-run with --apply to delete these object(s).")
	}
	if rep.Applied {
		removed := rep.Store.Removed + rep.Mirror.Removed + rep.ModuleCache.Removed + rep.Vendor.Removed
		freed := rep.Store.BytesFreed + rep.Mirror.BytesFreed + rep.ModuleCache.BytesFreed + rep.Vendor.BytesFreed
		fmt.Fprintf(w, "removed %d object(s), freeing %s\n", removed, storeBytes(freed))
	}
}

// renderGCClass 打印一层的候选清单。
func renderGCClass(w io.Writer, title string, c gcClass, dryRun bool) {
	if !c.Exists {
		fmt.Fprintf(w, "%s: %s does not exist (nothing to reclaim)\n", title, c.Root)
		return
	}
	fmt.Fprintf(w, "%s: %s\n", title, c.Root)
	if c.Objects == 0 {
		fmt.Fprintln(w, "  nothing to reclaim")
		return
	}
	if dryRun {
		fmt.Fprintf(w, "  would reclaim %d object(s), %s:\n", c.Objects, storeBytes(c.Bytes))
	} else {
		fmt.Fprintf(w, "  removed %d object(s), freeing %s:\n", c.Removed, storeBytes(c.BytesFreed))
	}
	const show = 10
	for i, it := range c.Candidates {
		if i == show {
			fmt.Fprintf(w, "    ... and %d more (sorted)\n", len(c.Candidates)-show)
			break
		}
		layout := ""
		if it.Layout != "" {
			layout = " (" + it.Layout + ")"
		}
		fmt.Fprintf(w, "    %-64s %10s%s\n", it.ID, storeBytes(it.Bytes), layout)
	}
}
