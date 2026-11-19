package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestMalformedArgumentsStayInsideTheContract 是"**参数值**"这一维的网。
//
// 与相邻两条判据的分工（写成一句话，免得下一个人以为它们是重复的）：
//
//	V39 参数维度    —— 有/没有参数、以及"多余参数"是否被看见（形状层）
//	V40 拼写        —— 顺序、重复 `--dir`、`--` 之后的 token（语义层）
//	本条            —— 值本身畸形时，**退出码、panic、stdout 通道**三条不能越界
//
// 为什么值得单列：退出码表只有 0–6，而"越界"只会从没被想到的组合里冒出来
// （空值、`--json=true`、重复 flag、未知 flag 与 `--json` 同现）。这三条断言
// 与具体命令无关，因此可以覆盖全部命令 × 全部畸形值，而不必为每条命令写期望。
//
// 2026-11-19 用它跑了 448 次组合，抓到两处真实的漂移：
//
//  1. 分流层的空 `--dir` 拒绝对**所有**命令都给信封，而命令自己的解析失败
//     只对接受 `--json` 的命令给——同一个命令的两种早期失败，两个答案；
//  2. `ngm run --json --bogus` 的 stdout 是空的（`run` 是唯一手写骨架，
//     解析失败时 ctx 还没被标记）——被派生的 flag 表漏掉，因为那张表只认
//     `newFlagSet`，而 run 用的是 `flag.NewFlagSet`。
//
// 两处都已在同一版修掉；这条判据留下来守它们。
func TestMalformedArgumentsStayInsideTheContract(t *testing.T) {
	isolateUserEnv(t)
	proj := t.TempDir()

	// 值这一维的样本。每条都要"语法上可能被误当成合法"，
	// 否则它只是在测用法文本，不是在测值处理。
	cases := []struct {
		name string
		args []string
	}{
		{"empty --dir", []string{"--dir="}},
		{"empty --dir + json", []string{"--dir=", "--json"}},
		{"json=true", []string{"--json=true"}},
		{"json=false", []string{"--json=false"}},
		{"json twice", []string{"--json", "--json"}},
		{"json then unknown flag", []string{"--json", "--definitely-not-a-flag"}},
		{"unknown flag then json", []string{"--definitely-not-a-flag", "--json"}},
		{"empty --level", []string{"--level=", "--json"}},
		{"empty --format", []string{"--format=", "--json"}},
		{"empty --engine", []string{"--engine=", "--json"}},
		{"dashdash then json", []string{"--", "--json"}},
		{"long --dir", []string{"--dir=" + strings.Repeat("x", 2048)}},
	}

	// 需要位置参数才能走到值处理的命令（来自 config_matrix_test.go 的同一份事实）。
	pos := map[string][]string{
		"init": {"github.com:x/app"}, "add": {"github:x/dep@v1", "--ref-type=tag"},
		"run": {"ok"}, "update": {"--all"}, "remove": {"github:x/dep"},
		"modules": {"list"}, "why": {"github:x/dep"}, "typecheck": {"--engine=fake"},
		"typedecl": {"--outdir=types", "--engine=fake"}, "build": {"entry.ts", "--engine=fake"},
		"transform": {"src.ts", "--loader=ts", "--engine=fake"}, "css": {"a.css", "--engine=fake"},
		"mappings": {"validate"}, "integrations": {"add", "vite"},
		"cache": {"clean"}, "store": {"usage"}, "config": {"validate"}, "engines": {"list"},
	}

	runs, jsonEnvelopes := 0, 0
	for _, spec := range commands {
		for _, c := range cases {
			args := append([]string{spec.Name}, pos[spec.Name]...)
			args = append(args, c.args...)

			// `--help` 不在样本里：它走的是"用户主动要用法"，stdout 是用法文本、退 0，
			// 不属于畸形的失败面。
			if hasToken(args, "--help") || hasToken(args, "-h") {
				continue
			}
			// 固定一个存在的目录，隔离掉"目录不存在"这一层——但样本里已经带了 `--dir`
			// 的就别再追加：`--dir=` 后面跟一个 `--dir=<proj>` 会让"空值"那条样本
			// 变成一条正常调用（Go 的 flag 是最后一个胜出）。
			if !hasDirFlag(args) {
				args = append(args, "--dir="+proj)
			}
			runs++

			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			if code < 0 || code > 6 {
				t.Errorf("%v: exit %d 越界（退出码表只有 0–6）\nstdout:\n%s\nstderr:\n%s",
					args, code, out.String(), errb.String())
				continue
			}
			if strings.Contains(errb.String(), "panic:") || strings.Contains(errb.String(), "goroutine ") {
				t.Errorf("%v: 参数处理 panic 了\n%s", args, errb.String())
				continue
			}

			asked := jsonAskedEarly(args)
			stdout := out.String()
			switch {
			case asked && spec.JSON:
				// 接受 `--json` 的命令：stdout 要么为空，要么恰好一份 JSON 文档。
				if strings.TrimSpace(stdout) == "" {
					continue
				}
				var v any
				if err := json.Unmarshal([]byte(stdout), &v); err != nil {
					t.Errorf("%v: 提了 --json，stdout 却不是恰好一份 JSON 文档（%v）:\n%s", args, err, stdout)
					continue
				}
				jsonEnvelopes++
			case asked && !spec.JSON:
				// 不接受 `--json` 的命令：不许往 stdout 塞东西（cli.md 规矩 4）。
				if strings.TrimSpace(stdout) != "" {
					t.Errorf("%v: 这条命令不接受 --json，stdout 必须为空，而现在有:\n%s", args, stdout)
				}
			}
		}
	}

	// 空跑保护：样本 × 命令必须真的跑出规模，并且**确实覆盖到**了信封那条分支。
	if runs < 200 {
		t.Fatalf("只跑了 %d 次组合——样本塌了，这条判据已经没在守东西", runs)
	}
	if jsonEnvelopes == 0 {
		t.Fatal("没有任何一次运行产出 JSON 信封——`asked && spec.JSON` 那条分支空跑了")
	}
	t.Logf("malformed values: %d runs over %d commands × %d shapes, %d JSON envelope(s)",
		runs, len(commands), len(cases), jsonEnvelopes)
}

func hasDirFlag(args []string) bool {
	for _, a := range args {
		if a == "--dir" || a == "-dir" || strings.HasPrefix(a, "--dir=") || strings.HasPrefix(a, "-dir=") {
			return true
		}
	}
	return false
}

func hasToken(args []string, tok string) bool {
	for _, a := range args {
		if a == tok {
			return true
		}
	}
	return false
}
