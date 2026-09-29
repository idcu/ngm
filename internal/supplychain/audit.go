package supplychain

import (
	"context"
	"fmt"
	"time"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
)

// AuditOptions 是一次审计的输入。
type AuditOptions struct {
	// OSV 是查询配置（缓存目录、端点、是否离线、是否强制刷新）。
	OSV OSVConfig
	// IgnoreSeverities 是被策略忽略的严重级别（来自 supplyChain.osvIgnoreSeverities）。
	IgnoreSeverities []string
	// Now 用于测试；为空时用 time.Now。
	Now func() time.Time
}

// Audit 对 ngm.lock 里的每个依赖查询已知漏洞。
//
// 一律按 **lock 中的 commit** 查询，而不是按版本号：ngm 锁的是 commit，
// 而"版本号没变、commit 变了"正是投毒的常见形态——按版本查会整类漏掉。
//
// 失败即返回错误（退出码由 errs 决定：3 配置/策略、4 网络且无缓存）。
// 它**不会**把"查询失败"降级成"没有漏洞"。
func Audit(ctx context.Context, lf *lock.File, opts AuditOptions) (*AuditReport, error) {
	if lf == nil {
		return nil, errs.New(errs.CodeConfigInvalid,
			"no ngm.lock to audit",
			"run `ngm install` to resolve dependencies and create the lock")
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}

	rep := &AuditReport{
		GeneratedAt:  now.UTC().Format("2006-01-02T15:04:05Z"),
		Findings:     make([]AuditFinding, 0, len(lf.Dependencies)),
		BySeverity:   map[string]int{},
		CoverageNote: CoverageNote,
	}

	for _, d := range lf.Dependencies {
		if d.Commit == "" {
			// 没有 commit 就无法按 commit 查询。静默跳过会让覆盖率看起来比实际高，
			// 因此作为 lock 数据不完整直接报错。
			return nil, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("dependency %s has no commit in ngm.lock", d.Name),
				"run `ngm install` to re-resolve and rewrite the lock")
		}

		vulns, err := QueryOSV(ctx, opts.OSV, d.Commit)
		if err != nil {
			return nil, err
		}
		kept, ignored := IgnoreSeverities(vulns, opts.IgnoreSeverities)
		rep.Ignored += ignored
		rep.Vulnerabilities += len(kept)
		for _, v := range kept {
			rep.BySeverity[v.Severity]++
		}

		rep.Findings = append(rep.Findings, AuditFinding{
			Name:    d.Name,
			Ref:     d.Ref,
			RefType: string(d.RefType),
			Commit:  d.Commit,
			SubPath: d.SubPath,
			Vulns:   kept,
		})
	}

	rep.Dependencies = len(lf.Dependencies)
	if rep.Vulnerabilities > 0 {
		rep.ExitCode = 1
	}
	return rep, nil
}
