package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/resolve"
)

// Validate 校验 ProjectFile 的字段语义。
//
// 返回的错误是给用户看的：包含字段路径与可操作 hint。
// 调用方应使用 errs.Wrap(CodeConfigInvalid, ...) 包装。
//
// 校验规则（与 guides/configuration.md 对齐）：
//   - schemaVersion == SchemaVersion（缺失按 SchemaVersion 补全）
//   - name / version 非空
//   - runtime ∈ {node, deno}
//   - dependencies[].refType ∈ {commit, tag, branch}
//   - dependencies[].name 非空且符合 "host:org/repo" 格式
//   - dependencies[].ref 非空
//   - postInstallPolicy ∈ {deny, prompt, allow}
//
// fieldErr 造一条"某个字段不合法"的错误：**消息说哪里不对，建议说该怎么改**。
//
// 为什么有这个函数（v0.49）：本包 `Validate` 的注释写着"返回的错误是给用户看的：
// 包含字段路径与可操作 hint"，而实现里三十处全是裸 `fmt.Errorf` / `errors.New`——
// 一句话也没告诉用户下一步该做什么，于是用户手里那句建议只能来自**调用方**的兜底
// （"fix the reported field, or run `ngm config validate`"），它讲的是"这文件整体坏了"，
// 而不是"这一行该改成什么"。
//
// 它同时是 `TestV49…` 的静态锚点：这个文件里**不许**再出现不带 `%w` 的裸造错误
// （带 `%w` 的包装不算——它把内层的建议原样带上来）。
func fieldErr(msg, hint string) error {
	return errs.New(errs.CodeConfigInvalid, msg, hint)
}

func (p *ProjectFile) Validate() error {
	if p == nil {
		// 例外（登记在 TestV49… 里）：这不是用户能写出来的状态——它是调用方的编程错误。
		return errors.New("project file is nil")
	}
	if p.SchemaVersion != 0 && p.SchemaVersion != SchemaVersion {
		return fieldErr(
			fmt.Sprintf("schemaVersion %d not supported; this build supports v%d",
				p.SchemaVersion, SchemaVersion),
			fmt.Sprintf("set `schemaVersion` to %d in ngm.json, or upgrade ngm", SchemaVersion),
		)
	}
	if strings.TrimSpace(p.Name) == "" {
		return fieldErr("name is required",
			"add a `name` like `github.com:my-org/app` to ngm.json")
	}
	if strings.TrimSpace(p.Version) == "" {
		return fieldErr("version is required",
			"add a `version` like `0.1.0` to ngm.json (it is free-form, but must not be empty)")
	}
	if !validRuntime(p.Runtime) {
		return fieldErr(
			fmt.Sprintf("runtime %q invalid; must be one of %v", p.Runtime, ValidRuntime()),
			fmt.Sprintf("set `runtime` to one of %v in ngm.json", ValidRuntime()),
		)
	}
	for i, d := range p.Dependencies {
		if err := d.Validate(); err != nil {
			return fmt.Errorf("dependencies[%d]: %w", i, err)
		}
	}
	if p.SupplyChain != nil {
		if err := p.SupplyChain.Validate(); err != nil {
			return fmt.Errorf("supplyChain: %w", err)
		}
	}
	if p.Vendor != nil {
		if err := p.Vendor.Validate(); err != nil {
			return fmt.Errorf("vendor: %w", err)
		}
	}
	return nil
}

// Validate 校验单个依赖。
//
// name 的格式校验委托给 resolve.ParseSlug（唯一事实源），避免两套 URL 规则漂移。
func (d *Dependency) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fieldErr("name is required",
			"give this dependency a `name` like `github:org/repo`")
	}
	if _, err := resolve.ParseSlug(d.Name); err != nil {
		return err // ParseSlug 自己带建议（唯一事实源）
	}
	if strings.TrimSpace(d.Ref) == "" {
		return fieldErr("ref is required",
			"add a `ref` (the tag / branch / commit this dependency is pinned to) "+
				"and a matching `refType`")
	}
	if !validRefType(d.RefType) {
		return fieldErr(
			fmt.Sprintf("refType %q invalid; must be one of %v (refType is required in ngm.json)",
				d.RefType, ValidRefType()),
			fmt.Sprintf("set `refType` to one of %v — ngm does not guess it from `ref`", ValidRefType()),
		)
	}
	if d.Path != "" {
		if err := validateDepPath(d.Path); err != nil {
			return err
		}
	}
	return nil
}

// validateDepPath 校验 monorepo 子路径：相对路径、不含 `..`、不含前导 `/`。
//
// **给建议**（v0.48）：它是唯一知道"该往哪个方向改"的那一层。
// 原先它返回裸 `fmt.Errorf`，于是用户拿到的建议只能是调用方那句兜底——
// 实测：`ngm add --path=/etc` 的 cause 说的是"路径必须相对"，
// 而用户看到的建议整句在讲 `<host>:<org>/<repo>[@<ref>]`（那是**仓库标识**的形状）。
// 为 path 挨骂，却被告知去检查别的地方。
func validateDepPath(p string) error {
	const hint = "use a path relative to the repository root, " +
		"e.g. `packages/core` (no leading `/`, no `.` or `..`)"
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("path %q must be relative (no leading slash)", p), hint)
	}
	segs := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
	if len(segs) == 0 {
		return errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("path %q is empty after normalization", p), hint)
	}
	for _, s := range segs {
		if s == "." || s == ".." {
			return errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("path %q must not contain `.` or `..`", p), hint)
		}
	}
	return nil
}

// Validate 校验 SupplyChainConfig 的**全部字段**（v0.2 起）。
//
// 为什么校验放在配置加载时：策略是安全门禁，非法取值必须在**任何网络访问之前**以 exit 3 失败。
// 这正是 ADR-009 要求的"策略与网络解耦"——不该为了发现一个写错的 host 先碰一次远端。
//
// 这里只校验**形状**（取值是否合法），不校验**语义**（该 host 是否可达、该 repo 是否存在）。
// 语义判定与门禁执行属 internal/supplychain（见 ADR-009）。
func (s *SupplyChainConfig) Validate() error {
	for i, h := range s.AllowedGitHosts {
		if err := validateAllowedHost(h); err != nil {
			return fmt.Errorf("allowedGitHosts[%d]: %w", i, err)
		}
	}
	for i, p := range s.AllowlistRepos {
		if err := validateRepoPattern(p); err != nil {
			return fmt.Errorf("allowlistRepos[%d]: %w", i, err)
		}
	}
	if s.MinimumReleaseAge != "" {
		if _, err := ParseISODuration(s.MinimumReleaseAge); err != nil {
			// 解析器自己只报"哪里看不懂"（八处裸错误），字段层补上"该怎么写"——
			// 一处建议代替八处，而且它知道这个值**该填在哪个字段里**（v0.49）。
			return fieldErr("minimumReleaseAge: "+err.Error(),
				"write `supplyChain.minimumReleaseAge` as an ISO 8601 duration, e.g. `P7D` or `PT12H`")
		}
	}
	for i, sev := range s.OSVIgnoreSeverities {
		if !isOSVSeverity(sev) {
			return fieldErr(
				fmt.Sprintf("osvIgnoreSeverities[%d]: %q invalid; must be one of {LOW, MEDIUM, HIGH, CRITICAL}", i, sev),
				"use the uppercase OSV severity names in `supplyChain.osvIgnoreSeverities`",
			)
		}
	}
	if s.PostInstallPolicy != "" {
		switch s.PostInstallPolicy {
		case "deny", "prompt", "allow":
		default:
			return fieldErr(
				fmt.Sprintf("postInstallPolicy %q invalid; must be one of {deny, prompt, allow}", s.PostInstallPolicy),
				"set `supplyChain.postInstallPolicy` to `deny`, `prompt` or `allow`",
			)
		}
	}
	return nil
}

// validateAllowedHost 校验 allowedGitHosts 的一个条目。
//
// **不允许通配符**：host 走精确匹配（ADR-009）。要按通配授权请用 allowlistRepos——
// 把 `*` 写进 host 是常见误解，报错里直接点明比让它静默匹配不上更有用。
func validateAllowedHost(h string) error {
	const hint = "write it in `supplyChain.allowedGitHosts` as a bare host, e.g. `github.com`"
	if h == "" {
		return fieldErr("host must not be empty", hint)
	}
	if strings.Contains(h, "*") {
		return fieldErr(
			fmt.Sprintf("%q contains a wildcard, but hosts are matched exactly; "+
				"use allowlistRepos for glob patterns (e.g. github.com/my-org/*)", h),
			"move the glob to `supplyChain.allowlistRepos`, or drop the `*` for exact matching",
		)
	}
	if strings.Contains(h, "://") {
		return fieldErr(
			fmt.Sprintf("%q looks like a URL; give the bare host instead (e.g. github.com)", h),
			hint)
	}
	if strings.ContainsAny(h, "/ \t") || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return fieldErr(
			fmt.Sprintf("%q is not a bare host name (expected something like github.com)", h),
			hint)
	}
	return nil
}

// validateRepoPattern 校验 allowlistRepos 的一个条目。
//
// 形状为 `host/org/repo`，`*` 不跨 `/`（ADR-009）。要求至少一个 `/`：
// 只有 host 的条目应当写进 allowedGitHosts，两者的语义不同，不要混用。
func validateRepoPattern(p string) error {
	const hint = "write it in `supplyChain.allowlistRepos` as host/org/repo, " +
		"e.g. `github.com/my-org/*` (`*` does not cross `/`)"
	if p == "" {
		return fieldErr("pattern must not be empty", hint)
	}
	if strings.Contains(p, "://") {
		return fieldErr(
			fmt.Sprintf("%q looks like a URL; write it as host/org/repo (e.g. github.com/my-org/*)", p),
			hint)
	}
	if strings.ContainsAny(p, " \t") {
		return fieldErr(fmt.Sprintf("%q contains whitespace", p), hint)
	}
	if !strings.Contains(p, "/") {
		return fieldErr(
			fmt.Sprintf("%q has no '/': a repo pattern is host/org/repo (e.g. github.com/my-org/*); "+
				"to allow a whole host, use allowedGitHosts", p),
			"add the org: `github.com/my-org/*`, or put the bare host in `allowedGitHosts`",
		)
	}
	if strings.Trim(p, "/*") == "" {
		return fieldErr(
			fmt.Sprintf("%q allows everything, which is the same as having no allowlist; "+
				"remove allowlistRepos if that is what you want", p),
			"name the org (e.g. `github.com/my-org/*`) or remove this entry",
		)
	}
	return nil
}

// isOSVSeverity 判断是否为合法的 OSV 严重级别（ASCII 大小写不敏感）。
func isOSVSeverity(s string) bool {
	for _, want := range []string{"LOW", "MEDIUM", "HIGH", "CRITICAL"} {
		if eqFoldASCII(s, want) {
			return true
		}
	}
	return false
}

// eqFoldASCII 做 ASCII 大小写不敏感比较。
//
// 配置取值都是 ASCII，因此不引入 strings.EqualFold 之外的依赖——这里直接用最简单、
// 无分配的实现，避免为一个 6 行的判断拉进不必要的东西。
func eqFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'a' <= x && x <= 'z' {
			x -= 'a' - 'A'
		}
		if 'a' <= y && y <= 'z' {
			y -= 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// Validate 校验 VendorConfig 字段语义。
func (v *VendorConfig) Validate() error {
	if v.Mode != "" {
		switch v.Mode {
		case "global", "local":
		default:
			return fieldErr(
				fmt.Sprintf("vendor.mode %q invalid; must be one of {global, local}", v.Mode),
				"set `vendor.mode` to `local` (vendor into the project) or `global` (share one store)")
		}
	}
	if v.LinkMode != "" {
		switch v.LinkMode {
		case "auto", "hardlink", "copy", "symlink":
		default:
			return fieldErr(
				fmt.Sprintf("vendor.linkMode %q invalid; must be one of {auto, hardlink, copy, symlink}", v.LinkMode),
				"set `vendor.linkMode` to one of {auto, hardlink, copy, symlink} (`auto` picks per platform)")
		}
	}
	if v.Mode == "global" && strings.TrimSpace(v.GlobalPath) == "" {
		return fieldErr("vendor.globalPath is required when vendor.mode = \"global\"",
			"add `vendor.globalPath` (where the shared vendor tree lives), or use vendor.mode = \"local\"")
	}
	return nil
}

// Validate 校验 GlobalFile 顶层字段。
func (g *GlobalFile) Validate() error {
	if g == nil {
		return nil
	}
	if g.SchemaVersion != 0 && g.SchemaVersion != SchemaVersion {
		return fieldErr(
			fmt.Sprintf("global schemaVersion %d not supported; this build supports v%d",
				g.SchemaVersion, SchemaVersion),
			fmt.Sprintf("set `schemaVersion` to %d in ~/.ngm/config.json, or upgrade ngm", SchemaVersion),
		)
	}
	if g.Git != nil && g.Git.DefaultProtocol != "" {
		switch g.Git.DefaultProtocol {
		case "ssh", "https":
		default:
			return fieldErr(
				fmt.Sprintf("git.defaultProtocol %q invalid; must be one of {ssh, https}", g.Git.DefaultProtocol),
				"set `git.defaultProtocol` to `ssh` or `https` in ~/.ngm/config.json")
		}
	}
	return nil
}

func validRuntime(r Runtime) bool {
	for _, v := range ValidRuntime() {
		if r == v {
			return true
		}
	}
	return false
}

func validRefType(r RefType) bool {
	for _, v := range ValidRefType() {
		if r == v {
			return true
		}
	}
	return false
}
