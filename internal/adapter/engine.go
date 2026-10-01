// Package adapter 实现引擎 adapter 模型（architecture/engine-adapter.md）。
//
// 核心原则（ADR-005：为什么引擎统一接口动态选择）：**ngm core 不做自研引擎**。
// transform / bundle / typeCheck / typeDecl / css 五种能力全部通过 adapter 调用
// 外部工具，第三方永久优先。
//
// 本包只做四件事：
//
//  1. 引擎清单（内置 + ngm.engines.json 覆盖）的读取与校验
//  2. 选项 → 命令行的翻译（按引擎名分派，见 subprocess.go）
//  3. subprocess 协议的执行与错误映射（modules/p4-ecosystem.md）
//  4. primary → fallbacks 的选择与回退
//
// 本包**不做**：解析源码、实现 transformer、管理 dev server / HMR。
// 这正是 ADR-005 的"明确不做"——ngm 的差异化不在引擎层。
package adapter

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// EngineKind 是引擎能力类别（architecture/engine-adapter.md §引擎接口）。
type EngineKind string

const (
	// KindTransform 单文件转换。
	KindTransform EngineKind = "transform"
	// KindBundle 打包。
	KindBundle EngineKind = "bundle"
	// KindTypeCheck 类型检查。
	KindTypeCheck EngineKind = "typeCheck"
	// KindTypeDecl 类型声明生成。
	KindTypeDecl EngineKind = "typeDecl"
	// KindCSS CSS 编译。
	KindCSS EngineKind = "css"
)

// AllKinds 按固定顺序返回所有能力类别。
//
// 顺序即输出顺序：`ngm engines list` 与 --json 都依赖它保持稳定，
// 让 diff 与快照测试可比。
func AllKinds() []EngineKind {
	return []EngineKind{KindTransform, KindBundle, KindTypeCheck, KindTypeDecl, KindCSS}
}

// IsValid 报告 k 是否为已知的能力类别。
func (k EngineKind) IsValid() bool {
	for _, v := range AllKinds() {
		if k == v {
			return true
		}
	}
	return false
}

// Engine 是所有引擎共享的基础接口（architecture/engine-adapter.md §引擎接口）。
//
// Available 的语义是"这台机器上现在能不能用"，而不是"配置里有没有声明"。
// 因此它必须真的探测可执行文件——清单表达的是意图，不是事实。
type Engine interface {
	// Name 是引擎名（清单条目的 name，如 esbuild）。
	Name() string
	// Kind 是该实例提供的能力类别。
	Kind() EngineKind
	// Version 返回引擎自报的版本（执行 `<program> --version`）。
	Version() (string, error)
	// Available 报告引擎当前是否可用。
	Available() bool
}

// ---------------------------------------------------------------------------
// 各能力接口
//
// 与文档的接口定义保持同一风格，只增加 context.Context：
// 引擎是子进程，必须能被 Ctrl+C 与超时取消，否则一个挂住的 tsc 会让
// `ngm build` 无法中断。这是本包相对文档的唯一签名偏差。
// ---------------------------------------------------------------------------

// TransformEngine 单文件转换能力。
type TransformEngine interface {
	Name() string
	Transform(ctx context.Context, input []byte, opts TransformOptions) (*TransformResult, error)
}

// BundleEngine 打包能力。
type BundleEngine interface {
	Name() string
	Bundle(ctx context.Context, entry string, opts BundleOptions) (*BundleResult, error)
}

// TypeCheckEngine 类型检查能力。
type TypeCheckEngine interface {
	Name() string
	Check(ctx context.Context, entry string, opts TypeCheckOptions) (*TypeCheckResult, error)
}

// TypeDeclEngine 类型声明生成能力。
type TypeDeclEngine interface {
	Name() string
	GenerateTypeDecl(ctx context.Context, entry string, opts TypeDeclOptions) (*TypeDeclResult, error)
}

// CSSEngine CSS 编译能力。
type CSSEngine interface {
	Name() string
	Compile(ctx context.Context, input []byte, opts CSSOptions) (*CSSResult, error)
}

// ---------------------------------------------------------------------------
// 选项与结果
// ---------------------------------------------------------------------------

// TransformOptions 是 transform 的输入选项。
type TransformOptions struct {
	// Loader 是输入语法（ts / tsx / js / jsx）；空表示按文件扩展名推断。
	Loader string
	// Target 是输出目标（如 es2020）。
	Target string
	// Format 是模块格式（esm / cjs / iife）。
	Format string
	// Minify 是否压缩。
	Minify bool
	// SourceMaps 是否生成 source map。
	SourceMaps bool
	// Extra 是引擎特有的附加选项，翻译为 `--<key>=<value>`。
	Extra map[string]any
}

// TransformResult 是 transform 的产物。
type TransformResult struct {
	// Code 是转换结果（引擎 stdout）。
	Code []byte
	// Warnings 是引擎 stderr 按行拆分的结果（转成 []byte 是原样保留诊断）。
	Warnings []string
}

// BundleOptions 是 bundle 的输入选项。
type BundleOptions struct {
	// Outfile 是输出文件；空表示只产出到 stdout（与直接跑 esbuild 一致）。
	Outfile string
	// Alias 是模块别名（来自 ngm.mappings.json），把 `github:org/repo` 指到 vendor 路径。
	Alias map[string]string
	// Target / Format / Platform 透传给引擎。
	Target   string
	Format   string
	Platform string
	// Minify 是否压缩；Production 会额外注入 NODE_ENV=production。
	Minify     bool
	Production bool
	// Extra 是引擎特有的附加选项。
	Extra map[string]any
}

// BundleResult 是 bundle 的产物。
type BundleResult struct {
	// Code 是产物内容；--outfile 给出时为空（产物已落盘）。
	Code []byte
	// Outfile 是实际写出的文件路径；未落盘时为空。
	Outfile string
	// Warnings 是**引擎自己**写到 stderr 的诊断，原样转述。
	Warnings []string
	// Notes 是 **ngm 自己**要说的话（例如某个选项被忽略及其原因）。
	//
	// 与 Warnings 分成两个字段，是因为它们的**出处**不同：用户要能分清
	// "引擎在报警"和"ngm 在解释自己做的事"。两者合并会让日志读起来像引擎说了
	// 一句它没说过的话。
	//
	// v0.5 之前这两条通道**各丢了一半**（同名的 `Warnings` 在两种结果里含义不同）：
	// bundle 丢 Notes —— `deno bundle` 的实验性提示写好了却没人读；
	// css 丢引擎 stderr —— postcss 的警告根本到不了用户。
	Notes []string
}

// TypeCheckOptions 是类型检查的输入选项。
type TypeCheckOptions struct {
	// TSConfig 是 tsconfig 路径；空表示由引擎自行发现。
	TSConfig string
	// Extra 是引擎特有的附加选项。
	Extra map[string]any
}

// TypeCheckResult 是类型检查的产物。
type TypeCheckResult struct {
	// Diagnostics 是引擎报告的问题（**原始文本**，不转述）。
	Diagnostics []string
}

// TypeDeclOptions 是类型声明生成的输入选项。
type TypeDeclOptions struct {
	// OutDir 是声明文件的输出目录。
	OutDir string
	// Extra 是引擎特有的附加选项。
	Extra map[string]any
}

// TypeDeclResult 是类型声明生成的产物。
type TypeDeclResult struct {
	// Files 是生成的声明文件路径。
	Files []string
}

// CSSOptions 是 CSS 编译的输入选项。
type CSSOptions struct {
	// Outfile 是输出文件；空表示产出到 stdout。
	Outfile string
	// Minify 是否压缩。
	Minify bool
	// Extra 是引擎特有的附加选项。
	Extra map[string]any
}

// CSSResult 是 CSS 编译的产物。
type CSSResult struct {
	// Code 是产物内容；--outfile 给出时为空。
	Code []byte
	// Outfile 是实际写出的文件路径。
	Outfile string
	// Warnings 是**引擎自己**写到 stderr 的诊断，原样转述。
	Warnings []string
	// Notes 是 **ngm 自己**要说的话（例如某个选项被忽略及其原因）。
	//
	// 存在的理由：有些引擎做不到某件事（postcss 没有内建压缩），
	// 静默忽略用户传的 flag 会让人以为压缩生效了。宁可明说。
	//
	// 此前 ngm 的说明被塞进了 `Warnings`，而引擎的 stderr 被丢掉——
	// 两者含义不同，混用之后一半信息必然丢失。见 BundleResult 上的同名字段。
	Notes []string
}

// ---------------------------------------------------------------------------
// 错误
// ---------------------------------------------------------------------------

// EngineError 是引擎失败的统一表达（modules/p4-ecosystem.md §错误格式）。
//
// 字段与协议一致，另加一个 Hint：P4 的 struct 是协议最小集，
// ngm 在此之上补一条可操作的下一步建议（安装方式 / 如何排查），
// 因为"引擎退出码 127"本身不构成提示。
type EngineError struct {
	// Code 是引擎进程的退出码；无法启动时为 -1。
	Code int
	// Message 是一句话描述。
	Message string
	// Stderr 是引擎原始 stderr，**保留原文不做改写**。
	//
	// 刻意保留：引擎自己的诊断（esbuild 的行列号与代码片段）比 ngm 的
	// 转述有用得多。ngm 只加"哪个引擎、怎么调的"这层上下文。
	Stderr string
	// Stdout 是引擎失败时写到 **stdout** 的内容。
	//
	// 存在的理由是一个真实缺陷：类型检查器未必把诊断写在 stderr——
	// **tsc 把 `error TS2322: …` 写在 stdout**。只保留 stderr 会让失败看起来
	// 像"引擎莫名退出 1"，而类型检查器最有价值的产物恰恰就是那条诊断。
	// 协议里"stdout 是产物、stderr 是诊断"是**约定**，不是引擎的保证。
	Stdout string
	// Retryable 表示同一引擎重试可能成功（临时资源占用、用户中断等）。
	//
	// v0.1 **不**做自动重试（development/v0.1-plan.md 的全局注意事项：
	// "失败重试策略需可配置（v0.1 固定值即可，但接口留位）"），因此这个字段
	// 目前只用于把"可以重试"这件事**告诉用户**（见 AsNgmError 的 hint），
	// 而不是驱动重试循环。ngm 不猜测临时性：只有能确知的情况才置位。
	Retryable bool
	// Fallback 表示可以尝试下一个 fallback 引擎。
	Fallback bool
	// Hint 是给用户的下一步建议（ngm 侧扩展，非 P4 协议字段）。
	Hint string
}

// Error 实现 error。消息里同时给出退出码与引擎的原始输出。
func (e *EngineError) Error() string {
	msg := e.Message
	if e.Code >= 0 {
		msg += " (engine exit " + strconv.Itoa(e.Code) + ")"
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "\n" + s
	}
	if s := strings.TrimSpace(e.Stdout); s != "" {
		msg += "\n" + s
	}
	return msg
}

// Diagnostics 返回引擎本次的输出行（stdout 在前、stderr 在后，各自按行拆分）。
//
// 给 CLI 用：类型检查的"失败"是**有内容**的失败，调用方需要把引擎的诊断
// 展示在与其成功路径相同的位置，而不是让用户只看到"引擎退出 1"。
func (e *EngineError) Diagnostics() []string {
	if e == nil {
		return nil
	}
	out := stderrLines([]byte(e.Stdout))
	return append(out, stderrLines([]byte(e.Stderr))...)
}

// AsNgmError 把引擎失败映射到 ngm 的退出码契约（architecture/observability.md）。
//
//	引擎不可用（找不到可执行文件）→ 5（引擎不可用）
//	引擎运行失败（非零退出）        → 1（失败）
//
// 为什么**不**透传引擎的退出码：ngm 的 0–5 是对外契约，让引擎的私有码穿透
// 会与 ngm 自身语义冲突——某个引擎恰好用 5 表示语法错误时，读日志的人会
// 以为"引擎不可用"。引擎自己的码保留在消息与 --json 里，信息不丢。
func (e *EngineError) AsNgmError() *errs.NgmError {
	if e.Code < 0 {
		return errs.New(errs.CodeEngineNotFound, e.Message, e.Hint)
	}
	hint := e.Hint
	if hint == "" {
		hint = "fix the reported problem; `ngm <command> --dry-run` prints the resolved command"
	}
	if e.Retryable {
		// 让 Retryable 成为**给用户的信息**而不是只写不读的字段：
		// 能确定是临时性失败时，直接告诉用户"再跑一次可能就好了"。
		hint = "this looks transient — re-running may succeed; " + hint
	}
	return errs.Wrap(errs.CodeRefDrift, e.Message, hint, fmt.Errorf("engine exit %d", e.Code))
}
