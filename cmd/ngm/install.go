package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

const installUsage = `ngm install — resolve dependencies and produce ngm.lock

USAGE:
  ngm install [--dir=<dir>] [--digest] [--frozen-lockfile] [--offline] [--json]

FLAGS:
  --dir               project directory containing ngm.json (default: .)
  --digest            also print each dependency's archiveDigest
  --frozen-lockfile   never resolve a new ref and never write ngm.lock
  --offline           never touch the network
  --json              write a machine-readable report to stdout (CI should use this);
                      progress text and warnings then go to stderr

BEHAVIOR (architecture/locking.md §更新策略):
  no ngm.lock   resolve from ngm.json → write lock → populate content store
  has ngm.lock  trust the lock (do NOT re-resolve refs); verify it still matches
                ngm.json, and make sure the content store is populated

CI MODES (v0.2 F):
  --frozen-lockfile         fail (exit 3) if ngm.lock is missing or does not
                            match ngm.json. Downloading is still allowed: a cold
                            content store may fetch. What it forbids is deciding
                            a new commit - that must come from the committed lock.
  --offline                 no network at all. A cold content store and a cold
                            mirror both fail (exit 4): "offline" must never mean
                            "installed something slightly different".
  --frozen-lockfile --offline
                            fully reproducible offline install - the CI default.
                            Needs ngm.lock committed and a warm content store.

EXIT CODES:
  0  installed
  1  verifyOnLock is on and the post-install re-check found drift or a policy failure
  2  integrity: a postinstall hook failed or timed out, or the vendored bytes did not verify
  3  configuration, policy or lock error (a definition that does not parse,
     --frozen-lockfile with a missing or out-of-sync lock, a policy that rejects the graph)
  4  Git or network failure (including --offline with a missing local mirror)
  5  a dependency declares a postinstall hook and Deno is missing or unusable
`

// runInstall 处理 `ngm install`。
//
// 与 `ngm update` 的分工（locking.md §更新策略）：
//
//	install  尊重既有 lock：不重新解析 ref，只确保内容落地
//	update   重新解析 ref → 新 commit → 新 digest → 刷新 lock
func runInstall(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("install", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	digestFlag := fs.Bool("digest", false, "also print each archiveDigest")
	frozen := fs.Bool("frozen-lockfile", false, "never resolve a new ref or write ngm.lock")
	offline := fs.Bool("offline", false, "never touch the network")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, installUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "digest", Bool: true},
		{Name: "frozen-lockfile", Bool: true}, {Name: "offline", Bool: true},
		{Name: "json", Bool: true},
	})); err != nil {
		failEarly(args, stdout, err.Error(),
			"run `ngm "+fs.Name()+" --help` for the flags it accepts")
		return 3
	}
	ctx = markJSONIfRequested(ctx, *jsonOut)
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, installUsage)
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
	roots := toDepSpecs(pf.Dependencies)

	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// --frozen-lockfile 是 CI 的核心承诺："装出来的东西必须与提交的 lock 一致"。
	// 因此缺 lock、或 lock 与声明不一致，都必须**失败**（exit 3）而不是退化成
	// 重新解析——后者会让 CI 在没人改 lock 的情况下装出别的内容，
	// 而"lock 已提交、安装即可复现"这个前提也就被悄悄绕过了。
	if *frozen {
		if lf == nil {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeConfigInvalid,
				"--frozen-lockfile: no ngm.lock at "+env.LockPath(),
				"commit ngm.lock to the repository, or run `ngm install` without --frozen-lockfile to create it"))
		}
		if missing := lockManifestMismatch(lf, roots); len(missing) > 0 {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeConfigInvalid,
				"--frozen-lockfile: ngm.lock does not match ngm.json ("+strings.Join(missing, ", ")+")",
				"run `ngm update` and commit the lock, or drop --frozen-lockfile"))
		}
	}
	// 离线且没有 lock：无从解析（读上游 manifest 需要网络），也不能凭空造一个
	if *offline && lf == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeGitFetch,
			"--offline: no ngm.lock to install from",
			"commit ngm.lock; an offline install can only materialise what the lock already pins"))
	}

	// --json 时人类文本改走 stderr（cli.md 的全局约定："机器可读输出走 stdout，
	// 人类文本与诊断走 stderr"）——stdout 必须只含一份 JSON。
	humanOut := stdout
	if *jsonOut {
		humanOut = stderr
	}
	rep := &writeReport{Version: writeReportVersion, Command: "install", Offline: *offline}

	// 决策：lock 是否仍与 ngm.json 一致？
	if lf != nil {
		missing := lockManifestMismatch(lf, roots)
		if len(missing) == 0 {
			code := installFromLock(ctx, env, pf, lf, *digestFlag, *offline, rep, humanOut, stderr)
			return emitWriteReport(ctx, stdout, stderr, rep, code, *jsonOut)
		}
		fmt.Fprintf(humanOut,
			"ngm.lock does not match ngm.json (%s); re-resolving\n",
			strings.Join(missing, ", "))
	}

	code := installFresh(ctx, env, pf, roots, *digestFlag, *offline, rep, humanOut, stderr)
	return emitWriteReport(ctx, stdout, stderr, rep, code, *jsonOut)
}

// materializeVendor 落地 vendor 树、写出 ngm.mappings.json，然后按策略处理安装后钩子。
//
// vendor 与 mappings 由同一份 content 子树派生，因此"映射指向的路径"
// 与"vendor 里真实的文件"必然一致。
//
// install（两条路径）与 update 共用本函数，因此 postinstall 的执行入口只有这一处：
// 钩子的语义是"代码就位之后要做的事"，三个入口各写一遍必然会漏掉某个。
func materializeVendor(
	ctx context.Context,
	env *projectEnv,
	pf *config.ProjectFile,
	items []digestNode,
	stdout, stderr io.Writer,
) error {
	mr, err := env.MaterializeVendorAndMappings(pf, items)
	if err != nil {
		return err
	}
	for _, w := range mr.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if werr := writeMappingsChecked(env, mr.Mappings); werr != nil {
		return werr
	}

	// 钩子在 vendor 与 mappings 都就位之后才跑：钩子看到的世界应当与最终状态一致。
	return runPostInstall(ctx, env, pf, items, stdout, stderr)
}

// lockManifestMismatch 返回 ngm.json 中无法由现有 lock 满足的声明。
//
// 判定口径：根声明必须在 lock 中存在，且 ref / refType 一致——
// 否则说明 ngm.json 被改动过，lock 已过期。
//
// 只检查**根声明**：lock 还包含传递依赖，它们的增减由重新解析决定。
func lockManifestMismatch(lf *lock.File, roots []resolve.DepSpec) []string {
	var out []string
	for _, r := range roots {
		d, ok := lf.Find(r.Name, r.SubPath)
		if !ok {
			out = append(out, r.Name+" (missing from lock)")
			continue
		}
		if d.Ref != r.Ref || resolve.RefType(d.RefType) != r.RefType {
			out = append(out, r.Name+" ("+d.Ref+" ("+d.RefType+") → "+r.Ref+" ("+string(r.RefType)+"))")
		}
	}
	return out
}

// installFresh 从 ngm.json 完整解析并生成 lock。
func installFresh(ctx context.Context, env *projectEnv, pf *config.ProjectFile, roots []resolve.DepSpec, showDigest, offline bool, rep *writeReport, stdout, stderr io.Writer) int {
	// 供应链策略：白名单在解析阶段生效（ADR-009），命中即 exit 3 并附来源链
	pol, perr := projectPolicy(pf)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	opts := env.GraphOptions()
	opts.CheckRepo = pol.CheckRepo
	if offline {
		// 解析要读上游 manifest，离线时只能来自本地 mirror（与 verify 同一语义）
		opts.EnsureMirror = offlineEnsureMirror(env)
	}

	g, err := resolve.ResolveGraph(ctx, roots, opts)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	// 晾晒期只能在解析**之后**判定：时间源是 commit 的 committer date
	if err := checkReleaseAges(ctx, env, g, pol); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 上游把 Git 依赖写在 package.json 等情况：提示但不阻断
	for _, w := range g.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	lf := lock.NewFile()
	now := time.Now()
	items := make([]digestNode, 0, len(g.Nodes))

	for _, n := range g.Nodes {
		// v0.68：一次算出（条目 + 内容 + digest），把材料交给 Put 复用——
		// 此前这里算一遍、Put 内部再算一遍，加 Put 自己的 tree 与 cat-file：
		// 每个依赖 6 个 git 子进程；现在 digest 阶段只剩 2 个（ls-tree + cat-file）。
		material, derr := git.BuildArchiveMaterial(ctx, env.GitOpts, n.MirrorPath, n.Commit)
		if derr != nil {
			return runErr(ctx, stdout, stderr, derr)
		}
		dg := material.Digest

		if _, perr := env.Store.Put(ctx, env.GitOpts, n.MirrorPath, vendor.Meta{
			Repo:     n.Name,
			Commit:   n.Commit,
			Digest:   dg,
			SubPath:  n.SubPath,
			Material: material,
		}); perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		lf.Dependencies = append(lf.Dependencies, lock.Dependency{
			Name:          n.Name,
			Ref:           n.Ref,
			RefType:       string(n.RefType),
			Commit:        n.Commit,
			ArchiveDigest: dg,
			ResolvedAt:    lock.FormatResolvedAt(now),
			VendorPath:    n.Repo.MirrorRelPath(),
			SubPath:       n.SubPath,
			// v0.73（ADR-028）：直接边写进 lock。n.RequiredBy 是解析出的
			// **直接引用者**节点 Key（根声明处为 "(root)"），序列化时由
			// Marshal 归一（排序 + 去重）——同一份解析 ⇒ 同一份字节。
			RequiredBy: n.RequiredBy,
		})
		items = append(items, digestNodeFromResolved(n, dg))
		if rep != nil {
			rep.Dependencies = append(rep.Dependencies, writeReportDep{
				Name: n.Name, Ref: n.Ref, RefType: string(n.RefType), SubPath: n.SubPath,
				Commit: n.Commit, ArchiveDigest: dg, VendorPath: n.Repo.MirrorRelPath(),
				Source: "resolved",
			})
		}
		printResolved(stdout, n, dg, showDigest)
	}

	// 因 root wins 被忽略的传递声明：必须在整张图展开完成后输出
	// （IgnoredRefs 在展开过程中逐步填充，循环内打印会漏掉后填充的）
	for _, n := range g.Nodes {
		for _, ig := range n.IgnoredRefs {
			fmt.Fprintf(stderr, "note: %s\n", ig)
		}
	}

	// vendor 落地 + mappings（从同一份 content 派生，保证指向一致）
	if err := materializeVendor(ctx, env, pf, items, stdout, stderr); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	lf.Sort()
	if err := writeLockChecked(env, lf); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	fmt.Fprintf(stdout, "\ninstalled %d dependencies; wrote %s and %s\n",
		len(lf.Dependencies), env.LockPath(), mappings.FileName)
	return autoVerifyAfterLock(ctx, env, pf, pol, offline, rep, stdout, stderr)
}

// installFromLock 尊重既有 lock：不重新解析 ref，只确保内容就绪。
//
// 这是 v0.1 的可复现性核心——同一 lock 在不同机器上必须安装出相同内容。
func installFromLock(ctx context.Context, env *projectEnv, pf *config.ProjectFile, lf *lock.File, showDigest, offline bool, rep *writeReport, stdout, stderr io.Writer) int {
	// verifyOnLock 需要策略：在这里读一次，避免走到底再解析一遍配置
	pol, perr := projectPolicy(pf)
	if perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}
	items := make([]digestNode, 0, len(lf.Dependencies))
	fromStore := 0

	// --offline 的"绝不触网"在这里落地：换成一个只认既有 mirror 的实现，
	// mirror 冷就直接 exit 4（而不是退化成 clone）。
	ensure := env.EnsureMirror
	if offline {
		ensure = offlineEnsureMirror(env)
	}

	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]

		repo, perr := resolve.ParseSlug(d.Name)
		if perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		// content store 命中即直接落地 vendor：**不需要 mirror，也就不需要网络**。
		//
		// 这不是性能优化，而是承诺问题。locking.md 明确写着"配合 content store 或
		// vendor，环境无网络也能安装与构建"——若命中时仍然 fetch，无网络的 CI 会
		// 在**什么都没变**的情况下失败，而"什么都没变"正是 CI 最常见的场景。
		if env.Store.Has(d.ArchiveDigest) {
			fromStore++
			items = append(items, digestNodeFromLock(d, repo))
			if rep != nil {
				rep.Dependencies = append(rep.Dependencies, writeReportDepFromLock(d, "store"))
			}
			fmt.Fprintf(stdout, "%s@%s (%s) → %s\n", d.Name, d.Ref, d.RefType, d.Commit)
			if showDigest {
				fmt.Fprintf(stdout, "  archiveDigest: %s\n", d.ArchiveDigest)
			}
			continue
		}

		// 未命中：需要 mirror 与对象。**按需取物**（v0.66，评估报告 P3）：
		// mirror 已存在且已含 lock 的 commit 时不做任何 fetch——那正是
		// "什么都没变"的常见情形，而 ADR-010 的哲学是"对象按需才取"。
		// 此前无条件 Ensure（已存在即增量 fetch），于是每次 install 都为一个
		// **已经在本地成立的事实**触网。
		//
		// 未命中时行为不变：Ensure 负责 clone / fetch，取不到对象由下面
		// 的 CommitExists 检查报错（"mirror 不完整或 commit 已被上游移除"）。
		mirrorPath := env.Mirror.PathFor(repo)
		present := false
		if git.IsBareMirror(mirrorPath) {
			if ok, cerr := git.CommitExists(ctx, env.GitOpts, mirrorPath, d.Commit); cerr == nil && ok {
				present = true
			}
		}
		if !present {
			var merr error
			mirrorPath, merr = ensure(ctx, repo)
			if merr != nil {
				return runErr(ctx, stdout, stderr, merr)
			}
		}

		// lock 的 commit 必须在 mirror 中可解析
		ok, cerr := git.CommitExists(ctx, env.GitOpts, mirrorPath, d.Commit)
		if cerr != nil {
			return runErr(ctx, stdout, stderr, cerr)
		}
		if !ok {
			return runErr(ctx, stdout, stderr, errs.New(
				errs.CodeGitFetch,
				d.Name+": commit "+git.ShortSHA(d.Commit)+" from ngm.lock is not present in the local mirror",
				"the mirror is incomplete or the lock points at a removed commit; "+
					"re-fetch (delete ~/.ngm/mirror/<host>/<org>/<repo>.git) or run `ngm update`"))
		}

		// Put 会重新计算 digest 并与 lock 比对，因此"lock 与内容不符"在这里被拒绝（exit 2）。
		//
		// v0.68 刻意**不**在这里传 Material：这里的 digest 是 lock 的**声明值**，
		// 而 Put 内部的重算**正是那条校验本身**（"这份内容真的是 lock 说的那个吗"）。
		// 复用上面的计算结果会把它变成同义反复——省下的 spawn 不值这个价。
		if _, perr := env.Store.Put(ctx, env.GitOpts, mirrorPath, vendor.Meta{
			Repo:    d.Name,
			Commit:  d.Commit,
			Digest:  d.ArchiveDigest,
			SubPath: d.SubPath,
		}); perr != nil {
			return runErr(ctx, stdout, stderr, perr)
		}

		items = append(items, digestNodeFromLock(d, repo))
		if rep != nil {
			rep.Dependencies = append(rep.Dependencies, writeReportDepFromLock(d, "fetched"))
		}
		fmt.Fprintf(stdout, "%s@%s (%s) → %s\n", d.Name, d.Ref, d.RefType, d.Commit)
		if showDigest {
			fmt.Fprintf(stdout, "  archiveDigest: %s\n", d.ArchiveDigest)
		}
	}

	// vendor 落地 + mappings（沿用 lock 的 digest，不重新解析）
	if err := materializeVendor(ctx, env, pf, items, stdout, stderr); err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	// 报告命中来源：用户需要一眼看出这次安装是不是完全离线的
	source := fmt.Sprintf("all %d served from the content store (no git access)", len(lf.Dependencies))
	if fromStore < len(lf.Dependencies) {
		source = fmt.Sprintf("%d from the content store, %d fetched",
			fromStore, len(lf.Dependencies)-fromStore)
	}
	if rep != nil {
		rep.FromStore = fromStore
	}
	fmt.Fprintf(stdout, "\ninstalled %d dependencies from %s (lock respected; %s); wrote %s\n",
		len(lf.Dependencies), env.LockPath(), source, mappings.FileName)
	return autoVerifyAfterLock(ctx, env, pf, pol, offline, rep, stdout, stderr)
}

// printResolved 打印一条解析结果。
func printResolved(w io.Writer, n *resolve.Node, dg string, showDigest bool) {
	pathNote := ""
	if n.SubPath != "" {
		pathNote = "#" + n.SubPath
	}
	fmt.Fprintf(w, "%s%s@%s (%s) → %s\n", n.Name, pathNote, n.Ref, n.RefType, n.Commit)
	if showDigest {
		fmt.Fprintf(w, "  archiveDigest: %s\n", dg)
	}
}

// ---------- install / update 的 --json 报告（v0.66） ----------
//
// 评估报告 P3："install / update 两个写命令没有 --json（CI 最需要的恰好没有
// 机器可读输出）"。两个命令回答的是同一个问题——"这次运行把每个依赖
// 解析/落地成了什么"——因此共用同一形状，差别只在 `command`。

// writeReportVersion 是这份报告的 schema 版本（与 verify 的报告各自演进）。
const writeReportVersion = 1

// writeReport 是 `ngm install` / `ngm update` 的 --json 报告。
type writeReport struct {
	Version int    `json:"version"`
	Command string `json:"command"` // install | update
	Offline bool   `json:"offline"`
	// Dependencies 按 name + subPath 排序（与 lock 同一口径）——结果确定、可 diff。
	Dependencies []writeReportDep `json:"dependencies"`
	// Count 是报告的依赖条数。
	Count int `json:"count"`
	// FromStore 是**内容取自 content store**（该条未触网）的条数。
	// 它是"这次安装有多离线"的直接读数，与人类输出里那句同源。
	FromStore int `json:"fromStore"`
	// Verification 是 verifyOnLock 复检的结论（策略未开启时为 nil/省略）。
	//
	// 为什么要有它（v0.66）：install / update 在 verifyOnLock 下可能以漂移
	// （exit 1）结束，而人读那侧会给出行动行——v0.32~v0.53 立下的"每一条失败
	// 都要说得出下一步"契约在机器那侧此前没有落处（那时这两个命令还没有
	// `--json`）。字段与人读**同源**：`remediation` 就是渲染 `→` 行用的那句话。
	Verification *writeReportVerify `json:"verification,omitempty"`
}

// writeReportVerify 是 verifyOnLock 复检的机器可读形态。
type writeReportVerify struct {
	// ExitCode 是复检自己的退出码（0/1/2/4）——它也是进程的退出码。
	ExitCode int `json:"exitCode"`
	// Drifts 覆盖未通过（或未能完成）的依赖，一条一行。
	Drifts []writeReportDrift `json:"drifts,omitempty"`
}

// writeReportDrift 是复检里一条未通过的依赖。
type writeReportDrift struct {
	Name        string `json:"name"`
	DriftKind   string `json:"driftKind,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Err         string `json:"error,omitempty"`
}

// writeReportDep 是报告里的一条依赖。
type writeReportDep struct {
	Name          string `json:"name"`
	Ref           string `json:"ref"`
	RefType       string `json:"refType"`
	SubPath       string `json:"subPath,omitempty"`
	Commit        string `json:"commit"`
	ArchiveDigest string `json:"archiveDigest"`
	VendorPath    string `json:"vendorPath"`
	// Source 是这一条内容的来源：
	//
	//	resolved  install 的 fresh 路径 / update：本次重新解析
	//	store     content store 命中（未触网）
	//	fetched   从 mirror 取（install 的 lock 路径）
	Source string `json:"source"`
}

// writeReportDepFromLock 从 lock 条目构造报告行（install 的 lock 路径）。
func writeReportDepFromLock(d *lock.Dependency, source string) writeReportDep {
	return writeReportDep{
		Name: d.Name, Ref: d.Ref, RefType: d.RefType, SubPath: d.SubPath,
		Commit: d.Commit, ArchiveDigest: d.ArchiveDigest, VendorPath: d.VendorPath,
		Source: source,
	}
}

// Marshal 返回确定格式的 --json 字节流（与其余报告同一纪律）。
func (r *writeReport) Marshal() ([]byte, error) {
	out := *r
	if out.Dependencies == nil {
		out.Dependencies = []writeReportDep{}
	}
	sort.SliceStable(out.Dependencies, func(i, j int) bool {
		if out.Dependencies[i].Name != out.Dependencies[j].Name {
			return out.Dependencies[i].Name < out.Dependencies[j].Name
		}
		return out.Dependencies[i].SubPath < out.Dependencies[j].SubPath
	})
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		// 编码固定结构不该失败；真失败说明进程状态异常。
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal install report",
			"re-run the command; if it persists, report it together with the output above", err)
	}
	return append(data, '\n'), nil
}

// emitWriteReport 在 --json 时把报告写到 stdout 并保留原有退出码。
//
// 退出码不变（v0.16 第②条规矩）：报告是**附加**的通道，不参与判定。
// 人读文本此时已全部导向 stderr（见 runInstall / runUpdate）。
func emitWriteReport(ctx context.Context, stdout, stderr io.Writer, rep *writeReport, code int, jsonOut bool) int {
	if !jsonOut {
		return code
	}
	rep.Count = len(rep.Dependencies)
	data, merr := rep.Marshal()
	if merr != nil {
		return runErr(ctx, stdout, stderr, merr)
	}
	if _, werr := stdout.Write(data); werr != nil {
		fmt.Fprintf(stderr, "write report: %v\n", werr)
		return errs.CodeInternal.ExitCode()
	}
	return code
}
