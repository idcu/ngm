package main

import (
	"context"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/supplychain"
)

// projectPolicy 构造项目当前生效的供应链策略。
//
// 未配置时返回一份**空策略**（不门禁）——把"没配策略"当成"全部拒绝"会让所有既有项目
// 突然装不上，那不是安全，那是事故。
//
// 放在 CLI 层而不是 resolve / supplychain：策略的**来源**是项目的 ngm.json，
// 而 resolve 不能依赖 config（config 依赖 resolve 做校验），所以由这里把两者接起来。
func projectPolicy(pf *config.ProjectFile) (*supplychain.Policy, error) {
	if pf == nil {
		return supplychain.FromConfig(nil)
	}
	return supplychain.FromConfig(pf.SupplyChain)
}

// checkReleaseAges 对每个依赖执行 minimumReleaseAge（晾晒期）门禁。
//
// 与白名单门禁的**时机不同**，这一点必须说清：白名单在每层开头、任何远端访问之前判定；
// 而晾晒期只能在解析**之后**判定，因为时间源是 commit 的 committer date，只有拿到 commit
// 才知道提交时间。所以它是唯一一个"先触网再判定"的策略门禁——这个代价已写入 ADR-009。
//
// 被 allowlistRepos 显式放行的仓库不受此约束（紧急通道，见 supply-chain.md）：
// 放行单个仓库比临时调低全局阈值安全得多——后者会同时对**所有**依赖失效。
func checkReleaseAges(ctx context.Context, env *projectEnv, g *resolve.Graph, p *supplychain.Policy) error {
	if p == nil || p.MinimumReleaseAge() <= 0 || g == nil {
		return nil
	}
	now := time.Now()
	for _, n := range g.Nodes {
		if p.IsAllowlisted(n.Repo.Host, n.Repo.Path) {
			continue
		}
		committed, err := git.CommitTime(ctx, env.GitOpts, n.MirrorPath, n.Commit)
		if err != nil {
			return err
		}
		if err := p.CheckMinimumAge(n.Key, committed, now); err != nil {
			return err
		}
	}
	return nil
}
