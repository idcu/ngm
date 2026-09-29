package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

// ---------------------------------------------------------------------------
// splitCommand
// ---------------------------------------------------------------------------

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in      string
		program string
		args    []string
		why     string
	}{
		{"esbuild", "esbuild", nil, "bare program"},
		{"deno check", "deno", []string{"check"}, "subcommand form (docs use this)"},
		{"tsc --emitDeclarationOnly", "tsc", []string{"--emitDeclarationOnly"}, "flag prefix"},
		{`"C:\Program Files\tool.exe" --flag`, `C:\Program Files\tool.exe`, []string{"--flag"},
			"quoted Windows path — without this, a legal path could not be expressed"},
		{`  "a b"   c  `, "a b", []string{"c"}, "extra whitespace"},
		{"", "", nil, "empty"},
		{"   ", "", nil, "whitespace only"},
		{`""`, "", nil, "an empty quoted program yields no program"},
	}

	for _, tc := range cases {
		program, args := splitCommand(tc.in)
		if program != tc.program {
			t.Errorf("%s: splitCommand(%q) program=%q want %q", tc.why, tc.in, program, tc.program)
		}
		if strings.Join(args, "\x00") != strings.Join(tc.args, "\x00") {
			t.Errorf("%s: splitCommand(%q) args=%q want %q", tc.why, tc.in, args, tc.args)
		}
	}
}

// TestSplitCommand_NoShellExpansion 固定"不经过 shell"这条安全边界。
//
// 清单来自仓库（外部输入），若 ngm 走 `sh -c` 就等于把任意命令执行权交给
// 任何一个 PR。因此这些字符必须原样作为参数内容，而不是被解释。
func TestSplitCommand_NoShellExpansion(t *testing.T) {
	command := `echo a&&b $(whoami) > out.txt | tee`
	program, args := splitCommand(command)
	if program != "echo" {
		t.Errorf("program=%q want echo", program)
	}
	want := []string{"a&&b", "$(whoami)", ">", "out.txt", "|", "tee"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("args=%q want %q (shell metacharacters must stay literal)", args, want)
	}
}

// ---------------------------------------------------------------------------
// 选择归一化：简写与完整写法等价（M6 验收项）
// ---------------------------------------------------------------------------

func TestResolveSelection_ShorthandEqualsFullForm(t *testing.T) {
	short, err := ResolveSelection("esbuild")
	if err != nil {
		t.Fatalf("shorthand: %v", err)
	}
	full, err := ResolveSelection(map[string]any{"primary": "esbuild"})
	if err != nil {
		t.Fatalf("full form: %v", err)
	}
	explicit, err := ResolveSelection(map[string]any{"primary": "esbuild", "fallbacks": []any{}})
	if err != nil {
		t.Fatalf("explicit empty fallbacks: %v", err)
	}

	for _, got := range []Selection{full, explicit} {
		if got.Primary != short.Primary {
			t.Errorf("primary=%q want %q", got.Primary, short.Primary)
		}
		if len(got.Fallbacks) != 0 {
			t.Errorf("fallbacks=%v want none (shorthand never auto-falls-back)", got.Fallbacks)
		}
		if len(got.Chain()) != 1 || got.Chain()[0] != "esbuild" {
			t.Errorf("chain=%v want [esbuild]", got.Chain())
		}
	}
}

func TestResolveSelection_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "null"},
		{"empty string", "", "empty string"},
		{"missing primary", map[string]any{"fallbacks": []any{"deno"}}, "missing `primary`"},
		{"primary not a string", map[string]any{"primary": 42}, "must be a string"},
		{"unknown key", map[string]any{"primry": "esbuild"}, `unknown key "primry"`},
		{"fallbacks not an array", map[string]any{"primary": "a", "fallbacks": "b"}, "must be an array"},
		{"fallback not a string", map[string]any{"primary": "a", "fallbacks": []any{1}}, "must be strings"},
		{"options not an object", map[string]any{"primary": "a", "options": []any{}}, "must be an object"},
		{"wrong type", 42, "must be a string or an object"},
	}

	for _, tc := range cases {
		_, err := ResolveSelection(tc.in)
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q should mention %q", tc.name, err.Error(), tc.want)
		}
	}
}

// TestResolveSelection_UnknownKeyIsFatal 固定"未知键报错"的严格性。
//
// engines 在 ngm.json 里是 `any`，结构体层的 DisallowUnknownFields 管不到它。
// 若这里静默忽略，`{"primry":"esbuild"}` 会被当成"没配引擎"，用户会以为
// 自己的配置生效了，实际跑的是默认引擎。
func TestResolveSelection_UnknownKeyIsFatal(t *testing.T) {
	_, err := ResolveSelection(map[string]any{"primary": "esbuild", "option": map[string]any{}})
	if err == nil {
		t.Fatal("a typo in the engine selection must be an error, not silently ignored")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code=%d want 3", code)
	}
}

func TestSelectionFor(t *testing.T) {
	raw := map[string]any{"bundle": "esbuild"}
	sel, ok, err := SelectionFor(raw, KindBundle)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if sel.Primary != "esbuild" {
		t.Errorf("primary=%q", sel.Primary)
	}

	if _, ok, err := SelectionFor(raw, KindCSS); ok || err != nil {
		t.Errorf("an unconfigured kind must report ok=false without error (ok=%v err=%v)", ok, err)
	}
	if _, ok, err := SelectionFor(nil, KindBundle); ok || err != nil {
		t.Errorf("nil config must be tolerated (ok=%v err=%v)", ok, err)
	}
}

// ---------------------------------------------------------------------------
// 清单
// ---------------------------------------------------------------------------

func TestBuiltinCatalog_IsWellFormed(t *testing.T) {
	cat := BuiltinCatalog()

	if cat.Version != CatalogVersion {
		t.Errorf("version=%d", cat.Version)
	}
	// v0.1 只适配 esbuild + self；预置未适配的引擎会让 `ngm engines list` 说谎
	if names := cat.NamesFor(KindBundle); strings.Join(names, ",") != "esbuild,self" {
		t.Errorf("bundle engines=%v want [esbuild self]", names)
	}
	for _, k := range AllKinds() {
		if _, ok := cat.Find(k, SelfEngineName); !ok {
			t.Errorf("`self` must exist for kind %s (dry-run/兜底 needs a plan for every kind)", k)
		}
	}

	// 结构问题必须为空；`esbuild` 是否安装取决于机器，因此只过滤 schema 类
	for _, issue := range cat.Validate() {
		if issue.Kind == IssueSchema {
			t.Errorf("builtin catalog has a schema problem: %s", issue.Message)
		}
	}
}

func TestCatalog_OverlayAndFind(t *testing.T) {
	projectDir := t.TempDir()
	ngmHome := t.TempDir()

	testutils.WriteFile(t, ngmHome, FileName, `{
  "version": 1,
  "engines": [
    {"name": "global-only", "kind": "bundle", "adapter": "subprocess", "command": "global-tool"}
  ]
}`)
	testutils.WriteFile(t, projectDir, FileName, `{
  "version": 1,
  "engines": [
    {"name": "esbuild", "kind": "bundle", "adapter": "subprocess", "command": "my-esbuild --fast"},
    {"name": "project-only", "kind": "css", "adapter": "subprocess", "command": "proj-css"}
  ]
}`)

	cat, err := LoadCatalog(projectDir, ngmHome)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	// 项目条目**整条**替换内置条目（不是字段级合并）
	esb, ok := cat.Find(KindBundle, "esbuild")
	if !ok {
		t.Fatal("esbuild/bundle missing")
	}
	if esb.Builtin {
		t.Error("a project override must not be marked built-in")
	}
	if esb.Command != "my-esbuild --fast" {
		t.Errorf("command=%q want `my-esbuild --fast`", esb.Command)
	}
	if esb.Program != "my-esbuild" || strings.Join(esb.Args, " ") != "--fast" {
		t.Errorf("derived program/args=%q/%v", esb.Program, esb.Args)
	}
	// 整条替换意味着内置的 defaultOptions 不再残留
	if len(esb.DefaultOptions) != 0 {
		t.Errorf("overriding must replace the whole entry, but defaults survived: %v", esb.DefaultOptions)
	}

	if _, ok := cat.Find(KindBundle, "global-only"); !ok {
		t.Error("the global catalog must contribute entries too")
	}
	if _, ok := cat.Find(KindCSS, "project-only"); !ok {
		t.Error("project-only entry missing")
	}
	if e, ok := cat.Find(KindBundle, SelfEngineName); !ok || !e.Builtin {
		t.Error("built-in entries must survive a user overlay")
	}

	// 排序确定性：同 kind 内按名字升序
	names := cat.NamesFor(KindBundle)
	if strings.Join(names, ",") != "esbuild,global-only,self" {
		t.Errorf("bundle order=%v want sorted", names)
	}
}

func TestLoadCatalog_RejectsBadVersion(t *testing.T) {
	projectDir := t.TempDir()
	testutils.WriteFile(t, projectDir, FileName, `{"version": 99, "engines": []}`)

	_, err := LoadCatalog(projectDir, "")
	if err == nil {
		t.Fatal("an unsupported catalog version must be an error")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code=%d want 3", code)
	}
}

func TestLoadCatalog_NoFileMeansBuiltinOnly(t *testing.T) {
	cat, err := LoadCatalog(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(cat.EntriesFor(KindBundle)) != 2 {
		t.Errorf("expected the builtin bundle entries, got %d", len(cat.EntriesFor(KindBundle)))
	}
}

func TestCatalogValidate_ReportsEveryProblemClass(t *testing.T) {
	cat := &Catalog{Version: CatalogVersion, Engines: []Entry{
		{Name: "ghost", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "ngm-definitely-not-a-real-binary"},
		{Name: "empty-cmd", Kind: KindBundle, Adapter: AdapterSubprocess},
		{Name: "bad-kind", Kind: "nope", Adapter: AdapterSubprocess, Command: "x"},
		{Name: "bad-adapter", Kind: KindBundle, Adapter: "nope", Command: "x"},
		{Name: "wasm-one", Kind: KindBundle, Adapter: AdapterWasm, Command: "x"},
		{Name: "fake-stub", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "x", Stub: true},
		{Name: "embed-unknown", Kind: KindBundle, Adapter: AdapterEmbed},
		{Name: "dup", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "x"},
		{Name: "dup", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "x"},
	}}
	cat.normalize()

	issues := cat.Validate()
	byEntry := map[string][]IssueKind{}
	for _, is := range issues {
		byEntry[is.Entry] = append(byEntry[is.Entry], is.Kind)
	}

	// 注意 bad-kind 的标签用的是**非法 kind 值本身**（bad-kind/nope）：
	// 条目标识由 name+kind 组成，kind 非法时无法归一到合法 kind。
	want := map[string]IssueKind{
		"ghost/bundle":         IssueUnavailable,
		"empty-cmd/bundle":     IssueSchema,
		"bad-kind/nope":        IssueSchema,
		"bad-adapter/bundle":   IssueSchema,
		"wasm-one/bundle":      IssueUnimplemented,
		"fake-stub/bundle":     IssueSchema,
		"embed-unknown/bundle": IssueUnimplemented,
		"dup/bundle":           IssueSchema,
	}
	for entry, kind := range want {
		found := false
		for _, got := range byEntry[entry] {
			if got == kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: expected a %s issue, got %v", entry, kind, byEntry[entry])
		}
	}

	// 退出码：schema 错误优先于可用性问题（清单结构坏了，可用性结论没意义）
	if code := ExitCode(issues); code != 3 {
		t.Errorf("exit code=%d want 3 (schema errors take precedence; issues: %v)", code, issues)
	}

	// 只有可用性问题时才是 5
	onlyUnavailable := []Issue{{Entry: "ghost/bundle", Kind: IssueUnavailable, Message: "not on PATH"}}
	if code := ExitCode(onlyUnavailable); code != 5 {
		t.Errorf("exit code=%d want 5 when only availability is wrong", code)
	}
	if code := ExitCode(nil); code != 0 {
		t.Errorf("exit code=%d want 0 for a clean catalog", code)
	}
}

// ---------------------------------------------------------------------------
// argv 翻译
// ---------------------------------------------------------------------------

func TestBuildInvocation_EsbuildBundle(t *testing.T) {
	entry, ok := BuiltinCatalog().Find(KindBundle, "esbuild")
	if !ok {
		t.Fatal("builtin esbuild/bundle missing")
	}

	inv, err := buildInvocation(entry, buildRequest{
		Options: BundleOptions{
			Outfile:    "dist/index.js",
			Production: true,
			Alias:      map[string]string{"github:o/r": "./ngm.vendor/github.com/o/r"},
		},
		EntryFile: "src/index.ts",
	})
	if err != nil {
		t.Fatalf("buildInvocation: %v", err)
	}

	want := []string{
		"--bundle",
		"--outfile=dist/index.js",
		// 选项按键排序，保证 argv 可复现
		`--define:process.env.NODE_ENV="production"`,
		"--minify",
		"--alias:github:o/r=./ngm.vendor/github.com/o/r",
		"src/index.ts", // 入口永远在最后
	}
	if got := strings.Join(inv.Args, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("args:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	if inv.ReadsStdin {
		t.Error("bundle reads the entry from the filesystem, not stdin")
	}
}

func TestBuildInvocation_EsbuildTransformUsesStdin(t *testing.T) {
	entry, _ := BuiltinCatalog().Find(KindTransform, "esbuild")

	inv, err := buildInvocation(entry, buildRequest{
		Options: TransformOptions{Loader: "ts", SourceMaps: true},
	})
	if err != nil {
		t.Fatalf("buildInvocation: %v", err)
	}
	if !inv.ReadsStdin {
		t.Error("transform must pipe input via stdin (the docs' 小文件 path)")
	}
	// sourcemap 必须是 inline：stdout 输出不允许外链 source map
	joined := strings.Join(inv.Args, " ")
	for _, want := range []string{"--loader=ts", "--sourcemap=inline"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q should contain %q", joined, want)
		}
	}
}

func TestBuildInvocation_TransformNeedsLoader(t *testing.T) {
	entry, _ := BuiltinCatalog().Find(KindTransform, "esbuild")
	_, err := buildInvocation(entry, buildRequest{Options: TransformOptions{}})
	if err == nil {
		t.Fatal("reading from stdin without a loader must be rejected (esbuild would guess wrong)")
	}
}

func TestBuildInvocation_GenericCustomEngine(t *testing.T) {
	entry := Entry{
		Name: "my-tool", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "my-tool",
	}
	entry.Program, entry.Args = splitCommand(entry.Command)

	inv, err := buildInvocation(entry, buildRequest{
		Options:   BundleOptions{Outfile: "out/index.js", Extra: map[string]any{"mode": "prod"}},
		EntryFile: "src/x.ts",
	})
	if err != nil {
		t.Fatalf("buildInvocation: %v", err)
	}
	want := []string{"--kind=bundle", "--mode=prod", "--outfile=out/index.js", "src/x.ts"}
	if got := strings.Join(inv.Args, " "); got != strings.Join(want, " ") {
		t.Errorf("args=%q want %q (P4: options as --key=value, file as a positional)", got, strings.Join(want, " "))
	}
}

// TestBuildInvocation_EsbuildRefusesTypeCheck 固定"禁止静默降级"。
//
// esbuild 只删类型标注、不做检查。"当作通过"是最危险的失败模式：
// CI 会以为类型是干净的。因此必须明确报错。
func TestBuildInvocation_EsbuildRefusesTypeCheck(t *testing.T) {
	entry, _ := BuiltinCatalog().Find(KindTransform, "esbuild")
	entry.Kind = KindTypeCheck

	_, err := buildInvocation(entry, buildRequest{Options: TypeCheckOptions{}, EntryFile: "a.ts"})
	if err == nil {
		t.Fatal("esbuild must not be accepted as a type-checking engine")
	}
	if code := errs.ExitCode(err); code != 5 {
		t.Errorf("exit code=%d want 5", code)
	}
	if !strings.Contains(err.Error(), "type-check") {
		t.Errorf("error should say what esbuild cannot do: %v", err)
	}
}

// ---------------------------------------------------------------------------
// self stub
// ---------------------------------------------------------------------------

func TestSelfEngine_RefusesEverything(t *testing.T) {
	entry, ok := BuiltinCatalog().Find(KindBundle, SelfEngineName)
	if !ok {
		t.Fatal("self/bundle missing")
	}
	eng, err := NewEngine(entry, t.TempDir())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if !eng.Available() {
		t.Error("the embedded stub is always available (it is in-process)")
	}

	ctx := context.Background()
	var failures []error
	if _, e := eng.(BundleEngine).Bundle(ctx, "a.ts", BundleOptions{}); e != nil {
		failures = append(failures, e)
	}
	if _, e := eng.(TransformEngine).Transform(ctx, []byte("x"), TransformOptions{}); e != nil {
		failures = append(failures, e)
	}
	if _, e := eng.(TypeCheckEngine).Check(ctx, "a.ts", TypeCheckOptions{}); e != nil {
		failures = append(failures, e)
	}
	if _, e := eng.(TypeDeclEngine).GenerateTypeDecl(ctx, "a.ts", TypeDeclOptions{}); e != nil {
		failures = append(failures, e)
	}
	if _, e := eng.(CSSEngine).Compile(ctx, []byte("a{}"), CSSOptions{}); e != nil {
		failures = append(failures, e)
	}

	if len(failures) != 5 {
		t.Fatalf("the stub must refuse all 5 capabilities, got %d refusals", len(failures))
	}
	for _, e := range failures {
		var ee *EngineError
		if !asEngineError(e, &ee) {
			t.Errorf("expected *EngineError, got %T", e)
			continue
		}
		if ee.Code != -1 {
			t.Errorf("a stub never runs, so Code must be -1 (got %d)", ee.Code)
		}
		if !ee.Fallback {
			t.Error("the stub must allow falling back to another engine")
		}
		if errs.ExitCode(ee.AsNgmError()) != 5 {
			t.Error("a stub refusal maps to exit 5 (no usable engine)")
		}
	}
}

// asEngineError 是 errors.As 的本地薄封装（避免为一行引入 errors 到本文件的
// 所有导入者）。
func asEngineError(err error, target **EngineError) bool {
	ee, ok := err.(*EngineError)
	if ok {
		*target = ee
	}
	return ok
}

// ---------------------------------------------------------------------------
// 执行链（回退 / 退出码）
// ---------------------------------------------------------------------------

// fakeCatalog 构造一个只含指定条目的清单，并把 fake 引擎的路径写进去。
func fakeCatalog(t *testing.T, fakePath string, entries ...Entry) *Catalog {
	t.Helper()
	cat := &Catalog{Version: CatalogVersion, Engines: entries}
	if fakePath != "" {
		// 路径可能含空白，用引号包裹——这也是 splitCommand 支持引号的原因
		cat.Engines = append(cat.Engines, Entry{
			Name: "fake", Kind: KindBundle, Adapter: AdapterSubprocess,
			Command: `"` + fakePath + `"`,
		})
	}
	cat.normalize()
	return cat
}

func TestRunner_FallsBackWhenPrimaryIsUnavailable(t *testing.T) {
	testutils.MustHaveGit(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	cat := fakeCatalog(t, fake,
		Entry{Name: "ghost", Kind: KindBundle, Adapter: AdapterSubprocess, Command: "ngm-definitely-not-a-real-binary"})
	r := NewRunner(cat, t.TempDir())

	var warned []string
	r.OnWarn(func(format string, args ...any) { warned = append(warned, format) })

	res, err := r.Bundle(context.Background(),
		Selection{Primary: "ghost", Fallbacks: []string{"fake"}}, "src/index.ts", BundleOptions{})
	if err != nil {
		t.Fatalf("the fallback engine should have produced a bundle: %v", err)
	}
	if len(res.Code) == 0 {
		t.Error("expected bundle output on stdout")
	}
	if len(warned) == 0 {
		t.Error("a fallback must be announced on stderr — otherwise users cannot tell which engine ran")
	}
}

func TestRunner_AllUnavailableExits5(t *testing.T) {
	cat := fakeCatalog(t, "", Entry{
		Name: "ghost", Kind: KindBundle, Adapter: AdapterSubprocess,
		Command: "ngm-definitely-not-a-real-binary",
	})
	r := NewRunner(cat, t.TempDir())

	_, err := r.Bundle(context.Background(), Selection{Primary: "ghost"}, "src/index.ts", BundleOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if code := errs.ExitCode(err); code != 5 {
		t.Errorf("exit code=%d want 5 (no engine ever ran)", code)
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error should say the engine is missing: %v", err)
	}
}

func TestRunner_EngineFailureExits1AndKeepsStderr(t *testing.T) {
	testutils.MustHaveGit(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	t.Setenv("FAKE_EXIT", "3")
	t.Setenv("FAKE_STDERR", "error: unexpected token at line 12")

	cat := fakeCatalog(t, fake)
	r := NewRunner(cat, t.TempDir())

	_, err := r.Bundle(context.Background(), Selection{Primary: "fake"}, "src/index.ts", BundleOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if code := errs.ExitCode(err); code != 1 {
		t.Errorf("exit code=%d want 1 (the engine ran and failed)", code)
	}
	// 引擎自己的诊断必须原样保留——它是用户唯一有用的线索
	if !strings.Contains(err.Error(), "unexpected token at line 12") {
		t.Errorf("the engine's stderr must be preserved verbatim:\n%v", err)
	}
}

func TestRunner_UnknownEngineNameIsConfigError(t *testing.T) {
	r := NewRunner(BuiltinCatalog(), t.TempDir())
	_, err := r.Bundle(context.Background(), Selection{Primary: "nope"}, "a.ts", BundleOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code=%d want 3 (an unknown name is a configuration error)", code)
	}
	// Hint 只在 FormatHuman 里输出（NgmError.Error() 不含它），因此断言 render 后的文本
	if human := errs.FormatHuman(err); !strings.Contains(human, "available") {
		t.Errorf("the hint should list the known engines:\n%s", human)
	}
}

// TestRunner_DryRunNeverExecutes 固定 --dry-run 的语义：只规划，不执行。
//
// 用假引擎的 dump 文件作为证据：如果 ngm 真调了引擎，文件就会出现。
func TestRunner_DryRunNeverExecutes(t *testing.T) {
	testutils.MustHaveGit(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	dump := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("FAKE_DUMP_ARGS", dump)

	cat := fakeCatalog(t, fake)
	r := NewRunner(cat, t.TempDir())

	plans, err := r.Plans(KindBundle, Selection{Primary: "fake"},
		BundleOptions{Outfile: "out.js"}, "src/index.ts")
	if err != nil {
		t.Fatalf("Plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans=%d want 1", len(plans))
	}
	p := plans[0]
	if p.Engine != "fake" || !p.Available {
		t.Errorf("plan=%+v", p)
	}
	// 自定义引擎走通用映射：能力类别显式给出，参数按键排序
	if !strings.Contains(p.CommandLine(), "--kind=bundle") {
		t.Errorf("plan command=%q should carry --kind=bundle", p.CommandLine())
	}
	if !strings.Contains(p.CommandLine(), "--outfile=out.js") {
		t.Errorf("plan command=%q should carry --outfile", p.CommandLine())
	}
	if _, err := os.Stat(dump); err == nil {
		t.Fatal("--dry-run must not execute the engine")
	}
}

// TestRunner_DryRunPlansTheStub 固定"stub 只允许出现在规划里"。
func TestRunner_DryRunPlansTheStub(t *testing.T) {
	r := NewRunner(BuiltinCatalog(), t.TempDir())

	plans, err := r.Plans(KindBundle, Selection{Primary: SelfEngineName}, BundleOptions{}, "a.ts")
	if err != nil {
		t.Fatalf("planning the stub must work: %v", err)
	}
	if len(plans) != 1 || !plans[0].Stub {
		t.Fatalf("plans=%+v want one stub plan", plans)
	}

	// 真跑就必须拒绝（禁止静默降级）
	_, berr := r.Bundle(context.Background(), Selection{Primary: SelfEngineName}, "a.ts", BundleOptions{})
	if berr == nil {
		t.Fatal("running the stub must fail")
	}
	if code := errs.ExitCode(berr); code != 5 {
		t.Errorf("exit code=%d want 5", code)
	}
}
