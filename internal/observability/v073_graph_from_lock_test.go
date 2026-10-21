package observability

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
)

// v0.73：`GraphFromLock` 的判据（[ADR-028](../../docs/adr/adr-028-dependency-graph-in-lock.md)）。
//
// 它守的是一条：**从 lock 重建出来的图，必须与解析出来的图给出同样的答案**——
// 否则 `ngm why` 的"离线路径"就是另一个答案源，而两个答案源迟早会不一致。

// diamondLock 造一个跨层菱形：A→C 且 A→B→C（C 被两个父节点直接引用）。
func diamondLock() *lock.File {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{
		{Name: "github:a/app", Ref: "v1", RefType: "tag", RequiredBy: []string{lock.RootRequirer}},
		{Name: "github:b/util", Ref: "v1", RefType: "tag", RequiredBy: []string{"github:a/app"}},
		{Name: "github:c/lib", Ref: "v1", RefType: "tag", RequiredBy: []string{"github:a/app", "github:b/util"}},
	}
	return lf
}

func TestV73GraphFromLockShape(t *testing.T) {
	g := GraphFromLock(diamondLock())

	if len(g.Nodes) != 3 {
		t.Fatalf("nodes = %d", len(g.Nodes))
	}
	byKey := map[string]*resolve.Node{}
	for _, n := range g.Nodes {
		byKey[n.Key] = n
	}
	// 身份：name；RootDeclared 由根标记派生。
	if !byKey["github:a/app"].RootDeclared {
		t.Error("requiredBy 含根标记 ⇒ RootDeclared 为真")
	}
	if byKey["github:b/util"].RootDeclared || byKey["github:c/lib"].RootDeclared {
		t.Error("只有根声明的节点才是 RootDeclared")
	}
	// 根标记必须翻译成 resolve.RootMarker，否则 childrenOf 会把它当成一个普通节点 Key。
	for _, from := range byKey["github:a/app"].RequiredBy {
		if from != resolve.RootMarker {
			t.Errorf("根标记没有翻译成 resolve.RootMarker: %q", from)
		}
	}
	// 边：C 的两个直接引用者都在。
	got := strings.Join(byKey["github:c/lib"].RequiredBy, ",")
	if got != "github:a/app,github:b/util" {
		t.Errorf("C 的直接引用者 = %q", got)
	}
}

func TestV73GraphFromLockGivesTheSamePaths(t *testing.T) {
	g := GraphFromLock(diamondLock())

	paths, truncated := FindPaths(g, "github:c/lib", 0)
	if truncated {
		t.Error("只有两条路径，不应被截断")
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %d (%v)，菱形应有两条：A→C 与 A→B→C", len(paths), paths)
	}
	// 顺序由 childrenOf 的排序决定（子节点按 Key 字节序）：b/util 先于 c/lib，
	// 因此先走完"经过 B 的那条"。顺序本身也是契约的一部分——它是确定的。
	want := []string{
		"ngm.json → github:a/app@v1 → github:b/util@v1 → github:c/lib@v1",
		"ngm.json → github:a/app@v1 → github:c/lib@v1",
	}
	for i, p := range paths {
		if got := strings.Join(p, " → "); got != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got, want[i])
		}
	}
}

// 空 lock 与 nil 都不得 panic——`why` 在这种 lock 上只是"图里没有它"。
func TestV73GraphFromLockEmpty(t *testing.T) {
	if g := GraphFromLock(nil); len(g.Nodes) != 0 {
		t.Errorf("nil lock ⇒ 空图，got %d 个节点", len(g.Nodes))
	}
	if g := GraphFromLock(lock.NewFile()); len(g.Nodes) != 0 {
		t.Errorf("空 lock ⇒ 空图，got %d 个节点", len(g.Nodes))
	}
}
