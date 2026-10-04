// Package resolve 实现依赖解析：URL 归一化、refType 解析、依赖图与冲突检测。
//
// 本文件只负责第 1 步（Git URL 归一化）；refType 解析与依赖图分别在 reftype.go / graph.go。
//
// 规范唯一事实源：architecture/dependency-resolution.md §1（4 种协议表格）。
package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// Canonical 是归一化后的 Git 仓库地址：`<host>/<path>`。
//
// 不变式（所有构造路径必须保证，Normalize 是两个合法构造入口之一）：
//   - Host 为小写、不含协议 / userinfo / 端口 / 尾部斜杠
//   - Path 已去掉前导 / 尾随 `/`、已去掉尾部 `.git`、不含空段
//   - String() 的输出再次送入 Normalize 得到等价值（幂等）
//
// 术语对照：
//   - canonical（本类型）：Git 操作与 mirror 路径使用，如 `github.com/org/repo`
//   - slug：ngm.json / ngm.lock / CLI 输出使用，如 `github:org/repo`（见 Slug）
//
// 两者关系由 slugHosts / hostSlugs 双向映射定义，且必须保持同步。
type Canonical struct {
	Host string
	Path string
}

// String 返回 canonical 形式：`github.com/org/repo`。
//
// 对齐 architecture/dependency-resolution.md §1 的"canonical 形式"列。
func (c Canonical) String() string {
	return c.Host + "/" + c.Path
}

// Slug 返回依赖标识形式：`github:org/repo`。
//
// 与 String() 的区别：GitHub / Gitee / GitLab 三个已知 host 折叠为短名；
// 其他 host（如自建 gitlab.example.com）保留完整 host。
//
// 对齐 guides/configuration.md 的 `"name": "github:my-org/utils"` 与
// architecture/observability.md 的 `github:org/utils@v1.2.3` 输出。
func (c Canonical) Slug() string {
	return slugHost(c.Host) + ":" + c.Path
}

// IsZero 报告 Canonical 是否为零值（便于调用方做空值检查）。
func (c Canonical) IsZero() bool { return c.Host == "" && c.Path == "" }

// Equal 比较两个 Canonical 是否指向同一仓库。
//
// 注意：Host 已保证小写；Path 区分大小写（Git 路径大小写敏感）。
func (c Canonical) Equal(o Canonical) bool {
	return c.Host == o.Host && c.Path == o.Path
}

// MirrorRelPath 返回该仓库在 mirror 层下的相对路径（不含 .git 后缀）。
//
// 布局：`<host>/<org>/<repo>`（见 M1.3 的 mirror 层 `~/.ngm/mirror/`）。
// 返回的路径一定是斜杠分隔（不随 Windows 变 \\）——由调用方用 filepath.FromSlash 转换。
func (c Canonical) MirrorRelPath() string {
	return c.Host + "/" + c.Path
}

// slugHosts：canonical host → slug 短名。
//
// 仅收录已知的公共托管服务；其他 host 保持完整。新增 host 时必须同步 hostSlugs。
var slugHosts = map[string]string{
	"github.com": "github",
	"gitee.com":  "gitee",
	"gitlab.com": "gitlab",
}

// hostSlugs：shorthand 短名 → canonical host（Normalize 用）。
var hostSlugs = map[string]string{
	"github": "github.com",
	"gitee":  "gitee.com",
	"gitlab": "gitlab.com",
}

// supportedSchemes 是 URL 形式下允许的协议。
//
// 与 dependency-resolution §1 的四种输入形式对应：
//   - https → 输入形式 1
//   - ssh   → 输入形式 2 的 url-like 变体（git@host:path 是 scp-like，见 splitSCPLike）
//   - git   → 输入形式 3
//   - http  → 未在 §1 列出，但作为 https 的镜像/内网场景被显式支持
var supportedSchemes = map[string]bool{
	"https": true,
	"http":  true,
	"git":   true,
	"ssh":   true,
}

func slugHost(host string) string {
	if s, ok := slugHosts[host]; ok {
		return s
	}
	return host
}

// Normalize 把任意受支持的 Git 地址形式归一化为 Canonical。
//
// 支持形式（与 architecture/dependency-resolution.md §1 对齐，另含 idempotent 保证所需的形式）：
//
//	https://github.com/org/repo.git      → github.com/org/repo
//	http://github.com/org/repo           → github.com/org/repo
//	git@github.com:org/repo.git          → github.com/org/repo
//	ssh://git@github.com:22/org/repo.git → github.com/org/repo
//	git://github.com/org/repo.git        → github.com/org/repo
//	github:org/repo                      → github.com/org/repo   (shorthand)
//	gitee:org/repo                       → gitee.com/org/repo
//	gitlab:group/subgroup/repo           → gitlab.com/group/subgroup/repo
//	github.com:org/repo                  → github.com/org/repo   (显式 host + 冒号)
//	github.com/org/repo                  → github.com/org/repo   (canonical 幂等)
//
// 所有失败都返回 *errs.NgmError（Code = CodeConfigInvalid, 退出码 3），Hint 给出可操作示例。
func Normalize(input string) (Canonical, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return Canonical{}, invalidURL(input,
			"provide a git address, e.g. `github:org/repo` or `https://github.com/org/repo.git`")
	}

	// nil 字节 / 控制字符破坏后续 git 命令参数，必须前置拒绝。
	if hasControlChar(raw) {
		return Canonical{}, invalidURL(input, "remove control characters from the git address")
	}

	host, path, err := splitHostPath(raw)
	if err != nil {
		return Canonical{}, err
	}
	host = strings.ToLower(host)

	host = expandShorthand(host)
	if host == "" {
		return Canonical{}, invalidURL(input, "host is empty; e.g. `github:org/repo`")
	}
	path = cleanRepoPath(path)
	if path == "" {
		return Canonical{}, invalidURL(input,
			"repository path is empty; expected `<host>/<org>/<repo>`")
	}
	if err := validateHost(host); err != nil {
		return Canonical{}, err
	}
	if err := validateRepoPath(input, path); err != nil {
		return Canonical{}, err
	}

	return Canonical{Host: host, Path: path}, nil
}

// MustNormalize 是 Normalize 的 panic 版本，仅用于测试与常量初始化。
func MustNormalize(input string) Canonical {
	c, err := Normalize(input)
	if err != nil {
		panic(err)
	}
	return c
}

// splitHostPath 把原始输入拆成 (host, path)。
//
// 判定顺序（避免多种形式互相误判）：
//  1. 含 `://` → URL 形式
//  2. 含 `@` → scp-like（user@host:path）
//  3. 含 `:` → shorthand（host:path；host 可为 `github` 或 `github.com`）
//  4. 含 `/` 且首个 `/` 前含 `.` → 裸 canonical（host/path，用于幂等）
//
// 其他情况报错。
func splitHostPath(raw string) (host, path string, err error) {
	// 反斜杠在 Git 地址中始终是分隔符的误写（Windows 路径习惯）；
	// 先统一为 `/`，后续所有判定都基于正斜杠。
	raw = strings.ReplaceAll(raw, "\\", "/")

	// 1) URL 形式
	if i := strings.Index(raw, "://"); i > 0 {
		return splitURLForm(raw, i)
	}

	// 2) scp-like：user@host:path
	if strings.Contains(raw, "@") {
		return splitSCPLike(raw)
	}

	// 3) shorthand / 显式 host + 冒号：host:path
	if i := strings.Index(raw, ":"); i > 0 {
		host = raw[:i]
		path = raw[i+1:]
		// 单独的端口形式 `host.com:8080/x` 会被误判成 host=host.com, path=8080/x。
		// 这里保守：host 段不含 `.` 时视为 shorthand（由 expandShorthand 校验）；
		// 含 `.` 时也接受（github.com:org/repo）。
		return host, path, nil
	}

	// 4) 裸 canonical（含点 host + `/`），保证 Normalize 幂等
	if i := strings.Index(raw, "/"); i > 0 {
		candidateHost := raw[:i]
		if strings.Contains(candidateHost, ".") {
			return candidateHost, raw[i+1:], nil
		}
	}

	return "", "", invalidURL(raw,
		"unsupported git address form; use one of: `https://host/org/repo`, `git@host:org/repo`, `git://host/org/repo`, `github:org/repo`")
}

// splitURLForm 处理含 `://` 的输入；colonIdx 是 `://` 中 `:` 的下标。
func splitURLForm(raw string, colonIdx int) (host, path string, err error) {
	scheme := strings.ToLower(raw[:colonIdx])
	if !supportedSchemes[scheme] {
		return "", "", invalidURL(raw,
			fmt.Sprintf("unsupported scheme %q; supported: https, http, git, ssh", scheme))
	}
	rest := raw[colonIdx+3:]

	// 去掉 userinfo（user@ 或 user:pass@）
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	// 现在 rest = host[:port]/path
	slash := strings.Index(rest, "/")
	if slash <= 0 {
		return "", "", invalidURL(raw, "missing repository path after host")
	}
	hostPart := rest[:slash]
	path = rest[slash+1:]
	if strings.Trim(path, "/") == "" {
		return "", "", invalidURL(raw, "missing repository path after host")
	}
	// 端口：Canonical 只承载 host + path（lock / mirror 路径 / slug 的共同基础），
	// 因此 ngm 无法把端口带到任何下游。
	//
	// **默认端口**与不写端口语义完全相同，可以安全丢弃；
	// **非默认端口**此前被同一段代码静默剥掉——后果是 ngm 去连默认端口上的另一个服务，
	// 而报错说"检查网络连通性"（CONNECT 隧道 502），把用户引向错误的方向
	// （v0.11 C 组 D1 实测）。这里改为**如实拒绝**：一个名字里带端口的地址，
	// ngm 要么完整地用它、要么明确说不支持，绝不悄悄换一个地址去连。
	//
	// 为什么不是"顺手支持"：端口要进 Canonical，就要同时定义它在 lock、mirror 目录
	// （Windows 文件名不允许 `:`）与 slug 里的形状 —— 那是 schema 级变更，
	// 按 metrics「已知限制」的口径，先有真实需求再做。
	if c := strings.LastIndex(hostPart, ":"); c >= 0 {
		host, port := hostPart[:c], hostPart[c+1:]
		if port == "" {
			return "", "", invalidURL(raw, "missing port number after `:` in "+hostPart)
		}
		if def, ok := defaultSchemePorts[scheme]; ok && port == def {
			hostPart = host
		} else {
			return "", "", invalidURL(raw, fmt.Sprintf(
				"explicit port %q is not supported: ngm identifies a repository by host and path only, "+
					"so it cannot carry a non-default port into the lock file, the mirror layout or the slug",
				hostPart))
		}
	}
	return hostPart, path, nil
}

// defaultSchemePorts 是各受支持 scheme 的默认端口。
//
// 只有**与默认值相同**的显式端口可以被丢弃——那与不写端口是同一件事。
// 其它端口一律由 splitURLForm 明确拒绝。
var defaultSchemePorts = map[string]string{
	"https": "443",
	"http":  "80",
	"git":   "9418",
	"ssh":   "22",
}

// splitSCPLike 处理 `[user@]host:path`（scp 语法，无 `://`）。
func splitSCPLike(raw string) (host, path string, err error) {
	at := strings.LastIndex(raw, "@")
	after := raw[at+1:]
	colon := strings.Index(after, ":")
	if colon <= 0 {
		return "", "", invalidURL(raw,
			"scp-like address must be `user@host:org/repo`")
	}
	host = after[:colon]
	path = after[colon+1:]
	if host == "" {
		return "", "", invalidURL(raw, "scp-like address has empty host")
	}
	return host, path, nil
}

// expandShorthand 把 shorthand host 展开为 canonical host。
//
// 规则：
//   - 命中 hostSlugs（github/gitee/gitlab）→ 展开为 .com 域名
//   - 含 `.`（已是域名）→ 原样返回
//   - 其他 → 原样返回（由 validateHost 决定是否拒绝）
func expandShorthand(host string) string {
	if full, ok := hostSlugs[host]; ok {
		return full
	}
	return host
}

// cleanRepoPath 规范化路径：去前后斜杠、去尾部 `.git`、压缩重复斜杠。
func cleanRepoPath(path string) string {
	p := strings.TrimSpace(path)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	return p
}

// validateHost 拒绝明显非法的 host。
//
// 规则：必须含 `.`（或命中 hostSlugs 已展开）；不得含空格 / `..`；
// 不得以 `-` / `.` 开头或结尾。
func validateHost(host string) error {
	if !strings.Contains(host, ".") {
		return invalidURL(host,
			fmt.Sprintf("unknown host %q; use a short name (github/gitee/gitlab) or a full domain (e.g. gitlab.example.com)", host))
	}
	if strings.ContainsAny(host, " \t") || strings.Contains(host, "..") {
		return invalidURL(host, "host contains illegal characters")
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") ||
		strings.HasPrefix(host, "-") || strings.HasSuffix(host, "-") {
		return invalidURL(host, "host must not start or end with `-` or `.`")
	}
	for _, r := range host {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == '_' {
			continue
		}
		return invalidURL(host, "host contains illegal character")
	}
	return nil
}

// validateRepoPath 校验路径：至少两段（org/repo），每段非空且不含非法字符。
//
// 私有 GitLab 的 group/subgroup/repo 多级路径是合法的（≥2 段即可）。
func validateRepoPath(original, path string) error {
	segs := strings.Split(path, "/")
	if len(segs) < 2 {
		return invalidURL(original,
			fmt.Sprintf("repository path %q must have at least 2 segments (org/repo)", path))
	}
	for _, s := range segs {
		if s == "" {
			return invalidURL(original, "repository path has an empty segment (double slash?)")
		}
		if s == "." || s == ".." {
			return invalidURL(original, "repository path must not contain `.` or `..`")
		}
		if strings.ContainsAny(s, " \t") {
			return invalidURL(original, "repository path contains whitespace")
		}
	}
	return nil
}

func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// invalidURL 构造统一的 URL 类错误（退出码 3）。
func invalidURL(input, hint string) error {
	return errs.New(
		errs.CodeConfigInvalid,
		fmt.Sprintf("invalid git address: %q", input),
		hint,
	)
}
