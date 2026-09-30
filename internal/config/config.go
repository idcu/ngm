// Package config 实现 ngm 的配置加载与三级合并。
//
// 三级合并顺序（见 modules/p0-core.md §2）：
//
//	内置（builtin, 编译进二进制）
//	→ 全局（global, ~/.ngm/config.json）
//	→ 项目（project, ./ngm.json）
//
// 后者覆盖前者的字段。数组字段按"替换"语义（非追加）；
// 用户若想保留 builtin 列表，必须显式写完整数组。
//
// 校验与查询命令（ngm config validate|show）的实现位于 cmd/ngm/config.go。
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/resolve"
)

// SchemaVersion 是 ngm.json / ngm.lock / ngm.mappings.json 等所有协议文件的
// 当前 schema 大版本。版本演进规则见 guides/configuration.md 的"schema 版本策略"。
//
// 注意：本常量同时也是配置文件中的 "schemaVersion" 字段。当前仅 v1。
const SchemaVersion = 1

// Runtime 宿主运行时（见 guides/configuration.md）。
type Runtime string

const (
	RuntimeNode Runtime = "node"
	RuntimeDeno Runtime = "deno"
)

// RefType 依赖引用的 ref 类型。配置中必填，不允许省略。
//
// 这是 resolve.RefType 的类型别名——单一事实源在 resolve 包，
// 此处别名让 config 的调用方无需额外 import。
//
// 推断规则（仅 ngm add 交互辅助，配置中必须显式）见 guides/configuration.md。
type RefType = resolve.RefType

const (
	RefTypeCommit = resolve.RefTypeCommit
	RefTypeTag    = resolve.RefTypeTag
	RefTypeBranch = resolve.RefTypeBranch
)

// ValidRefType 列出所有合法 refType，便于校验与错误提示。
func ValidRefType() []RefType { return resolve.ValidRefTypes() }

// ValidRuntime 列出所有合法 runtime。
func ValidRuntime() []Runtime { return []Runtime{RuntimeNode, RuntimeDeno} }

// ProjectFile 是项目根目录的 ngm.json。
type ProjectFile struct {
	SchemaVersion int                `json:"schemaVersion"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Runtime       Runtime            `json:"runtime"`
	Main          string             `json:"main,omitempty"`
	Types         string             `json:"types,omitempty"`
	Dependencies  []Dependency       `json:"dependencies"`
	Engines       *EnginesConfig     `json:"engines,omitempty"`
	Vendor        *VendorConfig      `json:"vendor,omitempty"`
	SupplyChain   *SupplyChainConfig `json:"supplyChain,omitempty"`
}

// Dependency 声明中的一个依赖。
type Dependency struct {
	Name    string  `json:"name"`
	Ref     string  `json:"ref"`
	RefType RefType `json:"refType"`
	// Path 可选，monorepo 子路径（M1 完整实现；M0 仅记录与校验格式）。
	Path string `json:"path,omitempty"`
}

// EnginesConfig 引擎选择；支持简写与完整写法。
//
// 简写 `"transform": "esbuild"` 等价于 `{"primary": "esbuild", "fallbacks": []}`（不做自动回退）。
type EnginesConfig struct {
	Transform any `json:"transform,omitempty"`
	Bundle    any `json:"bundle,omitempty"`
	TypeCheck any `json:"typeCheck,omitempty"`
	TypeDecl  any `json:"typeDecl,omitempty"`
	CSS       any `json:"css,omitempty"`
}

// EngineRef 单个引擎引用（带 spec 的完整形态）。
type EngineRef struct {
	Primary   string         `json:"primary"`
	Fallbacks []string       `json:"fallbacks,omitempty"`
	Options   map[string]any `json:"options,omitempty"`
}

// AsMap 返回 engines 段的原始值映射（能力类别名 → 简写或完整写法）。
//
// 为什么返回 map 而不是让调用方逐个读字段：引擎选择的形态是
// "简写字符串 | 完整对象"的联合类型（字段类型为 any），只有归一化器
// （adapter.ResolveSelection）知道怎么解释它。config 只负责把原始值原样
// 交出去、不做解释——这样"配置解析"与"引擎语义"互不依赖，任一侧改字段
// 都不会牵动另一侧。
//
// nil 接收者返回 nil（项目没有 engines 段），调用方无需判空。
func (e *EnginesConfig) AsMap() map[string]any {
	if e == nil {
		return nil
	}
	out := map[string]any{}
	for _, f := range []struct {
		key string
		val any
	}{
		{"transform", e.Transform},
		{"bundle", e.Bundle},
		{"typeCheck", e.TypeCheck},
		{"typeDecl", e.TypeDecl},
		{"css", e.CSS},
	} {
		if f.val != nil {
			out[f.key] = f.val
		}
	}
	return out
}

// VendorConfig vendor 模式与 linkMode 配置。
type VendorConfig struct {
	Mode       string `json:"mode,omitempty"`       // global | local（默认 local）
	Commit     bool   `json:"commit,omitempty"`     // 提交 vendor/ 到仓库
	LinkMode   string `json:"linkMode,omitempty"`   // auto | hardlink | copy | symlink
	GlobalPath string `json:"globalPath,omitempty"` // mode=global 时使用
}

// SupplyChainConfig v0.2 供应链策略。字段集在 v0.1 期间保留为空结构以便向后兼容。
type SupplyChainConfig struct {
	AllowedGitHosts     []string `json:"allowedGitHosts,omitempty"`
	AllowlistRepos      []string `json:"allowlistRepos,omitempty"`
	MinimumReleaseAge   string   `json:"minimumReleaseAge,omitempty"`
	OSVIgnoreSeverities []string `json:"osvIgnoreSeverities,omitempty"`
	PostInstallPolicy   string   `json:"postInstallPolicy,omitempty"`
	VerifyOnLock        *bool    `json:"verifyOnLock,omitempty"`
}

// GlobalFile 是 ~/.ngm/config.json 的 schema。
type GlobalFile struct {
	SchemaVersion int                `json:"schemaVersion,omitempty"`
	Git           *GlobalGit         `json:"git,omitempty"`
	Engines       *GlobalEngines     `json:"engines,omitempty"`
	Permissions   *GlobalPermissions `json:"permissions,omitempty"`
}

// GlobalGit 全局 Git 配置（凭证透传规则）。
type GlobalGit struct {
	DefaultProtocol string            `json:"defaultProtocol,omitempty"`
	TokenEnvVars    map[string]string `json:"tokenEnvVars,omitempty"`
}

// GlobalEngines 全局默认引擎（项目未指定时生效）。
type GlobalEngines struct {
	DefaultTransform string `json:"defaultTransform,omitempty"`
	DefaultBundle    string `json:"defaultBundle,omitempty"`
	DefaultTypeCheck string `json:"defaultTypeCheck,omitempty"`
}

// GlobalPermissions v0.3 命名空间权限。M0 保留结构与读取，便于后续阶段实施。
type GlobalPermissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Resolved 是三级合并后的最终配置视图。CLI 与各子命令消费它。
//
// 设计取舍：Resolved 不是"扁平快照"，而是分别保留 builtin/global/project 三层以
// 便于 `ngm config show` 输出来源。同时 Effective 字段是实际生效的合并结果。
type Resolved struct {
	// Project 来源（可能为 nil——非项目目录中运行时不报错）
	Project *ProjectFile
	// Global 来源（~/.ngm/config.json 不存在时为 nil）
	Global *GlobalFile
	// Builtin 是编译进二进制的默认值
	Builtin *BuiltinDefaults
	// Effective 是三级合并后实际生效的配置（包含 builtin → global → project 的覆盖结果）。
	// 当前为 Project 的合并后视图；若 Project 为 nil，则退化为 builtin。
	Effective *ProjectFile
	// GlobalEffective 是三级合并 GlobalFile 的结果；类似 Effective。
	GlobalEffective *GlobalFile
}

// BuiltinDefaults 是编译进二进制的默认值。任何项目字段若未在 project/global 中设置，
// 使用这里定义的回退值。
type BuiltinDefaults struct {
	Runtime     Runtime
	LinkMode    string // auto
	VendorMode  string // local
	GitProtocol string // ssh
}

// DefaultBuiltin 返回新的默认值快照。
func DefaultBuiltin() *BuiltinDefaults {
	return &BuiltinDefaults{
		Runtime:     RuntimeNode,
		LinkMode:    "auto",
		VendorMode:  "local",
		GitProtocol: "ssh",
	}
}

// GlobalPath 返回全局配置文件的路径（`<home>/.ngm/config.json`）。
//
// 导出它是为了让"写到哪"只有一个定义：测试要在隔离环境里放一份配置，
// 如果它自己拼一遍路径，两边迟早会漂移，而那种漂移的表现是
// "测试写的配置没被读到"——排查起来很费时间。
func GlobalPath(home string) string {
	return filepath.Join(home, ".ngm", "config.json")
}

// Load 加载并合并配置。
//
//	projectDir: 项目根目录；空字符串表示无项目配置
//	homeDir:    用户主目录；空字符串表示不加载全局配置
//
// 返回的 Resolved 中所有指针都不会为 nil，便于调用方无 nil 检查地访问。
func Load(projectDir, homeDir string) (*Resolved, error) {
	builtin := DefaultBuiltin()

	var project *ProjectFile
	if projectDir != "" {
		p, err := readProjectFile(filepath.Join(projectDir, "ngm.json"))
		if err != nil {
			return nil, err
		}
		project = p
	}

	var global *GlobalFile
	if homeDir != "" {
		g, err := readGlobalFile(GlobalPath(homeDir))
		if err != nil {
			return nil, err
		}
		global = g
	}

	effective := mergeProject(builtin, project)
	globalEff := mergeGlobal(global)

	return &Resolved{
		Project:         project,
		Global:          global,
		Builtin:         builtin,
		Effective:       effective,
		GlobalEffective: globalEff,
	}, nil
}

func readProjectFile(path string) (*ProjectFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return decodeProject(f, path)
}

func readGlobalFile(path string) (*GlobalFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return decodeGlobal(f, path)
}

func decodeProject(r io.Reader, path string) (*ProjectFile, error) {
	var p ProjectFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func decodeGlobal(r io.Reader, path string) (*GlobalFile, error) {
	var g GlobalFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return &g, nil
}

// mergeProject 在 builtin 默认值上覆盖 project 字段。
//
// 设计：保留 Project 原值（除被 builtin 显式覆盖的字段），返回新 ProjectFile。
// 当前 v0.1 仅有 vendor.linkMode / vendor.mode / runtime 三个回退项。
func mergeProject(builtin *BuiltinDefaults, project *ProjectFile) *ProjectFile {
	if project == nil {
		// 仅 builtin 视图：把所有字段填上以便 Effective 直接访问。
		return &ProjectFile{
			SchemaVersion: SchemaVersion,
			Runtime:       builtin.Runtime,
			Vendor: &VendorConfig{
				Mode:     builtin.VendorMode,
				LinkMode: builtin.LinkMode,
			},
		}
	}
	if project.Runtime == "" {
		project.Runtime = builtin.Runtime
	}
	if project.SchemaVersion == 0 {
		project.SchemaVersion = SchemaVersion
	}
	if project.Vendor == nil {
		project.Vendor = &VendorConfig{
			Mode:     builtin.VendorMode,
			LinkMode: builtin.LinkMode,
		}
	} else {
		if project.Vendor.Mode == "" {
			project.Vendor.Mode = builtin.VendorMode
		}
		if project.Vendor.LinkMode == "" {
			project.Vendor.LinkMode = builtin.LinkMode
		}
	}
	return project
}

func mergeGlobal(g *GlobalFile) *GlobalFile {
	if g == nil {
		return &GlobalFile{}
	}
	return g
}
