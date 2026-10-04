package main

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestV19EveryCommandDocumentsItsExitCodes 固定"每个命令的用法文本里都写出自己的退出码"。
//
// 为什么需要它：`ngm <cmd> --help` 打印的那份文本是**用户看到的第一手契约**，
// 而退出码是脚本唯一能看的东西。此前 21 个命令里有 8 个**根本没有** EXIT CODES 段
// （init / add / update / remove / mappings / cache / store / config），
// 而 `install` 的一段是**不完整的**：它写着 0/3/4，却实际会退 2（postinstall 钩子失败）
// 与 5（钩子需要 Deno 而 Deno 不在）——两处都有代码路径与既有测试支撑。
//
// 判据故意取窄：**有 EXIT CODES 段**且段内有至少一行 `<0-5> <说明>`。
// 它不证明"列全了"——那需要数据流分析，见 TestV19ExitCodeSections的结构说明。
func TestV19EveryCommandDocumentsItsExitCodes(t *testing.T) {
	codeLine := regexp.MustCompile(`(?m)^\s+[0-5]\s+\S`)
	for _, spec := range commands {
		t.Run(spec.Name, func(t *testing.T) {
			if !strings.Contains(spec.Usage, "EXIT CODES") {
				t.Fatalf("`ngm %s` has no EXIT CODES section; the usage text is the user's "+
					"first-hand contract and exit codes are what scripts read:\n%s", spec.Name, spec.Usage)
			}
			section := spec.Usage[strings.Index(spec.Usage, "EXIT CODES"):]
			if !codeLine.MatchString(section) {
				t.Errorf("the EXIT CODES section of `ngm %s` lists no `<code> <explanation>` line:\n%s",
					spec.Name, section)
			}
		})
	}
}

// TestV19NoInternalStageLabelsInUserFacingText 固定一条边界：
// **内部阶段标签（M0…M7）不许出现在用户可见的字符串里**。
//
// 为什么需要它：`M\d` 是项目**内部**的里程碑编号，用户从没听说过。它此前泄漏到两处
// 用户可见的位置——`ngm add` 成功后打印的 "… (M3)"，以及 `update` 用法里那句
// "M1 阶段本命令完成…；ngm.lock 的写入属于 M3"（**而且那句话是错的**：update 今天就写 lock）。
//
// 判据用 go/scanner 只看**字符串字面量**——注释里可以出现（那是设计记录），
// 字符串里不行（那会打印给人看）。大小写敏感：否则 `sha256.Sum256` 里的 `m2`
// 会被当成阶段标签（第一版扫描就踩了这个）。
func TestV19NoInternalStageLabelsInUserFacingText(t *testing.T) {
	label := regexp.MustCompile(`\bM[0-9]\b`)
	var files []string
	for _, root := range []string{".", filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			files = append(files, p)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no Go files scanned — a net that matches nothing is not a net")
	}

	checked := 0
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var sc scanner.Scanner
		fset := token.NewFileSet()
		sc.Init(fset.AddFile(file, -1, len(src)), src, nil, 0)
		for {
			_, tok, lit := sc.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING {
				continue
			}
			checked++
			if loc := label.FindString(lit); loc != "" {
				t.Errorf("%s: the string literal %q contains the internal stage label %q — "+
					"users never see M0…M7; say what the command does instead",
					file, truncateLit(lit), loc)
			}
		}
	}
	sort.Strings(files)
	t.Logf("scanned %d string literal(s) across %d non-test Go file(s)", checked, len(files))
}

func truncateLit(s string) string {
	if len(s) <= 60 {
		return s
	}
	return s[:60] + "…"
}
