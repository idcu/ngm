package resolve

import "strings"

// Protocol 是访问远端 Git 仓库时使用的传输协议。
//
// 默认值来自全局配置 `~/.ngm/config.json` 的 `git.defaultProtocol`（见
// guides/configuration.md）；缺省为 ssh。
type Protocol string

const (
	// ProtocolSSH 走 SSH（scp-like 形式 `git@host:org/repo.git`），
	// 认证由 ssh-agent / ~/.ssh/config 处理，ngm 不碰 token。
	ProtocolSSH Protocol = "ssh"
	// ProtocolHTTPS 走 HTTPS（`https://host/org/repo.git`），
	// 认证由 git credential helper / 环境变量 token 处理。
	ProtocolHTTPS Protocol = "https"
)

// IsValid 报告 p 是否为受支持的协议。
func (p Protocol) IsValid() bool {
	return p == ProtocolSSH || p == ProtocolHTTPS
}

// ParseProtocol 解析配置中的协议字符串；空值回退到 fallback（若 fallback 也非法则为 ssh）。
func ParseProtocol(s string, fallback Protocol) Protocol {
	switch Protocol(strings.ToLower(strings.TrimSpace(s))) {
	case ProtocolSSH:
		return ProtocolSSH
	case ProtocolHTTPS:
		return ProtocolHTTPS
	default:
		if fallback.IsValid() {
			return fallback
		}
		return ProtocolSSH
	}
}

// DefaultSSHUser 是 SSH 协议下的默认用户名。
//
// 绝大多数 Git host 使用 `git`；自建服务若不同，可由调用方通过 CloneURLWithUser 覆盖。
const DefaultSSHUser = "git"

// CloneURL 返回用于 `git clone` / `git ls-remote` 的可访问 URL。
//
// 关键区别：Canonical.String() 是**标识符**（`github.com/org/repo`），
// 不是可 clone 的地址；本方法把它转换为真实 URL：
//
//	ProtocolSSH   → git@github.com:org/repo.git
//	ProtocolHTTPS → https://github.com/org/repo.git
//
// 安全：绝不把 token 写入 URL（security-model §"token 与凭证管理"）。
// 需要凭证时依赖 git 自身的 credential helper 或环境变量透传。
func (c Canonical) CloneURL(p Protocol) string {
	return c.CloneURLWithUser(p, DefaultSSHUser)
}

// CloneURLWithUser 与 CloneURL 相同，但允许覆盖 SSH 用户名。
func (c Canonical) CloneURLWithUser(p Protocol, sshUser string) string {
	if p == ProtocolHTTPS {
		return "https://" + c.Host + "/" + c.Path + ".git"
	}
	user := sshUser
	if user == "" {
		user = DefaultSSHUser
	}
	return user + "@" + c.Host + ":" + c.Path + ".git"
}

// HTTPSURL 返回 HTTPS 形式的 URL（不含 .git 后缀的变体见 HTTPSURLWithoutSuffix）。
func (c Canonical) HTTPSURL() string {
	return "https://" + c.Host + "/" + c.Path + ".git"
}
