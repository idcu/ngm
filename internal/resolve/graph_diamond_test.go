package resolve_test

import (
	"context"
	"testing"

	"github.com/idcu/ngm/internal/resolve"
)

// TestV68CrossLayerDiamondIsResolvedOnce 固定"先与 seen 对账，再解析"（评估报告 P1#3）。
//
// 跨层菱形：A→B→D 且 A→C→D。修复前 D 会被**完整解析两次**——第二次含一次真实的
// `EnsureMirror`（对已存在的 mirror 就是一次 `git fetch`）加一次 ref 解析：
// 复杂度从 O(节点) 退化成 O(边)。
//
// 判据用 EnsureMirror 的**调用次数**（精确、与机器无关），不用时间。
func TestV68CrossLayerDiamondIsResolvedOnce(t *testing.T) {
	w := newFixtureWorld(t)
	w.add("github:w/leaf", leafRepo(t, "v1"))

	mid := func(name string) {
		r := repoWithDeps(t, dep("github:w/leaf", "v1"))
		r.Tag("v1", false)
		w.add(name, r)
	}
	mid("github:w/mid")
	mid("github:w/mid2")

	top := repoWithDeps(t, dep("github:w/mid", "v1"), dep("github:w/mid2", "v1"))
	top.Tag("v1", false)
	w.add("github:w/top", top)

	base := w.opts()
	calls := map[string]int{}
	opts := base
	opts.EnsureMirror = func(ctx context.Context, repo resolve.Canonical) (string, error) {
		calls[repo.Slug()]++
		return base.EnsureMirror(ctx, repo)
	}

	g, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/top", Ref: "v1", RefType: resolve.RefTypeTag},
	}, opts)
	if err != nil {
		t.Fatalf("ResolveGraph: %v", err)
	}

	if n := calls["github:w/leaf"]; n != 1 {
		t.Errorf("跨层菱形里 leaf 被解析了 %d 次，want 1——"+
			"重复的那次含一次真实的 fetch，且不产生任何新信息", n)
	}

	// **来源信息不丢**：跳过解析的那条路径必须继续做它真正的工作（合并来源）。
	node, ok := g.Find("github:w/leaf")
	if !ok {
		t.Fatal("leaf 不在图里")
	}
	if len(node.RequiredBy) != 2 {
		t.Errorf("leaf.RequiredBy = %v, want 恰好两条来源（两个中间节点各一条）", node.RequiredBy)
	}
}
