package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// Plan 描述一次引擎调用**将要**执行什么。
//
// 它是 `--dry-run` 的全部输出，也是排查"参数为什么不对"最快的入口：
// 把 ngm 实际会传给引擎的 argv 摊开给用户看，比让他猜有用得多。
type Plan struct {
	// Engine 是引擎名（如 esbuild）。
	Engine string `json:"engine"`
	// Kind 是能力类别。
	Kind string `json:"kind"`
	// Adapter 是 adapter 类型。
	Adapter string `json:"adapter"`
	// Program 是可执行文件名（embed adapter 时为空）。
	Program string `json:"program,omitempty"`
	// Args 是入口参数（不含程序名，已合并 defaultOptions 与配置 options）。
	Args []string `json:"args,omitempty"`
	// ReadsStdin 为 true 表示输入走 stdin。
	ReadsStdin bool `json:"readsStdin,omitempty"`
	// Stub 为 true 表示该条目不产出产物（仅兜底 / dry-run）。
	Stub bool `json:"stub,omitempty"`
	// Builtin 为 true 表示条目来自内置清单。
	Builtin bool `json:"builtin,omitempty"`
	// Available 报告引擎当前是否可用（可执行文件是否在 PATH）。
	Available bool `json:"available"`
	// Version 是探测到的实际版本；探测失败时为空。
	Version string `json:"version,omitempty"`
}

// CommandLine 返回便于展示的一行命令。
func (p Plan) CommandLine() string {
	switch {
	case p.Program == "" && len(p.Args) == 0:
		return "(embedded)"
	case p.Program == "":
		return strings.Join(p.Args, " ")
	default:
		return strings.Join(append([]string{p.Program}, p.Args...), " ")
	}
}

// Runner 依据一次选择执行能力调用，并处理 primary → fallbacks 回退。
//
// 它是本包唯一的"执行入口"：CLI 只与 Runner 打交道，不直接构造引擎，
// 因此引擎选择的优先级、回退规则、错误映射都只有一处实现。
type Runner struct {
	catalog *Catalog
	// dir 是引擎的工作目录（通常为项目根）。相对路径参数（入口、outfile）
	// 都相对它解析。
	dir  string
	warn func(format string, args ...any)
	// beforeRun 在每次真正执行某个引擎之前调用（见 OnEngine）。
	beforeRun func(entry Entry) error
}

// NewRunner 创建执行器。dir 为空表示继承当前进程的 cwd。
func NewRunner(catalog *Catalog, dir string) *Runner {
	return &Runner{catalog: catalog, dir: dir}
}

// OnWarn 注册回退过程的说明回调（默认丢弃）。
//
// CLI 把它接到 stderr：回退是**隐式**发生的行为，用户必须能看见
// （primary 挂了却只看到 fallback 的输出，会让人误判哪个引擎在干活）。
func (r *Runner) OnWarn(fn func(format string, args ...any)) { r.warn = fn }

// OnEngine 注册"即将执行某个引擎"的回调；返回错误即中止。
//
// CLI 用它施加 `run:<engine>` 权限。调用点在 preflight 的最后一步：
// 判定发生在**真正要跑的那个引擎**上（含回退链上的每一个），而不是选择阶段——
// 选择里可能带着永远不会用到的 fallback，因为一个用不到的引擎拒绝整条命令
// 是在为难用户。返回的错误不进入回退：权限被拒是配置问题，换个引擎只会换种错法。
func (r *Runner) OnEngine(fn func(entry Entry) error) { r.beforeRun = fn }

func (r *Runner) warnf(format string, args ...any) {
	if r.warn != nil {
		r.warn(format, args...)
	}
}

// NewEngine 依据清单条目构造引擎实例。
//
// 构造**不**探测可执行文件是否在位：`ngm engines list` 需要列出所有条目并
// 逐个报告可用性，构造阶段就失败会让"哪个引擎缺了"无从得知。
// 可用性判断交给 Engine.Available。
func NewEngine(entry Entry, dir string) (Engine, error) {
	switch entry.Adapter {
	case AdapterSubprocess:
		if entry.Program == "" {
			return nil, errs.New(errs.CodeConfigInvalid,
				"engine `"+entry.Name+"` has an empty `command`", "")
		}
		return newSubprocessEngine(entry, dir), nil
	case AdapterWasm:
		if entry.Program == "" {
			return nil, errs.New(errs.CodeConfigInvalid,
				"engine `"+entry.Name+"` has an empty `command`",
				"point `command` at a WASI command module (see docs/adr/adr-011-wasm-runtime.md)")
		}
		return newWasmEngine(entry, dir), nil
	case AdapterEmbed:
		if entry.Name == SelfEngineName {
			return newSelfEngine(entry), nil
		}
		return nil, errs.New(errs.CodeEngineNotFound,
			fmt.Sprintf("no embedded engine named %q in this build", entry.Name),
			"only the `"+SelfEngineName+"` stub is embedded; declare a subprocess engine in "+FileName)
	case AdapterRemote:
		// 刻意不写"尚未实现"：那是在承诺一件事，而这里已经有结论了（ADR-013）。
		// 用户需要知道的是"这条路不会通"，以及为什么。
		return nil, errs.New(errs.CodeEngineNotFound,
			fmt.Sprintf("adapter %q is excluded by decision", entry.Adapter),
			"a remote engine would send your source off this machine, and no local check could prove "+
				"the artifact matches it - see docs/adr/adr-013-remote-adapter.md")
	default:
		return nil, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unknown adapter %q for engine %q", entry.Adapter, entry.Name), "")
	}
}

// Resolve 返回该选择的引擎链（primary 在前，fallbacks 依次在后）。
//
// 链上任一名字不在清单里都是**配置错误**（exit 3）：用户写了一个 ngm 不认识的
// 引擎名，静默忽略它会让人以为配置生效了。错误里列出该能力类别的可用引擎名。
func (r *Runner) Resolve(kind EngineKind, sel Selection) ([]Entry, error) {
	names := sel.Chain()
	if len(names) == 0 {
		return nil, errs.New(errs.CodeConfigInvalid,
			"no engine selected for "+string(kind),
			"pass --engine=<name>"+availableHint(r.catalog, kind))
	}

	out := make([]Entry, 0, len(names))
	for _, name := range names {
		entry, ok := r.catalog.Find(kind, name)
		if !ok {
			return nil, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("no `%s` engine named %q in the catalog", kind, name),
				"pick one of the known engines, or declare yours in "+FileName+
					availableHint(r.catalog, kind))
		}
		out = append(out, entry)
	}
	return out, nil
}

// availableHint 生成"可用引擎"提示（无可用引擎时不返回半句话）。
func availableHint(c *Catalog, kind EngineKind) string {
	names := c.NamesFor(kind)
	if len(names) == 0 {
		return ""
	}
	return " (available: " + strings.Join(names, ", ") + ")"
}

// Plans 返回该 kind 将尝试的命令序列，**不做任何调用**。
//
// 这是 `--dry-run` 的实现，也是 self 引擎唯一被允许出现的场合
// （ADR-005："self 只做兜底、dry-run、离线 stub"）。
func (r *Runner) Plans(kind EngineKind, sel Selection, opts any, entryFile string) ([]Plan, error) {
	chain, err := r.Resolve(kind, sel)
	if err != nil {
		return nil, err
	}

	plans := make([]Plan, 0, len(chain))
	for _, entry := range chain {
		p := Plan{
			Engine:  entry.Name,
			Kind:    string(entry.Kind),
			Adapter: string(entry.Adapter),
			Program: entry.Program,
			Stub:    entry.Stub,
			Builtin: entry.Builtin,
		}

		if entry.Adapter == AdapterSubprocess {
			merged := withSelection(entry, sel, opts)
			inv, ierr := buildInvocation(entry, buildRequest{Options: merged, EntryFile: entryFile})
			if ierr != nil {
				return nil, ierr
			}
			p.Args = inv.Args
			p.ReadsStdin = inv.ReadsStdin
		}

		if eng, eerr := NewEngine(entry, r.dir); eerr == nil {
			p.Available = eng.Available()
			if v, verr := eng.Version(); verr == nil {
				p.Version = v
			}
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// ---------------------------------------------------------------------------
// 能力调用
// ---------------------------------------------------------------------------

// Bundle 执行打包。entryFile 是入口文件（相对 Runner 的工作目录解析）。
func (r *Runner) Bundle(ctx context.Context, sel Selection, entryFile string, opts BundleOptions) (*BundleResult, error) {
	return runChain(ctx, r, KindBundle, sel,
		func(ctx context.Context, entry Entry) (*BundleResult, error) {
			eng, err := NewEngine(entry, r.dir)
			if err != nil {
				return nil, err
			}
			be, ok := eng.(BundleEngine)
			if !ok {
				return nil, notCapable(entry, "bundle")
			}
			o := withBundleSelection(entry, sel, opts)
			if perr := r.preflight(eng, entry, buildRequest{Options: o, EntryFile: entryFile}); perr != nil {
				return nil, perr
			}
			return be.Bundle(ctx, entryFile, o)
		})
}

// Transform 执行单文件转换。
func (r *Runner) Transform(ctx context.Context, sel Selection, input []byte, opts TransformOptions) (*TransformResult, error) {
	return runChain(ctx, r, KindTransform, sel,
		func(ctx context.Context, entry Entry) (*TransformResult, error) {
			eng, err := NewEngine(entry, r.dir)
			if err != nil {
				return nil, err
			}
			te, ok := eng.(TransformEngine)
			if !ok {
				return nil, notCapable(entry, "transform")
			}
			o := withTransformSelection(entry, sel, opts)
			if perr := r.preflight(eng, entry, buildRequest{Options: o}); perr != nil {
				return nil, perr
			}
			return te.Transform(ctx, input, o)
		})
}

// Check 执行类型检查。
func (r *Runner) Check(ctx context.Context, sel Selection, entryFile string, opts TypeCheckOptions) (*TypeCheckResult, error) {
	return runChain(ctx, r, KindTypeCheck, sel,
		func(ctx context.Context, entry Entry) (*TypeCheckResult, error) {
			eng, err := NewEngine(entry, r.dir)
			if err != nil {
				return nil, err
			}
			tc, ok := eng.(TypeCheckEngine)
			if !ok {
				return nil, notCapable(entry, "typeCheck")
			}
			o := withCheckSelection(entry, sel, opts)
			if perr := r.preflight(eng, entry, buildRequest{Options: o, EntryFile: entryFile}); perr != nil {
				return nil, perr
			}
			return tc.Check(ctx, entryFile, o)
		})
}

// GenerateTypeDecl 生成类型声明。
func (r *Runner) GenerateTypeDecl(ctx context.Context, sel Selection, entryFile string, opts TypeDeclOptions) (*TypeDeclResult, error) {
	return runChain(ctx, r, KindTypeDecl, sel,
		func(ctx context.Context, entry Entry) (*TypeDeclResult, error) {
			eng, err := NewEngine(entry, r.dir)
			if err != nil {
				return nil, err
			}
			td, ok := eng.(TypeDeclEngine)
			if !ok {
				return nil, notCapable(entry, "typeDecl")
			}
			o := withDeclSelection(entry, sel, opts)
			if perr := r.preflight(eng, entry, buildRequest{Options: o, EntryFile: entryFile}); perr != nil {
				return nil, perr
			}
			return td.GenerateTypeDecl(ctx, entryFile, o)
		})
}

// Compile 编译 CSS。
func (r *Runner) Compile(ctx context.Context, sel Selection, input []byte, opts CSSOptions) (*CSSResult, error) {
	return runChain(ctx, r, KindCSS, sel,
		func(ctx context.Context, entry Entry) (*CSSResult, error) {
			eng, err := NewEngine(entry, r.dir)
			if err != nil {
				return nil, err
			}
			ce, ok := eng.(CSSEngine)
			if !ok {
				return nil, notCapable(entry, "css")
			}
			o := withCSSSelection(entry, sel, opts)
			if perr := r.preflight(eng, entry, buildRequest{Options: o}); perr != nil {
				return nil, perr
			}
			return ce.Compile(ctx, input, o)
		})
}

// notCapable 报告条目与其 kind 不符（清单被手改坏时的护栏）。
func notCapable(entry Entry, want string) error {
	return errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("engine %q (kind %s) cannot provide %s", entry.Name, entry.Kind, want),
		"check the `kind` field of this entry in "+FileName)
}

// unavailableError 报告引擎不可用（未安装 / 不在 PATH）。
//
// 在 spawn 之前先问一次 Available()，是为了给出"请安装 X"这样的可操作提示，
// 而不是让 exec 返回一个含糊的 `cannot run`。竞态（探测后引擎被卸载）由
// runProcess 的启动失败路径兜住。
func unavailableError(entry Entry) *EngineError {
	target := entry.Program
	if target == "" {
		target = entry.Name
	}
	if entry.Adapter == AdapterWasm {
		// "was not found on PATH" 对 wasm 是错的措辞：模块是**文件**，
		// 按相对工作目录解析。指错地方会让用户去查 PATH，而问题在文件不在那儿。
		return &EngineError{
			Code:     -1,
			Message:  fmt.Sprintf("engine `%s` is not available (wasm module %q was not found)", entry.Name, target),
			Fallback: true,
			Hint:     "check the `command` of this entry in " + FileName + " (it is resolved relative to the project directory)",
		}
	}
	return &EngineError{
		Code:     -1,
		Message:  fmt.Sprintf("engine `%s` is not available (`%s` was not found on PATH)", entry.Name, target),
		Fallback: true,
		Hint:     installHint(entry.Name, target),
	}
}

// preflight 在真正执行前做三项判定，**顺序是刻意的**：
//
//  1. 该能力在这台引擎上是否可实现（如 esbuild 不能做类型检查）。
//     这是配置层面的永久事实——装引擎也解决不了，因此必须先说。
//  2. 引擎是否可用（可执行文件在不在 PATH）。这是环境层面，可以修。
//  3. 权限是否允许执行它（`run:<exe>`）。
//
// 反过来的话，把 esbuild 配成 typeCheck 的用户会先看到"请安装 esbuild"，
// 装完仍然失败，白跑一趟且更困惑。两类问题的退出码相同（都是 5），
// 但**哪一个先被说出来**决定了用户能不能一次修对。
//
// 权限排在最后，理由与上面一致：引擎根本没装时，正确的退出码是 5（工具缺失，
// 脚本据此区分"环境问题"与"配置问题"）。若权限先判，一个拼错的引擎名会被
// 报成"请把 run:ngm-definitely-not-a-real-engine 加进配置"——用户照做之后
// 仍然跑不了，而那条权限永远不会有用。
func (r *Runner) preflight(eng Engine, entry Entry, req buildRequest) error {
	if _, err := buildInvocation(entry, req); err != nil {
		return err
	}
	if !eng.Available() {
		return unavailableError(entry)
	}
	if r.beforeRun != nil {
		if err := r.beforeRun(entry); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 选项分层
// ---------------------------------------------------------------------------

// mergeOptionMaps 合并两组选项，b 覆盖 a（返回新 map，不改动入参）。
func mergeOptionMaps(a, b optionSet) optionSet {
	out := optionSet{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// withSelection 按选项的具体类型施加"清单默认 < 配置"的合并（供 Plans 使用）。
//
// 用类型 switch 而不是反射：5 个类型是封闭集合，穷举比反射可读，
// 且新增 kind 时编译器会提醒漏了一处。
func withSelection(entry Entry, sel Selection, opts any) any {
	switch o := opts.(type) {
	case BundleOptions:
		return withBundleSelection(entry, sel, o)
	case TransformOptions:
		return withTransformSelection(entry, sel, o)
	case TypeCheckOptions:
		return withCheckSelection(entry, sel, o)
	case TypeDeclOptions:
		return withDeclSelection(entry, sel, o)
	case CSSOptions:
		return withCSSSelection(entry, sel, o)
	default:
		return opts
	}
}

func withBundleSelection(entry Entry, sel Selection, o BundleOptions) BundleOptions {
	o.Extra = mergeOptionMaps(mergedOptions(entry, sel), o.Extra)
	return o
}

func withTransformSelection(entry Entry, sel Selection, o TransformOptions) TransformOptions {
	o.Extra = mergeOptionMaps(mergedOptions(entry, sel), o.Extra)
	return o
}

func withCheckSelection(entry Entry, sel Selection, o TypeCheckOptions) TypeCheckOptions {
	o.Extra = mergeOptionMaps(mergedOptions(entry, sel), o.Extra)
	return o
}

func withDeclSelection(entry Entry, sel Selection, o TypeDeclOptions) TypeDeclOptions {
	o.Extra = mergeOptionMaps(mergedOptions(entry, sel), o.Extra)
	return o
}

func withCSSSelection(entry Entry, sel Selection, o CSSOptions) CSSOptions {
	o.Extra = mergeOptionMaps(mergedOptions(entry, sel), o.Extra)
	return o
}

// ---------------------------------------------------------------------------
// 回退
// ---------------------------------------------------------------------------

// attempt 记录一次失败的引擎尝试。
type attempt struct {
	entry Entry
	msg   string
}

// runChain 按 primary → fallbacks 顺序执行，返回第一个成功的结果。
//
// 回退规则（architecture/engine-adapter.md §运行时选择"按顺序尝试、失败回退"）：
//
//	stub 条目              → 回退（配置在说"这里没有真实引擎"）
//	引擎不可用              → 回退
//	引擎非零退出 / 启动失败  → 回退（文档明说"失败回退"）
//	被取消（Ctrl+C）        → **不回退**（用户意图是停止）
//	非引擎错误（配置错误等）  → **不回退**（换成别的引擎只会换一种错法）
//
// 全部失败时逐条列出每次尝试的原因，而不是只报最后一个：只报最后一个会让人
// 以为"primary 没问题，是 fallback 挂了"，而真正的问题在最前面。
func runChain[T any](
	ctx context.Context,
	r *Runner,
	kind EngineKind,
	sel Selection,
	exec func(ctx context.Context, entry Entry) (T, error),
) (T, error) {
	var zero T

	chain, err := r.Resolve(kind, sel)
	if err != nil {
		return zero, err
	}

	var (
		attempts []attempt
		lastErr  error
		executed bool
	)

	for i, entry := range chain {
		res, cerr := exec(ctx, entry)
		if cerr == nil {
			return res, nil
		}

		var ee *EngineError
		if !errors.As(cerr, &ee) {
			return zero, cerr
		}
		if ee.Code >= 0 {
			executed = true
		}
		attempts = append(attempts, attempt{entry: entry, msg: firstLine(ee.Message)})
		lastErr = ee

		if !ee.Fallback {
			break
		}
		if i < len(chain)-1 {
			r.warnf("engine %s failed (%s); falling back to %s",
				entry.Name, firstLine(ee.Message), chain[i+1].Name)
		}
	}

	// 一次都没跑起来 → 5（引擎不可用）；跑过但失败 → 1（失败）
	code := errs.CodeEngineNotFound
	hint := "install one of the listed engines, or declare your own in " + FileName
	if executed {
		code = errs.CodeRefDrift
		hint = "fix the reported problem; `--dry-run` prints the exact command ngm resolved"
	}

	lines := make([]string, 0, len(attempts))
	for _, a := range attempts {
		lines = append(lines, a.entry.Name+": "+a.msg)
	}
	var msg string
	if len(lines) == 1 {
		msg = fmt.Sprintf("%s: %s", kind, lines[0])
	} else {
		msg = fmt.Sprintf("%s: all %d engines failed (%s)", kind, len(lines), strings.Join(lines, "; "))
	}

	ngmErr := errs.Wrap(code, msg, hint, lastErr)
	if executed {
		// 走的是退出码 1（与 verify 的"引用漂移"同一个数字），但这里发生的事
		// 是"引擎跑了却失败"——显示名必须说这件事，否则用户会去找漂移。
		ngmErr = ngmErr.Labeled("EngineFailed")
	}
	return zero, ngmErr
}
