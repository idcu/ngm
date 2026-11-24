package supplychain

import (
	"fmt"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
)

// DefaultMinimumReleaseAge 是 minimumReleaseAge 的**全局默认**晾晒期（ADR-053）。
// 对齐 pnpm 11+ 的 1440 分钟与 Deno 2.9 的 24h 默认；防的是"新版本投毒在数小时内被撤回"的窗口。
// 与 ADR-050 不同：现在完全没有 supplyChain 段（sc == nil）也套用这个默认值。
// 关闭方式必须是显式的 minimumReleaseAge: "PT0S"，见 FromConfig。
const DefaultMinimumReleaseAge = 24 * time.Hour

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
	// configured 表示项目是否**显式**写了 supplyChain 段。
	// 全局默认晾晒期与它无关（ADR-053）：IsEmpty() 只回答"有没有显式配置"，
	// 绝不能拿它判断是否执行门禁。
	configured bool
	// trustPolicy 为空或 "no-downgrade"（ADR-053）。
	// config 层暂无对应字段，由 WithTrustPolicy 显式开启；见 ADR-053 的接线事项。
	trustPolicy string
	// ageGateActive 表示 CLI 是否要**执行**晾晒期门禁。
	// ADR-053 把 24h 写进了 Policy 默认值，但 CLI 的强制执行切换需要先把既有 e2e
	// 夹具（刚提交的 commit）统一加上 PT0S / --allow-fresh；切换前只有显式写了
	// supplyChain 段的项目会被门禁。见 ADR-053「接线事项」。
	ageGateActive bool
}

// FromConfig 从配置构造策略。
//
// 形状校验已在 config 层完成（ProjectFile.Validate → SupplyChainConfig.Validate）；
// 这里只做归一化与再校验，因此返回 error 表示**内部不一致**而非用户输入错误。
// 例外：config 层目前拒绝 PT0S（ParseISODuration 的零时长语义），而 ADR-053
// 把 PT0S 定为**项目级显式关闭**；本函数特判它，config 层的同步改动见 ADR-053。
func FromConfig(sc *config.SupplyChainConfig) (*Policy, error) {
	// ADR-053：全局默认——无段、有段无字段，都是 24h。
	p := &Policy{minAge: DefaultMinimumReleaseAge}
	if sc == nil {
		return p, nil
	}
	p.configured = true

	for _, h := range sc.AllowedGitHosts {
		if h == "" {
			return nil, fmt.Errorf("supplyChain.allowedGitHosts contains an empty host")
		}
		p.hosts = append(p.hosts, strings.ToLower(h))
	}
	p.repoPatterns = append(p.repoPatterns, sc.AllowlistRepos...)

	if sc.MinimumReleaseAge != "" {
		// ADR-053：PT0S 是显式的"项目级关闭"——用户写清楚等于承认要放开晾晒期。
		// 不能放行 P0D：它不是合法时长（ParseISODuration 自 v0.1 起拒绝），
		// 放行它会让"配了 P0D 却没生效"的错觉复活（ADR-009 最反感的那种误读）。
		if sc.MinimumReleaseAge == "PT0S" {
			p.minAge = 0
		} else {
			d, err := config.ParseISODuration(sc.MinimumReleaseAge)
			if err != nil {
				return nil, fmt.Errorf("supplyChain.minimumReleaseAge: %w", err)
			}
			p.minAge = d
		}
	}

	// 有显式段且晾晒期 > 0 才执行门禁；无段时 24h 是 API 默认值（V211），
	// 但 CLI 强制执行要等夹具迁移（ADR-053 接线事项）。
	p.ageGateActive = p.minAge > 0

	for _, s := range sc.OSVIgnoreSeverities {
		p.ignoreSevs = append(p.ignoreSevs, strings.ToUpper(s))
	}

	p.postInstall = sc.PostInstallPolicy
	if p.postInstall == "" {
		p.postInstall = "deny" // 与文档一致：deny 是默认，也是 v0.2 的实际行为
	}
	// ADR-053：信任策略由配置驱动；WithTrustPolicy 仍保留给测试与显式副本。
	if sc.TrustPolicy != "" {
		p = p.WithTrustPolicy(sc.TrustPolicy)
	}
	if sc.VerifyOnLock != nil {
		p.verifyOnLock = *sc.VerifyOnLock
	}
	return p, nil
}

// IsEmpty 表示项目**没有显式写 supplyChain 段**（历史判据沿用此读法）。
//
// ADR-053 之后它不是"门禁是否执行"的判据：即使 IsEmpty() 为 true，
// 全局默认的 24h 晾晒期依然生效。判门禁请直接调用对应的 Check* 方法。
func (p *Policy) IsEmpty() bool {
	return p == nil || !p.configured
}

// MinimumReleaseAge 返回生效中的晾晒期；0 表示显式关闭（PT0S 或 --allow-fresh）。
// 无 supplyChain 段时返回全局默认 24h（ADR-053）。
func (p *Policy) MinimumReleaseAge() time.Duration {
	if p == nil {
		return 0
	}
	return p.minAge
}

// AgeGateActive 表示 CLI 当前是否要执行晾晒期门禁。
//
// 与 MinimumReleaseAge 分开的原因见 ADR-053：默认值已就位，强制执行切换依赖
// e2e 夹具先加 PT0S / --allow-fresh，否则所有用"刚提交的 commit"的夹具都会红。
func (p *Policy) AgeGateActive() bool {
	return p != nil && p.ageGateActive
}

// WithoutMinimumAge 返回一份**跳过晾晒期**的副本（其他门禁不变）——供 `--allow-fresh` 单命令逃生口。
// 刻意复制而非原地改：原策略可能被同一进程里别的命令再用，原地改会让"一次性逃生"泄漏出去。
func (p *Policy) WithoutMinimumAge() *Policy {
	if p == nil {
		return nil
	}
	q := *p
	q.minAge = 0 // 只关晾晒期；trustPolicy 等其他门禁一律保留
	q.ageGateActive = false
	return &q
}

// WithTrustPolicy 返回一份开启了指定信任策略的副本（当前只认 "no-downgrade"）。
//
// 用副本而非原地改，与 WithoutMinimumAge 同一理由：别让一次性的策略变更泄漏到
// 同一进程里别的命令。config 层尚无 trustPolicy 字段（见 ADR-053 的接线事项），
// 这个函数是 CLI / 测试显式开启门禁的入口；空值表示关闭。
func (p *Policy) WithTrustPolicy(v string) *Policy {
	if p == nil {
		return nil
	}
	q := *p
	q.trustPolicy = strings.ToLower(strings.TrimSpace(v))
	return &q
}

// TrustPolicy 返回当前信任策略取值（"" 或 "no-downgrade"）。
func (p *Policy) TrustPolicy() string {
	if p == nil {
		return ""
	}
	return p.trustPolicy
}

// NoDowngrade 是否开启"锚点不得降级"门禁。
func (p *Policy) NoDowngrade() bool {
	return p != nil && p.trustPolicy == "no-downgrade"
}

// VerifyOnLock 表示 install / update 后是否自动触发 verify。
func (p *Policy) VerifyOnLock() bool {
	return p != nil && p.verifyOnLock
}

// IgnoredSeverities 返回策略忽略的严重级别（osvIgnoreSeverities，已归一化为大写）。
//
// 由 audit 用来过滤结果。放在这里而不是让调用方去读 config，
// 是为了让"策略怎么说"只有一个出口——忽略名单将来若要加语义（例如按 CVE 豁免），
// 只改本包即可。
func (p *Policy) IgnoredSeverities() []string {
	if p == nil {
		return nil
	}
	return p.ignoreSevs
}

// PostInstallPolicy 返回原始取值（deny / prompt / allow）。
func (p *Policy) PostInstallPolicy() string {
	if p == nil || p.postInstall == "" {
		return "deny"
	}
	return p.postInstall
}

// PostInstallIsActive 表示该策略在**当前版本**下是否真的会执行依赖的钩子。
//
// v0.3 起（ADR-009 决策 5 修订）只有 `allow` 会执行，且仅在沙箱内、只执行 JS 钩子：
//
//   - `deny`（默认）：不执行。有钩子时输出会**明说**它们没有被执行——
//     一个"配了但没生效"的字段比没有字段更危险。
//   - `prompt`：不执行。ngm 是**非交互**工具（CI 里没有 TTY），"询问用户"没有真实形态；
//     因此它的含义是"别自动执行，把决定权留在配置里显式表达"。
//   - `allow`：执行。
//
// 提供这个方法而不是让调用方自行比较字符串，是为了让这套语义在**代码里**只有一个出口。
func (p *Policy) PostInstallIsActive() bool {
	return p != nil && p.postInstall == "allow"
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
