package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// 本文件是供应链策略在解析阶段的**门禁**（见 ADR-009）。
//
// 它只做两件事：对本层每个待解析节点调用注入的判定函数，以及在命中时附上来源链。
// 策略本身（字段语义、host / repo 匹配）在 internal/supplychain——
// resolve 不依赖它，只依赖一个函数值（import 方向：config 依赖 resolve）。

// checkFrontierPolicy 对本层每个待解析节点执行策略判定。
//
// 遍历顺序即 frontier 顺序，因此"哪个节点先被发现违规"是确定的——
// 层内并发不会改变报错结果（与 ResolveGraph 的确定性约定一致）。
func checkFrontierPolicy(items []frontierItem, seen map[string]*Node, check func(host, repoPath string) error) error {
	for i, it := range items {
		repo, err := ParseSlug(it.spec.Name)
		if err != nil {
			// name 非法属于配置错误，与策略无关：原样返回（exit 3）
			return err
		}
		if err := check(repo.Host, repo.Path); err != nil {
			return withProvenance(err, provenanceChain(seen, it, sameKeyItems(items, i)))
		}
	}
	return nil
}

// sameKeyItems 统计本层中与被检查项**同 Key** 的其他项数量。
//
// 同 Key 出现多次意味着菱形依赖：同一个依赖由多个上游引入。此时来源链只能显示
// 其中一条路径，必须说明还有别的——否则使用者会以为修好一条就完事了。
func sameKeyItems(items []frontierItem, self int) int {
	k := items[self].spec.Key()
	n := 0
	for i, it := range items {
		if i != self && it.spec.Key() == k {
			n++
		}
	}
	return n
}

// provenanceChain 构造 `(root) → … → violator` 的来源链。
//
// 为什么必须有它：只说"某依赖不被允许"，使用者无法处置——必须回答"是谁把它引进来的"。
// 这也正是 RequiredBy 存**节点 Key** 而不是 Name 的原因（monorepo 子路径下 Name 会重复）。
//
// extraPaths 是同层中引入同一节点的其他上游数量（菱形依赖）。
func provenanceChain(seen map[string]*Node, it frontierItem, extraPaths int) string {
	var path []string
	cur := it.from
	// 上限用于防御环：正常图按 Key 去重不会成环，但这里不该依赖上游的正确性
	for i := 0; i < 256 && cur != "" && cur != rootMarker; i++ {
		path = append(path, cur)
		n, ok := seen[cur]
		if !ok || len(n.RequiredBy) == 0 {
			break
		}
		cur = n.RequiredBy[0]
	}
	// 反转为 root → … 的顺序
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	out := append([]string{rootMarker}, path...)
	out = append(out, it.spec.Key())
	chain := strings.Join(out, " → ")

	if extraPaths > 0 {
		chain += fmt.Sprintf("   (+%d more path(s) lead here)", extraPaths)
	}
	return chain
}

// withProvenance 把来源链附到策略错误上，**保留原有错误码与 Hint**。
//
// 刻意不重新表述策略层的语义：那边给的 Hint 是可操作的（该往 allowedGitHosts /
// allowlistRepos 里加什么），这里只补"从哪儿来的"。
func withProvenance(err error, chain string) error {
	if ne, ok := err.(*errs.NgmError); ok {
		return errs.New(ne.Code,
			fmt.Sprintf("%s\n  via: %s", ne.Message, chain),
			ne.Hint)
	}
	return errs.Wrap(errs.CodeConfigInvalid,
		fmt.Sprintf("supply chain policy rejected a dependency (via %s)", chain), "", err)
}
