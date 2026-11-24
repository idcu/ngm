package main

import (
	"context"
	"errors"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/supplychain"
)

// projectPolicy 构造项目当前生效的供应链策略。
//
// ADR-053 起，未配置 supplyChain 段也返回一份带 24h 晾晒期的策略：
// "全局默认 + 显式 PT0S 关闭"取代了 ADR-009 的"没配策略=不门禁"底线，但仅限
// minimumReleaseAge——白名单等其它门禁仍保持 ADR-009 的空策略语义。
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
	if !p.AgeGateActive() || g == nil {
		return nil
	}
	now := time.Now()
	for _, n := range g.Nodes {
		// local-dir 的 Commit 是 dirDigest、没有 git 时间源；内容寻址目录不接受晾晒期门禁。
		if n.RefType == resolve.RefTypeLocalDir {
			continue
		}
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

// checkTrustNoDowngrade 在图解析后、写新 lock 前拦截"锚点强度降级"（ADR-053）。
//
// 用旧 lock 的条目按 name#subPath 建索引，与新图的节点逐一同键比较：
//   - 策略未开启、没有旧 lock、旧条目缺失：放行（新依赖没有"历史强度"可比）；
//   - 只比较 RefType 的强度阶梯，具体内容变化仍由 digest 门禁负责。
//
// 纯函数、不碰 I/O，便于 V212 判据直接构造图与旧 lock 验证四条分支。
func checkTrustNoDowngrade(pol *supplychain.Policy, previous *lock.File, g *resolve.Graph) error {
	if pol == nil || !pol.NoDowngrade() || previous == nil || g == nil {
		return nil
	}
	old := make(map[string]lock.Dependency, len(previous.Dependencies))
	for _, d := range previous.Dependencies {
		old[d.Key()] = d
	}
	for _, n := range g.Nodes {
		prev, ok := old[n.Key]
		if !ok {
			continue // 缺失旧条目 ⇒ 新依赖，没有可降级的锚点
		}
		if err := pol.CheckAnchorNoDowngrade(prev.RefType, string(n.RefType)); err != nil {
			var ne *errs.NgmError
			if errors.As(err, &ne) {
				return errs.Wrap(errs.CodeConfigInvalid, n.Key+": "+ne.Message, ne.Hint, err)
			}
			return errs.Wrap(errs.CodeConfigInvalid, n.Key+": "+err.Error(),
				"pin the ref to a commit or tag, or explicitly disable supplyChain.trustPolicy", err)
		}
	}
	return nil
}
