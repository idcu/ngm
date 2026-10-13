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
//	· 括号出现在 label 或 target 里时的既有（古怪）行为要**保持一致**。
func TestV91DocsLinksGoldenCases(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-docs-links.sh")

	dir := t.TempDir()
	writeFileAt(t, dir, "docs/b.md", "# b\n")
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
		"[目标里有括号](./b(c).md)",                               // 11 坏（按既有语义取最后一个 '(' 之后）
		"[标签里有括号 (x)](./b.md)",                              // 12 好
		"[两级向上](../docs/sub/../b.md)",                       // 13 好（先退再进）
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
		"README.md: 3:[坏](./docs/missing-root.md)",
	}
	sort.Strings(want)

	if strings.Join(broken, "\n") != strings.Join(want, "\n") {
		t.Errorf("黄金用例的结论变了。\n得到：\n  %s\n期望：\n  %s\n原始输出：\n%s",
			strings.Join(broken, "\n  "), strings.Join(want, "\n  "), text)
	}
	if !strings.Contains(text, "found 6 broken links") {
		t.Errorf("计数行也要对：\n%s", text)
	}
}
