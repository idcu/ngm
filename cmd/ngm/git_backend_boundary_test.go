package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGitBackendBoundary 守 ADR-038 的运行时边界：
// 除 internal/core/git 外，非测试 Go 文件不得直接启动 git 子进程。
//
// 测试 fixture 可以直连 git（构造仓库/HTTP backend 是测试职责），所以这里只
// 扫描非 _test.go。发现新的直连点应当在 core/git 增加 API，而不是散落 exec。
func TestGitBackendBoundary(t *testing.T) {
	root := repoRoot(t)
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "ngm.vendor" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel := rel(root, path)
		if strings.HasPrefix(rel, "internal/core/git/") || strings.HasPrefix(rel, "internal/testutils/") {
			return nil
		}
		checked++
		full, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(full, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "exec" {
				return true
			}
			idx := -1
			switch sel.Sel.Name {
			case "Command":
				idx = 0
			case "CommandContext":
				idx = 1
			default:
				return true
			}
			if idx >= len(call.Args) {
				return true
			}
			lit, ok := call.Args[idx].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, _ := strconv.Unquote(lit.Value); s == "git" {
				t.Errorf("%s directly starts git; add a helper in internal/core/git", rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan Git backend boundary: %v", err)
	}
	if checked < 50 {
		t.Fatalf("only %d non-test files checked — boundary test is vacuous", checked)
	}
	if _, err := fs.Stat(os.DirFS(root), "internal/core/git/spawn.go"); err != nil {
		t.Fatalf("internal/core/git/spawn.go missing: %v", err)
	}
}
