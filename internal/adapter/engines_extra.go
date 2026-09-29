package adapter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// 本文件是 v0.2 E 组新增引擎的 argv 翻译：typescript（tsc）/ deno / postcss。
//
// 契约与 esbuildInvocation 完全一致：
//
//   - 只负责"怎么调"，**不**执行任何进程（因此可单测、可被 --dry-run 复用）
//   - 某个能力类别做不到时必须**明确报错**（exit 5 + 可操作提示），
//     绝不静默当作成功——esbuild 不做类型检查这件事已经踩过一次
//   - 入口文件作为位置参数放在最后

// denoMinBundle 是 `deno bundle` 可用的最低版本。
//
// Deno 2.4 才把 bundle 作为实验特性带回；低于该版本没有这个子命令。
// 这里显式判定而不是让 deno 自己去报错：用户该看到的是
// "你的 deno 版本不支持 bundle，请改用 esbuild"，而不是一条与 ngm 无关的参数错误。
const denoMinBundle = "2.4"

// ---------------------------------------------------------------------------
// typescript（tsc）
// ---------------------------------------------------------------------------

// typescriptInvocation 生成 tsc 的参数。
//
//	类型检查   tsc --noEmit [--project <tsconfig>] [<entry>]
//	声明生成   tsc --emitDeclarationOnly --declaration [--outDir <dir>] [<entry>]
//
// `--noEmit` 是**必须**的：不带它 tsc 会真的产出 JS，那不是类型检查该干的事，
// 而且会在项目里留下没人预期的文件。
func typescriptInvocation(entry Entry, req buildRequest) (invocation, error) {
	switch o := req.Options.(type) {
	case TypeCheckOptions:
		args := []string{"--noEmit"}
		if o.TSConfig != "" {
			// --project 与位置参数互斥（tsc 会报错），因此给了 project 就不再传入口
			args = append(args, "--project", o.TSConfig)
		} else if req.EntryFile != "" {
			args = append(args, req.EntryFile)
		}
		m := optionSet{}
		for k, v := range o.Extra {
			m.set(k, v)
		}
		return invocation{Args: append(args, m.flagArgs()...)}, nil

	case TypeDeclOptions:
		args := []string{"--declaration"}
		if o.OutDir != "" {
			args = append(args, "--outDir", o.OutDir)
		}
		if req.EntryFile != "" {
			args = append(args, req.EntryFile)
		}
		m := optionSet{}
		for k, v := range o.Extra {
			m.set(k, v)
		}
		return invocation{Args: append(args, m.flagArgs()...)}, nil

	default:
		return invocation{}, errs.New(errs.CodeEngineNotFound,
			"tsc provides typeCheck and typeDecl only",
			"use esbuild for bundle / transform / css")
	}
}

// ---------------------------------------------------------------------------
// deno
// ---------------------------------------------------------------------------

// denoInvocation 生成 deno 的参数。
//
// 清单 command 的前缀决定子命令：`deno check` → 类型检查、`deno bundle` → 打包。
// 因此这里只补可变参数，不重复子命令名。
func denoInvocation(entry Entry, req buildRequest) (invocation, error) {
	switch o := req.Options.(type) {
	case TypeCheckOptions:
		args := []string{}
		if o.TSConfig != "" {
			// deno 用 deno.json 而非 tsconfig；用户既给了就透传（deno 支持 --config）
			args = append(args, "--config", o.TSConfig)
		}
		if req.EntryFile != "" {
			args = append(args, req.EntryFile)
		}
		m := optionSet{}
		for k, v := range o.Extra {
			m.set(k, v)
		}
		return invocation{Args: append(args, m.flagArgs()...)}, nil

	case BundleOptions:
		v, verr := probeVersion(entry)
		if verr != nil {
			return invocation{}, errs.Wrap(errs.CodeEngineNotFound,
				"deno/bundle: cannot determine the installed deno version",
				"install Deno >= "+denoMinBundle+", or bundle with esbuild", verr)
		}
		if !versionAtLeast(v, denoMinBundle) {
			return invocation{}, errs.New(errs.CodeEngineNotFound,
				fmt.Sprintf("deno %s cannot bundle: `deno bundle` needs >= %s (experimental)", v, denoMinBundle),
				"upgrade Deno, or bundle with esbuild: `ngm build --engine=esbuild`")
		}
		var args []string
		m := optionSet{}
		for k, v := range o.Extra {
			m.set(k, v)
		}
		args = append(args, m.flagArgs()...)
		if req.EntryFile != "" {
			args = append(args, req.EntryFile)
		}
		if o.Outfile != "" {
			// 位置参数给出输出文件（`deno bundle <entry> <out>`）。
			// 刻意不猜 flag 名：2.4 的 bundle 是实验特性，参数面可能变，
			// 编一个 --outfile 上去会得到一个与版本相关的假象。
			args = append(args, o.Outfile)
		}
		return invocation{
			Args:  args,
			Notes: []string{"deno bundle is experimental and its flags vary by version; check `deno bundle --help`"},
		}, nil

	default:
		return invocation{}, errs.New(errs.CodeEngineNotFound,
			"deno provides typeCheck and bundle only in this build",
			"use esbuild for transform / css")
	}
}

// ---------------------------------------------------------------------------
// postcss
// ---------------------------------------------------------------------------

// postcssInvocation 生成 postcss-cli 的参数。
//
//	postcss [--use <plugin>]... [--output <file>]   （输入走 stdin）
//
// 插件**保持用户给定的顺序**：postcss 的插件顺序是有语义的（先 autoprefixer
// 还是先 nesting 结果不同），因此这里不能像普通选项那样按键排序。
func postcssInvocation(entry Entry, req buildRequest) (invocation, error) {
	o, ok := req.Options.(CSSOptions)
	if !ok {
		return invocation{}, errs.New(errs.CodeEngineNotFound,
			"postcss provides css only",
			"use esbuild for bundle / transform")
	}

	var args []string
	var notes []string
	for _, p := range stringList(o.Extra["use"]) {
		args = append(args, "--use", p)
	}
	if o.Outfile != "" {
		args = append(args, "--output", o.Outfile)
	}
	m := optionSet{}
	for k, v := range o.Extra {
		if k == "use" {
			continue
		}
		m.set(k, v)
	}
	args = append(args, m.flagArgs()...)

	if o.Minify {
		// postcss 本身不压缩，压缩由 cssnano 这类插件提供。
		// 静默忽略 --minify 会让人以为产物被压缩过，因此明确说出来。
		notes = append(notes, "postcss has no built-in minifier: --minify was ignored; "+
			"declare cssnano in `use` or compile CSS with esbuild")
	}
	return invocation{Args: args, ReadsStdin: true, Notes: notes}, nil
}

// ---------------------------------------------------------------------------
// 版本比较
// ---------------------------------------------------------------------------

// reVersionNumber 从版本串里取出第一段数字序列（"deno 2.4.3" → "2.4.3"）。
var reVersionNumber = regexp.MustCompile(`\d+(?:\.\d+)*`)

// versionAtLeast 判定 actual 是否不低于 min（按 major.minor.patch 逐段比较）。
//
// 只比较 min 里写到的段数：`min="2.4"` 时 2.4.0 / 2.4.3 / 2.5.0 都算满足，
// 2.3.9 不算。解析不出版本号时返回 false——**"不知道"不等于"够新"**，
// 放行一个无法确定版本的实验特性会让失败发生在更后面、更难诊断的地方。
func versionAtLeast(actual, min string) bool {
	found := reVersionNumber.FindString(actual)
	if found == "" {
		return false
	}
	a, m := versionParts(found), versionParts(min)
	if len(a) == 0 || len(m) == 0 {
		return false
	}
	for i := range m {
		if i >= len(a) {
			return false
		}
		if a[i] != m[i] {
			return a[i] > m[i]
		}
	}
	return true
}

func versionParts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// stringList 把选项值归一化为字符串列表（支持字符串与数组两种写法）。
func stringList(v any) []string {
	switch t := v.(type) {
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
	case []string:
		return t
	case []any:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
