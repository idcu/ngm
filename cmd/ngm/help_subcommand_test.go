package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestV12SubcommandHelpPrintsItsOwnUsage 固定 `ngm <cmd> --help` 的行为。
//
// 两件事一起钉住：
//
//  1. **可达**：v0.11 之前根级 --help 的扫描跨越整条 args，`ngm verify --help`
//     拿到的是根帮助，子命令帮助那一分支是**死代码**——它的文本
//     "help not yet implemented" 因此没有任何人见过。
//  2. **同一份文本**：帮助复用该命令的 xxxUsage 常量，而不另写一份。
//     同一件事写在第二个地方就一定会有一处不一致，所以这里断言**逐字相等**，
//     而不是"包含某个关键字"。
func TestV12SubcommandHelpPrintsItsOwnUsage(t *testing.T) {
	for _, spec := range commands {
		for _, flag := range []string{"--help", "-h"} {
			t.Run(spec.Name+" "+flag, func(t *testing.T) {
				if strings.TrimSpace(spec.Usage) == "" {
					t.Fatalf("command %q has no usage text", spec.Name)
				}
				// 用法文本必须以自己的名字开头：否则多半是复制粘贴时忘了改。
				if first := firstLine(spec.Usage); !strings.HasPrefix(first, "ngm "+spec.Name) {
					t.Errorf("usage must open with %q (copy-pasted constant?): %q",
						"ngm "+spec.Name, first)
				}

				stdout := &bytes.Buffer{}
				stderr := &bytes.Buffer{}
				if code := dispatch([]string{spec.Name, flag}, stdout, stderr); code != 0 {
					t.Fatalf("exit=%d want 0: help is what the user asked for", code)
				}
				if got := stdout.String(); got != spec.Usage {
					t.Errorf("--help must print the command's own usage verbatim\n--- got ---\n%s\n--- want ---\n%s", got, spec.Usage)
				}
				if stderr.Len() != 0 {
					t.Errorf("help goes to stdout, stderr must stay empty; got:\n%s", stderr.String())
				}
			})
		}
	}
}

// TestV12RootHelpIsScopedToBeforeTheSubcommand 固定根级 flag 的作用域：
// 子命令**之前**的 --help/--version 是根级的，之后的属于该子命令。
func TestV12RootHelpIsScopedToBeforeTheSubcommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"bare --help", []string{"--help"}, rootUsage},
		{"--help before the subcommand", []string{"--help", "verify"}, rootUsage},
		{"--help after the subcommand", []string{"verify", "--help"}, verifyUsage},
		{"-h after the subcommand", []string{"store", "-h"}, storeUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			if code := dispatch(tc.args, stdout, stderr); code != 0 {
				t.Fatalf("exit=%d want 0", code)
			}
			if stdout.String() != tc.want {
				t.Errorf("wrong help text for %v\n--- got ---\n%s", tc.args, stdout.String())
			}
		})
	}
}

// flagRegistrationRe 匹配 flagset 上的注册调用，如 fs.Bool("orphans", ...)。
// 只看非测试文件，且只认名字里有 "fs" 的接收者——本包所有 flagset 都叫 fs。
var flagRegistrationRe = regexp.MustCompile(`\b(\w*[fF]s\w*)\.(String|Bool|Duration|Int|Int64|Uint)\("([a-z0-9-]+)"`)

// TestV12EveryRegisteredFlagIsDocumented 是一张**扫源码**的机械网：
// 每个注册到 flagset 上的 flag，都必须在同文件里被写出来过（用法文本或说明）。
//
// 为什么需要它：命令行 flag 此前**没有任何机械网**（配置结构体字段有
// internal/config/field_wiring_test.go，flag 没有）。一个只注册、从不被写下的 flag
// 不会让任何检查变红——用户只会发现自己不知道该传什么。
//
// **它证明什么、不证明什么**：它证明"这个 flag 至少被写下来过一次"，
// 不证明"写得足够清楚"，也不检查出处的形状（`[--json]` 内联与独立 FLAGS 段落都算）。
// 判据故意取这个宽度：形状检查会误报，而**会误报的门禁会被忽略**。
// 反方向（文档写了但没注册）不在这里断言——同一份文本里合法地会出现
// 别的命令的 flag 与字面量。
func TestV12EveryRegisteredFlagIsDocumented(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)

		var registered []string
		for _, m := range flagRegistrationRe.FindAllStringSubmatch(src, -1) {
			registered = append(registered, m[3])
		}
		if len(registered) == 0 {
			continue
		}
		checked += len(registered)

		// 只在**原始字符串字面量**（用法文本都是 `` ` `` 引起来的常量）里找文档。
		// 不搜整份源码，是因为注册调用自己的描述文本里可能出现别的 flag 名
		// （`--older-than` 的说明里就写着 `--orphans`），那会让网被自己满足。
		documented := rawStringLiterals(src)

		var missing []string
		for _, name := range registered {
			if !mentionRe(name).MatchString(documented) {
				missing = append(missing, "--"+name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s registers flag(s) that no usage/copy text in that file mentions: %s",
				file, strings.Join(missing, ", "))
		}
	}
	if checked == 0 {
		t.Fatal("the scan found no flag registrations at all — a net that matches nothing is not a net")
	}
	t.Logf("checked %d flag registration(s) across cmd/ngm", checked)
}

// mentionRe 匹配 `--name` 且其后**不是**名字字符——否则 `--dir` 会被
// `--directory` 意外满足，网的宽度就变成了"看起来像"。
func mentionRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`--` + regexp.QuoteMeta(name) + `(?:[^a-z0-9-]|$)`)
}

// rawStringLiteralRe 匹配 Go 的原始字符串字面量（反引号段）。
var rawStringLiteralRe = regexp.MustCompile("(?s)`([^`]*)`")

// rawStringLiterals 把所有原始字符串段（反引号引起来的部分）拼起来：
// 本项目的用法文本一律是 const xxxUsage = <raw string> 形态。
func rawStringLiterals(src string) string {
	var sb strings.Builder
	for _, m := range rawStringLiteralRe.FindAllStringSubmatch(src, -1) {
		sb.WriteString(m[1])
		sb.WriteString("\n")
	}
	return sb.String()
}

// firstLine 返回文本的第一行。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestV12ExtraPositionalArgsAreRefused 固定"多给的输入不会被丢掉"。
//
// `ngm css` 是本目录里唯一**没有**位置参数计数校验的命令：`ngm css a.css b.css`
// （或 shell 展开的 `ngm css dist/*.css`）此前只编译第一个文件、exit 0、
// 一句话都不说。glob 展开是它最常见的用法，而用户没有理由怀疑自己丢了一半输入。
func TestV12ExtraPositionalArgsAreRefused(t *testing.T) {
	code, out := runCaptureCode(t, "css", "a.css", "b.css")
	if code != 3 {
		t.Fatalf("exit=%d want 3 (用法错误):\n%s", code, out)
	}
	if !strings.Contains(out, "ngm css") {
		t.Errorf("the refusal must print the command's usage:\n%s", out)
	}
}
