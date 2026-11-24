package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV230NonGoalsAreExplicit 固定 E11：非目标只写在能力矩阵的「非目标」段里，
// 且配置层没有 dependsOn 字段（任务编排一旦进配置，就得有图语义，那是明确不做的目标）。
//
// 此前这些词写在 ADR-042 的补记里；单篇 ADR 已随历史文档清理移出工作树，
// 能力矩阵是能力归属的唯一事实源，因此断言改读它。五个非目标词与无 dependsOn
// 的代码断言都保留——变异检验：删掉矩阵里任意一个词 → "五个非目标"子测试红；
// 往 internal/config 里加一个 json:"dependsOn" 字段 → 最后一条断言红。
func TestV230NonGoalsAreExplicit(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "internals", "capability-matrix.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	t.Run("non-goals section exists", func(t *testing.T) {
		if !strings.Contains(doc, "## 非目标") {
			t.Error("capability-matrix.md must carry a 非目标 section — it is the single source of truth for what ngm does not do")
		}
	})

	t.Run("all five non-goals are explicit", func(t *testing.T) {
		for _, want := range []string{
			"registry",  // registry 包管理
			"semver",    // 语义化版本解析
			"publish",   // 发布到 registry
			"workspace", // 多包 workspace 求解
			"多版本共存",     // 同一依赖多版本并存
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("capability-matrix.md 非目标段 must list %q", want)
			}
		}
	})

	t.Run("remote adapter stays excluded", func(t *testing.T) {
		if !strings.Contains(doc, "remote") || !strings.Contains(doc, "永久排除") {
			t.Error("capability-matrix.md must keep the remote adapter excluded, not revived")
		}
	})

	t.Run("no dependsOn config field", func(t *testing.T) {
		files, err := filepath.Glob(filepath.Join("..", "..", "internal", "config", "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("cannot enumerate config sources: %v", err)
		}
		for _, f := range files {
			b, rerr := os.ReadFile(f)
			if rerr != nil {
				t.Fatal(rerr)
			}
			src := string(b)
			if strings.Contains(src, `json:"dependsOn`) || strings.Contains(src, "DependsOn") {
				t.Errorf("config source %s declares a task-graph field dependsOn", f)
			}
		}
	})
}
