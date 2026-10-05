package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// emptyHintCeiling 是**用户层**（`cmd/ngm`）里"空 hint 构造点"的上限。
//
// 它现在是 **0**——一道硬门禁，不再是棘轮。
//
// 这个数变过：v0.28 立它时定的是 11，理由写在当时的复盘里——
// "有些错误（'这东西本来就不存在'）确实没有比'它不存在'更有用的下一步"。
// v0.29 把那 11 处**逐条看了一遍**：每一处都有真实的下一步
// （`--dir` 解析失败 → 传绝对路径；写文件失败 → 检查可写性，并说清**已经写了什么、没写什么**；
// 依赖定义非法 → 给出 slug 的形状与 `--help`）。
// 于是判断翻案，上限收到 0。**"没法更好"这种结论，也得逐条看过才能下。**
//
// 这些错误会**直接**送到 `runErr` → `FormatHuman`，也就是用户眼前：
// 没有 hint 就等于**没有下一步**。
//
// 顺带一提：v0.28 修的"沿 `Cause` 链继承 hint"并没有让这条变松——
// 继承只在**下层有建议**时有效；这些站点下面接的是原生 `error`，没有可继承的东西。
const emptyHintCeiling = 0

// TestV28UserFacingErrorsCarryAHint 守住：用户层构造的每条错误都要给出下一步。
//
// 判据是"**不许有**空 hint 字面量"（变量拼出来的静态看不出来，不算）。
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

// TestV29TheFirstRunErrorTellsYouWhatToDo 是上一条的**端到端**对照：
// 前面那张网证明"源码里写了 hint"，这一张证明**它真的出现在用户眼前**。
//
// 场景选的是最典型的一种：`ngm init` 的目标路径建不出来。
// 用"父路径是个**文件**"制造它——确定性的（`MkdirAll` 必失败），
// 而且正是第一次上手时可能撞到的东西（路径打错、或把文件当目录）。
func TestV29TheFirstRunErrorTellsYouWhatToDo(t *testing.T) {
	isolateUserEnv(t)
	base := t.TempDir()

	blocker := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("this is a file, not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(blocker, "app")

	var out, errb bytes.Buffer
	code := dispatch([]string{"init", "github.com:x/app", "--runtime=node", "--dir=" + target}, &out, &errb)
	if code != 3 {
		t.Fatalf("a directory that cannot be created is a configuration error (exit 3), got %d:\n%s",
			code, errb.String())
	}
	msg := errb.String()
	if !strings.Contains(msg, "hint:") {
		t.Errorf("the first command a user runs gave them no next step:\n%s", msg)
	}
	if !strings.Contains(msg, "writable") {
		t.Errorf("the hint should name the likely cause (writability / missing parent):\n%s", msg)
	}
	// 边界：**入口都没建出来**，所以不许报告"已创建"。
	if strings.Contains(msg, "created ") {
		t.Errorf("nothing was created, so nothing may be reported as created:\n%s", msg)
	}
}
