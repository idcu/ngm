package main

import (
	"context"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/supplychain"
	"github.com/idcu/ngm/internal/verify"
)

// autoVerifyAfterLock 在策略要求时，对刚落地/刷新的 lock 自动跑一遍 verify。
//
// 语义（architecture/supply-chain.md §verifyOnLock）：install / update **之后**的
// 独立复查。它不撤回那两个命令已经写下的东西，只是不允许"写完了却说不出它是否一致"。
//
// 两条刻意的选择：
//
//  1. **结论按 verify 自己的退出码返回**（0/1/2/4），不折成 0。
//     "装好了，但锁定的 ref 已经指向别处"如果被报成成功，这条策略就没有意义。
//  2. **只在策略明确开启时执行**。verify 要重新解析每个 ref（100 依赖约 3 秒、且需要网络），
//     默认替所有用户付这个代价不是好交易；`verifyOnLock` 无默认值正是这个意思。
//
// writeRep（可为 nil）在非 nil 时收到复检的机器可读形态（v0.66）：
// 人读那侧每条 `→ …` 行动行在 JSON 里必须有落处——`--json` 存在之前，
// "verifyOnLock 撞漂移"这条路径的脚本只能看到退出码 1。
func autoVerifyAfterLock(ctx context.Context, env *projectEnv, pf *config.ProjectFile,
	pol *supplychain.Policy, offline bool, writeRep *writeReport, stdout, stderr io.Writer) int {
	if pol == nil || !pol.VerifyOnLock() {
		return 0
	}

	lf, err := lock.Read(env.LockPath())
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if lf == nil {
		return 0
	}

	opts := verify.Options{
		Offline:      offline,
		MirrorRoot:   env.Layout.MirrorRoot(),
		ContentRoot:  env.Layout.ContentRoot(),
		VendorRoot:   env.VendorRoot(pf),
		GitOpts:      env.GitOpts,
		Protocol:     env.Protocol,
		EnsureMirror: env.EnsureMirror,
	}
	if offline {
		// 与 install/verify 共用同一实现：三条命令对"不联网"只有一种含义
		opts.EnsureMirror = offlineEnsureMirror(env)
	} else {
		// 在线：与 verify 同一条取数策略（ADR-010）
		opts.ResolveRemoteRef = env.RemoteRefResolver()
	}

	vrep, verr := verify.Run(ctx, lf, opts)
	if verr != nil {
		return runErr(ctx, stdout, stderr, verr)
	}

	if writeRep != nil {
		if writeRep.Verification == nil {
			writeRep.Verification = &writeReportVerify{}
		}
		writeRep.Verification.ExitCode = vrep.Summary.ExitCode
	}

	fmt.Fprintf(stdout, "\nverifyOnLock: re-checked %d dependencies\n", len(vrep.Dependencies))
	// 只展开未通过的：通过的依赖对用户没有信息量，失败细节才是行动依据
	for i := range vrep.Dependencies {
		d := &vrep.Dependencies[i]
		if d.DriftKind == verify.DriftNone && d.Err == "" {
			continue
		}
		renderVerifyDep(stdout, d)
		// 与人读同源：这里的 Remediation 就是渲染 `→` 行时用的那句话。
		if writeRep != nil {
			writeRep.Verification.Drifts = append(writeRep.Verification.Drifts, writeReportDrift{
				Name:        d.Name,
				DriftKind:   string(d.DriftKind),
				Remediation: d.Remediation,
				Err:         d.Err,
			})
		}
	}
	fmt.Fprintf(stdout, "%s\n", verifySummaryLine(vrep, offline))
	return vrep.Summary.ExitCode
}
