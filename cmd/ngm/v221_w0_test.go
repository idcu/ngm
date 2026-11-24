package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// W0/E2：定位页不是"再写一篇介绍"，而是把四问与非目标钉死在文档里。
// 覆盖集来自页面本身：小标题 + 逐行事实；删掉任意一问（例如"构建谁做"）必须变红。
func TestV221PositioningCoversFourQuestions(t *testing.T) {
	const rel = "docs/guides/positioning.md"
	doc := readRepoFile(t, rel)

	questions := []string{"依赖从哪来", "构建谁做", "压缩谁做", "任务谁编排"}
	for _, q := range questions {
		re := regexp.MustCompile(`(?m)^##\s+.*` + regexp.QuoteMeta(q))
		if !re.MatchString(doc) {
			t.Errorf("%s 缺少四问之一的小标题 %q", rel, q)
		}
	}

	lines := strings.Split(doc, "\n")
	// 非目标必须与"不做"同一句话出现，避免只在别处提了一嘴就算数。
	for _, term := range []string{"registry", "semver", "publish", "workspace"} {
		found := false
		for _, line := range lines {
			lower := strings.ToLower(line)
			if strings.Contains(lower, term) &&
				(strings.Contains(line, "不做") || strings.Contains(line, "不提供")) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: %q 必须出现在一句明确的「不做」声明里", rel, term)
		}
	}

	// 引擎与编排器都必须明确"外置/外部"，而不是被说成 ngm 自带实现。
	for _, group := range []struct {
		terms []string
		label string
	}{
		{[]string{"esbuild", "tsc", "postcss"}, "构建/压缩引擎"},
		{[]string{"npm scripts", "Just", "Taskfile"}, "任务编排器"},
	} {
		for _, term := range group.terms {
			found := false
			for _, line := range lines {
				if strings.Contains(line, term) &&
					(strings.Contains(line, "外置") || strings.Contains(line, "外部")) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: %s %q 必须出现在一句明确的「外置/外部」声明里", rel, group.label, term)
			}
		}
	}

	if !strings.Contains(doc, "../internals/capability-matrix.md") {
		t.Error("定位页必须链到能力矩阵（能力归属的唯一事实源）")
	}
	if _, err := os.Stat(filepath.Join(repoRoot(t), "docs", "internals", "capability-matrix.md")); err != nil {
		t.Errorf("能力矩阵链接目标不存在: %v", err)
	}
	for _, adr := range []string{"ADR-044", "ADR-052"} {
		if !strings.Contains(doc, adr) {
			t.Errorf("定位页必须引用 %s 的决策边界", adr)
		}
	}
	if !strings.Contains(doc, "常见误解") {
		t.Error("定位页必须有一节「常见误解」")
	}
}
