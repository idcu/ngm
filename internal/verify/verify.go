package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

// DefaultConcurrency 是依赖级并发度的默认值。
//
// 取 8 的依据是本机实测（100 依赖、8 核）：
//
//	并发 4   online 5.09s / offline 2.81s
//	并发 8   online 3.37s / offline 1.97s
//	并发 16  online 3.32s / offline 1.92s   ← 已无收益
//
// 也就是说 8 是曲线的拐点：再往上只是让磁盘与进程表更挤，而小机器上
// 并发过高反而更差。并行的对象是**外部 git 进程的等待**，不是 CPU 计算。
const DefaultConcurrency = 8

// Options 控制一次 verify。
//
// 所有外部依赖都以参数注入（而非在包内读环境变量），使本包可测试、且
// --offline 的"绝不触网"承诺由调用方通过 EnsureMirror 明确表达。
type Options struct {
	// Offline 为 true 时绝不触网：ref 对比退化为本地 mirror 快照，结果标记 stale。
	Offline bool
	// Deep 为 true 时对 vendor 树做逐文件内容哈希（默认只比结构与元数据）。
	Deep bool
	// Strict 为 true 时把"预期更新"也视为失败（退出码 1）。
	Strict bool
	// AllowDrift 为 true 时不因"非预期漂移"失败（退出码 0）。不影响 critical。
	AllowDrift bool

	// MirrorRoot 是层 1 根目录（ref 对比与 digest 重放都基于它）。
	MirrorRoot string
	// ContentRoot 是层 2 根目录。
	ContentRoot string
	// VendorRoot 是层 3 根目录（调用方已按 vendor.mode 展开为 local / global）。
	VendorRoot string

	// GitOpts 是 git 子进程选项（含 Secrets 脱敏集合）。
	GitOpts git.Options
	// Protocol 是访问远端时使用的协议。
	Protocol resolve.Protocol

	// Concurrency 是依赖级并发度；<=0 时用 DefaultConcurrency。
	//
	// 并行的不是哈希（那部分很便宜），而是**等待 git 子进程**——
	// 复盘 §3.3 已确认 verify 的成本几乎全在 spawn 上。
	Concurrency int

	// ResolveRemoteRef 在**在线**模式下把 ref 解析为 commit，**不传输对象**。
	//
	// 为什么要单独一个注入点（ADR-010）：ref 判定只需要远端自己广播的 refs
	// （一次 `ls-remote`），不需要先把对象搬下来。注入点存在是因为 URL / 协议 /
	// 凭证都在调用方，本包不该知道。
	//
	// nil 表示按老办法——从本地 mirror 解析（`--offline` 走这条）。
	ResolveRemoteRef func(ctx context.Context, repo resolve.Canonical, ref string, refType resolve.RefType) (string, error)

	// EnsureMirror 确保层 1 就绪并返回裸仓库路径。
	//
	// 非 --offline 时通常是"缺失即 clone、存在即增量 fetch"；
	// --offline 时必须是"只返回已存在的 mirror，缺失即报 CodeGitFetch"。
	// 这一处注入即区分了两种模式，本包不再自行判断是否触网。
	EnsureMirror func(ctx context.Context, repo resolve.Canonical) (string, error)
}

// Run 对 lock 中的依赖逐个执行三级检查。
//
// 返回的 error 只表示**无法开始或无法继续**的致命错误（lock 非法、slug 非法）。
// 单个依赖的网络/资源失败记录在该依赖的 Err 字段，并让整体退出码为 4——
// 这样一次 --json 调用能同时呈现"哪几条断定失败"与"哪几条没查完"。
func Run(ctx context.Context, lf *lock.File, opts Options) (Report, error) {
	rep := Report{
		Version:      ReportVersion,
		Offline:      opts.Offline,
		Deep:         opts.Deep,
		Strict:       opts.Strict,
		AllowDrift:   opts.AllowDrift,
		Dependencies: []DepResult{},
	}
	if lf == nil {
		return rep, errs.New(errs.CodeConfigInvalid, "no lock file to verify", "")
	}
	if err := lf.Validate(); err != nil {
		return rep, err
	}

	// 并发执行：verify 的成本几乎全在 git 子进程启动（复盘 §3.3），而各依赖的
	// 检查彼此独立（各自一个 mirror；同一 mirror 的并发由 vendor.Mirror 的
	// 按路径加锁保证），因此可以并行摊掉这段等待。
	//
	// **结果必须按 lock 顺序**落位：报告要可 diff、可快照，顺序不能随调度抖动。
	// 因此写入 results[i] 而不是 append。
	store := vendor.NewContentStore(opts.ContentRoot)
	conc := opts.Concurrency
	if conc <= 0 {
		conc = DefaultConcurrency
	}
	if len(lf.Dependencies) < conc {
		conc = len(lf.Dependencies)
	}
	results := make([]DepResult, len(lf.Dependencies))

	var (
		mu    sync.Mutex
		fatal error
		wg    sync.WaitGroup
		sem   = make(chan struct{}, conc)
	)
	for i := range lf.Dependencies {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := verifyOne(ctx, &lf.Dependencies[i], store, opts)

			mu.Lock()
			defer mu.Unlock()
			results[i] = res
			if err != nil && fatal == nil {
				fatal = err
			}
		}(i)
	}
	wg.Wait()
	if fatal != nil {
		return rep, fatal
	}
	rep.Dependencies = results

	rep.Summarize()
	return rep, nil
}

// verifyOne 对单个依赖执行三级检查。
//
// error 只用于"连检查都无法进行"的致命情况；网络/资源类失败写进 DepResult.Err，
// 让其余依赖仍能得到结论（部分报告比中止更有用）。
func verifyOne(ctx context.Context, d *lock.Dependency, store *vendor.ContentStore, opts Options) (DepResult, error) {
	res := DepResult{
		Name:          d.Name,
		Ref:           d.Ref,
		RefType:       d.RefType,
		SubPath:       d.SubPath,
		Commit:        d.Commit,
		ArchiveDigest: d.ArchiveDigest,
		VendorPath:    d.VendorPath,
		Stale:         opts.Offline,
		Checks:        []CheckResult{},
	}

	repo, err := resolve.ParseSlug(d.Name)
	if err != nil {
		// lock.Read 已校验过 name，这里是纵深防御：非法 slug 属于配置问题（exit 3）
		return res, err
	}

	// ---- 层 1：让 mirror **存在**（缺失才 clone）
	//
	// 刻意**不**在这里 fetch：常见情形是"什么都没变"，那时 fetch 不产生新信息——
	// 它只是把"ref 仍指向 Locked 的 commit"这个**已在远端成立**的事实搬到本地。
	// ref 判定只需要远端广播，对象按需再取（ADR-010）。
	//
	// 检查一、二依赖层 1，检查三不依赖。因此层 1 不可用时只跳过前两项，
	// 落地完整性照样能查——少报一条结论比整条依赖弃查更有用。
	mirrorPath := mirrorPathFor(opts, repo)
	if !git.IsBareMirror(mirrorPath) {
		if _, merr := opts.EnsureMirror(ctx, repo); merr != nil {
			return finishSkippingRefAndDigest(res, merr, mirrorPath, d, store, opts)
		}
		mirrorPath = mirrorPathFor(opts, repo)
	}

	// ---- 检查一：ref → commit（只读 ref 广播，不传对象）
	refCheck, resolved := checkRef(ctx, repo, d, opts)
	res.ResolvedCommit = resolved
	res.Checks = append(res.Checks, refCheck)

	if refCheck.Operational {
		res.Err = refCheck.Detail
	} else {
		// ---- 检查二：digest 重放（本地；必要时先取对象）
		//
		// **不做存在性预检**：那是成功路径上白花的一次 spawn（100 依赖 = 100 次），
		// 而成功路径正是常态。改成"缺 commit 时返回一个可识别的信号"，由这里决定
		// 是取对象重试、还是按离线规则报资源缺失（ADR-010）。
		digestCheck, derr := checkDigest(ctx, mirrorPath, d, opts)

		if errors.Is(derr, errCommitMissing) {
			switch {
			case opts.Offline:
				// 离线不许取对象：资源缺失 → exit 4（observability.md 的退出码表）
				res.Checks = append(res.Checks, digestCheck)
				res.Err = digestCheck.Detail
				derr = errs.New(errs.CodeGitFetch, d.Name+": "+digestCheck.Detail+" (--offline)",
					"the local mirror is cold; run once without --offline (or `ngm install`) to warm it")

			default:
				// 在线：取对象后重试一次。
				//
				// 这一步是 ADR-010 点名的关键：本地缺 commit **不等于**上游丢弃了它
				// （可能只是从未取过）。必须先取过才能下那个结论，否则会把
				// "我们没下载"误报成"上游删了"——那是误判，比慢更严重。
				if _, merr := opts.EnsureMirror(ctx, repo); merr != nil {
					digestCheck.Detail += "; could not refresh the mirror to confirm: " + merr.Error()
					res.Checks = append(res.Checks, digestCheck)
					res.Err = digestCheck.Detail
					derr = merr
					break
				}
				mirrorPath = mirrorPathFor(opts, repo)
				digestCheck, derr = checkDigest(ctx, mirrorPath, d, opts)
				if errors.Is(derr, errCommitMissing) {
					// 取过了仍然没有 → 现在这个推断才有证据：上游丢弃了该提交
					// （force push 后 gc、仓库重建等）。这不是网络故障，而是非预期漂移。
					digestCheck.Drift = DriftUnexpected
					digestCheck.Operational = false
					digestCheck.Detail += "; the refreshed mirror no longer contains it, " +
						"so the upstream discarded that commit"
					derr = nil
				}
				res.Checks = append(res.Checks, digestCheck)
				if derr != nil {
					res.Err = derr.Error()
				}
			}
		} else {
			res.Checks = append(res.Checks, digestCheck)
			if derr != nil {
				res.Err = derr.Error()
			}
		}
	}

	// ---- 检查三：落地完整性（完全本地；--deep 的重放需要层 1 的形状信息）
	res.Checks = append(res.Checks, checkLanding(ctx, mirrorPath, d, store, opts))

	// ---- 聚合：取最严重的分类
	res.DriftKind = aggregateDrift(res.Checks)
	res.Remediation = remediationFor(&res)
	return res, nil
}

// mirrorPathFor 返回某仓库在 mirror 中的路径（纯字符串推导，**不触网**）。
func mirrorPathFor(opts Options, repo resolve.Canonical) string {
	return vendor.NewMirror(opts.MirrorRoot, opts.GitOpts).PathFor(repo)
}

// finishSkippingRefAndDigest 处理"层 1 不可用"：只跳过前两项检查。
func finishSkippingRefAndDigest(res DepResult, merr error, mirrorPath string,
	d *lock.Dependency, store *vendor.ContentStore, opts Options) (DepResult, error) {
	res.Err = merr.Error()
	res.Checks = append(res.Checks, CheckResult{
		Check:       CheckRef,
		Status:      StatusFail,
		Operational: true,
		Detail:      "the local mirror is unavailable, so the ref and digest checks were skipped",
	})
	res.Checks = append(res.Checks, checkLanding(context.Background(), mirrorPath, d, store, opts))
	res.DriftKind = aggregateDrift(res.Checks)
	res.Remediation = remediationFor(&res)
	return res, nil
}

// errCommitMissing 是内部信号：lock 的 commit 不在本地 mirror 里。
//
// 它**不是**最终结论（"上游丢弃了该提交"）——那需要先取过对象才有证据
// （ADR-010）。调用方据此决定取对象重试，还是按离线规则报资源缺失。
var errCommitMissing = errors.New("commit missing from the local mirror")

// checkRef 是检查一：重新解析 refType → commit，并与 lock 的 commit 对比。
//
// 返回 (检查结果, 本次解析到的 commit；解析失败时为空)。
//
// **它自己不取对象**——除非 ref 已经变了（那时判漂移性质需要祖先关系）。
// 这是 ADR-010 的关键：把"判性质"从"解析"里拆出来，因为只有前者需要对象。
func checkRef(ctx context.Context, repo resolve.Canonical, d *lock.Dependency, opts Options) (CheckResult, string) {
	rt := resolve.RefType(d.RefType)
	if !rt.IsValid() {
		return CheckResult{Check: CheckRef, Status: StatusFail,
			Detail: "unsupported refType " + d.RefType}, ""
	}

	resolved, err := resolveTargetRef(ctx, repo, d, rt, opts)
	if err != nil {
		return refResolveFailure(opts, d, err), ""
	}

	if resolved == d.Commit {
		return CheckResult{Check: CheckRef, Status: StatusOK,
			Detail: fmt.Sprintf("%s %s still resolves to %s", d.RefType, d.Ref, git.ShortSHA(d.Commit))}, resolved
	}

	// ref 已变：判性质需要祖先关系，也就要对象——这是按需取物的第一种情形。
	// 取不到对象时**不得**猜性质：猜"历史被改写"会把"没下载"误报成 force push。
	if _, merr := opts.EnsureMirror(ctx, repo); merr != nil {
		return CheckResult{Check: CheckRef, Status: StatusFail, Operational: true,
			Detail: fmt.Sprintf("the ref moved (%s → %s), but the mirror could not be refreshed to "+
				"classify the drift: %v", git.ShortSHA(d.Commit), git.ShortSHA(resolved), merr)}, resolved
	}

	kind, why := classifyRefDrift(ctx, repo, d, resolved, opts)
	return CheckResult{Check: CheckRef, Status: StatusFail, Drift: kind, Detail: why}, resolved
}

// resolveTargetRef 把 ref 解析为 commit。
//
// 在线模式走注入的**远端**解析（ADR-010）：读远端自己广播的 refs，不传对象。
// 离线模式仍是本地 mirror 快照，并由调用方标记 stale。
func resolveTargetRef(ctx context.Context, repo resolve.Canonical, d *lock.Dependency,
	rt resolve.RefType, opts Options) (string, error) {
	if opts.ResolveRemoteRef != nil {
		return opts.ResolveRemoteRef(ctx, repo, d.Ref, rt)
	}
	return resolve.ResolveRef(ctx, repo, d.Ref, rt, resolve.ResolveOptions{
		MirrorDir: opts.MirrorRoot,
		Protocol:  opts.Protocol,
		Secrets:   opts.GitOpts.Secrets,
	})
}

// refResolveFailure 把"ref 解析失败"分类：网络类 → 操作性失败；其余 → 非预期漂移。
//
// 后者的唯一现实成因是"该 ref 已不存在于上游"（被删除/改名）——
// 对 verify 而言这是"ref 没了"，不是配置错误。
func refResolveFailure(opts Options, d *lock.Dependency, err error) CheckResult {
	if codeOf(err) == errs.CodeGitFetch {
		where := "the local mirror"
		if opts.ResolveRemoteRef != nil {
			where = "the remote"
		}
		return CheckResult{Check: CheckRef, Status: StatusFail, Operational: true,
			Detail: "cannot read refs from " + where + ": " + err.Error()}
	}
	return CheckResult{Check: CheckRef, Status: StatusFail, Drift: DriftUnexpected,
		Detail: fmt.Sprintf("%s %q can no longer be resolved: %v", d.RefType, d.Ref, err)}
}

// classifyRefDrift 把"ref 解析结果与 lock 不一致"分类。
//
// 依据 observability.md §"预期更新" vs "非预期漂移"：
//
//	branch 快进（lock.commit 是 resolved 的祖先）→ expected（分支前进 N 个 commit）
//	branch 非快进                              → unexpected（历史被改写 / force push）
//	tag 指向变化                                → unexpected（tag 被重打）
//	其它 refType                                → unexpected（锁与声明不符）
//
// 注意两种迹象的可观测现象完全相同（ref 指向了别的 commit），区别只在祖先关系，
// 所以 fast-forward 判定是这里唯一的信息来源。
func classifyRefDrift(ctx context.Context, repo resolve.Canonical, d *lock.Dependency, resolved string, opts Options) (DriftKind, string) {
	switch resolve.RefType(d.RefType) {
	case resolve.RefTypeBranch:
		mirrorPath := vendor.NewMirror(opts.MirrorRoot, opts.GitOpts).PathFor(repo)
		ff, err := git.IsAncestor(ctx, opts.GitOpts, mirrorPath, d.Commit, resolved)
		if err != nil {
			// 无法证明祖先关系时保守处理：按"历史被改写"上报，交由人工确认。
			// 反向（当成快进而放行）会让 force push 悄悄通过门禁。
			return DriftUnexpected, fmt.Sprintf(
				"branch %s moved %s → %s and fast-forward could not be proven (%v); treating it as rewritten history",
				d.Ref, git.ShortSHA(d.Commit), git.ShortSHA(resolved), err)
		}
		if ff {
			return DriftExpected, fmt.Sprintf(
				"branch %s advanced: %s → %s (fast-forward)",
				d.Ref, git.ShortSHA(d.Commit), git.ShortSHA(resolved))
		}
		return DriftUnexpected, fmt.Sprintf(
			"branch %s was rewritten: %s is not an ancestor of %s (force push?)",
			d.Ref, git.ShortSHA(d.Commit), git.ShortSHA(resolved))

	case resolve.RefTypeTag:
		return DriftUnexpected, fmt.Sprintf(
			"tag %s was moved: ngm.lock pins %s but it now points at %s",
			d.Ref, git.ShortSHA(d.Commit), git.ShortSHA(resolved))

	default:
		return DriftUnexpected, fmt.Sprintf(
			"ref %s (%s) resolves to %s, which differs from the pinned commit %s",
			d.Ref, d.RefType, git.ShortSHA(resolved), git.ShortSHA(d.Commit))
	}
}

// checkDigest 是检查二：从 mirror 的同一 commit 重建清单、重算 digest，与 lock 对比。
//
// 完全本地、可离线（ADR-008；observability.md §ngm verify）。
// 它回答的是："本地 mirror 里那份内容，还是不是当初锁定的那一份。"
//
// 返回值约定：error 非 nil **仅表示操作性失败**（无法完成重放），调用方据此
// 区分"判定为不匹配"（critical，exit 2）与"没查成"（exit 4）。
func checkDigest(ctx context.Context, mirrorPath string, d *lock.Dependency, opts Options) (CheckResult, error) {
	// 先尝试重放，只有**失败**时才去判定"是操作性失败还是 commit 不存在"。
	//
	// 顺序是有意的：成功路径上因此少一次 git 子进程（100 个依赖就是 100 次 spawn，
	// 而 spawn 正是 verify 的主要成本，见复盘 §3.3）；失败时才补那一次判定。
	// 分类结论与之前完全一致——只是把它挪到了真正需要的分支上。
	replayed, err := git.BuildArchiveDigest(ctx, opts.GitOpts, mirrorPath, d.Commit)
	if err == nil {
		if replayed != d.ArchiveDigest {
			return CheckResult{Check: CheckDigest, Status: StatusFail, Drift: DriftCritical,
				Detail: fmt.Sprintf(
					"digest replay mismatch: ngm.lock says %s but the local mirror replays to %s "+
						"(manifest spec %s)",
					d.ArchiveDigest, replayed, digest.ManifestVersion)}, nil
		}
		return CheckResult{Check: CheckDigest, Status: StatusOK,
			Detail: "archiveDigest replays identically from the local mirror"}, nil
	}

	exists, eerr := git.CommitExists(ctx, opts.GitOpts, mirrorPath, d.Commit)
	if eerr == nil && !exists {
		// 只报"缺"，**不**下结论。分类在 verifyOne：只有它知道这次允许不允许取对象，
		// 而"上游丢弃了该提交"必须先取过才有证据（ADR-010）。
		return CheckResult{
			Check:       CheckDigest,
			Status:      StatusFail,
			Operational: true,
			Detail: fmt.Sprintf("commit %s from ngm.lock is not present in the local mirror",
				git.ShortSHA(d.Commit)),
		}, errCommitMissing
	}

	return CheckResult{Check: CheckDigest, Status: StatusFail, Operational: true,
		Detail: "digest replay failed: " + err.Error()}, err
}

// checkLanding 是检查三：落地完整性。
//
// 默认（浅）——只做 stat 级比对，代价与树规模无关：
//
//  1. content store 里该 digest 就绪，且 meta.json 与 lock 完全一致
//  2. vendor 落地路径存在
//  3. vendor 树与 content 树结构一致（路径集合 / symlink 目标 / 文件大小）
//
// --deep 追加两项全量字节校验：
//
//  4. 用「Git 权威的路径与模式 + 落地字节」重算 digest，与 lock 的 archiveDigest 比对
//  5. vendor 树与 content 树逐文件 sha256 比对
//
// 第 4 项不可省：默认 layout（hardlink）下 vendor 与 content 是同一 inode，
// 就地篡改会同时改变两侧，第 5 项因此看不出异常；只有对着 lock 重放 digest
// 才能发现层 2 被篡改。
//
// 任何不一致都按完整性失败处理（driftKind: critical → exit 2）。
// 依据：observability.md 的全局退出码表把 `2` 定义为"完整性失败"，
// digest 重放不匹配只是它的典型来源。
func checkLanding(ctx context.Context, mirrorPath string, d *lock.Dependency, store *vendor.ContentStore, opts Options) CheckResult {
	var problems []string
	var notes []string

	// 1) content store 的存在性与 meta 一致性
	hasStore := store.Has(d.ArchiveDigest)
	if !hasStore {
		problems = append(problems, fmt.Sprintf(
			"content store has no usable entry for %s (looked in %s)",
			d.ArchiveDigest, store.PathForDigest(d.ArchiveDigest)))
	} else if meta, err := store.ReadMeta(d.ArchiveDigest); err != nil {
		problems = append(problems, "content store meta.json is unreadable: "+err.Error())
	} else {
		problems = append(problems, metaMismatches(d, meta)...)
	}

	// 2) --deep：用「Git 权威的路径与模式 + 落地字节」重算 digest，与 lock 比对。
	//
	// 这是唯一能发现**层 2 内容被就地篡改**的检查：默认 layout 下 vendor 与
	// content 是同一 inode，篡改会同时改变两侧，因此"vendor ↔ content 比对"
	// （第 3 步）看不出任何异常；只有对着 lock 里的 digest 重放才能发现。
	if opts.Deep {
		switch {
		case mirrorPath == "":
			notes = append(notes, "the content digest could not be replayed because the local mirror is unavailable")
		case hasStore:
			got, derr := replayDigestFromStore(ctx, mirrorPath, store.TreePath(d.ArchiveDigest), d, opts)
			if derr != nil {
				problems = append(problems, "cannot replay the digest from the content store: "+derr.Error())
			} else if got != d.ArchiveDigest {
				problems = append(problems, fmt.Sprintf(
					"content store digest mismatch: the installed tree hashes to %s but ngm.lock says %s",
					got, d.ArchiveDigest))
			}
		}
	}

	// 3) vendor 树 vs content 树（结构；--deep 时追加逐文件字节比对）
	vendorTree := filepath.Join(opts.VendorRoot, filepath.FromSlash(vendor.VendorPathFor(d.VendorPath, d.SubPath)))
	vr := vendor.VerifyResult{}
	if _, err := os.Stat(vendorTree); err != nil {
		problems = append(problems, "vendor path is missing: "+vendorTree)
	} else {
		compare := vendor.VerifyVendorTreeShallow
		if opts.Deep {
			compare = vendor.VerifyVendorTree
		}
		var verr error
		vr, verr = compare(vendorTree, contentTreeFor(store, d))
		if verr != nil {
			problems = append(problems, "cannot compare vendor with the content store: "+verr.Error())
		} else {
			problems = append(problems, vr.Mismatches...)
		}
	}

	if len(problems) > 0 {
		return CheckResult{Check: CheckLanding, Status: StatusFail, Drift: DriftCritical,
			Detail: strings.Join(problems, "; ")}
	}

	mode := "structure only"
	if opts.Deep {
		mode = "structure + digest replay + content hashes"
	}
	detail := fmt.Sprintf("vendor tree matches the content store (%d files, %d symlinks; %s)",
		vr.Files, vr.Symlinks, mode)
	if len(notes) > 0 {
		detail += "; note: " + strings.Join(notes, "; ")
	}
	return CheckResult{Check: CheckLanding, Status: StatusOK, Detail: detail}
}

// replayDigestFromStore 用「Git 权威的形状 + content store 的实际字节」重算 digest。
//
// 为什么形状（路径与模式）取自 Git，而不是直接遍历落地目录：
//
//	文件系统无法完整还原 Git 语义。最典型的是可执行位——Windows 上 100755
//	不可表示，若按目录推断模式，同一份内容在不同平台会算出不同 digest，
//	把正常的构建判成完整性失败。所以：**形状来自 mirror 的 commit tree，
//	字节来自落地内容树**。这样既不依赖文件系统元数据，又能检出任何字节篡改。
//
// 传入的 storeRoot 必须是 **完整内容树根**（不含 monorepo 子路径）：lock 里的
// archiveDigest 覆盖整仓，子路径只在 vendor/mappings 层收窄。
//
// 返回的 error 表示"无法完成重放"（文件缺失、无法读取）——调用方按完整性失败
// 处理：证明不出来就必须阻断。
func replayDigestFromStore(ctx context.Context, mirrorPath, storeRoot string, d *lock.Dependency, opts Options) (string, error) {
	entries, err := git.ListTree(ctx, opts.GitOpts, mirrorPath, d.Commit)
	if err != nil {
		return "", err
	}

	records := make([]digest.Record, 0, len(entries))
	for _, e := range entries {
		if e.Mode == digest.ModeTree {
			continue
		}
		full := filepath.Join(storeRoot, filepath.FromSlash(e.Path))

		var content []byte
		if e.Mode == digest.ModeSymlink {
			target, lerr := os.Readlink(full)
			if lerr != nil {
				return "", fmt.Errorf("%s: expected a symlink in the content store: %w", e.Path, lerr)
			}
			content = []byte(target)
		} else {
			content, err = os.ReadFile(full)
			if err != nil {
				return "", fmt.Errorf("%s: %w", e.Path, err)
			}
		}

		records = append(records, digest.Record{
			Path:       e.Path,
			Mode:       e.Mode,
			BlobSHA256: digest.HashBytes(content),
		})
	}

	return digest.Digest(digest.BuildManifest(records)), nil
}

// metaMismatches 比对 content store 的 meta.json 与 lock 声明。
//
// **只校验身份类字段，不校验来源类字段**（repo / commit / subPath）。
//
// 原因：层 2 是内容寻址的——相同内容只能存在一份，因此两个依赖（乃至两个
// 项目）内容相同时**合法地共享同一个条目**，而 meta.json 记录的是**首次写入者**
// 的来源。把 repo / commit 差异当成失败，等于把"内容去重生效"误报成
// "完整性被破坏"，会让 verify 在正常仓库上大面积假阳性。
//
// 真正需要拦住的只有两件事：
//
//   - digest 目录名与 meta 自述不符 → store 被手工搬动/拼接（目录名不再是内容的身份）
//   - 清单规范版本变化 → ADR-008 升级后旧 digest 不再可解释
//
// "层 2 里的字节是否仍是 lock 声明的那一份"由 --deep 的 digest 重放回答；
// 只靠 meta.json 是答不了的（那是自述，不是证据）。
func metaMismatches(d *lock.Dependency, meta *vendor.Meta) []string {
	var out []string
	if meta.Digest != d.ArchiveDigest {
		out = append(out, fmt.Sprintf(
			"content store entry %s describes itself as %s (the store is inconsistent)",
			d.ArchiveDigest, meta.Digest))
	}
	if meta.ManifestVersion != digest.ManifestVersion {
		out = append(out, fmt.Sprintf(
			"content store entry was written with manifest spec %q but this build uses %q",
			meta.ManifestVersion, digest.ManifestVersion))
	}
	return out
}

// contentTreeFor 返回该依赖在 content store 中的内容树根（已展开 monorepo 子路径）。
//
// 与 cmd 层 env.ContentReader 的口径一致：monorepo 只落地并校验子目录。
func contentTreeFor(store *vendor.ContentStore, d *lock.Dependency) string {
	root := store.TreePath(d.ArchiveDigest)
	sub := strings.Trim(filepath.ToSlash(d.SubPath), "/")
	if sub == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(sub))
}

// aggregateDrift 取各检查中最严重的分类（critical > unexpected > expected）。
func aggregateDrift(checks []CheckResult) DriftKind {
	worst := DriftNone
	for _, c := range checks {
		if c.Drift.Rank() > worst.Rank() {
			worst = c.Drift
		}
	}
	return worst
}

// remediationFor 给出下一步建议（observability.md 输出示例里的 `→` 行）。
//
// 语气按严重度递进：expected 是"可以接受"，unexpected 是"去确认"，
// critical 是"禁止构建，先查清楚"。
func remediationFor(res *DepResult) string {
	switch {
	case res.Err != "":
		return "the check could not complete; re-run without --offline to refresh the local mirror"
	case res.DriftKind == DriftExpected:
		return fmt.Sprintf("expected update, not blocking; run `ngm update %s` to accept it", res.Name)
	case res.DriftKind == DriftUnexpected:
		return fmt.Sprintf("confirm the change with the upstream owner, then run `ngm update %s`; "+
			"if nobody moved it, the upstream repository may be compromised", res.Name)
	case res.DriftKind == DriftCritical:
		return "do NOT build from this tree; investigate how the bytes changed, " +
			"then re-run `ngm install` to restore provable content"
	default:
		return ""
	}
}

// codeOf 取出 NgmError 的错误码；非 NgmError 返回 CodeConfigInvalid。
//
// 保守取配置错误：无法识别的失败不应被当成"网络问题"而给出 exit 4，
// 那会让真正的缺陷被误判为可重试。
func codeOf(err error) errs.Code {
	var ne *errs.NgmError
	if errors.As(err, &ne) {
		return ne.Code
	}
	return errs.CodeConfigInvalid
}
