package main

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// TestDocsAnchorsResolve 检查 `docs/` 里 `[label](file.md#anchor)` 的锚点是否真的存在。
//
// 为什么需要（v0.9 B 组）：文档检查只验证"目标文件在不在"，**从不看锚点**
// （`scripts/check-docs-links.sh` 会把 `#…` 剥掉）。于是死锚点可以长期存在——
// 收尾审计一次就查出 2 条（`#7-v06-候选项…`，标题里其实是"候选"），
// 它们点进去只会**静默跳到页首**，没有任何东西会报出来。
//
// 为什么用 Go 测试而不是 shell 脚本：
//   - 锚点要按 Unicode 类别处理中文（`\p{L}`），而 `grep -P` 的 Unicode 行为依赖
//     locale——换一台机器就可能退回按字节匹配（本项目在这上面栽过：v0.8 复盘 §5.8）；
//   - 放在 `cmd/ngm` 的测试里，它随 `go test ./...` 在本机与 CI 一起跑，
//     **不需要新的 CI job**（少一处会各自漂移的接线）。
//
// 规则（近似 GitHub 的 slugger，并在有疑问处**放宽**而不是收紧）：
//
//	小写 · 空格转 `-` · 保留字母/数字/标记/连接符（`_` 属此类）/`-` ·
//	其余一律去掉（含 `.`：`v0.6` → `v06`，这一点由仓库内既有锚点多处印证）·
//	同名标题按出现顺序加 `-1`、`-2`
//
// **放宽的那一半**：标题里若含"符号类"字符（`≠`、箭头、emoji…），规则不确定
// GitHub 是去掉还是保留，于是两个变体**都算存在**。放宽只可能漏报（把一个真死的
// 锚点当成通过），不会误报——而误报会让这条门禁被忽略，那比漏报更糟。
func TestDocsAnchorsResolve(t *testing.T) {
	// 相对**仓库根**的路径空间：docs/ 递归 + 仓库根的 *.md。
	// 根 README 是最多人读的一份文档，此前不在任何检查范围内（v0.10 B 组）。
	repoRoot := filepath.Join("..", "..")
	docsRoot := filepath.Join(repoRoot, "docs")

	files := map[string]bool{}
	var docs []string
	err := filepath.WalkDir(docsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, rerr := filepath.Rel(docsRoot, path)
		if rerr != nil {
			return rerr
		}
		rel = "docs/" + filepath.ToSlash(rel)
		files[rel] = true
		docs = append(docs, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/: %v", err)
	}
	if len(docs) < 10 {
		t.Fatalf("只找到 %d 个文档——这条检查多半在扫错目录（docs 树不该这么小）", len(docs))
	}
	// 根 README：显式列出来，缺了就是缺了（不静默当成"没有这个文件"）。
	const rootReadme = "README.md"
	if _, serr := os.Stat(filepath.Join(repoRoot, rootReadme)); serr == nil {
		files[rootReadme] = true
		docs = append(docs, rootReadme)
	} else if !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", rootReadme, serr)
	}
	sort.Strings(docs)

	// 每个文件的标题 slug 集合（严格 + 放宽两个变体）。
	slugs := map[string]map[string]bool{}
	for _, rel := range docs {
		data, rerr := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		set := map[string]bool{}
		seen := map[string]int{}
		for _, line := range strings.Split(string(data), "\n") {
			heading, ok := headingText(line)
			if !ok {
				continue
			}
			for _, slug := range slugVariants(heading) {
				if n := seen[slug]; n == 0 {
					set[slug] = true
				} else {
					set[slug+"-"+strconv.Itoa(n)] = true
				}
				seen[slug]++
			}
		}
		slugs[rel] = set
	}

	linkRe := regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	// 行内代码里的例子不是链接：`[label](x.md#anchor)` 这种形状会出现在
	// "教人怎么写文档"的地方。不剥掉它，这条检查就会对着**示例**报错——
	// 而会误报的门禁会被忽略（`scripts/check-docs-links.sh` 同样剥掉，两处规则一致）。
	codeRe := regexp.MustCompile("`[^`]*`")
	var problems []string
	checked := 0

	for _, rel := range docs {
		data, rerr := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for i, rawLine := range strings.Split(string(data), "\n") {
			line := codeRe.ReplaceAllString(rawLine, "")
			for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
				target := strings.TrimSpace(m[1])
				if target == "" || strings.HasPrefix(target, "http://") ||
					strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") ||
					strings.HasPrefix(target, "tel:") || strings.HasPrefix(target, "file:") {
					continue
				}
				anchor := ""
				targetRel := ""
				if strings.HasPrefix(target, "#") {
					anchor = strings.TrimPrefix(target, "#")
					targetRel = rel
				} else {
					fp, a, ok := strings.Cut(target, "#")
					if !ok || a == "" {
						continue
					}
					anchor = a
					joined := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(rel), fp)))
					if !files[joined] {
						continue // 文件在不在由 check-docs-links 负责
					}
					targetRel = joined
				}
				checked++
				if !slugs[targetRel][anchor] {
					problems = append(problems, describe(rel, i+1, m[0], targetRel, anchor, slugs[targetRel]))
				}
			}
		}
	}

	// 空跑保护：一条"什么都没验"的检查会一路绿。
	if checked < 10 {
		t.Fatalf("只验了 %d 个锚点——检查多半失效了，而不是文档都没问题", checked)
	}
	if len(problems) > 0 {
		t.Errorf("%d 个锚点不存在（点进去只会静默跳到页首）：\n%s",
			len(problems), strings.Join(problems, "\n"))
	}
	t.Logf("anchors checked: %d", checked)
}

// describe 给出一条可读的失败信息，并列出目标文件里"形近"的锚点。
func describe(file string, line int, link, target, anchor string, available map[string]bool) string {
	prefix := anchor
	if utf8.RuneCountInString(prefix) > 4 {
		prefix = string([]rune(prefix)[:4])
	}
	var near []string
	for s := range available {
		if strings.HasPrefix(s, prefix) {
			near = append(near, s)
		}
	}
	sort.Strings(near)
	if len(near) > 4 {
		near = near[:4]
	}
	hint := ""
	if len(near) > 0 {
		hint = "  该文件里形近的锚点：" + strings.Join(near, " | ")
	}
	// 路径**原样**打印：`file` 已经是相对仓库根的路径（v0.10 B 组把扫描范围扩到根 README
	// 时，这里曾经硬编码着 `docs/` 前缀，于是失败信息会把根 README 说成 `docs/README.md`
	// ——报错指错文件，等于让人去翻错地方）。
	return "  " + file + ":" + strconv.Itoa(line) + "  " + link +
		"  → " + target + "#" + anchor + " 不存在。" + hint
}

// headingText 取出 Markdown 标题（`#` 开头、最多 6 个）的正文。
func headingText(line string) (string, bool) {
	s := strings.TrimRight(line, " \t\r")
	i := 0
	for i < len(s) && s[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i == len(s) || s[i] != ' ' {
		return "", false
	}
	return strings.TrimSpace(s[i+1:]), true
}

// keptRune 报告"这个字符在 GitHub 的锚点里会保留"——字母、数字、标记、连接符（`_` 属此类）、
// 以及连字符。其余（标点、符号、emoji）去掉。
func keptRune(r rune) bool {
	return r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r) ||
		unicode.IsMark(r) || unicode.Is(unicode.Pc, r)
}

// slugVariants 返回一个标题的可接受锚点变体。
//
// 第一个是**严格**变体（保留字母/数字/标记/连接符/`-`，其余全去）。
// 若标题里含"符号类"字符（`≠`、箭头、emoji 等不属于 字母/数字/标记/连接符/
// 空格/标点 的字符），再返回一个**放宽**变体：那些字符也保留。
// 理由见文件头：规则不确定时放宽，代价只是漏报。
func slugVariants(heading string) []string {
	lower := strings.ToLower(heading)
	strict := slug(lower, false)
	out := []string{strict}
	for _, r := range lower {
		if !keptRune(r) && r != ' ' && !unicode.IsPunct(r) {
			lenient := slug(lower, true)
			if lenient != strict {
				out = append(out, lenient)
			}
			break
		}
	}
	return out
}

func slug(lower string, keepSymbols bool) string {
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case keptRune(r):
			b.WriteRune(r)
		case keepSymbols && !unicode.IsPunct(r):
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}
