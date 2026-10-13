package main

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// v0.91：文档链接检查的**黄金用例**——把 v0.91 那次重写用过的等价性夹具固化下来。
//
// 背景：v0.90 把每条链接一次 `cd … && pwd` 换成纯 bash（133.72s → 33.07s），
// v0.91 再把"剥反引号 + 找链接 + 提 target + 规范化"整体搬进**一个 awk 进程**
// （33.07s → 1.33s，实测）。重写的凭据是当时与旧实现做的**逐字对照**——
// 但那次对照是一次性的：如果只留在复盘里，下一次改这个脚本的人就没有它了。
//
// 这条网就是那次对照的常驻版本：20 个刁钻用例，**逐条**断言结论。
// 尤其是这几处（都是"看起来等价、其实不等价"的高危区）：
//
//	· 反引号里的示例**不许**被当成链接；
//	· 一行里两条链接**都要**被看到；
//	· `..` 一直退到根（`docs/development/../../docs/x` 这种）；
//	· 根目录文件（base 为空）拼出的路径**不能**带前导 `/`（否则相对链接变绝对路径）；
//	· 括号出现在 label 或 target 里：**v0.92 起 target 里的括号按平衡括号解析**
//	  （旧的"截到第一个 `)`"会把它切碎 ⇒ 既不报也看不见——那处已知漏检已关上，
//	  但"带括号且存在"这一侧仍要断言，才能证明新解析不是"见括号就报"）。
func TestV91DocsLinksGoldenCases(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-docs-links.sh")

	dir := t.TempDir()
	writeFileAt(t, dir, "docs/b.md", "# b\n")
	writeFileAt(t, dir, "docs/b(c).md", "# 名字里带括号的文件（v0.92 起这类链接才看得见）\n")
	writeFileAt(t, dir, "docs/sub/c.md", "# c\n")
	writeFileAt(t, dir, "docs/note.txt", "# txt\n")
	writeFileAt(t, dir, "docs/vendor/v.md", "# v\n")
	writeFileAt(t, dir, "docs/a.md", strings.Join([]string{
		"# a",
		"",
		"[好的](./b.md)",            // 1 好
		"[好（上一级）](../docs/b.md)",  // 2 好
		"[好（子目录）](./sub/c.md)",    // 3 好
		"[好（锚点）](./b.md#section)", // 4 好（锚点被剥掉）
		"[好（txt）](./note.txt)",    // 5 好
		"[坏](./missing.md)",       // 6 坏
		"[坏（子目录）](./sub/nope.md)", // 7 坏
		"[坏（绝对根）](/nope.md)",      // 8 坏
		"示例：`[不是链接](./nowhere.md)` 与 `[也不是](./nowhere.md)`", // 不算
		"两个在一行：[坏1](./x1.md) 与 [坏2](./x2.md)",               // 9、10 坏
		"[外链](https://example.com/x.md)",                    // 不算
		"[锚点](#local)",                                      // 不算
		"[不是 md](./image.png)",                              // 不算
		"[目标里有括号](./b(c).md)",                               // 好：v0.92 起这类链接才被解析（旧实现把它切成 `c` ⇒ 看不见）；目标存在
		"[标签里有括号 (x)](./b.md)",                              // 好（括号在 label 里，不影响）
		"[两级向上](../docs/sub/../b.md)",                       // 好（先退再进）
		"[目标带括号且存在](./b(c).md)",                             // 好（证明新解析不是"见括号就报"）
		"[嵌套括号但缺失](./missing(c).md)",                        // **坏**：v0.92 的**行为变更**——旧实现看不见这类链接
		"[括号不配平](./unbalanced(b-missing.md)",                // 坏（回退路径：取最后一个 '(' 之后——与旧实现一致）
		"",
	}, "\n"))
	writeFileAt(t, dir, "README.md", "# root\n\n[坏](./docs/missing-root.md)\n[好](./docs/b.md)\n")
	writeFileAt(t, dir, "docs/vendor/v.md", "# vendor 里的文件不参与\n")

	cmd := exec.Command(bash, filepath.ToSlash(script), "docs", "README.md")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		t.Fatalf("有坏链接时必须非 0 退出：\n%s", text)
	}

	var broken []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "BROKEN LINK in ") {
			broken = append(broken, strings.TrimSuffix(strings.TrimPrefix(line, "BROKEN LINK in "), "\r"))
		}
	}
	sort.Strings(broken)

	want := []string{
		"docs/a.md: 10:[坏（绝对根）](/nope.md)",
		"docs/a.md: 12:[坏1](./x1.md)",
		"docs/a.md: 12:[坏2](./x2.md)",
		"docs/a.md: 8:[坏](./missing.md)",
		"docs/a.md: 9:[坏（子目录）](./sub/nope.md)",
		"docs/a.md: 21:[括号不配平](./unbalanced(b-missing.md)",
		"docs/a.md: 20:[嵌套括号但缺失](./missing(c).md)",
		"README.md: 3:[坏](./docs/missing-root.md)",
	}
	sort.Strings(want)

	if strings.Join(broken, "\n") != strings.Join(want, "\n") {
		t.Errorf("黄金用例的结论变了。\n得到：\n  %s\n期望：\n  %s\n原始输出：\n%s",
			strings.Join(broken, "\n  "), strings.Join(want, "\n  "), text)
	}
	if !strings.Contains(text, "found 8 broken links") {
		t.Errorf("计数行也要对：\n%s", text)
	}
}
