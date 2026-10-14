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

// **Go 侧的"仓库内链接"扫描，唯一的实现**（v0.97 抽出来）。
//
// 为什么要抽：v0.96 的大小写判据自己写了一遍解析，而它**漏了围栏代码块那条规则**
// （shell 的 `link_scan` 在 v0.96 才加上）⇒ 一条写在围栏块里的示例链接会被 Go 侧
// 当成真链接去查大小写（潜在误报，而"会误报的门禁会被忽略"）。
// 抽成一处之后，**Go 侧的两种判据（大小写 / 与 shell 对照）用的是同一份解析**。
//
// 与 shell 的 `link_scan` **同口径**（两边必须一致，由 `TestV097…` 守着）：
//   · 文件集：`docs/**/*.md` + 根 `README.md`（跳过 node_modules / vendor / ngm.vendor / dist）；
//   · **围栏代码块整块跳过**（``` 开、``` 关，每个文件重新开始）；
//   · 行内代码（成对反引号）剥掉；
//   · destination 按**平衡括号**解析（v0.92）；
//   · 跳过跨仓 / 锚点 / 被忽略目录里的目标。

type linkRef struct {
	line   int    // 1-based 行号
	raw    string // 原始匹配，例如 `[标签](./a.md)`
	target string // destination（已剥锚点），例如 `./a.md`
}

var reInlineCode = regexp.MustCompile("`[^`]*`")
var reURLScheme = regexp.MustCompile(`^(https?|mailto|tel|file):`)

// repoLinkRefs 返回（文件相对路径 → 该文件里被检查的链接）。文件集合已排序。
func repoLinkRefs(t *testing.T, root string) map[string][]linkRef {
	t.Helper()
	out := map[string][]linkRef{}
	var files []string

	docsRoot := filepath.Join(root, "docs")
	_ = filepath.WalkDir(docsRoot, func(p string, d os.DirEntry, err error) error {
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
			files = append(files, p)
		}
		return nil
	})
	if readme := filepath.Join(root, "README.md"); fileExists(readme) {
		files = append(files, readme)
	}
	sort.Strings(files)

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读 %s: %v", f, err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)

		var refs []linkRef
		inFence := false
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimLeft(line, " \t")
			if strings.HasPrefix(trimmed, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			cleaned := reInlineCode.ReplaceAllString(line, "")
			for _, m := range linkMatches(cleaned) {
				target := normalizeTarget(m.target)
				if target == "" {
					continue
				}
				refs = append(refs, linkRef{line: i + 1, raw: m.raw, target: target})
			}
		}
		if len(refs) > 0 {
			out[rel] = refs
		}
	}
	return out
}

type linkMatch struct{ raw, target string }

// linkMatches 抽出一行里所有 `[label](dest)`——**平衡括号**口径（与 shell 同）。
func linkMatches(line string) []linkMatch {
	var out []linkMatch
	rest := line
	for {
		open := strings.Index(rest, "](")
		if open < 0 {
			return out
		}
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
		labelStart := strings.LastIndex(rest[:open], "[")
		out = append(out, linkMatch{raw: rest[labelStart : end+1], target: rest[open+2 : end]})
		rest = rest[end+1:]
	}
}

// normalizeTarget 过滤掉不该查的目标，并剥掉锚点；返回空串表示"不查"。
func normalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "#") || reURLScheme.MatchString(target) {
		return ""
	}
	if idx := strings.Index(target, "#"); idx >= 0 {
		target = target[:idx]
	}
	// 根相对（以 `/` 开头）**要查**——它相对仓库根（与 GitHub 一致），
	// 此前 Go 侧直接跳过它，于是与 shell 侧对同一批输入给出不同结论
	// （v0.97 的一致性对照当场抓到：shell 报、Go 不报）。
	if target == "" || isIgnoredTarget(target) {
		return ""
	}
	return target
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

// resolveRef 把一处链接解析成相对仓库根的路径（已清理 `..`）。
//
// 注意：必须从**绝对**路径出发再 `filepath.Rel`——`Rel(绝对, 相对)` 在 Windows 上直接报错，
// 而报错在这里会被当成"这条链接不用查"静默跳过（v0.96 的牙齿当场抓住了这个写法）。
func resolveRef(root, relFile, target string) (string, bool) {
	target = filepath.ToSlash(target)
	if strings.HasPrefix(target, "/") {
		// 根相对：相对**仓库根**（不是相对文件所在目录）——与 shell 侧同口径。
		return strings.TrimPrefix(path.Clean(target), "/"), true
	}
	abs := filepath.ToSlash(filepath.Join(root, filepath.FromSlash(relFile)))
	base := path.Dir(abs)
	resolved := path.Clean(path.Join(base, target))
	rel, err := filepath.Rel(filepath.ToSlash(root), resolved)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
