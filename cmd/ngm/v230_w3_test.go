package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV230TaskOrchestrationNonGoalsAreExplicit 固定 E11：任务编排非目标只写在
// ADR-042 的补记里，且配置层没有 dependsOn 字段（capability matrix 由另一处维护）。
//
// 变异检验：删掉补记里的"增量调度"一项 → "五个非目标"子测试立刻红；恢复 → 绿。
// 往 internal/config 里加一个 json:"dependsOn" 字段 → 最后一条断言红；恢复 → 绿。
func TestV230TaskOrchestrationNonGoalsAreExplicit(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "adr", "adr-042-run-outside-sandbox.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	adr := string(raw)

	t.Run("append-only 补记 exists", func(t *testing.T) {
		if !strings.Contains(adr, "## 补记") || !strings.Contains(adr, "2026-11-24") {
			t.Error("ADR-042 must carry an appended 补记, not a new ADR")
		}
	})

	t.Run("all non-goals are explicit", func(t *testing.T) {
		for _, want := range []string{
			"任务图",         // 任务依赖图
			"并行调度",        // 并行
			"增量调度",        // 增量
			"watch 调度",    // watch
			"安装依赖型 `run`", // 安装型 run
			".bin` shim",  // shim 非目标
		} {
			if !strings.Contains(adr, want) {
				t.Errorf("ADR-042 补记 must list the non-goal %q", want)
			}
		}
	})

	t.Run("non-goal decision threshold", func(t *testing.T) {
		for _, want := range []string{"实测收益", "确定性风险", "单开"} {
			if !strings.Contains(adr, want) {
				t.Errorf("反案门槛 must state %q", want)
			}
		}
	})

	t.Run("bin shim still points at ADR-043", func(t *testing.T) {
		if !strings.Contains(adr, "ADR-043") || !strings.Contains(adr, "adr-043-vendored-bin-shim.md") {
			t.Error(".bin shim must stay an ADR-043 boundary, not be claimed as supported here")
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
