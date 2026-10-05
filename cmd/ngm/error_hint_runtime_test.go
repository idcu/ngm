package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// reErrPrefix 识别"这是错误文本"：一行的开头是某个错误码的显示名。
//
// 覆盖 `errs.Code` 的默认名与 v0.23 引入的两个显示名，外加非 NgmError 的兜底前缀。
var reErrPrefix = regexp.MustCompile(
	`(?m)^(RefDrift|DigestMismatch|ConfigInvalid|GitFetch|EngineNotFound|EngineFailed|AuditHook|error):`)

// TestV30ErrorsThatReachTheUserAreNotSilentAndCarryAHint 把 v0.28/v0.29 那条线**收到运行时**。
//
// 前面两版分别修了"建议在包装时丢掉"（渲染处沿 `Cause` 链回退）与"用户层 11 处没有下一步"
// （硬门禁 0/47）。但两者都是**静态**的：读源码只能证明"写了建议"，
// 证明不了"用户真的看得到"。这张网用**现成的 64 个退出码用例**在运行时取证据：
//
//	① 非零退出**不许静默**——一个不说话的失败比一个说错话的失败更难查；
//	② 输出里若出现**错误前缀**，那就必须带 `hint:`——这是"给下一步"的契约。
//
// 另一半（28 个非零用例）走的是**报告**：`verify` 的漂移表、`tree` 的清单之类——
// 它们不套这条契约，因为报告本身就是"为什么不通过"的完整说明。
// 判据把这两类分开数：报告那一类不参与断言，但要**报出来**（否则"跳过"会悄悄变成"通过"）。
func TestV30ErrorsThatReachTheUserAreNotSilentAndCarryAHint(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	silent, errorText, withHint, reports := 0, 0, 0, 0

	for _, trig := range exitCodeMeasured {
		if trig.code == 0 {
			continue
		}
		home := isolateUserEnv(t)
		args := append([]string{trig.cmd}, trig.args...)
		if !trig.noDir {
			args = append(args, "--dir="+buildFixture(t, home, trig.setup))
		}
		for k, v := range trig.env {
			t.Setenv(k, v)
		}

		var out, errb bytes.Buffer
		got := dispatch(args, &out, &errb)
		if got == 0 {
			t.Errorf("`ngm %s` was expected to fail but exited 0 — this case no longer measures what it claims",
				trig.cmd)
			continue
		}

		stderr := errb.String()
		if strings.TrimSpace(stderr+out.String()) == "" {
			silent++
			t.Errorf("`ngm %s` exited %d without saying anything — a silent failure is the hardest kind to debug",
				trig.cmd, got)
			continue
		}
		if !reErrPrefix.MatchString(stderr) {
			reports++ // 报告式失败：不套这条契约
			continue
		}
		errorText++
		if !strings.Contains(stderr, "hint:") {
			withHint--
			t.Errorf("`ngm %s` exited %d with an error that gives no next step:\n%s",
				trig.cmd, got, stderr)
			continue
		}
		withHint++
	}

	// 判据的可达性：如果有一天所有失败都变成"报告"，这张网会**静默地什么都不查**。
	if errorText == 0 {
		t.Fatal("no failing case produced error text — the net silently stopped checking anything")
	}
	if reports == 0 {
		t.Fatal("no failing case produced a report — either the split changed or the net is looking at the wrong output")
	}
	t.Logf("runtime hint census: %d error-text failures (all with a hint: %d), %d report-style failures, %d silent",
		errorText, withHint, reports, silent)
}

// emptyHintRepoCeiling 是全仓库"空 hint 构造点"的棘轮上限。
//
// 用户层（`cmd/ngm`）已经是硬门禁 **0**（见 `TestV28UserFacingErrorsCarryAHint`）。
// 这一条管的是 internal：118 处还没有逐个补——它们**不一定**会让用户看不到建议
// （v0.28 的继承修好之后，链上任何一层有建议就够），所以这里不要求零，
// 只要求**不许变多**，并把分布打进测试输出。
const emptyHintRepoCeiling = 118

// TestV30EmptyHintCeilingIsARepositoryWideRatchet 看住 internal 那一半。
//
// 为什么要有一道**全仓库**的棘轮：用户层的门禁只管 `cmd/ngm`。
// 一条新加的错误如果是写在 `internal/` 里的、且下面没有可继承的建议，
// 用户在屏幕上就什么下一步都看不到——而用户层的门禁**看不到它**。
func TestV30EmptyHintCeilingIsARepositoryWideRatchet(t *testing.T) {
	byDir := map[string][2]int{} // 目录 → {站点数, 空 hint}
	empty := 0

	root := "../.."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		slash := filepath.ToSlash(path)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.Contains(slash, "/.git/") || strings.Contains(slash, "/testdata/") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil
		}
		dir := strings.TrimPrefix(filepath.ToSlash(filepath.Dir(slash)), filepath.ToSlash(root)+"/")
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
			var idx, want int
			switch sel.Sel.Name {
			case "New":
				idx, want = 2, 3
			case "Wrap":
				idx, want = 2, 4
			default:
				return true
			}
			c := byDir[dir]
			c[0]++
			if len(call.Args) == want {
				if lit, isLit := call.Args[idx].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
					if s, uerr := strconv.Unquote(lit.Value); uerr == nil && strings.TrimSpace(s) == "" {
						c[1]++
						empty++
					}
				}
			}
			byDir[dir] = c
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(byDir) == 0 {
		t.Fatal("no errs.New/Wrap call site found — the net's scope shrank to nothing")
	}

	for dir, c := range byDir {
		if c[1] > 0 {
			t.Logf("  %-28s sites=%-4d empty=%d", dir, c[0], c[1])
		}
	}
	if empty > emptyHintRepoCeiling {
		t.Errorf("the repository has %d errs.New/Wrap sites with an empty hint, the ceiling is %d — "+
			"a new error without a next step (and without one to inherit) is invisible to the user-layer gate",
			empty, emptyHintRepoCeiling)
	}
	t.Logf("repository-wide empty hints: %d (ceiling %d) across %d directories",
		empty, emptyHintRepoCeiling, len(byDir))
}
