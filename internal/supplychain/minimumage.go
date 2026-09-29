package supplychain

import (
	"fmt"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// CheckMinimumAge 校验一个依赖是否达到"晾晒期"（minimumReleaseAge）。
//
// 时间源由调用方传入（从本地 mirror 读到的 committer date，见 git.CommitTime）：
// 本函数保持**纯计算**，这样"读时间"与"判门禁"两件事可以各自测试与替换。
//
// 命中（依赖太新）返回 `CodeConfigInvalid`（exit 3）——与白名单门禁保持一致口径：
// 这不是"依赖里发现了问题"（那是 exit 1），而是**你的策略不允许它进入**。
//
// 被 `allowlistRepos` 显式放行的仓库不受此约束（见 IsAllowlisted）：紧急引入安全修复时，
// 显式放行单个仓库比临时调低全局阈值安全得多——后者对**所有**依赖同时失效。
func (p *Policy) CheckMinimumAge(name string, committedAt, now time.Time) error {
	if p == nil || p.minAge <= 0 {
		return nil
	}
	if committedAt.IsZero() {
		return errs.New(errs.CodeGitFetch,
			fmt.Sprintf("cannot check the release age of %s: no committer date", name),
			"the commit is missing from the local mirror; run install once with network access")
	}

	age := now.Sub(committedAt)
	if age >= p.minAge {
		return nil
	}

	return errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("%s was committed %s ago, younger than the %s minimum release age",
			name, formatAge(age), formatAge(p.minAge)),
		fmt.Sprintf("wait until %s, or allowlist this repository explicitly for an urgent fix "+
			"(prefer that over lowering the global threshold)",
			committedAt.Add(p.minAge).Format(time.RFC3339)))
}

// IsAllowlisted 表示该仓库是否被 `allowlistRepos` 显式放行。
//
// 放行的含义不只是"可以使用"，还包括**不受 minimumReleaseAge 约束**——
// 这是文档写明的紧急通道（见 supply-chain.md §时间源与局限）。
// 未配置 allowlistRepos 时一律返回 false（没有显式名单就没有例外）。
func (p *Policy) IsAllowlisted(host, repoPath string) bool {
	if p == nil || len(p.repoPatterns) == 0 {
		return false
	}
	target := host + "/" + repoPath
	for _, pat := range p.repoPatterns {
		if MatchRepoPattern(pat, target) {
			return true
		}
	}
	return false
}

// formatAge 把时长渲染成人能读的形式。
//
// Go 默认会把三天渲染成 `72h0m0s`；门禁报错必须让人一眼看出"还不够老"，
// 而不是逼读者自己换算。
func formatAge(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	days := int(d / (24 * time.Hour))
	rest := d - time.Duration(days)*24*time.Hour
	hours := int(rest / time.Hour)
	rest -= time.Duration(hours) * time.Hour
	mins := int(rest / time.Minute)

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
