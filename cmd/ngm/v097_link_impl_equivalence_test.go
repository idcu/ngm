package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// v0.97：**两条实现必须对同一批输入给出同一批坏链接**。
//
// 链接检查现在有两条实现（这是有意的分工，见 v0.96）：
//
//	shell：`scripts/check-docs-links.sh`（存在性；awk 一个进程 + 内建 `-f`/`-e`）
//	Go：  `linkrefs_test.go` 的 `repoLinkRefs`（大小写的判据用它，v0.97 起也是唯一一份 Go 解析）
//
// 分工只有在**两边对同一批输入一致**时才成立。v0.78 的教训正是这里：
// 当年"自检器比产品严"（同一件事两份实现，谁也没对过账），而发现它的代价是一次误判。
//
// 这条网把两份实现放在**同一份语料**上比结论（文件:行号:原文三元组，排序后逐条相等），
// 再在真仓库上跑一遍（两边都应当为空，且 Go 侧确实扫到了东西）。
//
// **刻意不放进语料的形态**：括号**不配平**的链接。那种写法按 CommonMark 根本不是链接，
// shell 侧的回退路径会把它打印成"从 `](` 到行尾"（消息形态不同，判定相同）——
// 它的**判定**由 v0.91 的黄金用例守着，这里只比"两边该逐字一致"的部分。
func TestV097LinkImplementationsAgree(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-docs-links.sh")

	dir := t.TempDir()
	writeFileAt(t, dir, "docs/b.md", "# b\n")
	writeFileAt(t, dir, "docs/b(c).md", "# 名字里带括号\n")
	writeFileAt(t, dir, "docs/sub/c.md", "# c\n")
	writeFileAt(t, dir, "docs/data.json", "{}\n")
	writeFileAt(t, dir, "docs/带 空格.md", "# 名字里有空格\n")
	writeFileAt(t, dir, "docs/vendor/v.md", "# 第三方\n")
	writeFileAt(t, dir, "docs/a.md", strings.Join([]string{
		"# a",
		"",
		"[好](./b.md)",
		"[好（上一级）](../docs/b.md)",
		"[好（子目录）](./sub/c.md)",
		"[好（锚点）](./b.md#section)",
		"[好（带括号）](./b(c).md)",
		"[好（空格）](./带 空格.md)",
		"[坏](./missing.md)",
		"[坏（子目录）](./sub/nope.md)",
		"[坏（绝对根）](/nope.md)",
		"[好（根相对）](/README.md)",
		"[坏（根相对但不存在）](/nope-at-root.md)",
		"示例：`[不算](./nowhere.md)` 与 `[也不算](./nowhere.md)`",
		"[坏1](./x1.md) 与 [坏2](./x2.md)",
		"[外链](https://example.com/x.md)",
		"[锚点](#local)",
		"[不是 md](./image.png)",
		"[坏（嵌套括号）](./missing(c).md)",
		"[好（非 md）](./data.json)",
		"[好（目录）](./sub/)",
		"[坏（目录）](./nope-dir/)",
		"[坏（源码）](./nope.go)",
		"[不算（被忽略的目录）](./vendor/x.md)",
		"```",
		"[不算（围栏块里的示例）](./nope-in-fence.md)",
		"```",
		"[坏（围栏块之后）](./missing-after-fence.md)",
		"",
	}, "\n"))
	writeFileAt(t, dir, "README.md", "# root\n\n[坏](./docs/missing-root.md)\n[好](./docs/b.md)\n")

	shellOut := runDocsLinks(t, bash, script, dir)
	shellSet := shellBrokenSet(t, shellOut)
	goSet := brokenLinksByGo(t, dir)

	if len(shellSet) == 0 || len(goSet) == 0 {
		t.Fatalf("两边都应当报出坏链接（否则这条对照什么都没比）：\n  shell: %v\n  go: %v", shellSet, goSet)
	}
	if strings.Join(shellSet, "\n") != strings.Join(goSet, "\n") {
		t.Errorf("两条实现对同一份语料的结论不一致（这正是一次误判的前身）：\n"+
			"  只有 shell 报了：%s\n  只有 Go 报了：%s", diffSets(shellSet, goSet), diffSets(goSet, shellSet))
	}

	// 真仓库：两边都应当为空，且 Go 侧确实扫到了链接（防"范围缩了"）。
	repo := repoRoot(t)
	repoOut := runDocsLinks(t, bash, script, repo, "docs", "README.md")
	if got := shellBrokenSet(t, repoOut); len(got) != 0 {
		t.Errorf("真仓库上 shell 侧报了坏链接：%v", got)
	}
	if got := brokenLinksByGo(t, repo); len(got) != 0 {
		t.Errorf("真仓库上 Go 侧报了坏链接：%v", got)
	}
	refs := repoLinkRefs(t, repo)
	total := 0
	for _, list := range refs {
		total += len(list)
	}
	if total < 1000 {
		t.Errorf("真仓库只扫到 %d 处链接 —— 范围缩了（这条对照会变成空比）", total)
	}
}

// runDocsLinks 跑一次链接检查并返回它的输出（在 dir 里跑，根参数按需给）。
func runDocsLinks(t *testing.T, bash, script, dir string, roots ...string) string {
	t.Helper()
	args := append([]string{filepath.ToSlash(script)}, roots...)
	cmd := exec.Command(bash, args...)
	cmd.Dir = dir
	out, _ := cmd.CombinedOutput() // 退出码不进断言：这里比的是"谁被判成坏"
	return string(out)
}

// shellBrokenSet 把 shell 的输出解析成与 Go 侧同形的三元组（file: line:raw），已排序。
func shellBrokenSet(t *testing.T, out string) []string {
	t.Helper()
	var set []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, "BROKEN LINK in ") {
			continue
		}
		rest := strings.TrimPrefix(line, "BROKEN LINK in ")
		i := strings.Index(rest, ": ")
		if i < 0 {
			t.Fatalf("看不懂的 shell 输出行：%q", line)
		}
		file, tail := rest[:i], rest[i+2:]
		j := strings.Index(tail, ":")
		if j < 0 {
			t.Fatalf("看不懂的 shell 输出行：%q", line)
		}
		set = append(set, file+":"+tail[:j]+":"+tail[j+1:])
	}
	sort.Strings(set)
	return set
}

// brokenLinksByGo 用 Go 侧的**唯一实现**算出坏链接集合（存在性判据与 shell 同口径：
// `.md`/`.txt` 要求是**文件**，其余只要存在）。
func brokenLinksByGo(t *testing.T, root string) []string {
	t.Helper()
	refs := repoLinkRefs(t, root)
	var out []string
	for file, list := range refs {
		for _, r := range list {
			rel, ok := resolveRef(root, file, r.target)
			if !ok {
				continue
			}
			abs := filepath.Join(root, filepath.FromSlash(rel))
			lower := strings.ToLower(r.target)
			broken := false
			if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") {
				st, err := os.Stat(abs)
				broken = err != nil || st.IsDir()
			} else {
				_, err := os.Stat(abs)
				broken = err != nil
			}
			if broken {
				out = append(out, file+":"+itoa(r.line)+":"+r.raw)
			}
		}
	}
	sort.Strings(out)
	return out
}

// diffSets 返回 a 里不在 b 里的条目（用于把失败信息说清）。
func diffSets(a, b []string) string {
	inB := map[string]bool{}
	for _, s := range b {
		inB[s] = true
	}
	var only []string
	for _, s := range a {
		if !inB[s] {
			only = append(only, s)
		}
	}
	if len(only) == 0 {
		return "（无）"
	}
	return "\n    " + strings.Join(only, "\n    ")
}
