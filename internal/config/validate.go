package config

import (
	"errors"
	"fmt"
	"strings"

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
func validateDepPath(p string) error {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("path %q must be relative (no leading slash)", p)
	}
	segs := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
	if len(segs) == 0 {
		return fmt.Errorf("path %q is empty after normalization", p)
	}
	for _, s := range segs {
		if s == "." || s == ".." {
			return fmt.Errorf("path %q must not contain `.` or `..`", p)
		}
	}
	return nil
}

// Validate 校验 SupplyChainConfig。仅校验 v0.1 字段（postInstallPolicy）；
// v0.2 完整校验另见 Documentation 模块。
func (s *SupplyChainConfig) Validate() error {
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
