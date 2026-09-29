package resolve

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
)

// UpstreamFile 是传递依赖的唯一来源文件（dependency-resolution.md §传递性依赖）。
//
// 明确纪律：**不递归 package.json**。若上游 package.json 含 Git 依赖，
// ngm 输出提示而不纳入（registry 生态归 pnpm/npm）。
const UpstreamFile = "ngm.json"

// UpstreamPackageJSON 用于"上游把 Git 依赖写在 package.json"的提示检测。
const UpstreamPackageJSON = "package.json"

// DefaultConcurrency 是默认解析并发度（modules/p0-core.md §3：默认 4 并发）。
const DefaultConcurrency = 4

// rootMarker 是 RequiredBy 中代表"根声明直接引用"的标记。
const rootMarker = "(root)"

// DepSpec 是一条依赖声明（来自根 ngm.json 或上游 ngm.json）。
//
// resolve 包不引用 config 包（config 依赖 resolve 做校验），因此这里定义
// 图解析所需的最小字段集，由 CLI 层做类型转换。
type DepSpec struct {
	// Name 是依赖标识（slug 形式）。
	Name string
	// Ref 是 ref 名或 commit hash。
	Ref string
	// RefType 必填（ADR-004）。上游缺失时解析报错。
	RefType RefType
	// SubPath 是 monorepo 子路径（可空）。
	SubPath string
}

// Key 返回声明在图中的唯一键。
//
// 同一仓库的不同 monorepo 子路径是**不同节点**（内容与 digest 都不同）。
func (d DepSpec) Key() string { return nodeKey(d.Name, d.SubPath) }

func nodeKey(name, subPath string) string {
	if subPath == "" {
		return name
	}
	return name + "#" + subPath
}

// Node 是依赖图中的一个已解析节点。
type Node struct {
	// Key 唯一标识节点（name 或 name#subPath）。
	Key string
	// Name 是 slug 形式的依赖标识。
	Name string
	// Repo 是归一化仓库坐标。
	Repo Canonical
	// Ref / RefType 是**最终生效**的声明（可能是根声明，见 root wins）。
	Ref     string
	RefType RefType
	// SubPath 是 monorepo 子路径。
	SubPath string
	// Commit 是解析结果（不可变锚点）。
	Commit string
	// MirrorPath 是该仓库的本地裸仓库路径（供 digest 生成与 content store 落地）。
	MirrorPath string
	// RootDeclared 表示该节点由根 ngm.json 显式声明。
	RootDeclared bool
	// Depth 是距根的距离（根节点为 0）。
	Depth int
	// RequiredBy 是引入该节点的**节点 Key**（根直接引用时记为 "(root)"）。
	//
	// 用 Key 而非 Name：monorepo 子路径下 Name 会重复，来源链必须能唯一定位节点。
	// 非 monorepo 节点的 Key 就等于 Name，因此既有输出与提示不受影响。
	RequiredBy []string
	// IgnoredRefs 记录因 root wins 而未生效的传递声明（用于提示用户）。
	IgnoredRefs []string
}

// Graph 是依赖图的解析结果。
type Graph struct {
	// Nodes 按 Key 字节序排序（确定性输出）。
	Nodes []*Node
	// Warnings 是不阻断流程但需让用户知晓的信息
	// （例如上游把 Git 依赖写在 package.json 里）。
	Warnings []string
}

// Len 返回节点数。
func (g *Graph) Len() int { return len(g.Nodes) }

// Sort 按 Key 排序节点，保证输出确定性。
func (g *Graph) Sort() {
	sort.SliceStable(g.Nodes, func(i, j int) bool { return g.Nodes[i].Key < g.Nodes[j].Key })
	sort.Strings(g.Warnings)
}

// Find 按键查找节点。
func (g *Graph) Find(key string) (*Node, bool) {
	for _, n := range g.Nodes {
		if n.Key == key {
			return n, true
		}
	}
	return nil, false
}

// GraphOptions 控制依赖图解析。
type GraphOptions struct {
	// EnsureMirror 确保某仓库的 mirror 就绪并返回裸仓库路径。
	//
	// 由调用方注入（通常为 vendor.Mirror 的闭包），使 resolve 包无需了解
	// mirror 的目录布局与更新策略。
	EnsureMirror func(ctx context.Context, repo Canonical) (mirrorPath string, err error)
	// Protocol 是访问远端时使用的协议。
	Protocol Protocol
	// Secrets 是错误输出脱敏用的字面量。
	Secrets []string
	// Concurrency 是层内并发度；<=0 时使用 DefaultConcurrency。
	Concurrency int
	// CheckRepo 是**供应链策略的注入点**（见 ADR-009）。
	//
	// 由调用方注入（通常为 supplychain.Policy.CheckRepo 的方法值），
	// 使 resolve 包无需依赖 config / supplychain——注意 import 方向：
	// config 依赖 resolve 做校验，resolve 不能反过来引用它们。
	//
	// host 为裸主机名、repoPath 为 `org/repo`。nil 表示未配置策略（不门禁）。
	// 判定在**每层开头、任何远端访问之前**执行；命中即返回 exit 3 并附完整来源链。
	CheckRepo func(host, repoPath string) error
}

// frontierItem 是待解析/待展开的一项。
type frontierItem struct {
	spec  DepSpec
	depth int
	// from 是**引入该项的节点 Key**（根直接声明时为 rootMarker）。
	//
	// 存 Key 而不是 Name：monorepo 子路径下同一仓库的多个节点 Name 相同、Key 不同，
	// 用 Name 回溯来源会指错节点（见 ADR-009 与 v0.2 计划的设计复核结论）。
	from string
}

// ResolveGraph 从根声明出发做广度优先解析，产出完整依赖图。
//
// 流程（dependency-resolution.md §解析流程 2–3 步）：
//
//  1. 根声明入队（标记 RootDeclared）
//  2. 逐层解析 refType → commit，层内并发（默认 4）
//  3. 读取上游 commit 的 ngm.json，展开传递依赖
//  4. 合并与冲突裁决（同 ref 合并 / root wins / 仅传递间冲突报错）
//
// 循环引用：按 Key 去重，已访问节点不重复展开。
//
// 确定性：层内并发不改变结果——节点集合由 Key 唯一确定，错误按层内顺序
// 取第一个，最终结果排序输出。
func ResolveGraph(ctx context.Context, roots []DepSpec, opts GraphOptions) (*Graph, error) {
	if opts.EnsureMirror == nil {
		return nil, errs.New(errs.CodeConfigInvalid,
			"internal error: GraphOptions.EnsureMirror is required", "")
	}
	conc := opts.Concurrency
	if conc <= 0 {
		conc = DefaultConcurrency
	}

	// 根声明集合：root wins 的裁决依据
	rootSet := make(map[string]DepSpec, len(roots))
	for _, r := range roots {
		rootSet[r.Key()] = r
	}

	g := &Graph{}
	seen := make(map[string]*Node, len(roots))

	frontier := make([]frontierItem, 0, len(roots))
	for _, r := range roots {
		frontier = append(frontier, frontierItem{spec: r, depth: 0, from: rootMarker})
	}

	for len(frontier) > 0 {
		// 供应链策略门禁：在**任何远端访问之前**判定（ADR-009 第 1 条）。
		//
		// 放在这里而不是解析之后有两点考虑：一是解析会触网，不该为一个必然被拒的
		// 依赖去访问远端；二是拒绝信息要附来源链，而链上的父节点此刻已在 seen 里。
		if opts.CheckRepo != nil {
			if err := checkFrontierPolicy(frontier, seen, opts.CheckRepo); err != nil {
				return nil, err
			}
		}

		batch, err := mergeFrontier(frontier, rootSet)
		if err != nil {
			return nil, err
		}

		if err := resolveBatch(ctx, batch, opts, conc); err != nil {
			return nil, err
		}

		// 阶段 1：把本层节点并入图（处理重复 Key 的合并与冲突）。
		//
		// 必须**先完成整层并入**再展开：同层节点之间存在引用关系
		// （典型：根声明的 A 与被覆盖的 A@other，二者同层），
		// 若边并入边展开，先展开的节点会找不到同伴节点，
		// 从而丢失"被根声明覆盖"的记录。
		var fresh []*Node
		for _, node := range batch {
			if existing, ok := seen[node.Key]; ok {
				if err := mergeIntoExisting(existing, node); err != nil {
					return nil, err
				}
				continue
			}
			seen[node.Key] = node
			g.Nodes = append(g.Nodes, node)
			fresh = append(fresh, node)
		}

		// 阶段 2：展开传递依赖（此时 seen 已含本层全部节点）
		var next []frontierItem
		for _, node := range fresh {
			children, warns, err := readUpstreamDeps(ctx, node, opts)
			if err != nil {
				return nil, err
			}
			g.Warnings = append(g.Warnings, warns...)

			for _, child := range children {
				// root wins：根已显式声明时以根为准（不报错，但记录被忽略的声明）
				if rootDecl, ok := rootSet[child.Key()]; ok {
					if rootDecl.Ref != child.Ref || rootDecl.RefType != child.RefType {
						// 记录到**被覆盖的节点**（根声明节点，已在阶段 1 并入）
						if overridden, ok := seen[child.Key()]; ok {
							overridden.IgnoredRefs = appendUnique(overridden.IgnoredRefs,
								fmt.Sprintf("%s@%s (%s) required by %s was overridden by the root declaration",
									child.Name, child.Ref, child.RefType, node.Name))
						}
					}
					child = rootDecl
				}
				next = append(next, frontierItem{spec: child, depth: node.Depth + 1, from: node.Key})
			}
		}
		frontier = next
	}

	g.Sort()
	return g, nil
}

// mergeFrontier 把同一层的声明按 Key 合并为待解析节点。
//
// 同层出现同 Key 意味着**多个上游声明了同一依赖**（菱形依赖）。此时：
//
//	ref 相同        → 合并来源，只解析一次
//	ref 不同 + 根声明存在 → root wins（以根为准）
//	ref 不同 + 根未声明   → **传递间冲突**，报错 exit 3
//
// 冲突必须在这里检出：若在此静默合并，解析阶段就只剩一个 ref，
// 冲突将永远无法被发现。
func mergeFrontier(items []frontierItem, rootSet map[string]DepSpec) ([]*Node, error) {
	byKey := make(map[string]*Node, len(items))
	order := make([]string, 0, len(items))

	for _, it := range items {
		k := it.spec.Key()
		n, ok := byKey[k]
		if !ok {
			byKey[k] = &Node{
				Key:          k,
				Name:         it.spec.Name,
				Ref:          it.spec.Ref,
				RefType:      it.spec.RefType,
				SubPath:      it.spec.SubPath,
				Depth:        it.depth,
				RootDeclared: it.depth == 0,
			}
			order = append(order, k)
			byKey[k].RequiredBy = appendUnique(nil, it.from)
			continue
		}

		if n.Ref != it.spec.Ref || n.RefType != it.spec.RefType {
			if rootDecl, ok := rootSet[k]; ok {
				// 根声明存在 → 以根为准（展开阶段通常已统一，这里是防御）
				n.Ref = rootDecl.Ref
				n.RefType = rootDecl.RefType
			} else {
				return nil, transitiveConflictError(k, n, it)
			}
		}
		if it.depth < n.Depth {
			n.Depth = it.depth
		}
		n.RequiredBy = appendUnique(n.RequiredBy, it.from)
	}

	out := make([]*Node, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out, nil
}

// transitiveConflictError 构造"仅传递间冲突"的错误（exit 3）。
//
// 消息必须给出**来源链**（哪个上游要求了哪个版本），否则用户无从下手。
func transitiveConflictError(key string, seen *Node, incoming frontierItem) error {
	return errs.New(
		errs.CodeConfigInvalid,
		fmt.Sprintf("conflicting transitive requirements for %s:\n  - %s (%s) required by %s\n  - %s (%s) required by %s",
			key,
			seen.Ref, seen.RefType, strings.Join(seen.RequiredBy, ", "),
			incoming.spec.Ref, incoming.spec.RefType, incoming.from),
		fmt.Sprintf("declare %s explicitly in the root ngm.json to resolve the conflict "+
			"(the root declaration wins)", key))
}

// resolveBatch 并发解析一批节点（refType → commit + mirror 就绪）。
//
// 错误确定性的实现：把每个节点的错误写入**按索引对齐**的切片（各 goroutine
// 写互不相交的位置，无数据竞争），最后按批次顺序取第一个错误。
func resolveBatch(ctx context.Context, batch []*Node, opts GraphOptions, conc int) error {
	if len(batch) == 0 {
		return nil
	}
	if conc > len(batch) {
		conc = len(batch)
	}

	errsByIndex := make([]error, len(batch))
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup

	for i, n := range batch {
		wg.Add(1)
		go func(i int, n *Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			errsByIndex[i] = resolveOne(ctx, n, opts)
		}(i, n)
	}
	wg.Wait()

	for _, err := range errsByIndex {
		if err != nil {
			return err
		}
	}
	return nil
}

// resolveOne 解析单个节点。
func resolveOne(ctx context.Context, n *Node, opts GraphOptions) error {
	repo, err := ParseSlug(n.Name)
	if err != nil {
		return err
	}
	n.Repo = repo

	mirrorPath, err := opts.EnsureMirror(ctx, repo)
	if err != nil {
		return err
	}
	n.MirrorPath = mirrorPath

	commit, err := ResolveRef(ctx, repo, n.Ref, n.RefType, ResolveOptions{
		// 直接用已就绪的 mirror 路径：EnsureMirror 已保证它可用，
		// 因此解析完全本地（离线可用）
		GitURL:   mirrorPath,
		Protocol: opts.Protocol,
		Secrets:  opts.Secrets,
	})
	if err != nil {
		return err
	}
	n.Commit = commit
	return nil
}

// mergeIntoExisting 把 incoming 合并进已有节点，或按规则报冲突。
//
// 规则（dependency-resolution.md §冲突检测）：
//
//	同 refType 同 ref      → 合并（累加来源）
//	已有的是根声明         → root wins（忽略传递声明，记录 IgnoredRefs）
//	传入的是根声明         → 覆盖（理论上根已在首层处理，这里是保险）
//	仅传递之间冲突         → 报错 exit 3，提示"在根 ngm.json 显式声明以覆盖"
func mergeIntoExisting(existing, incoming *Node) error {
	if existing.Ref == incoming.Ref && existing.RefType == incoming.RefType {
		existing.RequiredBy = appendUnique(existing.RequiredBy, incoming.RequiredBy...)
		if incoming.Depth < existing.Depth {
			existing.Depth = incoming.Depth
		}
		return nil
	}

	if existing.RootDeclared {
		existing.RequiredBy = appendUnique(existing.RequiredBy, incoming.RequiredBy...)
		existing.IgnoredRefs = appendUnique(existing.IgnoredRefs,
			fmt.Sprintf("%s@%s (%s) required by %s was overridden by the root declaration %s@%s (%s)",
				incoming.Name, incoming.Ref, incoming.RefType, strings.Join(incoming.RequiredBy, ", "),
				existing.Name, existing.Ref, existing.RefType))
		return nil
	}

	if incoming.RootDeclared {
		existing.Ref = incoming.Ref
		existing.RefType = incoming.RefType
		existing.Commit = incoming.Commit
		existing.MirrorPath = incoming.MirrorPath
		existing.Repo = incoming.Repo
		existing.RootDeclared = true
		existing.Depth = 0
		existing.RequiredBy = appendUnique(existing.RequiredBy, rootMarker)
		return nil
	}

	// 仅传递之间冲突：报错（根未声明）
	return errs.New(
		errs.CodeConfigInvalid,
		fmt.Sprintf("conflicting transitive requirements for %s:\n  - %s (%s) required by %s\n  - %s (%s) required by %s",
			existing.Key,
			existing.Ref, existing.RefType, strings.Join(existing.RequiredBy, ", "),
			incoming.Ref, incoming.RefType, strings.Join(incoming.RequiredBy, ", ")),
		fmt.Sprintf("declare %s explicitly in the root ngm.json to resolve the conflict (the root declaration wins)",
			existing.Key))
}

// readUpstreamDeps 读取上游 commit 的 ngm.json 并展开传递依赖。
//
// 同时返回提示（如上游把 Git 依赖写在 package.json 里）。
func readUpstreamDeps(ctx context.Context, node *Node, opts GraphOptions) ([]DepSpec, []string, error) {
	gitOpts := git.Options{Secrets: opts.Secrets}

	content, exists, err := git.ReadFileAtCommit(ctx, gitOpts, node.MirrorPath, node.Commit, UpstreamFile)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		// 「唯一来源」：缺失即视为无传递依赖；但若 package.json 里有 Git 依赖，提示用户
		warns, werr := checkUpstreamPackageJSON(ctx, node, gitOpts)
		return nil, warns, werr
	}

	var f upstreamJSONFile
	// 不启用 DisallowUnknownFields：上游可能使用更新版本的字段，
	// 应忽略而非报错（与 lock 的向前兼容策略一致）
	if err := json.Unmarshal(content, &f); err != nil {
		return nil, nil, errs.Wrap(errs.CodeConfigInvalid,
			fmt.Sprintf("%s: invalid %s at commit %s", node.Name, UpstreamFile, git.ShortSHA(node.Commit)),
			"the upstream repository has a malformed ngm.json", err)
	}

	out := make([]DepSpec, 0, len(f.Dependencies))
	for i, d := range f.Dependencies {
		if strings.TrimSpace(d.RefType) == "" {
			return nil, nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("%s: dependencies[%d] (%s) is missing `refType`",
					node.Name, i, d.Name),
				"refType is mandatory in ngm.json; ask the maintainers of "+node.Name+
					" to declare it explicitly (a ref alone cannot distinguish tag / branch / commit)")
		}
		rt := RefType(d.RefType)
		if !rt.IsValid() {
			return nil, nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("%s: dependencies[%d] (%s) has invalid refType %q",
					node.Name, i, d.Name, d.RefType),
				fmt.Sprintf("must be one of %v", ValidRefTypes()))
		}
		if strings.TrimSpace(d.Name) == "" {
			return nil, nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("%s: dependencies[%d] is missing `name`", node.Name, i), "")
		}
		if strings.TrimSpace(d.Ref) == "" {
			return nil, nil, errs.New(
				errs.CodeConfigInvalid,
				fmt.Sprintf("%s: dependencies[%d] (%s) is missing `ref`", node.Name, i, d.Name), "")
		}
		out = append(out, DepSpec{Name: d.Name, Ref: d.Ref, RefType: rt, SubPath: d.Path})
	}
	return out, nil, nil
}

// checkUpstreamPackageJSON 检测上游把 Git 依赖写在 package.json 的情况并给出提示。
//
// 行为：**不纳入**这些依赖（registry 生态归 pnpm/npm），只输出警告——
// dependency-resolution.md §传递性依赖明确要求"输出提示，建议上游迁移到 ngm.json"。
func checkUpstreamPackageJSON(ctx context.Context, node *Node, gitOpts git.Options) ([]string, error) {
	content, exists, err := git.ReadFileAtCommit(ctx, gitOpts, node.MirrorPath, node.Commit, UpstreamPackageJSON)
	if err != nil || !exists {
		return nil, err
	}
	var pj upstreamPackageJSON
	if json.Unmarshal(content, &pj) != nil {
		return nil, nil // package.json 损坏不影响 ngm 的判断
	}

	var found []string
	for name, spec := range pj.Dependencies {
		if looksLikeGitSpec(spec) {
			found = append(found, name)
		}
	}
	for name, spec := range pj.DevDependencies {
		if looksLikeGitSpec(spec) {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Strings(found)
	return []string{fmt.Sprintf(
		"%s: package.json declares Git dependencies (%s) but ngm reads only ngm.json — they were NOT installed; "+
			"ask the maintainers to migrate them to ngm.json",
		node.Name, strings.Join(found, ", "))}, nil
}

// upstreamJSONFile 是上游 ngm.json 中 ngm 关心的最小字段集。
//
// 只声明必要字段：resolve 包不能依赖 config（config 依赖 resolve），
// 而传递依赖展开只需要 name / ref / refType / path。
type upstreamJSONFile struct {
	Dependencies []struct {
		Name    string `json:"name"`
		Ref     string `json:"ref"`
		RefType string `json:"refType"`
		Path    string `json:"path"`
	} `json:"dependencies"`
}

// upstreamPackageJSON 用于检测"上游把 Git 依赖写在 package.json"。
type upstreamPackageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// looksLikeGitSpec 判断 npm 依赖声明是否指向 Git 仓库。
func looksLikeGitSpec(v string) bool {
	v = strings.TrimSpace(v)
	for _, prefix := range []string{
		"git+", "git://", "github:", "gitlab:", "bitbucket:",
		"https://github.com/", "https://gitlab.com/", "https://gitee.com/",
	} {
		if strings.HasPrefix(v, prefix) {
			return true
		}
	}
	return false
}

// appendUnique 追加不重复的字符串（保持首次出现顺序）。
func appendUnique(dst []string, items ...string) []string {
	for _, it := range items {
		if it == "" {
			continue
		}
		found := false
		for _, d := range dst {
			if d == it {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, it)
		}
	}
	return dst
}
