package adapter

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// ---------------------------------------------------------------------------
// typescript（tsc）
// ---------------------------------------------------------------------------

func TestTypescript_TypeCheckUsesNoEmit(t *testing.T) {
	// --noEmit 是类型检查的定义性参数：没有它 tsc 会产出 JS 文件
	inv, err := buildInvocation(Entry{Name: "typescript", Kind: KindTypeCheck, Program: "tsc"},
		buildRequest{Options: TypeCheckOptions{}, EntryFile: "src/index.ts"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inv.Args, " ")
	if !strings.Contains(joined, "--noEmit") {
		t.Errorf("tsc type-check must pass --noEmit, got %v", inv.Args)
	}
	if !strings.Contains(joined, "src/index.ts") {
		t.Errorf("the entry file should be passed, got %v", inv.Args)
	}
	if inv.ReadsStdin {
		t.Errorf("tsc reads from files, not stdin")
	}
}

// --project 与位置入口互斥（tsc 会报错），给了 project 就不能再传入口
func TestTypescript_ProjectReplacesEntryFile(t *testing.T) {
	inv, err := buildInvocation(Entry{Name: "typescript", Kind: KindTypeCheck, Program: "tsc"},
		buildRequest{Options: TypeCheckOptions{TSConfig: "tsconfig.build.json"}, EntryFile: "src/index.ts"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inv.Args, " ")
	if !strings.Contains(joined, "--project") || !strings.Contains(joined, "tsconfig.build.json") {
		t.Errorf("expected --project <tsconfig>, got %v", inv.Args)
	}
	if strings.Contains(joined, "src/index.ts") {
		t.Errorf("--project and a positional entry must not be combined: %v", inv.Args)
	}
}

func TestTypescript_TypeDeclEmitsDeclarations(t *testing.T) {
	inv, err := buildInvocation(Entry{Name: "typescript", Kind: KindTypeDecl, Program: "tsc"},
		buildRequest{Options: TypeDeclOptions{OutDir: "dist/types"}, EntryFile: "src/index.ts"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inv.Args, " ")
	for _, want := range []string{"--declaration", "--outDir", "dist/types"} {
		if !strings.Contains(joined, want) {
			t.Errorf("typeDecl should contain %q, got %v", want, inv.Args)
		}
	}
}

// 做不到的能力必须报错（exit 5），不能静默当作成功
func TestTypescript_RefusesOtherKinds(t *testing.T) {
	_, err := buildInvocation(Entry{Name: "typescript", Kind: KindBundle, Program: "tsc"},
		buildRequest{Options: BundleOptions{}, EntryFile: "src/index.ts"})
	if err == nil {
		t.Fatal("tsc must refuse to bundle")
	}
	if code := errs.ExitCode(err); code != 5 {
		t.Errorf("exit = %d, want 5 (engine unavailable)", code)
	}
}

// ---------------------------------------------------------------------------
// postcss
// ---------------------------------------------------------------------------

func TestPostcss_KeepsPluginOrder(t *testing.T) {
	// 插件顺序有语义（先 autoprefixer 还是先 nesting 结果不同），不能被排序打乱
	inv, err := buildInvocation(Entry{Name: "postcss", Kind: KindCSS, Program: "postcss"},
		buildRequest{Options: CSSOptions{
			Outfile: "dist/app.css",
			Extra:   map[string]any{"use": []any{"nesting", "autoprefixer"}},
		}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(inv.Args, " ")
	if !strings.Contains(joined, "--use nesting --use autoprefixer") {
		t.Errorf("plugin order must be preserved, got %v", inv.Args)
	}
	if !strings.Contains(joined, "--output dist/app.css") {
		t.Errorf("expected --output, got %v", inv.Args)
	}
	if !inv.ReadsStdin {
		t.Errorf("postcss input comes from stdin")
	}
}

// postcss 没有内建压缩：静默忽略 --minify 会让人以为产物被压缩过
func TestPostcss_SaysSoWhenMinifyIsIgnored(t *testing.T) {
	inv, err := buildInvocation(Entry{Name: "postcss", Kind: KindCSS, Program: "postcss"},
		buildRequest{Options: CSSOptions{Minify: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Notes) == 0 {
		t.Fatal("postcss must report that --minify cannot be honoured, not drop it silently")
	}
	if !strings.Contains(strings.Join(inv.Notes, " "), "minifier") {
		t.Errorf("the note should explain why: %v", inv.Notes)
	}
}

func TestPostcss_RefusesOtherKinds(t *testing.T) {
	_, err := buildInvocation(Entry{Name: "postcss", Kind: KindBundle, Program: "postcss"},
		buildRequest{Options: BundleOptions{}, EntryFile: "src/index.ts"})
	if err == nil {
		t.Fatal("postcss must refuse to bundle")
	}
	if code := errs.ExitCode(err); code != 5 {
		t.Errorf("exit = %d, want 5", code)
	}
}

// ---------------------------------------------------------------------------
// deno
// ---------------------------------------------------------------------------

func TestDeno_TypeCheckPassesEntry(t *testing.T) {
	inv, err := buildInvocation(Entry{Name: "deno", Kind: KindTypeCheck, Program: "deno", Args: []string{"check"}},
		buildRequest{Options: TypeCheckOptions{}, EntryFile: "src/index.ts"})
	if err != nil {
		t.Fatal(err)
	}
	// 子命令来自清单 command 的前缀（"deno check" → 前缀 ["check"]），
	// buildInvocation 会把它放在最前；builder 只补可变参数，不重复子命令。
	if strings.Join(inv.Args, " ") != "check src/index.ts" {
		t.Errorf("args = %v, want [check src/index.ts]", inv.Args)
	}
}

// ---------------------------------------------------------------------------
// 版本门槛
// ---------------------------------------------------------------------------

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		actual, min string
		want        bool
	}{
		{"2.4.3", "2.4", true},
		{"2.4.0", "2.4", true},
		{"2.5.0", "2.4", true},
		{"deno 2.4.3", "2.4", true}, // 版本串常带程序名前缀
		{"2.3.9", "2.4", false},
		{"1.46.0", "2.4", false},
		{"unknown", "2.4", false}, // 解析不出：按"不够新"处理
		{"", "2.4", false},
	}
	for _, c := range cases {
		if got := versionAtLeast(c.actual, c.min); got != c.want {
			t.Errorf("versionAtLeast(%q, %q) = %v, want %v", c.actual, c.min, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 内置清单与 optional 语义
// ---------------------------------------------------------------------------

func TestBuiltinCatalog_IncludesAdaptedEngines(t *testing.T) {
	c := BuiltinCatalog()
	for _, key := range []string{"typescript/typeCheck", "typescript/typeDecl", "postcss/css"} {
		found := false
		for _, e := range c.Engines {
			if e.Key() == key {
				found = true
				if !e.Optional {
					t.Errorf("%s must be optional: not every project type-checks or uses postcss", key)
				}
			}
		}
		if !found {
			t.Errorf("the built-in catalog should include %s", key)
		}
	}
}

// deno 刻意不进内置清单：它的 bundle 是实验特性，且会抢掉 typeCheck 的默认顺序
func TestBuiltinCatalog_ExcludesDeno(t *testing.T) {
	for _, e := range BuiltinCatalog().Engines {
		if e.Name == "deno" {
			t.Errorf("deno must stay opt-in (declared in ngm.engines.json), got %s", e.Key())
		}
	}
}

// 这条固定 E 组最关键的取舍：内置了 tsc / postcss，但"没装"不能变成 issue
func TestValidate_OptionalEnginesDoNotFailWhenMissing(t *testing.T) {
	for _, is := range BuiltinCatalog().Validate() {
		if strings.HasPrefix(is.Entry, "typescript/") || strings.HasPrefix(is.Entry, "postcss/") {
			t.Errorf("an optional engine that is not installed must not be an issue: %+v", is)
		}
	}
}
