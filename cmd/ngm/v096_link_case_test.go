package main

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
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
// 覆盖与 shell 检查**同一批文件与链接**（`docs/**/*.md` + 根 `README.md`；
// 行内代码剥掉；跨仓 / 锚点 / 被忽略目录跳过；destination 按**平衡括号**解析），
// 区别只在于：这里逐段核对**磁盘上的真实大小写**。
//
// 先量过再动手（与 v0.92 / v0.93 同一个纪律）：全仓 2127 处仓库内链接**全部一致**
// ⇒ 加这条判据不会引入任何新红，它守的是下一次。
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
	t.Logf("扫过 %d 处仓库内链接（%d 个文件），大小写全部一致", stats.links, stats.files)

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
			"",
		}, "\n"))

		mismatches, s := auditLinkCase(t, dir)
		if s.links != 3 {
			t.Fatalf("应扫到 3 处仓库内链接（行内代码里那条不算），got %d", s.links)
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

var reInlineCodeGo = regexp.MustCompile("`[^`]*`")

// auditLinkCase 返回（大小写不一致的明细，统计）。root 是仓库根。
//
// 抽成"输入 root"的函数是为了能用**临时树**给它上牙（真仓库里没有不一致的链接可试）。
func auditLinkCase(t *testing.T, root string) ([]string, linkCaseStats) {
	t.Helper()
	var stats linkCaseStats

	var mdFiles []string
	docsRoot := filepath.Join(root, "docs")
	err := filepath.WalkDir(docsRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "vendor", "ngm.vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			mdFiles = append(mdFiles, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/: %v", err)
	}
	if readme := filepath.Join(root, "README.md"); fileExists(readme) {
		mdFiles = append(mdFiles, readme)
	}
	sort.Strings(mdFiles)
	stats.files = len(mdFiles)

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

	var mismatches []string
	for _, f := range mdFiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		relFile, _ := filepath.Rel(root, f)
		relFile = filepath.ToSlash(relFile)
		baseDir := filepath.Dir(f)

		for i, line := range strings.Split(string(raw), "\n") {
			cleaned := reInlineCodeGo.ReplaceAllString(line, "")
			for _, target := range linkTargets(cleaned) {
				if target == "" || strings.HasPrefix(target, "#") {
					continue
				}
				if regexp.MustCompile(`^(https?|mailto|tel|file):`).MatchString(target) {
					continue
				}
				if idx := strings.Index(target, "#"); idx >= 0 {
					target = target[:idx]
				}
				if target == "" || strings.HasPrefix(target, "/") {
					continue // 绝对路径形态：存在性由 shell 检查管，这里只看相对链接的大小写
				}
				if isIgnoredTarget(target) {
					continue
				}
				stats.links++

				resolved := path.Clean(path.Join(filepath.ToSlash(baseDir), target))
				rel, err := filepath.Rel(filepath.ToSlash(root), resolved)
				if err != nil {
					continue
				}
				rel = filepath.ToSlash(rel)
				if badSeg, actual := caseMismatch(root, rel, namesIn); badSeg != "" {
					mismatches = append(mismatches,
						filepath.ToSlash(relFile)+":"+itoa(i+1)+": "+target+" → 磁盘上叫「"+actual+"」，不是「"+badSeg+"」")
				}
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
		for _, n := range names {
			if n == seg {
				cur = filepath.Join(cur, n)
				goto next
			}
		}
		// 段没找到：可能是"不存在"（shell 报），也可能是"大小写不同"
		for _, n := range names {
			if strings.EqualFold(n, seg) {
				return seg, n
			}
		}
		return "", "" // 真的不存在 ⇒ 不在这里报
	next:
	}
	return "", ""
}

// linkTargets 抽出 `[label](dest)` 的 dest——按**平衡括号**解析（与 shell 实现同口径，
// 见 v0.92：`](./b(c).md)` 的结尾是最后一个配平的 `)`）。
func linkTargets(line string) []string {
	var out []string
	rest := line
	for {
		open := strings.Index(rest, "](")
		if open < 0 {
			return out
		}
		// label 必须已经在前面出现过 `[`（否则 `]("` 这种碎片也算）
		if !strings.Contains(rest[:open], "[") {
			rest = rest[open+2:]
			continue
		}
		depth := 0
		end := -1
		for i := open + 2; i < len(rest); i++ {
			switch rest[i] {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					end = i
				} else {
					depth--
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			return out
		}
		out = append(out, strings.TrimSpace(rest[open+2:end]))
		rest = rest[end+1:]
	}
}

func isIgnoredTarget(target string) bool {
	slashed := "/" + filepath.ToSlash(target)
	for _, dir := range []string{"/node_modules/", "/vendor/", "/ngm.vendor/", "/dist/", "/.git/"} {
		if strings.Contains(slashed, dir) {
			return true
		}
	}
	return false
}
