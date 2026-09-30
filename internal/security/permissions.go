// Package security 实现 ngm 的权限模型（default-deny）。
//
// 模型来源：[安全模型](../../docs/architecture/security-model.md) 的权限表——
// 五个命名空间（read / write / net / run / env）各有**未被配置提及时**的默认档位，
// 三档分别是"允许""需配置""默认禁止"。
//
// 为什么要有它：v0.1 起 `~/.ngm/config.json` 的 `permissions` 段就存在，
// 但**没有任何地方施加它**——写下 `"deny": ["env:GITHUB_TOKEN"]` 不会阻止任何事。
// 一个只被解析、不被执行的权限配置比没有更糟：它让用户以为自己有了防线。
//
// 本包只做判定，不做施加：施加点由调用方决定（git 子进程环境、网络抓取、引擎派生），
// 这样"哪里有门禁"可以在调用点一眼看全，而不是藏在一个看似无害的检查函数里。
package security

import (
	"fmt"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// Namespace 是权限命名空间。
type Namespace string

const (
	// Read 是读取某路径。
	Read Namespace = "read"
	// Write 是写入某路径。
	Write Namespace = "write"
	// Net 是访问某个主机。
	Net Namespace = "net"
	// Run 是执行某个可执行文件。
	Run Namespace = "run"
	// Env 是把某个环境变量交给子进程（ngm 自己不解析其值）。
	Env Namespace = "env"
)

// Namespaces 返回全部命名空间（用于校验与帮助文本）。
func Namespaces() []Namespace {
	return []Namespace{Read, Write, Net, Run, Env}
}

// Permission 是一条权限：命名空间 + 目标。
//
// 目标按命名空间解释：路径（read/write）、主机名（net）、可执行文件名（run）、
// 环境变量名（env）。匹配是**精确的**——`net:github.com` 不等于 `net:api.github.com`。
//
// 刻意不做通配：通配需要一套自己的匹配语义（`*` 跨不跨 `.`？`a.*` 包不包含 `a`？），
// 而那套语义一旦写错，用户会以为某个 host 被允许而实际上没有。精确匹配易于解释、
// 易于测试，配置里多写几行比"说不清的允许范围"划算。
type Permission struct {
	Namespace Namespace
	Target    string
}

// String 返回配置里书写的形式（`net:github.com`）。
func (p Permission) String() string {
	if p.Target == "" {
		return string(p.Namespace)
	}
	return string(p.Namespace) + ":" + p.Target
}

// Parse 解析 `namespace:target` 形式。
//
// 未知命名空间是配置错误而不是"忽略掉"——静默忽略会让用户以为权限生效了。
func Parse(s string) (Permission, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return Permission{}, errs.New(errs.CodeConfigInvalid,
			"empty permission", "expected `namespace:target`, e.g. `net:github.com`")
	}

	ns, target, found := strings.Cut(raw, ":")
	ns = strings.ToLower(strings.TrimSpace(ns))
	target = strings.TrimSpace(target)

	known := false
	for _, n := range Namespaces() {
		if string(n) == ns {
			known = true
			break
		}
	}
	if !known {
		return Permission{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unknown permission namespace %q", ns),
			"supported namespaces: "+namespaceList())
	}
	if !found || target == "" {
		return Permission{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("permission %q has no target", raw),
			"write it as `"+ns+":<value>`, e.g. `"+exampleFor(Namespace(ns))+"`")
	}
	return Permission{Namespace: Namespace(ns), Target: target}, nil
}

// Tier 是某条权限**未被配置提及时**的档位。
type Tier int

const (
	// TierAllowed 允许：未配置也放行。
	TierAllowed Tier = iota
	// TierConfigurable 需配置：未配置即拒绝，提示把它加进 allow。
	TierConfigurable
	// TierForbidden 默认禁止：未配置即拒绝，且提示这是刻意的默认值。
	//
	// 与 TierConfigurable 的**行为完全相同**（都可以通过 allow 开启），
	// 差别只在提示语：一个是"你还没开"，一个是"默认关着是有原因的"。
	// 把两者做成不同行为会发明出一档"允许不了"的权限，而那与 ADR-009 的
	// postInstallPolicy 设计冲突（用户配置后应当能开启）。
	TierForbidden
)

func (t Tier) String() string {
	switch t {
	case TierAllowed:
		return "allowed"
	case TierConfigurable:
		return "needs configuration"
	default:
		return "denied by default"
	}
}

// DefaultTier 返回某条权限在未被配置提及时的档位。
//
// 取值依据 security-model.md 的权限表；两处需要解释：
//
//   - `env:` 默认**允许**透传。ngm 不读取、不解析 token（v0.1 的既有保证），
//     它只是把环境交给 git——默认拦下所有 token 会让认证在无人配置的情况下直接坏掉。
//     这条权限的用途是**收窄**（`deny: ["env:GITHUB_TOKEN"]`），不是默认开启。
//   - `read:` / `write:` 默认允许：读自己的项目、写 vendor 与 lock 本就是 ngm 的职能。
//     判定已实现，施加点是写入路径与（C 组的）沙箱。
func DefaultTier(p Permission) Tier {
	switch p.Namespace {
	case Read, Write, Env:
		return TierAllowed
	case Net:
		// 表格写的是 `net:github.com,gitee.com` 需配置——即"任何主机都要显式授权"。
		return TierConfigurable
	case Run:
		if p.Target == "git" {
			// git 是 ngm 的基本工具（mirror / cat-file / ls-remote 全靠它）
			return TierAllowed
		}
		return TierConfigurable
	default:
		return TierForbidden
	}
}

// Policy 是合并后的权限判定器。
//
// 判定顺序：**deny 优先于 allow**。同一条权限同时出现在两边时以 deny 为准——
// 这是唯一不会让"我明明禁了"落空的选择。
type Policy struct {
	allow map[string]bool
	deny  map[string]bool
	// source 是配置来源（用于错误提示里指出该改哪个文件）。
	source string
}

// NewPolicy 解析 allow / deny 列表。
//
// source 是人类可读的配置来源（如 `~/.ngm/config.json`），只用于提示。
func NewPolicy(allow, deny []string, source string) (*Policy, error) {
	pol := &Policy{
		allow:  make(map[string]bool, len(allow)),
		deny:   make(map[string]bool, len(deny)),
		source: source,
	}
	for _, list := range []struct {
		values []string
		into   map[string]bool
	}{{allow, pol.allow}, {deny, pol.deny}} {
		for _, raw := range list.values {
			p, err := Parse(raw)
			if err != nil {
				return nil, err
			}
			list.into[p.String()] = true
		}
	}
	return pol, nil
}

// Source 返回配置来源（用于提示）。
func (pol *Policy) Source() string {
	if pol == nil || pol.source == "" {
		return "~/.ngm/config.json"
	}
	return pol.source
}

// Check 判定一条权限，通过时返回 nil。
//
// 失败用 exit 3（配置类）：这不是运行时故障，而是"配置没写"——
// 提示里给出可以直接粘贴的那一行，用户改完重跑即可。
func (pol *Policy) Check(p Permission) error {
	if pol.Allows(p) {
		return nil
	}
	key := p.String()
	hint := "add " + quote(key) + " to `permissions.allow` in " + pol.Source()

	switch {
	case pol.isDenied(p):
		hint = "it is denied by `permissions.deny` in " + pol.Source() +
			"; remove " + quote(key) + " from that list to allow it"
	case DefaultTier(p) == TierForbidden:
		hint = hint + " (it is refused by default on purpose; enable it only if you know why)"
	default:
		hint = hint + " (a permission that is neither allowed nor denied is refused)"
	}

	return errs.New(errs.CodeConfigInvalid, "permission denied: "+key, hint)
}

// CheckRun 判定"执行某个可执行文件"（`run:<exe>`）。
//
// 这些一行包装不是装饰：调用点写 `pol.CheckRun("esbuild")` 比
// `pol.Check(Permission{Namespace: Run, Target: "esbuild"})` 更不容易写错命名空间，
// 而写错命名空间会得到一个静默放行的判定。
func (pol *Policy) CheckRun(exe string) error {
	return pol.Check(Permission{Namespace: Run, Target: exe})
}

// CheckNet 判定"访问某个主机"（`net:<host>`）。
func (pol *Policy) CheckNet(host string) error {
	return pol.Check(Permission{Namespace: Net, Target: host})
}

// CheckWrite 判定"写入某个路径"（`write:<path>`）。
func (pol *Policy) CheckWrite(path string) error {
	return pol.Check(Permission{Namespace: Write, Target: path})
}

// Allows 报告某条权限是否放行。
func (pol *Policy) Allows(p Permission) bool {
	if pol == nil {
		// 没有策略（例如未加载全局配置）时按默认档位判定，
		// 而不是"一律放行"——否则加载失败会变成绕过门禁的通道。
		return DefaultTier(p) == TierAllowed
	}
	if pol.isDenied(p) {
		return false
	}
	if pol.allow[p.String()] {
		return true
	}
	return DefaultTier(p) == TierAllowed
}

func (pol *Policy) isDenied(p Permission) bool { return pol != nil && pol.deny[p.String()] }

// DeniedEnvVars 返回被拒绝交给子进程的环境变量名。
//
// 施加方式**不是**在 ngm 里读取它们的值，而是从子进程环境里**剔除**这些变量：
// 与 v0.1 的"只透传、不解析"一致——ngm 依旧不碰 token 的内容。
func (pol *Policy) DeniedEnvVars() []string {
	if pol == nil {
		return nil
	}
	out := make([]string, 0, len(pol.deny))
	for key := range pol.deny {
		p, err := Parse(key)
		if err != nil || p.Namespace != Env {
			continue
		}
		out = append(out, p.Target)
	}
	sort.Strings(out)
	return out
}

// Conflicts 返回同时出现在 allow 与 deny 里的权限（deny 生效）。
//
// 供 `ngm config validate` 提示：这种配置多半是误写，而它的表现是
// "我明明允许了却还是被拒"——把话说在前面比让用户自己排查划算。
func (pol *Policy) Conflicts() []string {
	if pol == nil {
		return nil
	}
	var out []string
	for key := range pol.deny {
		if pol.allow[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func namespaceList() string {
	names := make([]string, 0, len(Namespaces()))
	for _, n := range Namespaces() {
		names = append(names, string(n))
	}
	return strings.Join(names, ", ")
}

func exampleFor(ns Namespace) string {
	switch ns {
	case Net:
		return "net:github.com"
	case Run:
		return "run:esbuild"
	case Env:
		return "env:GITHUB_TOKEN"
	case Read:
		return "read:ngm.vendor"
	default:
		return "write:ngm.vendor"
	}
}

func quote(s string) string { return "\"" + s + "\"" }
