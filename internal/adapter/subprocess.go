package adapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// versionProbeTimeout 限制版本探测的耗时。
//
// `ngm engines list` 会对每个条目探测版本；一个卡住的外部命令不能让
// 整个命令挂死（引擎由用户安装，质量不可控）。
const versionProbeTimeout = 5 * time.Second

// subprocessEngine 是 adapter=subprocess 的**通用驱动**。
//
// 一个实现驱动所有外部 CLI：清单提供"程序名 + 固定前缀参数"，可变参数由
// 按引擎名分派的 builder 生成（见 builders）。因此用户自定义引擎
// （modules/p4-ecosystem.md §用户自定义引擎）无需改 ngm 代码即可接入。
//
// 它同时实现全部 5 个能力接口——因为"调用外部命令"这件事与能力类别无关，
// 差异只在 argv 翻译。Runner 会把实例断言到目标能力接口后再调用，
// 断言不成立（如条目 kind 与调用不符）由 Runner 报错。
type subprocessEngine struct {
	entry Entry
	// dir 是引擎的工作目录（通常是项目根）。相对参数（outfile / 入口）
	// 都相对它解析，使"在哪个目录构建"成为显式信息。
	dir string

	mu       sync.Mutex
	probed   bool
	version  string
	probeErr error
}

func newSubprocessEngine(entry Entry, dir string) *subprocessEngine {
	return &subprocessEngine{entry: entry, dir: dir}
}

// Name 实现 Engine。
func (e *subprocessEngine) Name() string { return e.entry.Name }

// Kind 实现 Engine。
func (e *subprocessEngine) Kind() EngineKind { return e.entry.Kind }

// Available 实现 Engine：真的去 PATH 里找可执行文件。
//
// 刻意不缓存：用户在一次会话中装好引擎（或修正 PATH）后，下一次调用
// 应当立即生效。探测本身只是几次 stat，代价可忽略。
func (e *subprocessEngine) Available() bool {
	if e.entry.Program == "" {
		return false
	}
	_, err := exec.LookPath(e.entry.Program)
	return err == nil
}

// Version 实现 Engine：执行 `<program> <固定前缀参数> --version` 并取第一行。
//
// 结果在进程内缓存（多次询问只探测一次），并带超时。
func (e *subprocessEngine) Version() (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.probed {
		return e.version, e.probeErr
	}
	e.probed = true

	if !e.Available() {
		e.probeErr = &EngineError{
			Code:     -1,
			Message:  fmt.Sprintf("engine `%s` is not on PATH", e.entry.Program),
			Fallback: true,
			Hint:     installHint(e.entry.Name, e.entry.Program),
		}
		return "", e.probeErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	args := append(append([]string{}, e.entry.Args...), "--version")
	res, err := runProcess(ctx, e.entry.Program, args, e.dir, nil, nil)
	if err != nil {
		e.probeErr = err
		return "", err
	}
	if res.ExitCode != 0 {
		e.probeErr = &EngineError{
			Code:    res.ExitCode,
			Message: fmt.Sprintf("%s --version exited %d", e.entry.Program, res.ExitCode),
			Stderr:  string(res.Stderr),
		}
		return "", e.probeErr
	}

	// esbuild / tsc / deno 都把版本号作为 stdout 的第一行
	e.version = firstLine(string(res.Stdout))
	if e.version == "" {
		// 少数工具把版本写到 stderr
		e.version = firstLine(string(res.Stderr))
	}
	return e.version, nil
}

// Bundle 实现 BundleEngine。
func (e *subprocessEngine) Bundle(ctx context.Context, entry string, opts BundleOptions) (*BundleResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts, EntryFile: entry})
	if err != nil {
		return nil, err
	}
	env := e.envFor(opts.Production)
	res, err := runProcess(ctx, e.entry.Program, inv.Args, e.dir, nil, env)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	out := &BundleResult{Warnings: stderrLines(res.Stderr), Notes: inv.Notes}
	if opts.Outfile != "" {
		out.Outfile = opts.Outfile
	} else {
		out.Code = res.Stdout
	}
	return out, nil
}

// Transform 实现 TransformEngine。输入走 stdin（小文件），避免临时文件。
func (e *subprocessEngine) Transform(ctx context.Context, input []byte, opts TransformOptions) (*TransformResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts})
	if err != nil {
		return nil, err
	}
	res, err := runProcess(ctx, e.entry.Program, inv.Args, e.dir, stdinFor(inv, input), nil)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	return &TransformResult{Code: res.Stdout, Warnings: stderrLines(res.Stderr)}, nil
}

// Check 实现 TypeCheckEngine。
//
// 大部分类型检查器用**退出码**表达"有没有类型错误"（0 = 干净），
// 因此这里成功即返回空诊断；诊断文本来自 stdout+stderr。
func (e *subprocessEngine) Check(ctx context.Context, entry string, opts TypeCheckOptions) (*TypeCheckResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts, EntryFile: entry})
	if err != nil {
		return nil, err
	}
	res, err := runProcess(ctx, e.entry.Program, inv.Args, e.dir, stdinFor(inv, nil), nil)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	lines := stderrLines(res.Stderr)
	lines = append(lines, stderrLines(res.Stdout)...)
	return &TypeCheckResult{Diagnostics: lines}, nil
}

// GenerateTypeDecl 实现 TypeDeclEngine。
//
// 引擎把 .d.ts 写到 OutDir，ngm 只报告**实际出现的文件**——
// 不猜文件名（引擎的命名规则是它自己的事）。
func (e *subprocessEngine) GenerateTypeDecl(ctx context.Context, entry string, opts TypeDeclOptions) (*TypeDeclResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts, EntryFile: entry})
	if err != nil {
		return nil, err
	}
	res, err := runProcess(ctx, e.entry.Program, inv.Args, e.dir, stdinFor(inv, nil), nil)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	var files []string
	if opts.OutDir != "" {
		files, err = listFiles(e.dir, opts.OutDir)
		if err != nil {
			return nil, err
		}
	}
	_ = res
	return &TypeDeclResult{Files: files}, nil
}

// Compile 实现 CSSEngine。
func (e *subprocessEngine) Compile(ctx context.Context, input []byte, opts CSSOptions) (*CSSResult, error) {
	inv, err := buildInvocation(e.entry, buildRequest{Options: opts})
	if err != nil {
		return nil, err
	}
	res, err := runProcess(ctx, e.entry.Program, inv.Args, e.dir, stdinFor(inv, input), nil)
	if err != nil {
		return nil, annotate(err, e.entry, inv.Args)
	}
	// 两条通道**都要**：引擎的 stderr 此前被整个丢掉（postcss 的警告到不了用户），
	// 而 ngm 的说明此前借用了 Warnings 这个字段名。
	out := &CSSResult{Warnings: stderrLines(res.Stderr), Notes: inv.Notes}
	if opts.Outfile != "" {
		out.Outfile = opts.Outfile
	} else {
		out.Code = res.Stdout
	}
	return out, nil
}

// envFor 返回随本次调用追加的环境变量。
//
// 只加一个：`--production` 时同时给出 NODE_ENV，让引擎内部（以及被它加载的
// 插件）与命令行开关看到一致的世界观。esbuild 的 define 只影响被 bundle 的
// 源码，不等于引擎自身进程的环境。
func (e *subprocessEngine) envFor(production bool) []string {
	if !production {
		return nil
	}
	return []string{"NODE_ENV=production"}
}

// stdinFor 把输入注入 invocation：仅当引擎从 stdin 读时才传。
func stdinFor(inv invocation, input []byte) []byte {
	if !inv.ReadsStdin {
		return nil
	}
	return input
}

// annotate 给引擎失败补上"哪个引擎、怎么调的"上下文。
//
// 原始 stderr 原样保留（EngineError.Stderr），只在 Message 前面加上引擎名，
// 使用户一眼看出失败发生在 primary 还是 fallback 上。
func annotate(err error, entry Entry, args []string) error {
	var ee *EngineError
	if !errors.As(err, &ee) {
		return err
	}
	if !strings.HasPrefix(ee.Message, entry.Name) {
		ee.Message = entry.Key() + ": " + ee.Message
	}
	if ee.Hint == "" {
		ee.Hint = "resolved command: `" + strings.Join(append([]string{entry.Program}, args...), " ") + "`"
	}
	return ee
}

// ---------------------------------------------------------------------------
// 子进程执行
// ---------------------------------------------------------------------------

// runResult 是一次子进程执行的结果。
type runResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// runProcess 执行一次引擎命令，把失败映射为 *EngineError。
//
// 输入约定（modules/p4-ecosystem.md §subprocess 协议）：小文件走 stdin，
// 大文件/多文件走命令行路径参数；产物走 stdout，诊断走 stderr，退出码为成败。
func runProcess(ctx context.Context, program string, args []string, dir string, stdin []byte, extraEnv []string) (*runResult, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), extraEnv...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &runResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}
	if err == nil {
		return res, nil
	}

	// 被取消（Ctrl+C / 超时）：**不**回退到下一个引擎——用户意图是停止，
	// 换成另一个引擎继续跑会让人以为取消无效。
	if ctx.Err() != nil {
		return res, &EngineError{
			Code:      -1,
			Message:   fmt.Sprintf("%s was cancelled (%v)", program, ctx.Err()),
			Retryable: true,
			Fallback:  false,
		}
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, &EngineError{
			Code:    res.ExitCode,
			Message: fmt.Sprintf("%s failed", program),
			// stdout 一并保留：协议说"诊断走 stderr"，但 tsc 把类型错误写在
			// stdout。丢掉它等于把类型检查的产物扔掉。
			Stdout:   string(stdout.Bytes()),
			Stderr:   string(stderr.Bytes()),
			Fallback: true,
		}
	}

	// 启动类失败（找不到可执行文件、无执行权限）
	var execErr *exec.Error
	var pathErr *os.PathError
	if errors.As(err, &execErr) || errors.As(err, &pathErr) {
		return res, &EngineError{
			Code:     -1,
			Message:  fmt.Sprintf("cannot run `%s`", program),
			Fallback: true,
			Hint:     "install it and make sure it is on PATH, then re-run",
		}
	}
	return res, &EngineError{Code: -1, Message: "cannot run `" + program + "`", Fallback: true}
}

// installHint 给出可操作的安装建议。
//
// 引擎缺失是唯一会让用户"卡住"的失败模式，因此每个已知引擎都给具体命令，
// 而不是笼统的"请安装引擎"。
func installHint(name, program string) string {
	switch name {
	case "esbuild":
		return "install it with `npm i -g esbuild`, then re-run"
	case "typescript":
		return "install it with `npm i -g typescript`, then re-run"
	case "postcss":
		return "install it with `npm i -g postcss-cli`, then re-run"
	case "deno":
		return "install Deno (https://deno.com), then re-run"
	default:
		return fmt.Sprintf("install `%s` and make sure it is on PATH, then re-run", program)
	}
}

// firstLine 返回文本的第一行（去空白）。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// stderrLines 把诊断文本按行拆分（去掉空行）。
//
// 保留原文而不是转述：引擎的诊断格式（esbuild 带行列号与代码片段）
// 比 ngm 的复述有用得多。
func stderrLines(b []byte) []string {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimRight(line, " \t"); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// listFiles 列出 dir 下的所有文件，返回**相对 base** 的路径（`/` 分隔，字节序排序）。
//
// 为什么必须给 base：`dir` 来自配置或命令行，是**相对项目目录**的（引擎就在项目目录里
// 跑，它写出来的文件也在那儿），而本进程的 CWD 未必是项目目录。按 CWD 去找的后果不是
// 报错，而是"文件明明写出来了、却说没有产出"——这与 wasm 清单校验曾按 CWD 找模块
// 是同一类错。
//
// 返回相对 base 的路径而不是相对 dir：用户要看的是"文件在项目里的哪儿"。
//
// 目录不存在时返回空列表而不是错误：引擎"没产出任何声明"是一种合法结果，
// 报告比报错更有用（调用方据此给出"没有生成 .d.ts"的提示）。
func listFiles(base, dir string) ([]string, error) {
	root := dir
	if base != "" && !filepath.IsAbs(root) {
		root = filepath.Join(base, filepath.FromSlash(dir))
	}
	relBase := root
	if base != "" {
		relBase = base
	}

	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(relBase, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeEngineNotFound, "list "+dir, "", err)
	}
	sort.Strings(out)
	return out, nil
}

// ---------------------------------------------------------------------------
// 选项 → argv
// ---------------------------------------------------------------------------

// optionSet 是最终要翻译成 `--key=value` 的选项集合。
//
// 输出按键**排序**，使同一份输入永远产生同一条 argv——
// 这是 --dry-run 可快照、失败可复现的前提。
type optionSet map[string]any

// set 显式设置（覆盖已有值）。
func (s optionSet) set(k string, v any) { s[k] = v }

// flagArgs 把选项渲染为 `--key=value`（bool true → `--key`，false/null → 跳过）。
func (s optionSet) flagArgs() []string {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		v := s[k]
		if b, ok := v.(bool); ok {
			if b {
				out = append(out, "--"+k)
			}
			continue
		}
		if v == nil {
			continue
		}
		out = append(out, "--"+k+"="+scalarString(v))
	}
	return out
}

// scalarString 把选项值渲染为命令行字面量。
//
// 字符串**原样**输出（不加引号），因此调用方要自己带上需要的引号——
// 例如 esbuild 的 `--define:process.env.NODE_ENV="production"` 必须把
// 双引号作为参数内容的一部分。不经过 shell 正是为了让这件事可控。
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// JSON 数字统一是 float64；整数不要渲染成 1.0
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return fmt.Sprint(v)
	}
}

// mergedOptions 合并两层选项：清单 defaultOptions < 配置 options。
//
// 显式传入的 Extra 与命令行 typed 字段由 applyXxx 与 builder 依次覆盖，
// 因此完整优先级是：
//
//	entry.defaultOptions < ngm.json engines.<kind>.options < 调用参数 < 命令行 flag
func mergedOptions(entry Entry, sel Selection) optionSet {
	out := optionSet{}
	for k, v := range entry.DefaultOptions {
		out[k] = v
	}
	for k, v := range sel.Options {
		out[k] = v
	}
	return out
}

// invocation 是一次引擎调用的完整描述。
type invocation struct {
	// Args 是入口参数（不含程序名）。
	Args []string
	// ReadsStdin 为 true 表示引擎从 stdin 读输入（内容由能力方法注入）。
	//
	// 用标记而不是直接放字节：builder 只负责"怎么调"，输入内容由能力方法
	// 提供，两者分离才能让 --dry-run 复用同一份 argv（dry-run 没有输入）。
	ReadsStdin bool
	// Notes 是 builder 想告诉用户的说明（例如本次忽略了某个选项及原因）。
	Notes []string
}

// buildRequest 汇总一次 argv 生成的输入。
//
// 入口文件刻意**不**放进选项结构体：接口参数已经携带它（`Bundle(ctx, entry, opts)`），
// 再往 opts 里放一份就会出现"两个来源、谁赢"的问题。
type buildRequest struct {
	// Options 是该 kind 的选项：BundleOptions / TransformOptions / …
	Options any
	// EntryFile 是 bundle / typeCheck / typeDecl 的入口文件。
	EntryFile string
}

// builders 按引擎名分派 argv 翻译。
//
// 未登记的引擎名落到 generic（P4 的 subprocess 协议最小集），
// 这就是"用户自定义引擎无需改 ngm 代码即可接入"的实现方式。
var builders = map[string]func(Entry, buildRequest) (invocation, error){
	"esbuild":    esbuildInvocation,
	"typescript": typescriptInvocation,
	"deno":       denoInvocation,
	"postcss":    postcssInvocation,
}

// buildInvocation 生成一次调用的 argv。
//
// Options 必须是该 kind 对应的类型，由能力方法保证；类型不符返回配置错误
// 而不是 panic。清单里的固定前缀参数（command 除程序名之外的部分）永远在最前。
func buildInvocation(entry Entry, req buildRequest) (invocation, error) {
	fn, ok := builders[entry.Name]
	if !ok {
		fn = genericInvocation
	}
	inv, err := fn(entry, req)
	if err != nil {
		return inv, err
	}
	if len(entry.Args) > 0 {
		inv.Args = append(append([]string{}, entry.Args...), inv.Args...)
	}
	return inv, nil
}

// esbuildInvocation 生成 esbuild 的参数。
//
// 为什么 esbuild 需要专门的分派（architecture/engine-adapter.md §诚实说明 2
// "抽象过深有代价"）：它的选项名与语义是它自己的（--bundle / --alias: /
// --define:），硬套一套"通用"映射只会丢掉表达力。
func esbuildInvocation(entry Entry, req buildRequest) (invocation, error) {
	switch o := req.Options.(type) {
	case BundleOptions:
		return esbuildBundle(entry, o, req.EntryFile)
	case TransformOptions:
		return esbuildTransform(entry, o)
	case CSSOptions:
		return esbuildCSS(entry, o)
	case TypeCheckOptions:
		// esbuild **不做类型检查**：它只是把类型标注删掉。
		// 静默地"当作通过"是最危险的行为（CI 会以为类型是干净的），
		// 因此明确拒绝，并指出该换成哪个引擎。
		//
		// v0.5 复核修正：这里曾写"tsc / deno are not adapted in this build"，
		// 而 `typescript`（tsc）自 v0.2 起就在内置清单里——那句话把可用的引擎
		// 说成了不可用，用户照做会去自己声明一个本来已经内置的引擎。
		return invocation{}, errs.New(errs.CodeEngineNotFound,
			"esbuild does not type-check: it only strips type annotations",
			"use the built-in `typescript` engine instead: pass --engine=typescript, "+
				"or set \"typeCheck\": \"typescript\" in "+ProjectFileName+
				" (see modules/p4-ecosystem.md)")
	case TypeDeclOptions:
		return invocation{}, errs.New(errs.CodeEngineNotFound,
			"esbuild cannot emit .d.ts declarations",
			"declare a `typeDecl` engine in "+FileName+" (see modules/p4-ecosystem.md)")
	default:
		return invocation{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unsupported options type %T for esbuild", req.Options), "")
	}
}

// esbuildBundle 生成 bundle 的 argv。
func esbuildBundle(entry Entry, o BundleOptions, entryFile string) (invocation, error) {
	if strings.TrimSpace(entryFile) == "" {
		return invocation{}, errs.New(errs.CodeConfigInvalid, "bundle needs an entry file", "")
	}

	m := optionSet{}
	for k, v := range o.Extra {
		m.set(k, v)
	}
	// typed 字段（来自命令行）覆盖配置项
	if o.Target != "" {
		m.set("target", o.Target)
	}
	if o.Format != "" {
		m.set("format", o.Format)
	}
	if o.Platform != "" {
		m.set("platform", o.Platform)
	}
	if o.Minify || o.Production {
		m.set("minify", true)
	}
	if o.Production {
		// esbuild 的 --define 需要 JSON 字面量，因此值本身带双引号
		m.set("define:process.env.NODE_ENV", `"production"`)
	}

	args := []string{"--bundle"}
	if o.Outfile != "" {
		args = append(args, "--outfile="+o.Outfile)
	}
	args = append(args, m.flagArgs()...)

	// mappings → --alias:把 `github:org/repo` 指到 vendor 路径
	aliases := make([]string, 0, len(o.Alias))
	for k := range o.Alias {
		aliases = append(aliases, k)
	}
	sort.Strings(aliases)
	for _, k := range aliases {
		if v := strings.TrimSpace(o.Alias[k]); v != "" {
			args = append(args, "--alias:"+k+"="+v)
		}
	}

	// 入口放最后：esbuild 把位置参数一律当作 entry point
	args = append(args, entryFile)
	return invocation{Args: args}, nil
}

// esbuildTransform 生成单文件转换的 argv（走 stdin，避免临时文件）。
func esbuildTransform(entry Entry, o TransformOptions) (invocation, error) {
	m := optionSet{}
	for k, v := range o.Extra {
		m.set(k, v)
	}
	if o.Loader != "" {
		m.set("loader", o.Loader)
	}
	if o.Target != "" {
		m.set("target", o.Target)
	}
	if o.Format != "" {
		m.set("format", o.Format)
	}
	if o.Minify {
		m.set("minify", true)
	}
	if o.SourceMaps {
		// stdout 输出不允许外链 source map，只能用 inline
		m.set("sourcemap", "inline")
	}
	if m["loader"] == nil {
		return invocation{}, errs.New(errs.CodeConfigInvalid,
			"transform needs a `loader` option (ts / tsx / js / jsx) when reading from stdin",
			"pass --loader=<ext>, or set engines.transform.options.loader in ngm.json")
	}
	return invocation{Args: m.flagArgs(), ReadsStdin: true}, nil
}

// esbuildCSS 生成 CSS 编译的 argv（走 stdin）。
func esbuildCSS(entry Entry, o CSSOptions) (invocation, error) {
	m := optionSet{}
	for k, v := range o.Extra {
		m.set(k, v)
	}
	if o.Minify {
		m.set("minify", true)
	}
	// esbuild 需要 --loader=css 才知道 stdin 的语法
	m.set("loader", "css")

	args := m.flagArgs()
	if o.Outfile != "" {
		args = append(args, "--outfile="+o.Outfile)
	}
	return invocation{Args: args, ReadsStdin: true}, nil
}

// genericInvocation 是**未登记引擎**（用户自定义）的 argv 翻译。
//
// 覆盖 modules/p4-ecosystem.md §subprocess 协议 中可确定的部分，并把
// ngm 侧的具体映射固定下来（README 中同步写明）：
//
//	输入    bundle/typeCheck/typeDecl 用位置参数传文件路径；
//	        transform/CSS 走 stdin（加 `--stdin`）
//	类别    `--kind=<transform|bundle|typeCheck|typeDecl|css>`
//	选项    `--<key>=<value>`，按键排序（argv 因此可复现）
//	输出    `--outfile=<path>`；stdout 为产物，stderr 为诊断，退出码为成败
//
// 为什么需要 `--kind`：一个二进制可以同时注册多条清单条目（esbuild 自己
// 就同时提供 transform 与 bundle）。没有这个参数，自定义引擎无从判断这次
// 该做哪件事——它会看到一个入口文件和一个 outfile，猜不出来。
func genericInvocation(entry Entry, req buildRequest) (invocation, error) {
	m := optionSet{}
	entryFile := req.EntryFile
	readsStdin := false
	outfile := ""

	switch o := req.Options.(type) {
	case BundleOptions:
		outfile = o.Outfile
		m.set("kind", string(KindBundle))
		for k, v := range o.Extra {
			m.set(k, v)
		}
		if o.Target != "" {
			m.set("target", o.Target)
		}
		if o.Format != "" {
			m.set("format", o.Format)
		}
		if o.Minify || o.Production {
			m.set("minify", true)
		}
		alias := make([]string, 0, len(o.Alias))
		for k := range o.Alias {
			alias = append(alias, k)
		}
		sort.Strings(alias)
		for _, k := range alias {
			m.set("alias:"+k, o.Alias[k])
		}
	case TransformOptions:
		readsStdin = true
		m.set("kind", string(KindTransform))
		for k, v := range o.Extra {
			m.set(k, v)
		}
		if o.Loader != "" {
			m.set("loader", o.Loader)
		}
		if o.Target != "" {
			m.set("target", o.Target)
		}
		// Format / Minify / SourceMaps 必须转发，与上面 Bundle 分支、下面 CSS 分支一致。
		//
		// v0.5 修正：这里此前**只**转发 loader 与 target，于是用户自定义的 transform
		// 引擎收到 `--minify` 时会**静默忽略**它——产物没被压缩，而没有任何一句话
		// 说明这件事。这正是本项目反复登记的那一类缺陷（"声明了、没接线"），
		// 只是发生在 adapter 协议里。它一直没被发现，是因为 `transform` 在此之前
		// **没有任何命令入口**（v0.1 复盘 §6 起登记）；入口一开就会立刻被碰到。
		if o.Format != "" {
			m.set("format", o.Format)
		}
		if o.Minify {
			m.set("minify", true)
		}
		if o.SourceMaps {
			// 与 esbuild 的翻译一致：输出走 stdout，只能内联
			m.set("sourcemap", "inline")
		}
		m.set("stdin", true)
	case CSSOptions:
		readsStdin = true
		outfile = o.Outfile
		m.set("kind", string(KindCSS))
		for k, v := range o.Extra {
			m.set(k, v)
		}
		if o.Minify {
			m.set("minify", true)
		}
		m.set("stdin", true)
	case TypeCheckOptions:
		m.set("kind", string(KindTypeCheck))
		for k, v := range o.Extra {
			m.set(k, v)
		}
	case TypeDeclOptions:
		outfile = o.OutDir
		m.set("kind", string(KindTypeDecl))
		for k, v := range o.Extra {
			m.set(k, v)
		}
	default:
		return invocation{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("unsupported options type %T", req.Options), "")
	}

	if outfile != "" {
		m.set("outfile", outfile)
	}
	args := m.flagArgs()
	if entryFile != "" {
		// 文件路径作为位置参数（P4："大文件/多文件 → 直接传路径参数"）
		args = append(args, entryFile)
	}
	return invocation{Args: args, ReadsStdin: readsStdin}, nil
}
