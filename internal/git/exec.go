// Package git 封装 ngm 需要的 Git 操作：ls-remote、clone/fetch（mirror）、archive、认证透传。
//
// 认证纪律（architecture/security-model.md §"token 与凭证管理"）：
//   - ngm 不主动读取凭证；凭证由 git 自身的 ssh-agent / credential helper 处理
//   - 环境变量（GITHUB_TOKEN 等）原样透传给 git 子进程
//   - token 不得进入命令行参数、输出、日志、lock 与 ngm.json
//
// 本文件是命令执行与输出脱敏的基础设施；lsremote.go / mirror.go 建立其上。
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// NotInstalledHint 在 git 不在 PATH 时给出的统一建议。
const NotInstalledHint = "install Git (>= 2.30) and ensure it is on PATH, then re-run"

// Options 控制一次 git 子进程调用。
type Options struct {
	// Dir 是子进程工作目录；空表示继承当前进程的 cwd。
	Dir string
	// ExtraEnv 追加到 os.Environ() 之后（后者优先，与 exec.Cmd 语义一致）。
	ExtraEnv []string
	// AllowPrompt 为 true 时保留 git 的交互式提示（默认 false）。
	// CI 与自动化场景必须为 false，否则会在无 tty 时挂起或失败。
	AllowPrompt bool
	// Secrets 是需要在错误输出中被替换为 "***" 的字面量集合。
	// 由调用方传入（通常来自 tokenEnvVars 的实际值）；本包不主动读取环境变量。
	Secrets []string
	// Policy 是权限判定器（可为 nil，表示不施加权限）。
	//
	// 它在这里做两件事，覆盖**全部** git 子进程：
	//   - `run:git`：被拒绝时根本不启动 git
	//   - `env:<NAME>`：把被拒绝的环境变量从子进程环境里剔除
	//     （不是读取它的值——ngm 依旧不解析 token）
	Policy Permissions
}

// Permissions 是 git 需要的权限判定能力。
//
// 用接口而不是直接依赖 internal/security：本包只需要这两个方法，
// 测试也能给出一个最小实现，不必构造完整的策略对象。
type Permissions interface {
	CheckRun(exe string) error
	CheckNet(host string) error
	DeniedEnvVars() []string
}

// Result 是一次 git 调用的结果。
type Result struct {
	Stdout []byte
	Stderr []byte
	// ExitCode 为进程退出码；启动失败时为 -1。
	ExitCode int
}

// Run 执行 `git <args...>` 并返回结果。
//
// 返回值约定：
//   - 启动失败（git 不存在）→ *errs.NgmError(CodeEngineNotFound)? 不——git 属于网络/操作依赖，
//     按 observability.md 归类为 Git/网络失败（exit 4），Hint 提示安装 Git。
//   - 非零退出 → *errs.NgmError(CodeGitFetch)，Message 含脱敏后的 stderr 片段
//
// 调用方若需要把特定的非零退出解释为"ref 不存在"等语义，应自行检查 Result.ExitCode——
// 本函数在非零退出时总是返回 error，以便调用链不会忽略失败。
func Run(ctx context.Context, opts Options, args ...string) (*Result, error) {
	// 权限门禁在**启动之前**：拒绝 `run:git` 时不该留下任何副作用，
	// 也不该让用户从"git 失败了"去猜"其实是我的配置不允许执行它"。
	if opts.Policy != nil {
		if err := opts.Policy.CheckRun("git"); err != nil {
			return &Result{ExitCode: -1}, err
		}
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	cmd.Env = buildEnv(opts)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	noteSpawn()
	err := cmd.Run()
	res := &Result{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: 0,
	}

	if err == nil {
		return res, nil
	}

	// 启动类错误（git 不存在或不可执行）
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return res, errs.Wrap(errs.CodeGitFetch,
			"failed to run git", NotInstalledHint, err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return res, errs.Wrap(errs.CodeGitFetch,
			"failed to run git", NotInstalledHint, err)
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, errs.New(errs.CodeGitFetch,
			fmt.Sprintf("git %s failed (exit %d): %s",
				redact(strings.Join(args, " "), opts.Secrets),
				res.ExitCode,
				redact(trimStderr(res.Stderr), opts.Secrets)),
			hintForGitFailure(res.Stderr))
	}

	return res, errs.Wrap(errs.CodeGitFetch, "git invocation failed", "", err)
}

// repoLocatingEnvVars 是会把 git 从 `Options.Dir` 重定向到别处的环境变量。
//
// 为什么必须剔除：ngm 始终**显式**决定在哪个仓库里操作（mirror 的裸仓库、
// 或者根本不进仓库的 `ls-remote`）。如果用户的 shell 里残留了 GIT_DIR
// （钩子脚本、`git worktree` 会话、复杂 CI 常见），`git fetch` 会去改**另一个**
// 仓库——表现为 mirror 静默失效或数据错乱。ngm 不接受这种隐式重定向。
var repoLocatingEnvVars = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR":                   true,
	"GIT_NAMESPACE":                    true,
	"GIT_PREFIX":                       true,
}

// buildEnv 构造子进程环境。
//
// 关键点：
//   - 继承 os.Environ()（凭证由 git 自己通过 ssh-agent / credential helper /
//     GITHUB_TOKEN 等处理，ngm 不解析、不落盘）
//   - 剔除 repoLocatingEnvVars，保证 ngm 对"在哪个仓库操作"有完全控制权
//   - 默认注入 GIT_TERMINAL_PROMPT=0，避免无 tty 时挂起
//   - 强制 LC_ALL=C / LANG=C，让 git 错误信息稳定为英文，便于测试与 Hint 匹配
func buildEnv(opts Options) []string {
	// 被 `deny: ["env:X"]` 拒绝的变量从这里**剔除**。
	//
	// 剔除而不是"读取后置空"：ngm 至今不解析 token 的内容，这条性质不能因为
	// 权限功能而破掉。用户 deny 掉 GITHUB_TOKEN 之后，git 就是收不到它——
	// 于是一次需要认证的 fetch 会失败，而失败原因由 git 自己给出。
	var deniedEnv map[string]bool
	if opts.Policy != nil {
		for _, name := range opts.Policy.DeniedEnvVars() {
			if deniedEnv == nil {
				deniedEnv = map[string]bool{}
			}
			deniedEnv[strings.ToUpper(name)] = true
		}
	}

	base := os.Environ()
	env := make([]string, 0, len(base)+len(opts.ExtraEnv)+3)
	for _, e := range base {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		upper := strings.ToUpper(name)
		// Windows 环境变量名不区分大小写
		if repoLocatingEnvVars[upper] || deniedEnv[upper] {
			continue
		}
		env = append(env, e)
	}
	if !opts.AllowPrompt {
		env = append(env, "GIT_TERMINAL_PROMPT=0")
	}
	env = append(env, "LC_ALL=C", "LANG=C")
	env = append(env, opts.ExtraEnv...)
	return env
}

// hintForGitFailure 依据 stderr 内容给出可操作建议。
//
// 覆盖 architecture/security-model.md 要求的"错误 Hint 必须指向具体凭证配置方式"。
func hintForGitFailure(stderr []byte) string {
	s := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(s, "could not read username"),
		strings.Contains(s, "authentication failed"),
		strings.Contains(s, "permission denied"),
		strings.Contains(s, "access denied"),
		strings.Contains(s, "invalid username or password"):
		return "configure git credentials: use ssh-agent, `git credential` helper, " +
			"or export the host token env var (e.g. GITHUB_TOKEN). " +
			"ngm never reads or stores tokens itself"
	case strings.Contains(s, "could not resolve host"),
		strings.Contains(s, "unable to access"),
		strings.Contains(s, "connection refused"),
		strings.Contains(s, "timed out"),
		strings.Contains(s, "network is unreachable"):
		return "check network connectivity and the git host; use `--offline` only with a warm local mirror"
	case strings.Contains(s, "repository not found"),
		strings.Contains(s, "does not exist"),
		strings.Contains(s, "not a git repository"):
		return "verify the repository URL and that your account has access"
	case strings.Contains(s, "host key verification failed"):
		return "add the host to ~/.ssh/known_hosts (or `ssh-keyscan`), then re-run"
	default:
		return "run the failing git command manually to inspect the cause"
	}
}

// redact 把 secrets 中的字面量替换为 "***"，并脱敏 URL userinfo。
//
// 用于避免 token 通过错误信息 / 日志泄露（security-model §"token 与凭证管理"）。
func redact(s string, secrets []string) string {
	out := redactUserInfo(s)
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		out = strings.ReplaceAll(out, sec, "***")
	}
	return out
}

// userInfoRe 匹配 `<scheme>://user[:pass]@host` 中的 userinfo 部分。
var userInfoRe = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s]+@`)

// redactUserInfo 把 URL 中的 userinfo 替换为 `***@`。
//
// 例：`https://x-access-token:ghp_secret@github.com/o/r` → `https://***@github.com/o/r`
func redactUserInfo(s string) string {
	return userInfoRe.ReplaceAllString(s, "${1}***@")
}

// Redact 是 redact 的导出包装，供其他包（如 mirror）复用同一纪律。
func Redact(s string, secrets []string) string { return redact(s, secrets) }

// trimStderr 把 stderr 压缩为单行、截断到合理长度，便于放进错误消息。
func trimStderr(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "(no stderr)"
	}
	// 多行合并为 " | "，保留前若干行
	lines := strings.Split(s, "\n")
	const maxLines = 5
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "...(truncated)")
	}
	joined := strings.Join(lines, " | ")
	const maxLen = 1024
	if len(joined) > maxLen {
		joined = joined[:maxLen] + "...(truncated)"
	}
	return joined
}

// LookPath 报告 git 是否可用（供子命令在早期给出清晰错误）。
func LookPath() (string, error) {
	return exec.LookPath("git")
}
