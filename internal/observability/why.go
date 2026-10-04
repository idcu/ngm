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
	// Paths 是从根到该依赖的路径（同一依赖常被多个父节点引入）。
	//
	// 每条路径以 "ngm.json" 开头、以目标节点结尾，元素格式为 `name@ref`。
	//
	// **它可能是被截断的**：见 PathsTruncated。默认最多 MaxWhyPaths 条。
	Paths [][]string `json:"paths"`

	// PathsTruncated 为真表示 Paths **不是全部**：枚举在达到上限时停止。
	//
	// 它存在的唯一理由是：**截断必须可见**。否则"只看到 64 条"会被读成"只有 64 条"——
	// 那是这个项目最不能接受的一类错误（读数在说谎，而一切看起来都正常）。
	PathsTruncated bool `json:"pathsTruncated,omitempty"`

	// PathsLimit 是本次枚举的上限；0 表示未设上限（`--all`）。
	PathsLimit int `json:"pathsLimit,omitempty"`

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

// MaxWhyPaths 是 `ngm why` **默认**展开的路径条数上限（`--all` 解除）。
//
// 为什么必须有上界：路径数是**指数**的，放大它不需要很多节点，只需要图"宽"。
// 实测（这张网的数固定在 internal/observability 的测试里）：
//
//	节点数   路径数      耗时     分配     存活堆
//	    17      256    0.000s    0.1 MB    0.1 MB
//	    25    4 096    0.002s    1.7 MB    1.7 MB
//	    33   65 536    0.073s   63.3 MB   30.1 MB
//	    41 1 048 576   0.778s  626.1 MB  453.3 MB     ← 再宽一层就是 16 倍
//
// 而图的形状来自**上游的 ngm.json**（每个依赖自己的清单），不是本项目的输入：
// 一个写坏或恶意的上游，就能让 `ngm why` 在 CI 里吃掉几个 GB。
//
// 64 远大于"人真正会读的条数"（有用的答案通常 ≤ 5 条），同时把最坏情况钉住。
// 达到上限时**必须**在输出里说出来（PathsTruncated）——绝不静默截断。
// 决定与重开条件见 ADR-024。
const MaxWhyPaths = 64

// FindPaths 找出从根到目标节点的路径，**最多 max 条**（max <= 0 表示不限）。
//
// 反向遍历（自顶向下）而不是从 RequiredBy 回溯：RequiredBy 只记录了"父节点"，
// 回溯时无法保证路径顺序与去重；正向 DFS 天然按根→子展开，且便于做环保护与提前停止。
//
// 返回的 truncated 为真表示**至少还有一条未列出**。它由"多枚举一条"得出，
// 而不是由"碰到了上限"推断：图里**恰好**只有 max 条路径时，truncated 必须是 false——
// 否则报告会把"全部列完了"说成"还有更多"，同样是一种说谎。
func FindPaths(g *resolve.Graph, target string, max int) ([][]string, bool) {
	limit := 0
	if max > 0 {
		limit = max + 1
	}
	w := &walker{target: target, children: childrenOf(g), limit: limit}

	var roots []*resolve.Node
	for _, n := range g.Nodes {
		if n.RootDeclared {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Key < roots[j].Key })

	for _, r := range roots {
		if w.stopped {
			break
		}
		w.walk(r, nil, map[string]bool{})
	}
	if max > 0 && len(w.out) > max {
		return w.out[:max], true
	}
	return w.out, false
}

// walker 承载一次枚举的状态；stopped 让达到上限后**立刻停止遍历**——
// 上界必须同时约束"输出"和"工作量"，否则它只是把结果砍短，最坏情况照样要付。
type walker struct {
	target   string
	children map[string][]*resolve.Node
	limit    int // 0 = 无上限
	out      [][]string
	stopped  bool
}

func (w *walker) walk(n *resolve.Node, path []string, seen map[string]bool) {
	if w.stopped {
		return
	}
	path = append(path, label(n))
	if n.Key == w.target {
		w.out = append(w.out, append([]string{"ngm.json"}, path...))
		if w.limit > 0 && len(w.out) >= w.limit {
			w.stopped = true
		}
		return
	}
	if seen[n.Key] {
		return
	}
	seen[n.Key] = true
	defer delete(seen, n.Key)
	for _, c := range w.children[n.Key] {
		if w.stopped {
			return
		}
		w.walk(c, path, seen)
	}
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
	switch {
	case r.PathsTruncated:
		// 截断必须说出来。措辞刻意不写"至少 N 条路径"：N 是**列出**的条数，
		// 而它里面可能还夹着那条两元素的直接路径——说"至少"会多算。
		fmt.Fprintf(w, "传递依赖：已列出 %d 条（枚举达到上限 %d，还有更多未列出；用 --all 展开全部）\n",
			len(transitive), r.PathsLimit)
		for _, p := range transitive {
			fmt.Fprintf(w, "  %s\n", joinPath(p))
		}
	case len(transitive) == 0:
		fmt.Fprintf(w, "传递依赖：无\n")
	default:
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
