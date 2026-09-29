package supplychain

import (
	"fmt"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

// Policy 是一份**生效中的**供应链策略。
//
// 由 ngm.json 的 supplyChain 段构造（见 ADR-009）。零值 Policy 表示"未配置策略"，
// 此时任何检查都应放行——把"没配策略"当作"全部拒绝"会让所有既有项目瞬间装不上。
type Policy struct {
	hosts        []string // 小写化后的 host，精确匹配
	repoPatterns []string
	minAge       time.Duration
	ignoreSevs   []string // 大写化后的严重级别
	postInstall  string   // deny | prompt | allow（v0.2 只记录，不执行——见 ADR-009）
	verifyOnLock bool
}

// FromConfig 从配置构造策略。
//
// 形状校验已在 config 层完成（ProjectFile.Validate → SupplyChainConfig.Validate）；
// 这里只做归一化与再校验，因此返回 error 表示**内部不一致**而非用户输入错误。
func FromConfig(sc *config.SupplyChainConfig) (*Policy, error) {
	p := &Policy{}
	if sc == nil {
		return p, nil
	}

	for _, h := range sc.AllowedGitHosts {
		if h == "" {
			return nil, fmt.Errorf("supplyChain.allowedGitHosts contains an empty host")
		}
		p.hosts = append(p.hosts, strings.ToLower(h))
	}
	p.repoPatterns = append(p.repoPatterns, sc.AllowlistRepos...)

	if sc.MinimumReleaseAge != "" {
		d, err := config.ParseISODuration(sc.MinimumReleaseAge)
		if err != nil {
			return nil, fmt.Errorf("supplyChain.minimumReleaseAge: %w", err)
		}
		p.minAge = d
	}

	for _, s := range sc.OSVIgnoreSeverities {
		p.ignoreSevs = append(p.ignoreSevs, strings.ToUpper(s))
	}

	p.postInstall = sc.PostInstallPolicy
	if p.postInstall == "" {
		p.postInstall = "deny" // 与文档一致：deny 是默认，也是 v0.2 的实际行为
	}
	if sc.VerifyOnLock != nil {
		p.verifyOnLock = *sc.VerifyOnLock
	}
	return p, nil
}

// IsEmpty 表示这份策略不构成任何门禁（白名单为空且没有晾晒期）。
func (p *Policy) IsEmpty() bool {
	return p == nil || (len(p.hosts) == 0 && len(p.repoPatterns) == 0 && p.minAge == 0)
}

// MinimumReleaseAge 返回晾晒期；0 表示未配置（不门禁）。
func (p *Policy) MinimumReleaseAge() time.Duration {
	if p == nil {
		return 0
	}
	return p.minAge
}

// VerifyOnLock 表示 install / update 后是否自动触发 verify。
func (p *Policy) VerifyOnLock() bool {
	return p != nil && p.verifyOnLock
}

// PostInstallPolicy 返回原始取值（deny / prompt / allow）。
func (p *Policy) PostInstallPolicy() string {
	if p == nil || p.postInstall == "" {
		return "deny"
	}
	return p.postInstall
}

// PostInstallIsActive 表示该策略在**当前版本**下是否真的会改变行为。
//
// v0.2 永远返回 false：ngm 在任何配置下都不执行依赖脚本（见 ADR-009）。
// 之所以提供这个方法而不是让调用方自行判断字符串，是为了让"prompt/allow 只记录不执行"
// 这件事在**代码里**也有唯一出口——需要渲染提示时直接读它，不必各处重述措辞。
func (p *Policy) PostInstallIsActive() bool {
	return false
}

// CheckRepo 判断 `host/org/repo` 是否被策略允许。
//
// host 为裸主机名（如 github.com），repoPath 为 `org/repo`（不含 host）。
// 被拒绝时返回 errs.CodeConfigInvalid（exit 3）——策略拒绝属于配置/策略错误，
// 与"网络失败"（4）、"完整性失败"（2）是不同性质，CI 需要能区分。
//
// 调用方负责附上**来源链**：本函数只知道"这个节点不合规"，
// 说不出"是谁把它引进来的"（见 ADR-009 第 3 条）。
func (p *Policy) CheckRepo(host, repoPath string) error {
	if p == nil {
		return nil
	}
	target := host + "/" + repoPath

	if len(p.hosts) > 0 {
		ok := false
		for _, h := range p.hosts {
			if strings.EqualFold(h, host) {
				ok = true
				break
			}
		}
		if !ok {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("git host %q is not in supplyChain.allowedGitHosts", host),
				fmt.Sprintf("allowed hosts: %s; add %q to supplyChain.allowedGitHosts in ngm.json if it is trusted",
					strings.Join(p.hosts, ", "), host))
		}
	}

	if len(p.repoPatterns) > 0 {
		ok := false
		for _, pat := range p.repoPatterns {
			if MatchRepoPattern(pat, target) {
				ok = true
				break
			}
		}
		if !ok {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("%s is not in supplyChain.allowlistRepos", target),
				fmt.Sprintf("allowed patterns: %s; add a matching entry (e.g. %s/*) if this repository is trusted",
					strings.Join(p.repoPatterns, ", "), host+"/"+strings.SplitN(repoPath, "/", 2)[0]))
		}
	}

	return nil
}
