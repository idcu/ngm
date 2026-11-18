package adapter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProbe_VersionIsOnlyCalledFromGatedPaths 固定一条性质：
//
//	除 Catalog.Probe（探测前有 run:<engine> 闸门）与它专用的
//	probeVersion 辅助函数（只在已过闸门的真实执行路径上调用）外，
//	生产代码不得直接调用 .Version()——那是一次真实的 spawn。
//
// 为什么用源码机械断言而不是运行时探针计数：调用顺序（"preflight 之后自然
// 被闸门覆盖"）是约定，不是类型约束；任何新增路径直接调 Version() 都会
// 悄悄绕开闸门，而计数式测试只覆盖它跑到的那几条路径。
// 用 AST 而非字符串匹配，避免注释里提到 `.Version()` 被误判。
// 形态仿 internal/core/git 的 TestSpawn_OnlyOneFileBuildsProcesses。
func TestProbe_VersionIsOnlyCalledFromGatedPaths(t *testing.T) {
	allowed := map[string]bool{
		"catalog.go":       true, // Probe：probeGate 判定之后才 Version()
		"engines_extra.go": true, // probeVersion：仅服务已过闸门的真实执行
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sawAllowed := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if perr != nil {
			t.Fatal(perr)
		}
		hasDirectVersionCall := false
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok {
				if sel, sok := call.Fun.(*ast.SelectorExpr); sok && sel.Sel.Name == "Version" {
					hasDirectVersionCall = true
				}
			}
			return true
		})
		if allowed[name] {
			sawAllowed = sawAllowed || hasDirectVersionCall
			continue
		}
		if hasDirectVersionCall {
			t.Errorf("%s calls .Version() directly; version probing is a real spawn and must go "+
				"through Catalog.Probe (which checks run:<engine> before spawning)", name)
		}
	}
	if !sawAllowed {
		t.Error("no allowed .Version() call found — this check would pass vacuously; " +
			"if the probe path moved, update the allowed set")
	}
}
