package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV05TransformAcceptance 是 `ngm transform` 的可执行验收。
//
// 它用**假引擎**，因此不依赖本机装了什么、也不联网（真引擎那条单列在
// TestV05RealEsbuildTransform）。假引擎在这里正好够用：transform 的产物就是
// stdout 的字节，而 argv 可以精确断言。
//
// 覆盖的是"这个命令有没有真的驱动那项能力"：
//
//  1. 输入经 **stdin** 送到引擎（P4 协议里 transform 的约定），产物回 stdout
//  2. loader 从扩展名推断、且**说出来**；显式 --loader 覆盖推断
//  3. --outfile 由 ngm 落盘（transform 的引擎接口没有 outfile 出口）
//  4. minify / target / format / sourcemap 真的进了 argv
//  5. 缺 loader 是用法错误（stdin 没有扩展名可推断 / 扩展名不认识）
//  6. --dry-run 只打印、不执行
//  7. `engines.defaultTransform`（全局默认）真的能驱动这条命令——
//     该字段此前只是"被读了"，没有任何命令会用它
func TestV05TransformAcceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	testutils.AllowEngines(t, "fake-engine")
	home := isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// 项目**显式**声明 engines.transform，因此选中的是假引擎而不是内置的 esbuild。
	// （m6Project 会把这段 JSON 填进 ngm.json 的 engines 字段。）
	newProj := func() string {
		t.Helper()
		proj := m6Project(t, `{"transform":"fake"}`)
		m6Catalog(t, proj, fake, "transform")
		return proj
	}

	t.Run("a file goes through stdin and the result comes back on stdout", func(t *testing.T) {
		proj := newProj()
		testutils.WriteFile(t, proj, "src/index.ts", "export const x: number = 1;\n")
		// 假引擎把 stdin 原样写到 stdout —— 于是"产物 == 输入"这一条
		// 恰好证明了**输入确实是从 stdin 过去的**，而不是被当成文件路径传参。
		t.Setenv("FAKE_ECHO_STDIN", "1")
		read := m6Dump(t)

		code, out := runCaptureCode(t, "transform", "src/index.ts", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "export const x: number = 1;") {
			t.Errorf("stdout should carry the engine's output:\n%s", out)
		}

		args := read()
		if !hasArg(args, "--loader=ts") {
			t.Errorf("the loader inferred from .ts must reach the engine: %v", args)
		}
		// 推断必须**说出来**：用户要能看出"我选的"和"它替我选的"不是一回事
		if !strings.Contains(out, "inferred") {
			t.Errorf("the inferred loader must be announced on stderr:\n%s", out)
		}
	})

	t.Run("an explicit --loader overrides the extension", func(t *testing.T) {
		proj := newProj()
		testutils.WriteFile(t, proj, "src/thing.ts", "export const y = 1;\n")
		read := m6Dump(t)

		code, _ := runCaptureCode(t, "transform", "src/thing.ts", "--loader=tsx", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d", code)
		}
		args := read()
		if !hasArg(args, "--loader=tsx") {
			t.Errorf("explicit --loader must win: %v", args)
		}
		if hasArg(args, "--loader=ts") {
			t.Errorf("the inferred loader must not also be passed: %v", args)
		}
	})

	t.Run("stdin is the default input", func(t *testing.T) {
		proj := newProj()
		t.Setenv("FAKE_ECHO_STDIN", "1")
		withStdin(t, "export const fromPipe = true;\n")

		code, out := runCaptureCode(t, "transform", "--loader=ts", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "fromPipe") {
			t.Errorf("stdin should be forwarded to the engine:\n%s", out)
		}
	})

	t.Run("--outfile is written by ngm", func(t *testing.T) {
		proj := newProj()
		t.Setenv("FAKE_OUT", "/* transformed */\n")

		code, out := runCaptureCode(t, "transform", "--loader=ts", "--outfile=dist/out.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "transformed") {
			t.Errorf("the report should say what was written where:\n%s", out)
		}
		body, err := os.ReadFile(filepath.Join(proj, "dist", "out.js"))
		if err != nil {
			t.Fatalf("the output file was not written: %v", err)
		}
		if string(body) != "/* transformed */\n" {
			t.Errorf("written bytes = %q", body)
		}
	})

	t.Run("options reach the engine's argv", func(t *testing.T) {
		proj := newProj()
		read := m6Dump(t)

		code, _ := runCaptureCode(t, "transform", "--loader=ts", "--target=es2020",
			"--format=cjs", "--minify", "--sourcemap", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d", code)
		}
		args := read()
		for _, want := range []string{"--loader=ts", "--target=es2020", "--format=cjs", "--minify", "--sourcemap=inline"} {
			if !hasArg(args, want) {
				t.Errorf("argv is missing %q: %v", want, args)
			}
		}
	})

	t.Run("ngm does not guess a loader it has no basis for", func(t *testing.T) {
		proj := newProj()
		testutils.WriteFile(t, proj, "src/thing.xyz", "whatever\n")
		read := m6Dump(t)

		code, out := runCaptureCode(t, "transform", "src/thing.xyz", "--dir="+proj)
		if code != 0 {
			t.Fatalf("transform exit=%d:\n%s", code, out)
		}
		// 扩展名不认识时不猜：argv 里不该凭空出现 --loader=
		for _, a := range read() {
			if strings.HasPrefix(a, "--loader=") {
				t.Errorf("ngm must not invent a loader for an unknown extension: %v", a)
			}
		}
	})

	// 这条同时钉住两件事，它们曾经都做错过（见 transform.go 的注释）：
	//   1. "输入必须有 loader" 是 **esbuild 的**规则，不是 ngm 的——
	//      ngm 一度把它强加给所有引擎（自定义引擎可能压根不需要 loader）。
	//      该规则应当在 adapter 里判：`internal/adapter` 的
	//      TestBuildInvocation_EsbuildTransformNeedsLoader。
	//   2. 也因此，缺 loader 时 CLI **不该**抢先报错——那会挡住清单里声明好的
	//      loader（见上一条 defaultOptions 用例）。报错与否由 adapter 决定。
	t.Run("an engine that needs no loader is not given one", func(t *testing.T) {
		proj := newProj()
		read := m6Dump(t)
		withStdin(t, "export const fromPipe = true;\n")

		code, out := runCaptureCode(t, "transform", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a generic engine needing no loader must not be blocked; exit=%d:\n%s", code, out)
		}
		for _, a := range read() {
			if strings.HasPrefix(a, "--loader=") {
				t.Errorf("ngm must not add a loader the engine did not ask for: %v", a)
			}
		}
	})

	t.Run("a loader declared in the engine's defaultOptions is honoured", func(t *testing.T) {
		// 这条防的是本命令**第一版**的缺陷：CLI 自己判"没 --loader 就报错"，
		// 于是清单里声明好的 loader 被挡在门外——而 adapter 本来会用它。
		// 三处来源里只剩清单默认这一处可用（stdin 没有扩展名、命令行没给 --loader），
		// 因此这条用例恰好只测它。
		proj := m6Project(t, `{"transform":"fake"}`)
		testutils.WriteFile(t, proj, "ngm.engines.json", `{
  "version": 1,
  "engines": [
    {"name": "fake", "kind": "transform", "adapter": "subprocess",
     "command": "`+filepath.ToSlash(fake)+`", "defaultOptions": {"loader": "tsx"}}
  ]
}
`)
		read := m6Dump(t)
		withStdin(t, "export const fromPipe = true;\n")

		code, out := runCaptureCode(t, "transform", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a declared defaultOptions.loader must be enough; exit=%d:\n%s", code, out)
		}
		if args := read(); !hasArg(args, "--loader=tsx") {
			t.Errorf("the loader declared in the catalog must reach the engine: %v", args)
		}
	})

	t.Run("--dry-run prints the plan and runs nothing", func(t *testing.T) {
		proj := newProj()
		testutils.WriteFile(t, proj, "src/index.ts", "export const x = 1;\n")
		argvDump := filepath.Join(t.TempDir(), "argv.txt")
		t.Setenv("FAKE_DUMP_ARGS", argvDump)

		code, out := runCaptureCode(t, "transform", "src/index.ts", "--dry-run", "--dir="+proj)
		if code != 0 {
			t.Fatalf("dry-run exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "would transform src/index.ts") {
			t.Errorf("dry-run should say what it would do:\n%s", out)
		}
		// 引擎**从未启动**：argv 转储文件不该出现。
		// （这里刻意不用 m6Dump：它的读取器在文件缺失时会 t.Fatalf，
		//   而"缺失"正是本用例要断言的事实。）
		if _, err := os.Stat(argvDump); err == nil {
			t.Errorf("dry-run must not invoke the engine, but argv was dumped to %s", argvDump)
		}
	})

	t.Run("the global defaultTransform drives this command", func(t *testing.T) {
		// 项目**不声明** engines.transform（`{}`）：能否跑起来完全取决于全局默认是否生效。
		// 这条断言把 `engines.defaultTransform` 从"被读过"变成"有用"——
		// 在此命令出现之前，那个键没有任何可观察的效果。
		proj := m6Project(t, `{}`)
		m6Catalog(t, proj, fake, "transform")
		writeGlobalConfig(t, home, `{"engines":{"defaultTransform":"fake"}}`)

		code, out := runCaptureCode(t, "transform", "src/index.ts", "--dry-run", "--dir="+proj)
		if code != 0 {
			t.Fatalf("dry-run exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "primary: fake") {
			t.Errorf("the global default engine must fill in what the project left unset:\n%s", out)
		}
	})
}

// withStdin 把命令的 stdin 换成一段可读的文本，并在用例结束时还原。
//
// 用接缝（stdinReader）而不是真的往进程 stdin 写：验收测试在**进程内**跑，
// 进程的 stdin 属于 `go test`，不是这条管道。
func withStdin(t *testing.T, body string) {
	t.Helper()
	prev := stdinReader
	stdinReader = strings.NewReader(body)
	t.Cleanup(func() { stdinReader = prev })
}

// hasArg 报告 args 里是否出现过某个参数（精确匹配，不做前缀判断——
// `--loader=ts` 不该被 `--loader=tsx` 的断言蒙混过去）。
func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
