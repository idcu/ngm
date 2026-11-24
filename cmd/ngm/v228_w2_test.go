package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV228MinifyDeterminismScopeIsStated 固定 E9 的文档口径。
//
// 变异检验：删掉 build.md 里"不跨工具承诺"一句 → 第 2 条断言立刻红；恢复 → 绿。
func TestV228MinifyDeterminismScopeIsStated(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "guides", "build.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	t.Run("the three-element scope is stated", func(t *testing.T) {
		for _, want := range []string{"同输入", "同参数", "同引擎"} {
			if !strings.Contains(doc, want) {
				t.Errorf("build.md must state the %q element of the determinism scope", want)
			}
		}
		if !strings.Contains(doc, "esbuild") || !strings.Contains(doc, "回执字节") {
			t.Error("build.md must tie the promise to esbuild's minify receipt bytes")
		}
	})

	t.Run("no cross-tool promise", func(t *testing.T) {
		if !strings.Contains(doc, "不跨工具承诺") {
			t.Error("build.md must state that determinism is not promised across tools/engines")
		}
		for _, want := range []string{"不同压缩工具", "不同版本"} {
			if !strings.Contains(doc, want) {
				t.Errorf("build.md must spell out the non-promise for %q", want)
			}
		}
	})

	t.Run("evidence links are present", func(t *testing.T) {
		// 单篇 ADR 已随历史文档清理移出工作树；ADR-013 现在的唯一入口是汇总表。
		for _, want := range []string{
			"probe-reproducibility.sh",
			"ADR-013",
			"../adr/README.md",
		} {
			if !strings.Contains(doc, want) {
				t.Errorf("build.md must link %q as the evidence path", want)
			}
		}
	})
}
