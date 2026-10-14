package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// v0.96：**链接的大小写必须与磁盘一致**——否则同一个仓库在两台机器上给出不同结论。
//
// 为什么需要它：`scripts/check-docs-links.sh` 的存在性判据**平台相关**——
// Windows 的文件系统不区分大小写，于是 `[x](./Guides/a.md)`（磁盘上是 `guides/`）
// 在本机**通过**、在 Linux 的 CI 上**失败**。同一个仓库、同一份判据、两个结论：
// 这正是本项目反复吃亏的那种"看起来在检查"。
//
// 先量过再动手（与 v0.92 / v0.93 同一个纪律）：全仓 2127 处仓库内链接**全部一致**
// ⇒ 加这条判据不会引入任何新红，它守的是下一次。
//
// **扫描核心在 `linkrefs_test.go`**（v0.97 抽出去的：Go 侧唯一的实现，含围栏代码块规则——
// 它自己当初漏了那条规则，见那里的注释）。
func TestV96DocLinkCaseIsExact(t *testing.T) {
	root := repoRoot(t)
	mismatches, stats := auditLinkCase(t, root)
	if stats.links == 0 {
		t.Fatal("没有扫到任何仓库内链接 —— 这条判据的范围缩了（它会静默地什么都不守）")
	}
	if len(mismatches) > 0 {
		t.Errorf("这些链接的大小写与磁盘不一致（Windows 上会过、Linux 上会红）：\n  %s",
			strings.Join(mismatches, "\n  "))
	}
	t.Logf("扫过 %d 处仓库内链接（分布在 %d 个文件里），大小写全部一致", stats.links, stats.files)

	// 牙齿：临时树上造一条大小写不符的链接（真仓库里没有可试的），要求恰好报它一条。
	t.Run("牙齿：大小写不符必须被抓到", func(t *testing.T) {
		dir := t.TempDir()
		writeFileAt(t, dir, "docs/guides/a.md", "# a\n")
		writeFileAt(t, dir, "docs/x.md", strings.Join([]string{
			"# x",
			"",
			"[对](./guides/a.md)",
			"[错](./Guides/a.md)",
			"[也对](../docs/guides/a.md)",
			"`[行内代码里的不算](./Guides/a.md)`",
			"```",
			"[围栏块里的也不算](./Guides/a.md)",
			"```",
			"",
		}, "\n"))

		mismatches, s := auditLinkCase(t, dir)
		if s.links != 3 {
			t.Fatalf("应扫到 3 处仓库内链接（行内代码与围栏块里那两条都不算），got %d", s.links)
		}
		if len(mismatches) != 1 {
			t.Fatalf("应当只报大小写不符的那一条，got %v", mismatches)
		}
		got := mismatches[0]
		if !strings.Contains(got, "Guides") || !strings.Contains(got, "guides") {
			t.Errorf("报错要说清两边的写法（写的是哪个、磁盘上是哪个）：%q", got)
		}
	})
}

type linkCaseStats struct {
	files int
	links int
}

// auditLinkCase 返回（大小写不一致的明细，统计）。root 是仓库根。
//
// 抽成"输入 root"的函数是为了能用**临时树**给它上牙（真仓库里没有不一致的链接可试）。
func auditLinkCase(t *testing.T, root string) ([]string, linkCaseStats) {
	t.Helper()
	refs := repoLinkRefs(t, root)

	dirCache := map[string][]string{}
	namesIn := func(dir string) []string {
		if names, ok := dirCache[dir]; ok {
			return names
		}
		entries, err := os.ReadDir(dir)
		names := make([]string, 0, len(entries))
		if err == nil {
			for _, e := range entries {
				names = append(names, e.Name())
			}
		}
		dirCache[dir] = names
		return names
	}

	stats := linkCaseStats{files: len(refs)}
	var mismatches []string
	for file, list := range refs {
		for _, r := range list {
			stats.links++
			rel, ok := resolveRef(root, file, r.target)
			if !ok || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			if badSeg, actual := caseMismatch(root, rel, namesIn); badSeg != "" {
				mismatches = append(mismatches,
					file+":"+itoa(r.line)+": "+r.raw+" → 磁盘上叫「"+actual+"」，不是「"+badSeg+"」")
			}
		}
	}
	sort.Strings(mismatches)
	return mismatches, stats
}

// caseMismatch 逐段走一遍 rel（相对 root），返回第一个与磁盘大小写不符的段及磁盘上的写法。
// 返回空串表示一致（或者路径本来就不存在——那由 shell 检查负责报）。
func caseMismatch(root, rel string, namesIn func(string) []string) (badSeg, actual string) {
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return "", ""
	}
	cur := root
	for _, seg := range strings.Split(rel, "/") {
		names := namesIn(cur)
		found := false
		for _, n := range names {
			if n == seg {
				cur = filepath.Join(cur, n)
				found = true
				break
			}
		}
		if found {
			continue
		}
		// 段没找到：可能是"不存在"（shell 报），也可能是"大小写不同"
		for _, n := range names {
			if strings.EqualFold(n, seg) {
				return seg, n
			}
		}
		return "", "" // 真的不存在 ⇒ 不在这里报
	}
	return "", ""
}
