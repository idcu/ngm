package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.108：每一次 OSV 查询都必须带上网关门禁。
//
// 缺陷形态（上一轮审计）：`OSVConfig.CheckNet` 为 nil 时**没有门禁**——
// 而"忘了传"在调用点看起来与"策略放行"完全一样。这与项目里其它门禁
// （git 的 `run:git`、引擎的 `run:<engine>`、沙箱的 `net:<host>`）相反：
// 同一个错误在别处是拒绝，在这里是放行。
//
// 为什么**不**把 nil 改成内部错误：那是本包已文档化的语义
// （`OSVConfig` 的字段注释 + `nil CheckNet keeps the previous behaviour` 那条用例），
// 而且测试替身本来就没有策略。与 v0.105 修 `git.Options.Policy` 同一个判断：
// 施力点是**调用方必须传**，用一条源码级判据钉住，而不是改库的语义。
func TestV108EveryOSVQueryCarriesTheGate(t *testing.T) {
	root := repoRoot(t)

	var sites int
	var missing []string

	// 审计 T-08：旧版在字面量后 24 行窗口里找 `CheckNet:` 子串，注释里写同名字样即可蒙混。
	// 改为 AST：只认 OSVConfig 复合字面量里**真实存在**的 CheckNet 键值对。
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "ngm.vendor", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			name := ""
			switch typ := lit.Type.(type) {
			case *ast.Ident:
				name = typ.Name
			case *ast.SelectorExpr:
				name = typ.Sel.Name // 生产路径写作 supplychain.OSVConfig{...}
			}
			if name != "OSVConfig" {
				return true
			}
			sites++
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "CheckNet" {
					return true
				}
			}
			missing = append(missing, fset.Position(lit.Pos()).String())
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	for _, m := range missing {
		t.Errorf("an OSVConfig literal without `CheckNet:` at %s — "+
			"a nil gate means the query leaves the process without asking `net:<host>`", m)
	}
	if sites < 2 {
		t.Fatalf("only %d OSVConfig literal(s) scanned: the net's scope shrank to nothing", sites)
	}
}
