package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// flagReference 匹配人读文本里提到的长 flag。整词匹配：否则
// `--noEmit`（外部 tsc 的参数）会被切成 `--no`，冤枉 typecheck。
var flagReference = regexp.MustCompile(`--([A-Za-z0-9][A-Za-z0-9-]*)`)

// crossFileFlags 是"在本文件里没声明、但确实属于别处"的引用。
// 每条都要写清它指向哪里；只写"历史原因"不算理由。
var crossFileFlags = map[string]string{
	// flag.FlagSet 自带；root/main 的 --version 是全局的。
	"help":    "flag.FlagSet provides -h/--help to every command",
	"version": "root-level flag declared outside the command files",
	"dir":     "dispatch-level flag in root.go; most commands also declare it locally",
	"json":    "command-specific; root.go documents the envelope contract",
	// 生成给外部引擎 / git 的参数，不是 ngm 自己的 flag。
	"alias":       "esbuild --alias generated from ngm.mappings.json, not an ngm flag",
	"abbrev-ref":  "git argument in modules.go diagnostics",
	"empty":       "git argument in modules.go diagnostics",
	"format":      "git for-each-ref --format in modules.go collection index, not an ngm flag",
	"git-dir":     "git argument in modules.go diagnostics",
	"index-info":  "git argument in modules.go diagnostics",
	"list":        "git argument in modules.go diagnostics",
	"name-only":   "git argument in modules.go diagnostics",
	"quiet":       "git argument in modules.go diagnostics",
	"short":       "git argument in modules.go diagnostics",
	"stdin-paths": "git argument in modules.go diagnostics",
	"verify":      "git argument in modules.go diagnostics",
	// 跨文件但确实是 ngm 的 flag / 明确指向另一条命令。
	"engine":         "shared engine context documents the flag declared by build/transform/typecheck/typedecl",
	"offline":        "install/verify flag; help.go prints cross-command guidance",
	"out":            "sbom/attest --out; help.go explains the exit-code contract across commands",
	"apply":          "`ngm gc --apply`, named explicitly by store.go's usage text",
	"require-signed": "declared in verify.go; this file renders its report",
	"signatures":     "declared in verify.go; this file renders its report",
}

// TestHintsNameFlagsTheCommandAccepts 抓"报错让人去用一个不存在的 flag"。
//
// 起点：`ngm transform` 建输出目录失败时提示 "point --outdir at a directory
// that exists"，而 transform 的 flag 叫 `--outfile`。这类错位不会让任何现有
// 判据变红——脚本照样拿到 exit 3，人却按提示去试一个会被拒的选项。
//
// 口径：按**文件**派生 flag（同一文件里可以有多个子命令，如 modules.go），
// 文本只看字符串字面量；注释里的 `--xxx` 是给维护者看的，不算给用户的提示。
func TestHintsNameFlagsTheCommandAccepts(t *testing.T) {
	root := repoRoot(t)
	ents, err := os.ReadDir(filepath.Join(root, "cmd", "ngm"))
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	mentions := 0
	allReferenced := map[string]bool{}
	var diffs []string

	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(root, "cmd", "ngm", name)
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatal(perr)
		}
		declared := map[string]bool{}
		referenced := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok {
				if sel, sok := call.Fun.(*ast.SelectorExpr); sok && len(call.Args) > 0 {
					switch sel.Sel.Name {
					// v0.143：与 help_subcommand_test.go 的 flagRegistrationRe 同口径，
					// 补 Int64/Uint/Uint64——漏掉它们会把合法 hint 误报成"未声明的 flag"（审计 T-03）。
					case "String", "Bool", "Int", "Int64", "Uint", "Uint64", "Duration", "Float64":
						if lit, lok := call.Args[0].(*ast.BasicLit); lok && lit.Kind == token.STRING {
							if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
								declared[s] = true
							}
						}
					}
				}
			}
			if lit, lok := n.(*ast.BasicLit); lok && lit.Kind == token.STRING {
				if s, uerr := strconv.Unquote(lit.Value); uerr == nil {
					for _, m := range flagReference.FindAllStringSubmatch(s, -1) {
						// 大小写混写的名字属于外部工具（tsc 的 --noEmit 等），
						// 不是 ngm 的 flag。
						if m[1] != strings.ToLower(m[1]) {
							continue
						}
						referenced[m[1]] = true
						mentions++
					}
				}
			}
			return true
		})
		files++
		for flag := range referenced {
			allReferenced[flag] = true
			if declared[flag] {
				continue
			}
			if why, ok := crossFileFlags[flag]; ok {
				t.Logf("%s: --%s — %s", name, flag, why)
				continue
			}
			diffs = append(diffs, name+": --"+flag)
		}
	}
	if files < 20 || mentions < 100 {
		t.Fatalf("scan too small: %d files, %d flag mentions — the net is vacuous", files, mentions)
	}
	sort.Strings(diffs)
	for _, d := range diffs {
		t.Errorf("%s is not a flag declared in that file (fix the hint or register the cross-command reference)", d)
	}
	for flag := range crossFileFlags {
		if !allReferenced[flag] {
			t.Errorf("stale cross-file entry --%s: no hint mentions it any more; remove the entry", flag)
		}
	}
}
