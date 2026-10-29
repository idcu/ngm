package observability

import (
	"testing"
	"time"
)

// v0.106：条数上界不约束**搜索**，只约束**命中**。
//
// 缺陷形态：目标是"图里有、但从根不可达"的时候（lock 里有孤儿条目，或上游
// 声明的边断在半路），`FindPaths` 一条路径都不会命中，于是 DFS 把全部简单路径
// 走一遍——那是指数的（深度 20 的格是 1 048 576 条）。而报告里
// `pathsTruncated` 还是 **false**，读的人会以为"确实没有路径"。
//
// 判据两条：**有界**（时间）+ **说出来**（truncated）。
func TestV106WhyBoundsTheSearchEvenWhenNothingMatches(t *testing.T) {
	g := lattice(20) // 与 TestV17 同一个夹具：不限量时 1 048 576 条路径 / 0.78s / 626 MB

	start := time.Now()
	paths, truncated := FindPaths(g, "github:acme/not-in-this-graph", MaxWhyPaths)
	elapsed := time.Since(start)

	if len(paths) != 0 {
		t.Fatalf("an unreachable target must yield no paths, got %d", len(paths))
	}
	if !truncated {
		t.Error("the search was cut short by the work budget, so the report must say so — " +
			"otherwise `0 paths` reads as `no path exists`, which is a different claim")
	}
	// 不限量时 0.78s；有预算时应当是两个数量级更快。500ms 两边都留足余量。
	if elapsed > 500*time.Millisecond {
		t.Errorf("bounded search of an unreachable target took %v — "+
			"the work budget is not being applied", elapsed)
	}
	t.Logf("unreachable target: 0 paths in %v (unbounded would be 1048576 paths)", elapsed)
}

// `--all`（max <= 0）不受工作量预算约束：那是用户明确要求"全部"。
// 这条断言防的是"顺手把预算也加到 --all 上"——那会让 --all 名不副实。
func TestV106UnlimitedStillMeansUnlimited(t *testing.T) {
	g := lattice(6)
	paths, truncated := FindPaths(g, leafOf(6), 0)
	if len(paths) != MaxWhyPaths || truncated {
		t.Errorf("unlimited enumeration: paths=%d truncated=%v, want %d/false",
			len(paths), truncated, MaxWhyPaths)
	}
}
