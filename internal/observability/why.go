package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
)

// WhyReport 是 `ngm why` 的报告。
type WhyReport struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	RefType string `json:"refType"`
	Commit  string `json:"commit"`
	SubPath string `json:"subPath,omitempty"`

	// RootDeclared 为真表示该依赖由根 ngm.json 直接声明。
	RootDeclared bool `json:"rootDeclared"`
	// Paths 是从根到该依赖的**全部**路径（同一依赖常被多个父节点引入）。
	//
	// 每条路径以 "ngm.json" 开头、以目标节点结尾，元素格式为 `name@ref`。
	Paths [][]string `json:"paths"`

	Locked *LockedInfo `json:"locked,omitempty"`
}

// LockedInfo 是 lock 中关于该依赖的记录。
type LockedInfo struct {
	Commit        string `json:"commit"`
	ResolvedAt    string `json:"resolvedAt,omitempty"`
	ArchiveDigest string `json:"archiveDigest,omitempty"`
}

// ResolveTarget 把用户给出的依赖标识解析为节点 Key。
//
// 两种输入：`github:o/r`（Name）与 `github:o/r#sub`（Key）。
// 只给 Name 时若匹配到多个子路径节点，必须报歧义而不是随便挑一个——
// "为什么装了它"这个问题在 monorepo 下对不同子路径答案不同。
func ResolveTarget(g *resolve.Graph, arg string) (*resolve.Node, error) {
	if n, ok := g.Find(arg); ok {
		return n, nil
	}
	var matches []*resolve.Node
	for _, n := range g.Nodes {
		if n.Name == arg {
			matches = append(matches, n)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%s is not in the dependency graph", arg)
	case 1:
		return matches[0], nil
	default:
		subs := make([]string, 0, len(matches))
		for _, m := range matches {
			subs = append(subs, m.Key)
		}
		sort.Strings(subs)
		return nil, fmt.Errorf("%s is ambiguous: %v (pass the full key, e.g. %s)",
			arg, subs, subs[0])
	}
}

// FindPaths 找出从根到目标节点的全部路径。
//
// 反向遍历（自顶向下）而不是从 RequiredBy 回溯：RequiredBy 只记录了"父节点"，
// 回溯时无法保证路径顺序与去重；正向 DFS 天然按根→子展开，且便于做环保护。
func FindPaths(g *resolve.Graph, target string) [][]string {
	children := childrenOf(g)
	var out [][]string
	var roots []*resolve.Node
	for _, n := range g.Nodes {
		if n.RootDeclared {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Key < roots[j].Key })

	for _, r := range roots {
		walk(r, target, children, nil, map[string]bool{}, &out)
	}
	return out
}

func childrenOf(g *resolve.Graph) map[string][]*resolve.Node {
	byKey := make(map[string]*resolve.Node, len(g.Nodes))
	out := map[string][]*resolve.Node{}
	for _, n := range g.Nodes {
		byKey[n.Key] = n
	}
	for _, n := range g.Nodes {
		for _, parent := range n.RequiredBy {
			if parent == resolve.RootMarker || parent == n.Key {
				continue
			}
			if _, ok := byKey[parent]; ok {
				out[parent] = append(out[parent], n)
			}
		}
	}
	for k := range out {
		sort.SliceStable(out[k], func(i, j int) bool { return out[k][i].Key < out[k][j].Key })
	}
	return out
}

func walk(n *resolve.Node, target string, children map[string][]*resolve.Node, path []string, seen map[string]bool, out *[][]string) {
	path = append(path, label(n))
	if n.Key == target {
		*out = append(*out, append([]string{"ngm.json"}, path...))
		return
	}
	if seen[n.Key] {
		return
	}
	seen[n.Key] = true
	defer delete(seen, n.Key)
	for _, c := range children[n.Key] {
		walk(c, target, children, path, seen, out)
	}
}

func label(n *resolve.Node) string {
	if n.SubPath != "" {
		return n.Name + "#" + n.SubPath + "@" + n.Ref
	}
	return n.Name + "@" + n.Ref
}

// Marshal 输出机器可读报告。
func (r *WhyReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Render 渲染人类可读报告（结构对齐 observability.md §ngm why）。
func (r *WhyReport) Render(w io.Writer) {
	name := r.Name
	if r.SubPath != "" {
		name += "#" + r.SubPath
	}
	fmt.Fprintf(w, "%s@%s (%s) → %s\n\n", name, r.Ref, r.RefType, git.ShortSHA(r.Commit))

	if r.RootDeclared {
		fmt.Fprintf(w, "直接依赖：ngm.json 声明\n")
	} else {
		fmt.Fprintf(w, "直接依赖：否\n")
	}

	// 只把"经过别人的路径"算作传递：`ngm.json → x` 这样的两元素路径
	// 就是上面的直接声明本身，再列一遍会让人误以为它是被别人带进来的。
	var transitive [][]string
	for _, p := range r.Paths {
		if len(p) > 2 {
			transitive = append(transitive, p)
		}
	}
	if len(transitive) == 0 {
		fmt.Fprintf(w, "传递依赖：无\n")
	} else {
		fmt.Fprintf(w, "传递依赖：%d 条路径\n", len(transitive))
		for _, p := range transitive {
			fmt.Fprintf(w, "  %s\n", joinPath(p))
		}
	}
	if r.Locked != nil {
		fmt.Fprintf(w, "\n锁定：%s (%s)\n", git.ShortSHA(r.Locked.Commit), r.Locked.ResolvedAt)
		fmt.Fprintf(w, "archiveDigest: %s\n", r.Locked.ArchiveDigest)
	}
}

func joinPath(p []string) string {
	out := p[0]
	for _, s := range p[1:] {
		out += " → " + s
	}
	return out
}

// LockedFrom 从 lock 中取出该依赖的锁定信息。
func LockedFrom(lf *lock.File, name, subPath string) *LockedInfo {
	if lf == nil {
		return nil
	}
	d, ok := lf.Find(name, subPath)
	if !ok {
		return nil
	}
	return &LockedInfo{Commit: d.Commit, ResolvedAt: d.ResolvedAt, ArchiveDigest: d.ArchiveDigest}
}
