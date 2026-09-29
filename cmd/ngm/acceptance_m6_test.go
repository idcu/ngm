package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// m6Project 建一个带 ngm.json 的项目（engines 段由调用方给出）。
//
// 刻意**不**用 `ngm init` 再改写：这里要能精确控制 engines 段的写法
// （简写 / 完整写法 / 不写），那正是 M6 的验收对象之一。
func m6Project(t *testing.T, enginesJSON string) string {
	t.Helper()
	proj := t.TempDir()
	testutils.WriteFile(t, proj, "ngm.json", `{
  "schemaVersion": 1,
  "name": "github.com/m6-test/app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "src/index.ts",
  "dependencies": [],
  "engines": `+enginesJSON+`
}
`)
	testutils.WriteFile(t, proj, "src/index.ts", "export const x = 1\n")
	return proj
}

// m6CatalogEntry 是清单条目的测试侧表示（字段与 ngm.engines.json 一致）。
type m6CatalogEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Adapter string `json:"adapter"`
	Command string `json:"command"`
}

// m6WriteCatalog 写入 ngm.engines.json。
//
// 一律走 json.Marshal：手写字符串会把 Windows 路径里的 `\U` 变成非法转义
// （`C:\Users\...`），而那条错误看起来像 ngm 的解析 bug，排查方向完全跑偏。
func m6WriteCatalog(t *testing.T, proj string, entries ...m6CatalogEntry) {
	t.Helper()
	cat := struct {
		Version int              `json:"version"`
		Engines []m6CatalogEntry `json:"engines"`
	}{Version: 1, Engines: entries}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testutils.WriteFile(t, proj, "ngm.engines.json", string(data)+"\n")
}

// m6Catalog 把 fake 引擎登记到给定能力类别。
//
// 路径用引号包裹：临时目录可能含空格（Windows 用户名、CI 工作目录），
// 而 splitCommand 只支持"带引号的片段"这一种语法。
func m6Catalog(t *testing.T, proj, fakePath string, kinds ...string) {
	t.Helper()
	entries := make([]m6CatalogEntry, 0, len(kinds))
	for _, k := range kinds {
		entries = append(entries, m6CatalogEntry{
			Name: "fake", Kind: k, Adapter: "subprocess",
			Command: `"` + fakePath + `"`,
		})
	}
	m6WriteCatalog(t, proj, entries...)
}

// m6Dump 让假引擎把 argv 落盘，并返回读取器。
//
// 返回的是**函数**而不是路径：调用方先跑命令、再读，顺序错了就会读到
// "文件不存在"——那正是我们想用来证明"引擎没被调用"的信号。
func m6Dump(t *testing.T) func() []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("FAKE_DUMP_ARGS", path)
	return func() []string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the engine was never invoked (%v)", err)
		}
		return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
}

// TestM6Acceptance 是 development/v0.1-plan.md 中 M6 阶段的**可执行验收**。
//
// 逐条对应 M6 的验收标准：
//
//  1. 样例项目通过 `ngm build --engine=esbuild` 产出可运行 bundle —— 这里用
//     一个**可控的假引擎**做同样的事，使验收完全不依赖本机是否装了 esbuild、
//     也不联网（真实 esbuild 的用例单列在 TestM6Acceptance_RealEsbuild）
//  2. 引擎缺失场景 exit 5
//  3. 简写与完整写法配置等价
//  4. self stub 仅在 --dry-run 可用，非 dry-run 报错（禁止静默降级）
func TestM6Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	// =====================================================================
	// 验收 1：build 端到端（外部引擎 + mappings → alias + 产物落盘）
	// =====================================================================
	t.Run("build runs the engine with mappings as aliases", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, proj, fake, "bundle")
		testutils.WriteFile(t, proj, "ngm.mappings.json", `{
  "version": 1,
  "mappings": [
    {"from": "github:o/r", "to": "./ngm.vendor/github.com/o/r", "main": "./index.ts"}
  ]
}
`)
		read := m6Dump(t)

		code, out := runCaptureCode(t, "build", "--outfile=dist/app.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build exit=%d out=%s", code, out)
		}

		// 产物必须真的落盘（不是"stdout 有东西"就算过）
		if _, err := os.Stat(filepath.Join(proj, "dist", "app.js")); err != nil {
			t.Errorf("the bundle was not written: %v", err)
		}

		joined := strings.Join(read(), " ")
		for _, want := range []string{
			"--kind=bundle",
			"--outfile=dist/app.js",
			"--alias:github:o/r=./ngm.vendor/github.com/o/r",
			"src/index.ts",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("argv %q should contain %q", joined, want)
			}
		}
		if !strings.Contains(out, "bundled src/index.ts") {
			t.Errorf("out=%s", out)
		}
	})

	// =====================================================================
	// 验收 3：简写与完整写法等价
	// =====================================================================
	t.Run("shorthand and full form resolve to the same command", func(t *testing.T) {
		shortProj := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, shortProj, fake, "bundle")

		fullProj := m6Project(t, `{"bundle": {"primary": "fake", "fallbacks": []}}`)
		m6Catalog(t, fullProj, fake, "bundle")

		readShort := m6Dump(t)
		if code, out := runCaptureCode(t, "build", "--outfile=out.js", "--dir="+shortProj); code != 0 {
			t.Fatalf("shorthand build: %s", out)
		}
		shortArgs := readShort()

		readFull := m6Dump(t)
		if code, out := runCaptureCode(t, "build", "--outfile=out.js", "--dir="+fullProj); code != 0 {
			t.Fatalf("full-form build: %s", out)
		}
		fullArgs := readFull()

		if strings.Join(shortArgs, "\n") != strings.Join(fullArgs, "\n") {
			t.Errorf("`\"fake\"` and `{\"primary\":\"fake\",\"fallbacks\":[]}` must be equivalent:\n"+
				"  shorthand: %q\n  full form: %q", shortArgs, fullArgs)
		}
	})

	// =====================================================================
	// --dry-run：只规划，不执行
	// =====================================================================
	t.Run("dry-run plans without executing the engine", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, proj, fake, "bundle")

		dump := filepath.Join(t.TempDir(), "argv.txt")
		t.Setenv("FAKE_DUMP_ARGS", dump)

		code, out := runCaptureCode(t, "build", "--dry-run", "--dir="+proj)
		if code != 0 {
			t.Fatalf("dry-run exit=%d out=%s", code, out)
		}
		for _, want := range []string{"would bundle src/index.ts", "primary: fake", "--kind=bundle"} {
			if !strings.Contains(out, want) {
				t.Errorf("dry-run output should contain %q:\n%s", want, out)
			}
		}
		if _, err := os.Stat(dump); err == nil {
			t.Error("--dry-run must not execute the engine")
		}
	})

	// =====================================================================
	// 验收 4：self stub 只在 dry-run 可用（禁止静默降级）
	// =====================================================================
	t.Run("the self stub never produces artifacts", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "self"}`) // self 来自内置清单

		code, out := runCaptureCode(t, "build", "--outfile=out.js", "--dir="+proj)
		if code != 5 {
			t.Fatalf("running the stub must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "dry-run stub") {
			t.Errorf("the error should explain what `self` is:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(proj, "out.js")); err == nil {
			t.Error("the stub must not leave an artifact behind — a silent fallback is the one thing it must not do")
		}

		// dry-run 下允许：那正是它被设计出来做的事
		if code, out := runCaptureCode(t, "build", "--dry-run", "--dir="+proj); code != 0 {
			t.Errorf("--dry-run must accept the stub, got %d:\n%s", code, out)
		}
	})

	// =====================================================================
	// 验收 2：引擎缺失 → exit 5；引擎名不认识 → exit 3
	// =====================================================================
	t.Run("missing engine exits 5", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "ghost"}`)
		cat := `{
  "version": 1,
  "engines": [
    {"name": "ghost", "kind": "bundle", "adapter": "subprocess", "command": "ngm-definitely-not-a-real-engine"}
  ]
}
`
		testutils.WriteFile(t, proj, "ngm.engines.json", cat)

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a missing engine must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "not available") {
			t.Errorf("the error should say it is missing:\n%s", out)
		}
	})

	t.Run("unknown engine name exits 3", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "nope"}`)

		code, out := runCaptureCode(t, "build", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an unknown engine name is a config error (3), got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "available") {
			t.Errorf("the hint should list the known engines:\n%s", out)
		}
	})

	// =====================================================================
	// fallback：primary 挂了才用 fallbacks，并且**必须告知用户**
	// =====================================================================
	t.Run("fallback is used and announced", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": {"primary": "ghost", "fallbacks": ["fake"]}}`)
		m6WriteCatalog(t, proj,
			m6CatalogEntry{Name: "ghost", Kind: "bundle", Adapter: "subprocess",
				Command: "ngm-definitely-not-a-real-engine"},
			m6CatalogEntry{Name: "fake", Kind: "bundle", Adapter: "subprocess",
				Command: `"` + fake + `"`},
		)
		read := m6Dump(t)

		code, out := runCaptureCode(t, "build", "--outfile=out.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("the fallback should have succeeded: exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "falling back to fake") {
			t.Errorf("an implicit fallback must be visible on stderr:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(proj, "out.js")); err != nil {
			t.Errorf("the fallback engine's artifact is missing: %v", err)
		}
		if !strings.Contains(strings.Join(read(), " "), "--kind=bundle") {
			t.Error("the fallback engine should have received the same argv shape")
		}
	})

	// =====================================================================
	// typecheck：v0.1 未适配检查器 → 配置错误；esbuild 被误配 → 明确拒绝
	// =====================================================================
	t.Run("typecheck without an adapted engine exits 3", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, proj, fake, "bundle")

		code, out := runCaptureCode(t, "typecheck", "--dir="+proj)
		if code != 3 {
			t.Fatalf("no typeCheck engine configured must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "engines.typeCheck") {
			t.Errorf("the hint should say what to configure:\n%s", out)
		}
	})

	t.Run("esbuild is refused as a type checker", func(t *testing.T) {
		// 真实存在的误配：esbuild 只删类型标注、不做检查。
		// 用户会把它写进清单的 typeCheck 条目，因此这里也照那样声明。
		proj := m6Project(t, `{"typeCheck": "esbuild"}`)
		m6WriteCatalog(t, proj, m6CatalogEntry{
			Name: "esbuild", Kind: "typeCheck", Adapter: "subprocess", Command: "esbuild",
		})

		code, out := runCaptureCode(t, "typecheck", "src/index.ts", "--dir="+proj)
		if code != 5 {
			t.Fatalf("esbuild as a type checker must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "does not type-check") {
			t.Errorf("the error should explain why esbuild is not a substitute:\n%s", out)
		}
	})

	t.Run("typecheck passes when the engine exits 0", func(t *testing.T) {
		proj := m6Project(t, `{"typeCheck": "fake"}`)
		m6Catalog(t, proj, fake, "typeCheck")
		// 让假引擎保持安静：一个干净的类型检查器不该有输出。
		// 这同时固定了"判定只看退出码、不因 stderr 有内容就改判"。
		t.Setenv("FAKE_OUT", "\n")

		code, out := runCaptureCode(t, "typecheck", "src/index.ts", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a clean engine run must exit 0, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "no type errors") {
			t.Errorf("out=%s", out)
		}
	})

	// =====================================================================
	// css：输入走 stdin，产物落盘
	// =====================================================================
	t.Run("css pipes the input through the engine", func(t *testing.T) {
		proj := m6Project(t, `{"css": "fake"}`)
		m6Catalog(t, proj, fake, "css")
		const css = "body { color: red }\n"
		testutils.WriteFile(t, proj, "src/app.css", css)
		t.Setenv("FAKE_ECHO_STDIN", "1")

		code, out := runCaptureCode(t, "css", "src/app.css", "--outfile=dist/app.css", "--dir="+proj)
		if code != 0 {
			t.Fatalf("css exit=%d out=%s", code, out)
		}
		got, err := os.ReadFile(filepath.Join(proj, "dist", "app.css"))
		if err != nil {
			t.Fatalf("compiled css missing: %v", err)
		}
		if string(got) != css {
			t.Errorf("the engine should have received the file bytes on stdin, got %q", got)
		}

		if code, out := runCaptureCode(t, "css", "--dir="+proj); code != 3 {
			t.Errorf("css without an input file must exit 3, got %d:\n%s", code, out)
		}
	})

	// =====================================================================
	// engines list / info / validate
	// =====================================================================
	t.Run("engines list shows availability", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "fake"}`)
		m6WriteCatalog(t, proj,
			m6CatalogEntry{Name: "fake", Kind: "bundle", Adapter: "subprocess",
				Command: `"` + fake + `"`},
			m6CatalogEntry{Name: "ghost", Kind: "bundle", Adapter: "subprocess",
				Command: "ngm-definitely-not-a-real-engine"},
		)
		code, out := runCaptureCode(t, "engines", "list", "--dir="+proj)
		if code != 0 {
			t.Fatalf("list exit=%d out=%s", code, out)
		}
		for _, want := range []string{"NAME", "fake", "ok", "ghost", "not found", "built-in"} {
			if !strings.Contains(out, want) {
				t.Errorf("list output should contain %q:\n%s", want, out)
			}
		}

		// --json 必须可解析且字段齐全
		code, jout := runCaptureCode(t, "engines", "list", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("list --json exit=%d out=%s", code, jout)
		}
		var rows []struct {
			Name      string `json:"name"`
			Kind      string `json:"kind"`
			Adapter   string `json:"adapter"`
			Available bool   `json:"available"`
			Builtin   bool   `json:"builtin"`
		}
		if err := json.Unmarshal([]byte(jout), &rows); err != nil {
			t.Fatalf("--json is not valid JSON: %v\n%s", err, jout)
		}
		var sawFake, sawGhost, sawBuiltin bool
		for _, r := range rows {
			switch {
			case r.Name == "fake" && r.Available && !r.Builtin:
				sawFake = true
			case r.Name == "ghost" && !r.Available:
				sawGhost = true
			case r.Builtin:
				sawBuiltin = true
			}
		}
		if !sawFake || !sawGhost || !sawBuiltin {
			t.Errorf("json rows missing expectations (fake=%v ghost=%v builtin=%v): %+v",
				sawFake, sawGhost, sawBuiltin, rows)
		}
	})

	t.Run("engines info reports one engine across kinds", func(t *testing.T) {
		proj := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, proj, fake, "bundle", "transform")

		code, out := runCaptureCode(t, "engines", "info", "fake", "--dir="+proj)
		if code != 0 {
			t.Fatalf("info exit=%d out=%s", code, out)
		}
		for _, want := range []string{"fake (bundle)", "fake (transform)", "adapter:", "command:"} {
			if !strings.Contains(out, want) {
				t.Errorf("info output should contain %q:\n%s", want, out)
			}
		}

		if code, out := runCaptureCode(t, "engines", "info", "nobody", "--dir="+proj); code != 5 {
			t.Errorf("an unknown engine must exit 5, got %d:\n%s", code, out)
		}
	})

	t.Run("engines validate distinguishes schema from availability", func(t *testing.T) {
		// 结构错误 → 3（优先于可用性）
		broken := m6Project(t, `{}`)
		testutils.WriteFile(t, broken, "ngm.engines.json", `{"version": 99, "engines": []}`)
		if code, out := runCaptureCode(t, "engines", "validate", "--dir="+broken); code != 3 {
			t.Errorf("a bad catalog version must exit 3, got %d:\n%s", code, out)
		}

		// 只有可用性问题 → 5，且必须点名是哪个引擎
		missing := m6Project(t, `{}`)
		m6WriteCatalog(t, missing, m6CatalogEntry{
			Name: "ghost", Kind: "bundle", Adapter: "subprocess",
			Command: "ngm-definitely-not-a-real-engine",
		})
		code, out := runCaptureCode(t, "engines", "validate", "--dir="+missing)
		if code != 5 {
			t.Errorf("a missing engine must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "ghost/bundle") {
			t.Errorf("the report should name the entry:\n%s", out)
		}

		// 干净清单：除了内置 esbuild 是否安装（取决于机器）之外不应有别的 issue
		clean := m6Project(t, `{"bundle": "fake"}`)
		m6Catalog(t, clean, fake, "bundle")
		_, jout := runCaptureCode(t, "engines", "validate", "--json", "--dir="+clean)
		var payload struct {
			Issues []struct {
				Entry string `json:"entry"`
				Kind  string `json:"kind"`
			} `json:"issues"`
		}
		if err := json.Unmarshal([]byte(jout), &payload); err != nil {
			t.Fatalf("validate --json is not valid JSON: %v\n%s", err, jout)
		}
		for _, is := range payload.Issues {
			if !strings.HasPrefix(is.Entry, "esbuild/") {
				t.Errorf("unexpected issue in a clean catalog: %+v", is)
			}
		}
	})
}

// TestM6Acceptance_RealEsbuild 用真实 esbuild 跑一次完整构建。
//
// 它在 esbuild 未安装时跳过——本机（开发机）常常没有，CI 有专门的 job 装它。
// 之所以与 TestM6Acceptance 分开：上面那组是**协议**验收（hermetic、三平台
// 一致），这一条是**集成**验收（依赖外部工具的真实行为），混在一起会让
// 前者在缺工具的机器上被静默跳过。
func TestM6Acceptance_RealEsbuild(t *testing.T) {
	if _, err := exec.LookPath("esbuild"); err != nil {
		t.Skip("esbuild is not installed; the hermetic acceptance above already covers the protocol")
	}
	isolateUserEnv(t)

	proj := m6Project(t, `{"bundle": "esbuild"}`)
	testutils.WriteFile(t, proj, "src/index.ts", `const greet = (who: string): string => "hello " + who;
console.log(greet("world"));
`)

	code, out := runCaptureCode(t, "build", "--outfile=dist/app.js", "--dir="+proj)
	if code != 0 {
		t.Fatalf("real esbuild build failed: exit=%d out=%s", code, out)
	}
	bundle, err := os.ReadFile(filepath.Join(proj, "dist", "app.js"))
	if err != nil {
		t.Fatalf("the bundle was not written: %v", err)
	}
	// 类型标注必须已被剥离（这证明 esbuild 真的处理了 TypeScript）
	if strings.Contains(string(bundle), ": string") {
		t.Errorf("esbuild should have stripped the type annotation:\n%s", bundle)
	}
	if !strings.Contains(string(bundle), "hello ") {
		t.Errorf("bundle should contain the program:\n%s", bundle)
	}

	// --dry-run 不执行：即使引擎在位，也不应产生产物
	if err := os.Remove(filepath.Join(proj, "dist", "app.js")); err != nil {
		t.Fatal(err)
	}
	if code, out := runCaptureCode(t, "build", "--dry-run", "--outfile=dist/app.js", "--dir="+proj); code != 0 {
		t.Fatalf("dry-run exit=%d out=%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(proj, "dist", "app.js")); err == nil {
		t.Error("--dry-run must not produce artifacts")
	}
}
