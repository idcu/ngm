package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/errs"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// wasmMemoryLimitPages 是单个 wasm 引擎实例的内存上限（页，1 页 = 64 KiB）。
//
// 32 MiB：足够一个真实的转换器工作，又远小于"把机器吃光"。
// 这是 wasm adapter 相对 subprocess 的**实质优势**——Deno 沙箱没有稳定的
// 跨平台内存开关（ADR-012 决策 5 把那条记为未防护项），而这里能在**实例化阶段**
// 就拒绝超限的模块（实测：错误信息给出具体页数）。
const wasmMemoryLimitPages = 512

// wasmCompileCache 跨调用复用编译结果。
//
// 编译 wasm 是纯计算、与权限无关，因此共享是安全的；没有它，每次 bundle 都要
// 重新编译一遍模块（对反复调用的构建是明显的浪费）。
var wasmCompileCache = wazero.NewCompilationCache()

// wasmEngine 是 adapter=wasm 的驱动：在 wazero 里执行 WASI preview1 命令模块。
//
// ABI 见 ADR-011：argv 与 subprocess **同一套语义**——参数由 buildInvocation 生成，
// 本文件不重新定义任何参数含义；产物走 stdout、诊断走 stderr、退出码为成败。
// 因此同一个引擎可以两种形态提供，用户看到的选项含义不变。
//
// 与 subprocess 的三处差异都是"更少的能力"，且都不是被拒绝而是**不存在**：
//
//   - 不能派生进程（模块里没有 spawn 的导入）
//   - 不能开套接字（WASI preview1 无 socket 导入，实测：导入 sock_open 的模块被拒）
//   - 不能写文件：产物走 stdout，`--outfile` 由 ngm 代写（见 writeArtifact）
type wasmEngine struct {
	entry Entry
	// dir 是模块的工作目录，也是它唯一能读到的目录（以 `/` 预开放，只读）。
	dir string

	mu       sync.Mutex
	probed   bool
	version  string
	probeErr error
}

func newWasmEngine(entry Entry, dir string) *wasmEngine {
	return &wasmEngine{entry: entry, dir: dir}
}

// Name 实现 Engine。
func (e *wasmEngine) Name() string { return e.entry.Name }

// Kind 实现 Engine。
func (e *wasmEngine) Kind() EngineKind { return e.entry.Kind }

// Available 实现 Engine：模块文件存在且不是目录。
//
// 与 subprocess 的差异只在"什么算可用"：那里查 PATH，这里查文件。
// 语义保持一致——**清单表达意图，可用性必须真的探测**。
func (e *wasmEngine) Available() bool {
	path := e.modulePath()
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Version 实现 Engine：跑一次 `--version`。
//
// 与 subprocess 同一套约定（把 `--version` 追加到固定前缀参数后），因为 ABI 相同：
// 一个模块若不能在 `--version` 下报版本，那它对其他参数也不会有什么作为。
func (e *wasmEngine) Version() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.probed {
		return e.version, e.probeErr
	}
	e.probed = true

	if !e.Available() {
		e.probeErr = &EngineError{
			Code:     -1,
			Message:  fmt.Sprintf("wasm module %q does not exist", e.modulePath()),
			Fallback: true,
			Hint:     "check the `command` of this entry in " + FileName,
		}
		return "", e.probeErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	args := append(append([]string{}, e.entry.Args...), "--version")
	res, err := e.run(ctx, args, nil)
	if err != nil {
		e.probeErr = err
		return "", err
	}
	e.version = firstLine(string(res.Stdout))
	if e.version == "" {
		e.version = firstLine(string(res.Stderr))
	}
	return e.version, nil
}

// Bundle 实现 BundleEngine。
func (e *wasmEngine) Bundle(ctx context.Context, entry string, opts BundleOptions) (*BundleResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts, EntryFile: entry})
	if err != nil {
		return nil, err
	}
	res, err := e.run(ctx, e.entryArgs(inv), nil)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	out := &BundleResult{Warnings: stderrLines(res.Stderr), Notes: inv.Notes}
	if opts.Outfile != "" {
		if werr := e.writeArtifact(opts.Outfile, res.Stdout); werr != nil {
			return nil, werr
		}
		out.Outfile = opts.Outfile
	} else {
		out.Code = res.Stdout
	}
	return out, nil
}

// Transform 实现 TransformEngine。输入走 stdin（与 subprocess 协议一致）。
func (e *wasmEngine) Transform(ctx context.Context, input []byte, opts TransformOptions) (*TransformResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts})
	if err != nil {
		return nil, err
	}
	res, err := e.run(ctx, e.entryArgs(inv), stdinFor(inv, input))
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	return &TransformResult{Code: res.Stdout, Warnings: stderrLines(res.Stderr)}, nil
}

// Check 实现 TypeCheckEngine。
func (e *wasmEngine) Check(ctx context.Context, entry string, opts TypeCheckOptions) (*TypeCheckResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts, EntryFile: entry})
	if err != nil {
		return nil, err
	}
	res, err := e.run(ctx, e.entryArgs(inv), stdinFor(inv, nil))
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	lines := stderrLines(res.Stderr)
	lines = append(lines, stderrLines(res.Stdout)...)
	return &TypeCheckResult{Diagnostics: lines}, nil
}

// GenerateTypeDecl 实现 TypeDeclEngine。
//
// 声明文件由模块写到 stdout？不行——多文件无法用 stdout 表达。因此 typeDecl 在
// wasm 形态下**只支持产物落到 stdout 的模块**：ngm 把 stdout 当作声明内容写到
// OutDir 下模块自己给出的文件名是做不到的，所以这里要求模块用 `--outfile` 之外
// 的约定：**模块把文件名与内容按 JSON 行输出**。
//
// 这个约定看起来别扭，但它比"给模块写权限"划算得多：一旦允许写盘，vendor 与
// 项目目录的完整性就不再由 ngm 掌握（ADR-011 决策 3）。真需要多文件输出的引擎
// 应当用 subprocess 形态——那里它能写，且沙箱的边界由操作系统划定。
func (e *wasmEngine) GenerateTypeDecl(ctx context.Context, entry string, opts TypeDeclOptions) (*TypeDeclResult, error) {
	return nil, errs.New(errs.CodeConfigInvalid,
		"the wasm adapter cannot emit declaration files",
		"declaration output is multi-file and needs write access, which the wasm sandbox does not grant; "+
			"declare a `subprocess` engine for typeDecl (see "+FileName+")")
}

// Compile 实现 CSSEngine。
func (e *wasmEngine) Compile(ctx context.Context, input []byte, opts CSSOptions) (*CSSResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts})
	if err != nil {
		return nil, err
	}
	res, err := e.run(ctx, e.entryArgs(inv), stdinFor(inv, input))
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	// 与 subprocess 适配器保持同一套含义：Warnings 是引擎的 stderr，Notes 是 ngm 的说明
	out := &CSSResult{Warnings: stderrLines(res.Stderr), Notes: inv.Notes}
	if opts.Outfile != "" {
		if werr := e.writeArtifact(opts.Outfile, res.Stdout); werr != nil {
			return nil, werr
		}
		out.Outfile = opts.Outfile
	} else {
		out.Code = res.Stdout
	}
	return out, nil
}

// modulePath 返回模块文件路径（相对工作目录解析）。
func (e *wasmEngine) modulePath() string {
	p := strings.TrimSpace(e.entry.Program)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) || e.dir == "" {
		return p
	}
	return filepath.Join(e.dir, filepath.FromSlash(p))
}

// entryArgs 把清单里的固定前缀参数拼到本次调用的参数前面。
//
// 与 subprocess 侧同一规则（buildInvocation 的收尾也做同样的事）：两者必须一致，
// 否则同一个引擎在两种形态下 argv 会不同，而 ADR-011 的承诺正是"契约不变"。
func (e *wasmEngine) entryArgs(inv invocation) []string {
	return append(append([]string{}, e.entry.Args...), inv.Args...)
}

// writeArtifact 把模块的 stdout 落盘。
//
// **由 ngm 写，不是模块写**：沙箱不给模块任何写权限（ADR-011 决策 3），
// 而用户给了 `--outfile` 就期待文件出现。让 ngm 代写同时满足两件事——
// 权限边界不变，且 `--outfile` 的含义与 subprocess 形态一致。
func (e *wasmEngine) writeArtifact(outfile string, data []byte) error {
	path := outfile
	if !filepath.IsAbs(path) && e.dir != "" {
		path = filepath.Join(e.dir, filepath.FromSlash(outfile))
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return errs.Wrap(errs.CodeEngineNotFound, "create "+dir, "", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return errs.Wrap(errs.CodeEngineNotFound, "write "+outfile, "", err)
	}
	return nil
}

// run 执行一次模块调用，把失败映射为 *EngineError（与 subprocess 同一套契约）。
//
// args[0] 是模块路径（与子进程的 argv[0] 是程序名对应）；`--` 之后的参数
// 由调用方给出，含义与 subprocess 完全一致。
func (e *wasmEngine) run(ctx context.Context, args []string, stdin []byte) (*runResult, error) {
	modulePath := e.modulePath()
	if modulePath == "" {
		return nil, &EngineError{
			Code: -1, Fallback: true,
			Message: "wasm entry has an empty `command`",
			Hint:    "set `command` to the path of a .wasm module in " + FileName,
		}
	}

	bin, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, &EngineError{
			Code: -1, Fallback: true,
			Message: fmt.Sprintf("cannot read wasm module %q", modulePath),
			Hint:    "check the `command` of this entry in " + FileName,
		}
	}

	rtCfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(wasmMemoryLimitPages).
		WithCompilationCache(wasmCompileCache)
	rt := wazero.NewRuntimeWithConfig(ctx, rtCfg)
	defer func() { _ = rt.Close(context.Background()) }()

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, &EngineError{
			Code: -1, Fallback: true,
			Message: "cannot initialise the WASI host",
			Hint:    "this is an ngm bug; please report it with the module path",
		}
	}

	// 先用 ctx 编译：编译本身也可能被取消（大模块）。
	compiled, err := rt.CompileModule(ctx, bin)
	if err != nil {
		return nil, &EngineError{
			Code: -1, Fallback: true,
			Message: fmt.Sprintf("%s is not a valid wasm module", filepath.Base(modulePath)),
			Stderr:  err.Error(),
			Hint:    "ngm runs WASI preview1 command modules (ADR-011); check how the module was built",
		}
	}

	var stdout, stderr bytes.Buffer
	mc := wazero.NewModuleConfig().
		WithArgs(args...).
		WithStdout(&stdout).
		WithStderr(&stderr).
		// 只读预开放工作目录：模块需要读入口文件与它引用的文件，但没有任何
		// 理由写盘。写权限在类型层面就不给（ngm 自己代写产物）。
		WithFSConfig(wazero.NewFSConfig().WithReadOnlyDirMount(e.dir, "/")).
		WithName(e.entry.Name)
	if len(stdin) > 0 {
		mc = mc.WithStdin(bytes.NewReader(stdin))
	}

	res := &runResult{}
	done := make(chan error, 1)
	go func() {
		_, runErr := rt.InstantiateModule(ctx, compiled, mc)
		done <- runErr
	}()

	var runErr error
	select {
	case runErr = <-done:
	case <-ctx.Done():
		// 取消（Ctrl+C / 超时）：关闭运行时会中断在执行的模块。
		_ = rt.Close(context.Background())
		runErr = <-done
	}

	res.Stdout = stdout.Bytes()
	res.Stderr = stderr.Bytes()

	if ctx.Err() != nil {
		return res, &EngineError{
			Code:      -1,
			Message:   fmt.Sprintf("%s was cancelled (%v)", e.entry.Name, ctx.Err()),
			Retryable: true,
			Fallback:  false,
		}
	}

	if runErr == nil {
		return res, nil
	}

	var exitErr *sys.ExitError
	if errors.As(runErr, &exitErr) {
		code := int(exitErr.ExitCode())
		res.ExitCode = code
		if code == 0 {
			return res, nil
		}
		return res, &EngineError{
			Code: code,
			// Message 里点明是 wasm：用户看到"exit 1"时需要知道自己在看什么。
			Message: fmt.Sprintf("%s (wasm) failed", e.entry.Name),
			// stdout 一并保留：协议说"诊断走 stderr"，但类型检查器常把诊断写在
			// stdout（subprocess 侧为此专门保留过 Stdout）。
			Stdout:   string(res.Stdout),
			Stderr:   string(res.Stderr),
			Fallback: true,
		}
	}

	// trap / 实例化失败（缺导入、内存超限、栈溢出）
	return res, &EngineError{
		Code:     -1,
		Message:  fmt.Sprintf("%s (wasm) trapped", e.entry.Name),
		Stdout:   string(res.Stdout),
		Stderr:   strings.TrimSpace(runErr.Error()),
		Fallback: true,
	}
}
