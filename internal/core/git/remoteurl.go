package git

import (
	"net/url"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// ValidateRemoteURL 只接受 ngm 认识的远端地址形状。
//
// 为什么需要它（C3）：`git clone <url>` 会把 URL 交给**远程 helper**，
// 而 `ext::sh -c ...` / `fd::` 这类 helper 能执行任意命令；URL 以 `-` 开头
// 时还会被 git 当成选项（可以注入 `-c core.sshCommand=...`）。ngm 只支持
// 常规协议与本地路径，其余形状在构造子进程之前就拒绝。
//
// 允许：https/http/ssh/git/file 的 URL、scp 形式（`git@host:org/repo.git`）、
// 以及本地路径（绝对/相对/UNC/盘符——测试与预置 mirror 都依赖它）。
func ValidateRemoteURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return errs.New(errs.CodeConfigInvalid, "empty remote URL",
			"check the dependency declaration")
	}
	if strings.HasPrefix(s, "-") {
		return errs.New(errs.CodeConfigInvalid,
			"refusing a remote URL that starts with '-': "+s,
			"the address is passed to `git clone` directly; an option-shaped value is not a repository")
	}
	// `ext::` / `fd::` / 任何 `helper::addr` 都是远程 helper。
	if !strings.Contains(s, "://") && strings.Contains(s, "::") {
		return errs.New(errs.CodeConfigInvalid,
			"refusing git remote helper address: "+s,
			"only https/ssh/git/file URLs, scp-style addresses, or local paths are supported")
	}
	if _, network := HostFromRemoteURL(s); network {
		if i := strings.Index(s, "://"); i > 0 {
			switch strings.ToLower(s[:i]) {
			case "https", "http", "ssh", "git", "file":
				return nil
			}
			return errs.New(errs.CodeConfigInvalid,
				"unsupported remote URL scheme: "+s,
				"use https/ssh/git, or a local path")
		}
		return nil // scp 形式
	}
	return nil // 本地路径
}

// HostFromRemoteURL 从远端 URL 里取出主机名。
//
// ok=false 表示这不是网络地址（本地路径 / file:// / 盘符），此时**不需要** net 权限。
//
// 这个区分不是细节：ngm 的 mirror 完全可以是本地路径（测试、CI 预置镜像、
// `ngm add /srv/git/lib.git` 这类用法）。若按"slug 的主机名"判定，
// 一次纯本地的 fetch 会被当成网络访问拦下，用户则被提示去加一条
// 与他的操作毫无关系的权限——那种提示比报错更糟，它会教人配置错误的东西。
func HostFromRemoteURL(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "file://") {
		return "", false
	}

	// 本地路径：绝对路径、相对路径、UNC、Windows 盘符
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, `\\`) {
		return "", false
	}
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		if c := s[0]; (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return "", false
		}
	}

	// 标准 URL：https://host/... / ssh://user@host/...
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Hostname(), true
	}

	// scp 形式：git@github.com:org/repo.git
	if i := strings.IndexByte(s, ':'); i > 0 {
		head := s[:i]
		if at := strings.LastIndexByte(head, '@'); at >= 0 {
			head = head[at+1:]
		}
		if head != "" && !strings.ContainsAny(head, `/\`) {
			return head, true
		}
	}
	return "", false
}

// CheckNetAccess 在访问某个远端 URL 之前判定 net 权限。
//
// 传 nil 策略时放行（调用方没加载到权限配置）——注意这与"策略判定为默认档位"
// 不同：这里管的是"没有策略可用"的情形，而它只在配置读取失败时出现。
func CheckNetAccess(pol Permissions, rawURL string) error {
	if pol == nil {
		return nil
	}
	host, ok := HostFromRemoteURL(rawURL)
	if !ok {
		return nil
	}
	return pol.CheckNet(host)
}

// CheckRunAccess 判定能否执行某个可执行文件。
//
// 存在的理由是**顺序**：判定"去哪"有时本身要先跑 git（读 mirror 的 origin URL），
// 若先判 net，一个 run:git 被拒的配置会得到"net 权限不足"的报错——
// 用户于是去加一条没用的网络权限，而真正的原因始终没被说出来。
func CheckRunAccess(pol Permissions, exe string) error {
	if pol == nil {
		return nil
	}
	return pol.CheckRun(exe)
}
