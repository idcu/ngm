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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/resolve"
)

// SchemaVersion 是 ngm.json / ngm.lock / ngm.mappings.json 等所有协议文件的
// 当前 schema 大版本。版本演进规则见 guides/configuration.md 的"schema 版本策略"。
//
// 注意：本常量同时也是配置文件中的 "schemaVersion" 字段。**写入**仍使用 v1；
// **读取**同时接受 v1 与 v2（v0.102 起）——v2 目前只增加可选语义，不改变
// 已有字段的形状，因此旧文件无需迁移，新文件也不会被旧读取器误判为损坏。
//
// 为什么默认写入仍是 v1：v2 尚未定义任何 v1 无法表达的必填字段；让 init/add
// 写 v2 会把"读取向后兼容"变成又一次无收益的格式变更。
const SchemaVersion = 1

// MaxSchemaVersion 是当前读取器能接受的最高项目清单 schema 版本。
//
// 接受区间是 [1, MaxSchemaVersion]；0（字段缺失）按 SchemaVersion 处理。
const MaxSchemaVersion = 2

// SupportedSchemaVersion 报告某个 schemaVersion 是否可被本构建读取。
//
// 0 表示文件没写这个字段：按 SchemaVersion 处理，因此也算支持。
func SupportedSchemaVersion(v int) bool {
	return v == 0 || (v >= 1 && v <= MaxSchemaVersion)
}

// FileName 是项目清单的文件名（用户可编辑，必须提交进 Git）。
//
// v0.70 把它从字面量提成常量：`internal/attest` 要在语句的
// `externalParameters` 里写这个名字，而"名字写在第二个地方"迟早会漂移。
const FileName = "ngm.json"

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
	SchemaVersion int     `json:"schemaVersion"`
	Name          string  `json:"name"`
	Version       string  `json:"version"`
	Runtime       Runtime `json:"runtime"`
	Main          string  `json:"main,omitempty"`
	Types         string  `json:"types,omitempty"`
	// Publish 是模块的发布开关（仅对模块集合有意义）。
	//
	// nil = 跟随全局默认（`ngm modules index --releases`）；显式 true/false
	// 完全覆盖全局开关（ADR-037）。它只影响 release 索引生成，不影响安装/验证。
	Publish      *bool              `json:"publish,omitempty"`
	Dependencies []Dependency       `json:"dependencies"`
	Engines      *EnginesConfig     `json:"engines,omitempty"`
	Vendor       *VendorConfig      `json:"vendor,omitempty"`
	SupplyChain  *SupplyChainConfig `json:"supplyChain,omitempty"`
	// Scripts 是 `ngm run` 的回退脚本来源。
	//
	// package.json.scripts 是**权威**来源；只有那里没有同名脚本时，这里的定义才生效。
	// 两处同时定义同名脚本是配置错误（exit 3）——静默选一边会让"我明明改了脚本"
	// 变成不可解释的行为，而这类分歧只能靠点名两处来源来解决。
	Scripts map[string]string `json:"scripts,omitempty"`
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
//
// v0.5 移除了 `commit` 字段：ngm 从不执行 `git add` / `git commit`（写入路径只有
// lock / mappings / vendor / integrations 产物），它从未有过任何分支，而文档把它
// 描述成会生效——那正是配置接线检查要禁的形态。是否提交 vendor/ 是**使用方的仓库
// 策略**，不由本工具声明。这是一次有意的破坏性变更：写了 `vendor.commit` 的
// ngm.json 现在会被 `DisallowUnknownFields` 拒绝（exit 3），删掉该键即可。
type VendorConfig struct {
	Mode       string `json:"mode,omitempty"`       // global | local（默认 local）
	LinkMode   string `json:"linkMode,omitempty"`   // auto | hardlink | copy | symlink
	GlobalPath string `json:"globalPath,omitempty"` // mode=global 时使用；只能指向 ngm 全局 vendor 根或其子目录
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
	// Effective 是实际生效的**项目**配置视图：builtin → project 的覆盖结果。
	//
	// v0.66 起它是独立副本（此前与 Project 是同一指针——见 mergeProject）。
	// Project 为 nil 时退化为 builtin。
	//
	// **global 层不经过这里**（v0.66 更正，评估报告 P2#13）：`mergeGlobal` 至今
	// 是身份函数，全局字段的实际生效点在 CLI 层——`cmd/ngm/engine.go` 与
	// `cmd/ngm/env.go` 从 GlobalEffective 读 defaultProtocol / tokenEnvVars /
	// permissions，再自行决定如何作用于项目视图。这处注释此前写着
	// "包含 builtin → global → project 的覆盖结果"，描述的是意图，不是实现。
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

// EffectiveGlobalPath 返回**实际会被读取**的全局配置文件路径。
//
// NGM_HOME 是 ngm 用户态根目录（与 `vendor.DefaultLayout()` 的语义一致：它
// 直接**就是** `~/.ngm`，不再是"home"）。因此：
//
//	NGM_HOME 已设置 → <abs(NGM_HOME)>/config.json
//	否则            → <home>/.ngm/config.json（GlobalPath）
//
// 此前 `Load` 只认 `GlobalPath(home)`，于是 README 与 env.go 注释承诺的
// “NGM_HOME 覆盖 ~/.ngm（包括 config.json）”并不成立：把配置放进
// NGM_HOME 下不会被读取，而目录布局（mirror/content）却已经跟着 NGM_HOME 走了。
// 一个只对一半组件生效的覆盖比没有更糟——用户以为隔离了，实际没有。
//
// home 为空（取不到用户主目录）且 NGM_HOME 未设置时返回空串，表示不加载全局配置。
func EffectiveGlobalPath(home string) string {
	if h := strings.TrimSpace(os.Getenv("NGM_HOME")); h != "" {
		abs, err := filepath.Abs(h)
		if err != nil {
			return filepath.Join(h, "config.json")
		}
		return filepath.Join(abs, "config.json")
	}
	if home == "" {
		return ""
	}
	return GlobalPath(home)
}

// Load 加载并合并配置。
//
//	projectDir: 项目根目录；空字符串表示无项目配置
//	homeDir:    用户主目录；空字符串表示不加载全局配置
//
// 返回的 Resolved 中所有指针都不会为 nil，便于调用方无 nil 检查地访问。
//
// 本函数保持“显式 home ⇒ 显式路径”的旧语义（单元测试要能指定任意 home）；
// 需要尊重 NGM_HOME 的调用方用 LoadFrom + EffectiveGlobalPath。
func Load(projectDir, homeDir string) (*Resolved, error) {
	globalPath := ""
	if homeDir != "" {
		globalPath = GlobalPath(homeDir)
	}
	return LoadFrom(projectDir, globalPath)
}

// LoadFrom 加载并合并配置，全局配置路径**显式给出**。
//
// globalPath 为空表示不加载全局配置。它让 NGM_HOME 的换算只发生在一处
// （EffectiveGlobalPath），而不是散在每个调用点。
func LoadFrom(projectDir, globalPath string) (*Resolved, error) {
	builtin := DefaultBuiltin()

	var project *ProjectFile
	if projectDir != "" {
		p, err := readProjectFile(filepath.Join(projectDir, FileName))
		if err != nil {
			return nil, err
		}
		project = p
	}

	var global *GlobalFile
	if globalPath != "" {
		g, err := readGlobalFile(globalPath)
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
		return nil, globalConfigErr(fmt.Errorf("open %s: %w", path, err))
	}
	defer f.Close()
	return decodeGlobal(f, path)
}

// globalConfigErr 把**全局配置**的错误在配置层定型（v0.84 修 v0.83 引入的提示回归）。
//
// 为什么不能在上层（`newProjectEnv`）统一包一层：`config.Load` 同时读
// **项目清单**与**全局配置**，而两者该给的建议不同——
//
//	· 项目清单坏了 → "跑 `ngm config validate`"（TestV31 的失败面表钉着这条）
//	· 全局配置坏了 → "改 ~/.ngm/config.json 的字段，或把它移开跑默认值"
//
// 上层统一包一层会把前者压成后者（v0.83 实测：TestV31 两条子测试立刻红）。
// 规则：**一条错误在哪里定型，就在哪里**——配置文件的错误归配置层。
func globalConfigErr(cause error) error {
	var ngm *errs.NgmError
	if errors.As(cause, &ngm) && strings.TrimSpace(ngm.Hint) != "" {
		return cause // `Validate()` 的 fieldErr 已带逐字段建议，原样返回
	}
	return errs.New(errs.CodeConfigInvalid, cause.Error(),
		"fix the reported field in ~/.ngm/config.json, or move the file aside to run with defaults")
}

// stripBOM 跳过文件开头的 UTF-8 BOM。
//
// 为什么必须容忍它（v0.105 实测）：Windows PowerShell 5.1 的
// `Set-Content -Encoding utf8` 与 Notepad 的"UTF-8"默认都**写入 BOM**，
// 而 `encoding/json` 会在 BOM 上报 `invalid character '\ufeff'`。
// 症状是"文件肉眼看着完全正确、工具说不合法"，而 ngm 的第一顺位用户
// 恰好在 Windows 上——这是第一次上手就会撞到的死法。
//
// BOM 不是内容的一部分：JSON（RFC 8259 §8.1）要求解析器可以忽略它。
func stripBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && len(b) == 3 &&
		b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}

func decodeProject(r io.Reader, path string) (*ProjectFile, error) {
	var p ProjectFile
	dec := json.NewDecoder(stripBOM(r))
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
	dec := json.NewDecoder(stripBOM(r))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, globalConfigErr(fmt.Errorf("parse %s: %w", path, err))
	}
	if err := g.Validate(); err != nil {
		return nil, globalConfigErr(err)
	}
	return &g, nil
}

// mergeProject 在 builtin 默认值上覆盖 project 字段。
//
// 设计：**返回一份新对象，绝不修改入参**（v0.66 修，评估报告 P2#13）。
// 此前是原地修改：`Resolved.Effective` 与 `Resolved.Project` 因此是**同一指针**，
// 与 `Resolved` 的文档注释（"来源"与"合并视图"是两个视图）矛盾——
// 调用方改有效视图会悄悄改掉来源。Vendor 是指针字段，需要一层深拷贝
// （否则 `out.Vendor` 与 `project.Vendor` 仍指向同一对象）。
//
// 回退项：vendor.mode / vendor.linkMode / runtime / schemaVersion。
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
	out := *project
	if out.Runtime == "" {
		out.Runtime = builtin.Runtime
	}
	if out.SchemaVersion == 0 {
		out.SchemaVersion = SchemaVersion
	}
	if out.Vendor == nil {
		out.Vendor = &VendorConfig{
			Mode:     builtin.VendorMode,
			LinkMode: builtin.LinkMode,
		}
	} else {
		v := *out.Vendor
		if v.Mode == "" {
			v.Mode = builtin.VendorMode
		}
		if v.LinkMode == "" {
			v.LinkMode = builtin.LinkMode
		}
		out.Vendor = &v
	}
	return &out
}

func mergeGlobal(g *GlobalFile) *GlobalFile {
	if g == nil {
		return &GlobalFile{}
	}
	return g
}
