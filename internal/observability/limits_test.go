package observability

import (
	"fmt"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/resolve"
)

// TestV17WhyPathsAreBoundedAndSaySo 固定 why 的路径枚举上界（ADR-024）。
//
// 为什么需要它：路径数是**指数**的——放大它不需要很多节点，只需要图"宽"。
// 实测：41 个节点的格状图产出 1,048,576 条路径、分配 626 MB、0.78s。
// 而图的形状来自**上游的 ngm.json**，不是本项目的输入。没有上界时，
// 一个写坏或恶意的上游就能让 `ngm why` 在 CI 里吃掉几个 GB。
//
// 四条断言，缺一条都不够：
//
//	① 默认最多 MaxWhyPaths 条（**输出**有界）
//	② 有界时 truncated 为真（**截断可见**——否则"只看到 64 条"会被读成"只有 64 条"）
//	③ 枚举**立刻停止**（**工作量**有界——否则上界只是把结果砍短，最坏情况照样要付）
//	④ 恰好 MaxWhyPaths 条时 truncated 必须是 false（不能把"列完了"说成"还有更多"）
func TestV17WhyPathsAreBoundedAndSaySo(t *testing.T) {
	// 深度 20 的格：不限量时是 1,048,576 条路径（实测 0.78s / 626 MB）。
	g := lattice(20)
	leaf := leafOf(20)

	start := time.Now()
	paths, truncated := FindPaths(g, leaf, MaxWhyPaths)
	bounded := time.Since(start)

	if len(paths) != MaxWhyPaths {
		t.Errorf("paths=%d want exactly %d (the cap)", len(paths), MaxWhyPaths)
	}
	if !truncated {
		t.Error("truncated must be true when the cap was reached and more paths exist")
	}
	// ③ 的判据是时间，因此阈值要留足余量：不限量时是 0.78s，
	// 提前停止后应当是微秒级。100ms 在两边都有 8 倍以上的余量。
	if bounded > 100*time.Millisecond {
		t.Errorf("bounded enumeration took %v — the cap must bound the WORK, not just the output", bounded)
	}
	t.Logf("bounded: %d paths in %v (unbounded would be 1048576 paths / 626 MB)", len(paths), bounded)

	// ④ 恰好等于上限：列完了，就不许说"还有更多"。
	exact := lattice(6) // 2^6 = 64 条
	p64, tr64 := FindPaths(exact, leafOf(6), MaxWhyPaths)
	if len(p64) != MaxWhyPaths {
		t.Fatalf("exact-fit graph produced %d paths, want %d", len(p64), MaxWhyPaths)
	}
	if tr64 {
		t.Error("truncated must be false when the enumeration finished exactly at the cap")
	}

	// `--all` 走的就是 max<=0：不限量时必须给出全部。
	all, trAll := FindPaths(exact, leafOf(6), 0)
	if len(all) != MaxWhyPaths || trAll {
		t.Errorf("unlimited enumeration: paths=%d truncated=%v, want %d/false", len(all), trAll, MaxWhyPaths)
	}
}

// TestV17TreeExpansionIsBounded 固定 tree 的展开预算（ADR-024）。
//
// 树是**按路径展开**的（同一节点在多条路径下各出现一次），所以它与 why 的
// 路径数是同一个数——代价只会更大，因为它还要把每条路径渲染出来。
func TestV17TreeExpansionIsBounded(t *testing.T) {
	// 深度 13 的格：不限量时是 2^13 = 8192 条路径 > 4096 的预算。
	g := lattice(13)

	entries, truncated := BuildTree(g, nil, MaxTreeEntries)
	if !truncated {
		t.Error("truncated must be true when the budget ran out")
	}
	count := countEntries(entries)
	if count != MaxTreeEntries {
		t.Errorf("entries=%d want exactly %d (the budget stops expansion)", count, MaxTreeEntries)
	}
	if !hasTruncatedMarker(entries) {
		t.Error("the entry where expansion stopped must carry the marker — " +
			"otherwise a reader cannot tell where the tree ends")
	}
	t.Logf("bounded tree: %d entries, truncated=%v", count, truncated)

	// 预算之内：不许标截断（正常图形不受影响）。
	small := lattice(4)
	sEntries, sTruncated := BuildTree(small, nil, MaxTreeEntries)
	if sTruncated || hasTruncatedMarker(sEntries) {
		t.Error("a graph well inside the budget must not be marked as truncated")
	}
	if n := countEntries(sEntries); n != expandedEntries(4) {
		t.Errorf("small lattice: entries=%d want %d", n, expandedEntries(4))
	}

	// `--all` 走的就是 max<=0：不限量时必须展开全部。
	allEntries, allTruncated := BuildTree(g, nil, 0)
	if allTruncated || hasTruncatedMarker(allEntries) {
		t.Error("unlimited expansion must not be marked as truncated")
	}
	if n := countEntries(allEntries); n != expandedEntries(13) {
		t.Errorf("unlimited expansion: entries=%d want %d", n, expandedEntries(13))
	}
}

// expandedEntries 是 depth 层格状图**展开后**的条目数。
//
// 注意它**不是**路径数（2^depth）：树是按路径展开的，同一个共享节点会随每条
// 到达它的路径各出现一次。逐层求和：
//
//	根那层 2 个节点各 1 次       = 2
//	第 L 层 2 个节点各 2^(depth-L) 次
//	叶 1 个节点 2^depth 次
//
// 合计 3·2^depth - 2。depth=4 → 46，depth=13 → 24574。
// （v0.17 的第一次断言把这里当成 2^depth 了——网的第一次失败抓的是**我自己**的算术。）
func expandedEntries(depth int) int {
	return 3*(1<<depth) - 2
}

func countEntries(entries []*TreeEntry) int {
	n := 0
	for _, e := range entries {
		n++
		n += countEntries(e.Children)
	}
	return n
}

func hasTruncatedMarker(entries []*TreeEntry) bool {
	for _, e := range entries {
		if e.Truncated || hasTruncatedMarker(e.Children) {
			return true
		}
	}
	return false
}

func leafOf(depth int) string { return fmt.Sprintf("github:lat/leaf-%d", depth) }

// lattice 造一个 depth 层的格：每层的两个节点**都**依赖下一层的两个节点，
// 于是从根到叶的简单路径数 = 2^depth。
//
// `RequiredBy` 是**父节点**（谁依赖我），不是子节点——写反了会得到 0 条路径
// （v0.17 的第一次探针就是这么错的）。
func lattice(depth int) *resolve.Graph {
	g := &resolve.Graph{}
	levels := make([][2]string, depth+1) // levels[0] 是最深的叶，levels[depth] 是根声明的两个节点
	levels[0] = [2]string{leafOf(depth), leafOf(depth)}
	for level := 1; level <= depth; level++ {
		levels[level] = [2]string{
			fmt.Sprintf("github:lat/a%d-%d", depth, level),
			fmt.Sprintf("github:lat/b%d-%d", depth, level),
		}
	}

	add := func(key string, parents []string, root bool) {
		g.Nodes = append(g.Nodes, &resolve.Node{
			Key: key, Name: key, Ref: "v1", RefType: resolve.RefTypeTag, Commit: "deadbeef",
			RootDeclared: root, RequiredBy: parents,
		})
	}
	add(levels[0][0], []string{levels[1][0], levels[1][1]}, false)
	for level := 1; level <= depth; level++ {
		var parents []string
		if level+1 <= depth {
			parents = []string{levels[level+1][0], levels[level+1][1]}
		}
		for _, key := range levels[level] {
			add(key, parents, level == depth)
		}
	}
	return g
}
