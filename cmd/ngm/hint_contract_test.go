package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// emptyHintCeiling 是**用户层**（`cmd/ngm`）里"空 hint 构造点"的上限。
//
// 它是一道**棘轮**：只许往下走。理由是这些错误会**直接**送到 `runErr` →
// `FormatHuman`，也就是用户眼前——没有 hint 就等于**没有下一步**。
//
// v0.28 之前 `FormatHuman` 只打印最外层那条错误的 hint，于是"内层有建议、
// 外层没给"就丢掉了（实测：只剩 `cause:` 一行）。渲染处已修成沿 `Cause` 链取
// 第一条非空 hint，因此**空 hint 的构造点不再一定意味着用户看不到建议**——
// 但"直接构造一条没有建议的错误、下面也没有可继承的东西"仍然存在，
// 这个读数就是看着它的。
//
// 怎么降这个数：给那条错误写一句可执行的下一步（`ngm config validate` 这类）。
const emptyHintCeiling = 11

// TestV28UserFacingErrorsCarryAHint 是这道棘轮：用户层的空 hint 构造点不许变多。
//
// 它**不**要求零：有些错误（"这东西本来就不存在"）确实没有比"它不存在"更有用的
// 下一步，硬写一句会变成噪音。它守的是**不许变坏**，并把当前读数写进测试输出。
func TestV28UserFacingErrorsCarryAHint(t *testing.T) {
	var empty []string
	total := 0

	matches, err := filepath.Glob("./*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("cannot parse %s: %v", path, perr)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "errs" {
				return true
			}
			var hintIdx, want int
			switch sel.Sel.Name {
			case "New":
				hintIdx, want = 2, 3
			case "Wrap":
				hintIdx, want = 2, 4
			default:
				return true
			}
			total++
			if len(call.Args) != want {
				t.Errorf("%s: errs.%s with %d args, want %d",
					filepath.Base(path), sel.Sel.Name, len(call.Args), want)
				return true
			}
			lit, isLit := call.Args[hintIdx].(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true // 变量拼出来的：静态看不出来，不算空
			}
			s, uerr := strconv.Unquote(lit.Value)
			if uerr == nil && strings.TrimSpace(s) == "" {
				empty = append(empty, fmt.Sprintf("%s:%d", filepath.Base(path), fset.Position(call.Pos()).Line))
			}
			return true
		})
	}

	if total == 0 {
		t.Fatal("no errs.New/Wrap call site found in cmd/ngm — the net's scope shrank to nothing")
	}
	if len(empty) > emptyHintCeiling {
		sort.Strings(empty)
		t.Errorf("cmd/ngm has %d errs.New/Wrap sites with an empty hint, the ceiling is %d — "+
			"a user-facing error with no next step is half a message:\n  %s",
			len(empty), emptyHintCeiling, strings.Join(empty, "\n  "))
	}
	t.Logf("user-facing error sites: %d, empty hint: %d (ceiling %d)", total, len(empty), emptyHintCeiling)
}
