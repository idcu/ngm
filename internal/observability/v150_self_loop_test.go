package observability

import (
	"testing"

	"github.com/idcu/ngm/internal/core/resolve"
)

// TestV150TreeAndWhyAgreeOnSelfLoop 固定审计 P-02：
// why 的 childrenOf 显式丢弃自引用边，BuildTree 曾未过滤——同一张图两个答案。
func TestV150TreeAndWhyAgreeOnSelfLoop(t *testing.T) {
	g := &resolve.Graph{Nodes: []*resolve.Node{{
		Key:          "github:x/a",
		Name:         "github:x/a",
		RootDeclared: true,
		RequiredBy:   []string{resolve.RootMarker, "github:x/a"}, // 根引用 + 自环
	}}}
	entries, truncated := BuildTree(g, nil, 0)
	if truncated {
		t.Fatal("a one-node graph must not be truncated")
	}
	if got := countEntries(entries); got != 1 {
		t.Errorf("tree entries = %d, want 1 (the self-loop must not create a cycle node)", got)
	}
	paths, truncated := FindPaths(g, g.Nodes[0].Key, MaxWhyPaths)
	if truncated || len(paths) != 1 {
		t.Errorf("why paths = %d truncated=%v, want 1/false", len(paths), truncated)
	}
}
