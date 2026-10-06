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
func (p *ProjectFile) Validate() error {
	if p == nil {
		return errors.New("project file is nil")
	}
	if p.SchemaVersion != 0 && p.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"schemaVersion %d not supported; this build supports v%d",
			p.SchemaVersion, SchemaVersion,
		)
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(p.Version) == "" {
		return errors.New("version is required")
	}
	if !validRuntime(p.Runtime) {
		return fmt.Errorf("runtime %q invalid; must be one of %v", p.Runtime, ValidRuntime())
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
		return errors.New("name is required")
	}
	if _, err := resolve.ParseSlug(d.Name); err != nil {
		return err
	}
	if strings.TrimSpace(d.Ref) == "" {
		return errors.New("ref is required")
	}
	if !validRefType(d.RefType) {
		return fmt.Errorf(
			"refType %q invalid; must be one of %v (refType is required in ngm.json)",
			d.RefType, ValidRefType(),
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
			return fmt.Errorf("minimumReleaseAge: %w", err)
		}
	}
	for i, sev := range s.OSVIgnoreSeverities {
		if !isOSVSeverity(sev) {
			return fmt.Errorf(
				"osvIgnoreSeverities[%d]: %q invalid; must be one of {LOW, MEDIUM, HIGH, CRITICAL}",
				i, sev)
		}
	}
	if s.PostInstallPolicy != "" {
		switch s.PostInstallPolicy {
		case "deny", "prompt", "allow":
		default:
			return fmt.Errorf(
				"postInstallPolicy %q invalid; must be one of {deny, prompt, allow}",
				s.PostInstallPolicy,
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
	if h == "" {
		return errors.New("host must not be empty")
	}
	if strings.Contains(h, "*") {
		return fmt.Errorf(
			"%q contains a wildcard, but hosts are matched exactly; "+
				"use allowlistRepos for glob patterns (e.g. github.com/my-org/*)", h)
	}
	if strings.Contains(h, "://") {
		return fmt.Errorf("%q looks like a URL; give the bare host instead (e.g. github.com)", h)
	}
	if strings.ContainsAny(h, "/ \t") || strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return fmt.Errorf("%q is not a bare host name (expected something like github.com)", h)
	}
	return nil
}

// validateRepoPattern 校验 allowlistRepos 的一个条目。
//
// 形状为 `host/org/repo`，`*` 不跨 `/`（ADR-009）。要求至少一个 `/`：
// 只有 host 的条目应当写进 allowedGitHosts，两者的语义不同，不要混用。
func validateRepoPattern(p string) error {
	if p == "" {
		return errors.New("pattern must not be empty")
	}
	if strings.Contains(p, "://") {
		return fmt.Errorf("%q looks like a URL; write it as host/org/repo (e.g. github.com/my-org/*)", p)
	}
	if strings.ContainsAny(p, " \t") {
		return fmt.Errorf("%q contains whitespace", p)
	}
	if !strings.Contains(p, "/") {
		return fmt.Errorf(
			"%q has no '/': a repo pattern is host/org/repo (e.g. github.com/my-org/*); "+
				"to allow a whole host, use allowedGitHosts", p)
	}
	if strings.Trim(p, "/*") == "" {
		return fmt.Errorf(
			"%q allows everything, which is the same as having no allowlist; "+
				"remove allowlistRepos if that is what you want", p)
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
			return fmt.Errorf("vendor.mode %q invalid; must be one of {global, local}", v.Mode)
		}
	}
	if v.LinkMode != "" {
		switch v.LinkMode {
		case "auto", "hardlink", "copy", "symlink":
		default:
			return fmt.Errorf("vendor.linkMode %q invalid; must be one of {auto, hardlink, copy, symlink}", v.LinkMode)
		}
	}
	if v.Mode == "global" && strings.TrimSpace(v.GlobalPath) == "" {
		return errors.New("vendor.globalPath is required when vendor.mode = \"global\"")
	}
	return nil
}

// Validate 校验 GlobalFile 顶层字段。
func (g *GlobalFile) Validate() error {
	if g == nil {
		return nil
	}
	if g.SchemaVersion != 0 && g.SchemaVersion != SchemaVersion {
		return fmt.Errorf(
			"global schemaVersion %d not supported; this build supports v%d",
			g.SchemaVersion, SchemaVersion,
		)
	}
	if g.Git != nil && g.Git.DefaultProtocol != "" {
		switch g.Git.DefaultProtocol {
		case "ssh", "https":
		default:
			return fmt.Errorf("git.defaultProtocol %q invalid; must be one of {ssh, https}", g.Git.DefaultProtocol)
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
