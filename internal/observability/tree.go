package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/supplychain"
)

// TreeEntry 是依赖树中的一个节点。
type TreeEntry struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	RefType string `json:"refType"`
	Commit  string `json:"commit"`
	SubPath string `json:"subPath,omitempty"`
	Depth   int    `json:"depth"`

	// Drift 为真表示 ref 已不再指向 lock 中锁定的 commit。
	//
	// 判定口径与 verify 一致：拿 lock 里的 commit 与**当前解析结果**比。
	// 没有 lock 可比对时一律为 false——"不知道"不能被渲染成"没有漂移"。
	Drift        bool   `json:"drift"`
	LockedCommit string `json:"lockedCommit,omitempty"`

	// Cycle 表示此处存在一个环且已停止展开（不是无限递归）。
	Cycle bool `json:"cycle,omitempty"`

	// Truncated 表示此处因**达到展开预算**而停止，且至少还有一个子节点没列出。
	//
	// 它只在"确有未列出的子节点"时置位——预算恰好用尽在叶子上不算截断，
	// 否则标记会把"看完了"说成"没看完"。
	Truncated bool `json:"truncated,omitempty"`

	// Vulns 只有显式查询 OSV 时才会填充（ngm tree --osv）。
	// 为 nil 表示"未查询"，语义是**未知**，不是"没有漏洞"。
	Vulns    []supplychain.Vuln `json:"vulnerabilities,omitempty"`
	Children []*TreeEntry       `json:"children,omitempty"`
}

// TreeReport 是 `ngm tree` 的报告。
type TreeReport struct {
	Project string `json:"project"`
	// Dependencies 是图里的**节点总数**——它来自解析结果，不受展开预算影响。
	//
	// 注意它与 Entries 的区别：Entries 是展开后的**条目**（同一个节点会被
	// 多条路径各算一次），可能被截断；Dependencies 永远是完整的。
	Dependencies int          `json:"dependencies"`
	Drifted      int          `json:"drifted"`
	OSVChecked   bool         `json:"osvChecked"`
	Entries      []*TreeEntry `json:"entries"`

	// EntriesTruncated 为真表示 Entries **不完整**（达到展开预算）。
	// 与 why 的同名字段一样，它存在的唯一理由是**截断必须可见**。
	EntriesTruncated bool `json:"entriesTruncated,omitempty"`

	// EntriesLimit 是本次展开的预算；0 表示未设上限（`--all`）。
	EntriesLimit int `json:"entriesLimit,omitempty"`
}

// MaxTreeEntries 是 `ngm tree` **默认**展开的条目数上限（`--all` 解除）。
//
// 为什么必须有上界：树是**按路径展开**的（同一个节点在多条路径下各出现一次），
// 因此它的规模随图"宽"指数增长——与 why 的路径数是同一个数（见 MaxWhyPaths 的实测表：
// 41 个节点 → 一百万条路径）。树还会把每条路径**渲染出来**，代价只会更大。
//
// 4096 条目相当于数百 KB 的输出，已远大于本项目见过的任何真实依赖图，
// 同时把最坏情况钉住。达到预算时必须在输出里说出来（EntriesTruncated /
// 条目上的 Truncated），绝不静默截断。决定与重开条件见 ADR-024。
const MaxTreeEntries = 4096

// treeBudget 是展开预算：它**同时约束输出与工作量**——
// 预算用尽即停止展开，而不是先把整棵树建出来再砍短。
type treeBudget struct {
	left      int // <0 表示不限
	truncated bool
}

// take 取一个条目额度。返回 false 表示预算已尽（并记下截断）。
func (b *treeBudget) take() bool {
	if b.left < 0 {
		return true
	}
	if b.left == 0 {
		b.truncated = true
		return false
	}
	b.left--
	return true
}

// BuildTree 从已解析的依赖图构造树。
//
// 为什么必须重建图而不是读 lock：lock 里没有拓扑（它是扁平列表），
// 而"谁引入了谁"只在解析过程中存在。因此 tree / why 都走 ResolveGraph，
// 读取的是 mirror 里各依赖的 ngm.json——mirror 已就绪时**不需要网络**。
//
// 环的处理：依赖图原则上无环，但上游写坏 manifest 时可能出现。
// 这里沿路径记录已访问节点并停止展开（标记 Cycle），绝不无限递归。
func BuildTree(g *resolve.Graph, lf *lock.File, max int) ([]*TreeEntry, bool) {
	if g == nil {
		return nil, false
	}
	byKey := make(map[string]*resolve.Node, len(g.Nodes))
	children := make(map[string][]*resolve.Node)
	for _, n := range g.Nodes {
		byKey[n.Key] = n
	}
	// 边来自 RequiredBy（组 A 起存的是节点 Key，monorepo 下也不会指错）
	for _, n := range g.Nodes {
		for _, parent := range n.RequiredBy {
			if parent == resolve.RootMarker {
				continue
			}
			if _, ok := byKey[parent]; !ok {
				continue
			}
			children[parent] = append(children[parent], n)
		}
	}
	for k := range children {
		sort.SliceStable(children[k], func(i, j int) bool { return children[k][i].Key < children[k][j].Key })
	}

	budget := &treeBudget{left: max}
	if max <= 0 {
		budget.left = -1 // 不限
	}
	var out []*TreeEntry
	for _, n := range g.Nodes {
		if !n.RootDeclared {
			continue
		}
		if !budget.take() {
			break
		}
		out = append(out, buildEntry(n, children, lf, map[string]bool{n.Key: true}, 0, budget))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, budget.truncated
}

func buildEntry(n *resolve.Node, children map[string][]*resolve.Node, lf *lock.File, path map[string]bool, depth int, budget *treeBudget) *TreeEntry {
	e := &TreeEntry{
		Name:    n.Name,
		Ref:     n.Ref,
		RefType: string(n.RefType),
		Commit:  n.Commit,
		SubPath: n.SubPath,
		Depth:   depth,
	}
	if lf != nil {
		if d, ok := lf.Find(n.Name, n.SubPath); ok && d.Commit != "" {
			e.LockedCommit = d.Commit
			e.Drift = d.Commit != n.Commit
		}
	}
	for _, c := range children[n.Key] {
		// 预算在**加子节点之前**取：取不到就把"这里还没展开完"标出来。
		// 标记因此只在确有子节点没列出时出现（没有子节点时循环根本不会进）。
		if !budget.take() {
			e.Truncated = true
			return e
		}
		if path[c.Key] {
			cycle := &TreeEntry{Name: c.Name, Ref: c.Ref, RefType: string(c.RefType), Commit: c.Commit, Depth: depth + 1, Cycle: true}
			e.Children = append(e.Children, cycle)
			continue
		}
		next := map[string]bool{c.Key: true}
		for k := range path {
			next[k] = true
		}
		e.Children = append(e.Children, buildEntry(c, children, lf, next, depth+1, budget))
	}
	return e
}

// CountDrifted 统计漂移节点数（含嵌套）。
func CountDrifted(entries []*TreeEntry) int {
	n := 0
	for _, e := range entries {
		if e.Drift {
			n++
		}
		n += CountDrifted(e.Children)
	}
	return n
}

// MarkOSV 为树中每个节点查询已知漏洞。
//
// 刻意做成**显式调用**：让"打印一棵树"静默发起网络查询，与本项目
// "命令是否触网必须可预期"的纪律冲突。因此只有 --osv 才会走到这里。
// 未命中缓存且离线时返回错误（exit 4），不会降级成"没有漏洞"。
func MarkOSV(ctx context.Context, entries []*TreeEntry, cfg supplychain.OSVConfig, ignore []string) error {
	for _, e := range entries {
		if !e.Cycle && e.Commit != "" {
			vulns, err := supplychain.QueryOSV(ctx, cfg, e.Commit)
			if err != nil {
				return err
			}
			kept, _ := supplychain.IgnoreSeverities(vulns, ignore)
			if len(kept) > 0 {
				e.Vulns = kept
			}
		}
		if err := MarkOSV(ctx, e.Children, cfg, ignore); err != nil {
			return err
		}
	}
	return nil
}

// Marshal 输出机器可读报告。
func (r *TreeReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Render 渲染人类可读的依赖树（结构对齐 observability.md §ngm tree）。
//
// 未查询 OSV 时不画 ✗，也不写"无漏洞"——那会把"没查"读成"查过没事"。
func (r *TreeReport) Render(w io.Writer) {
	fmt.Fprintf(w, "%s\n", r.Project)
	for _, e := range r.Entries {
		renderEntry(w, e, "")
	}
	if r.EntriesTruncated {
		// 截断必须说出来：否则"树到这儿就是全部"会被读成事实。
		fmt.Fprintf(w, "\n（展开达到上限 %d，树不完整；用 --all 展开全部）\n", r.EntriesLimit)
	}
	if r.Drifted > 0 {
		fmt.Fprintf(w, "\n%d dependency ref(s) moved away from the locked commit (⚠)\n", r.Drifted)
	}
	if !r.OSVChecked {
		fmt.Fprintf(w, "vulnerability data not consulted; run `ngm audit` (or `ngm tree --osv`)\n")
	}
}

func renderEntry(w io.Writer, e *TreeEntry, prefix string) {
	label := e.Name
	if e.SubPath != "" {
		label += "#" + e.SubPath
	}
	line := fmt.Sprintf("%s@%s (%s) → %s", label, e.Ref, e.RefType, git.ShortSHA(e.Commit))
	switch {
	case e.Cycle:
		line += " ↺ cycle (not expanded)"
	case e.Truncated:
		line += " … 此处达到展开上限（还有子节点未列出）"
	case e.Drift:
		line += fmt.Sprintf(" ⚠ 漂移 (locked %s)", git.ShortSHA(e.LockedCommit))
	}
	if len(e.Vulns) > 0 {
		line += fmt.Sprintf(" ✗ %d known vuln(s)", len(e.Vulns))
	}
	fmt.Fprintf(w, "%s%s\n", prefix, line)
	for _, c := range e.Children {
		renderEntry(w, c, prefix+"│   ")
	}
}
