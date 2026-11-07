package observability

import "testing"

// v0.106：条数上界不约束**搜索**，只约束**命中**。
//
// 缺陷形态：目标是"图里有、但从根不可达"的时候（lock 里有孤儿条目，或上游
// 声明的边断在半路），`FindPaths` 一条路径都不会命中，于是 DFS 把全部简单路径
// 走一遍——那是指数的（深度 20 的格是 1 048 576 条）。而报告里
// `pathsTruncated` 还是 **false**，读的人会以为"确实没有路径"。
//
// 判据两条：**有界**（工作量）+ **说出来**（truncated）。
//
// v0.131：把"有界"的判据从墙钟改成**访问计数**。原断言"500ms 内完成"测量的是
// 机器速度，并发负载下会偶发误伤（h7 验证时 645.97ms）；而这条用例真正想守的
// 边界是"工作量预算在 MaxWhyWork 次访问处停手"。计数与负载无关，同一条图在
// 任何速度的机器上都得到同一个数。预算没有调大，也没有加 retry——只是把代理
// 指标换成了它代理的那个量。
func TestV106WhyBoundsTheSearchEvenWhenNothingMatches(t *testing.T) {
	g := lattice(20) // 与 TestV17 同一个夹具：不限量时 1 048 576 条路径 / 0.78s / 626 MB

	paths, truncated, visited := findPaths(g, "github:acme/not-in-this-graph", MaxWhyPaths, MaxWhyWork)

	if len(paths) != 0 {
		t.Fatalf("an unreachable target must yield no paths, got %d", len(paths))
	}
	if !truncated {
		t.Error("the search was cut short by the work budget, so the report must say so — " +
			"otherwise `0 paths` reads as `no path exists`, which is a different claim")
	}

	// 结构性断言：walk 先自增再与预算比较，所以预算在 MaxWhyWork 处的下一次
	// （第 MaxWhyWork+1 次）访问停手。若预算没生效，这条图会一路走到 1 048 576
	// 条路径之后（visited 远超此值）；若有人挪动常量，这里立刻红。
	if visited != MaxWhyWork+1 {
		t.Errorf("visited=%d, want %d (MaxWhyWork+1): the work budget must be the thing "+
			"that stopped the search", visited, MaxWhyWork+1)
	}

	// 预算参数必须真的控制停点：给 5 次就停在第 6 次。这条防的是"visited 恰好
	// 等于某个写死的数"的假绿——把参数忽略、直接用常量也能过上面那条。
	_, tr5, v5 := findPaths(g, "github:acme/not-in-this-graph", MaxWhyPaths, 5)
	if !tr5 || v5 != 6 {
		t.Errorf("injected budget 5: visited=%d truncated=%v, want 6/true", v5, tr5)
	}

	// 生产入口确实带着预算：visited 不外露，所以这里钉"停过"这个事实；
	// 上面的两条把常量与参数分别钉住，这一条钉住 0 参数的 `FindPaths` 用的就是它们。
	p2, tr2 := FindPaths(g, "github:acme/not-in-this-graph", MaxWhyPaths)
	if len(p2) != 0 || !tr2 {
		t.Errorf("FindPaths: paths=%d truncated=%v, want 0/true", len(p2), tr2)
	}
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
