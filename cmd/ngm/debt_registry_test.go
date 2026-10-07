package main

import (
	"strings"
	"testing"
)

// v0.52：**把"刻意不修的"与"挂着的"变成具名的、可数的**。
//
// 这一系列反复用过的招（负例名单 v0.15 · 缺口名单 v0.22 · 退 0 登记 v0.37 ·
// JSON 不适用登记 v0.50）：**契约看不见的地方必须具名**——
// 否则"我知道它在那儿"与"我漏了它"长得一模一样。
//
// 候选表里的条目把这件事的代价演了一遍：v0.47~v0.50 的候选表里挂着同一条
// "归一化清单"，四版没做；真做的时候成本是一个 25 行的助手 + 四行迁移（v0.51）。
//
//	**没有证据与没有缺陷，在候选表里长得一样。**
//
// 所以这张表要求每条写清两件事：**代价**（做掉它要花什么）与**从哪一版开始挂**。
// 判据：
//
//	① `cost` 与 `since` 必须非空——**空说明它还没被想清楚**；
//	② `kind` 只能是 `debtKinds` 里的那几种（其中"已暂缓"是**人的决定**，不是判断）；
//	③ 总数是**棘轮**：只许变少或持平；要变多，先改这个数并想清楚为什么。
//
// 这张表是**给人看的**（它记的是判断），但它由机器守住格式与规模——
// 与 v0.38 的 `exitZeroByDesign`、v0.50 的 `jsonNotApplicable` 同一套路。
type debt struct {
	what  string // 一句话说清是什么
	kind  string // 见 debtKinds
	cost  string // 做掉它要花什么（"不需要做"也是一种答案，但要写清为什么）
	since string // 从哪一版开始挂着
}

var debtKinds = map[string]string{
	"故意":   "**已经决定不做**，并写清了理由（不是忘了）",
	"未做":   "**值得做、也做得动**，只是还没做——写出代价是为了让人判断值不值",
	"未立纪律": "**还只是一句口号**，没有机械判据钉住它",
	"已暂缓":  "**由人决定先不做**（不是我们判断的）",
}

// debtCeiling 是这张表的**棘轮**（当前条数；只许变少或持平）。
//
// v0.52: 8 → v0.53: **6**（两条"未做"做掉了：机器侧的逐条计数断言、
// "覆盖集从哪来"的一次普查）。**棘轮往下走才说明它在工作**。
const debtCeiling = 5

var debts = []debt{
	{
		what: "`internal/config/duration.go` 的 10 处错误不带建议",
		kind: "故意",
		cost: "不需要做：字段层补建议更贴切（一处代替十处），而且它知道该填在哪个键里。" +
			"这条登记由 TestV49… 的允许数钉住（在源码里长得很像'漏了'）",
		since: "v0.49",
	},
	{
		what: "internal 里 118 处空 hint",
		kind: "故意",
		cost: "不需要做：v0.28 的继承修好之后，链上任何一层有建议就够；用户层已是硬门禁 0。" +
			"运行时普查（v0.30）给出的抽样答案是 15/15 都有",
		since: "v0.30",
	},
	// v0.54 记下的那条「根级位置的 flag 被静默丢弃」已在 v0.55 做掉：
	// 根级只认 --help/-h/--version，其余一律拒绝（并带上 v0.54 的信封）。
	{
		what:  "`tree` 的**去重展开**（同一节点被多条路径各展开一次）",
		kind:  "未做",
		cost:  "大：形状会变，**需先立 ADR**（v0.38 起就写着这句）",
		since: "v0.38",
	},
	{
		what:  "补发 4 个 GitHub release（`v0.5.0`~`v0.8.0`）与 48 个 Gitee 发行版",
		kind:  "已暂缓",
		cost:  "由人决定先不做（候选表里多次标注 ⏸️）——不是我们判断的，所以列在这里而不是删掉",
		since: "v0.5",
	},
	{
		what:  "正常网络下的**联网复核**（真实远端跑一遍端到端）",
		kind:  "未做",
		cost:  "需要一台网络正常的机器：本机策略把 net 拒绝了，所以这一路只能靠替身与 stub",
		since: "v0.8",
	},
}

func TestV52TheDeferredListIsNamedAndCounted(t *testing.T) {
	if len(debts) > debtCeiling {
		t.Fatalf("这张表有 %d 条，而棘轮是 %d——变多之前先想清楚：\n"+
			"一个'未做'被记下来的时候，最该写的是**代价**；写不出代价的那条，多半还没被想清楚",
			len(debts), debtCeiling)
	}

	byKind := map[string]int{}
	for i, d := range debts {
		if strings.TrimSpace(d.what) == "" {
			t.Errorf("第 %d 条没有 what", i+1)
		}
		if strings.TrimSpace(d.cost) == "" {
			t.Errorf("第 %d 条（%s）没有写**代价**——没有代价的登记，与'我忘了它'没有区别", i+1, d.what)
		}
		if strings.TrimSpace(d.since) == "" {
			t.Errorf("第 %d 条（%s）没写**从哪一版起挂着**——挂了几版正是这条表要回答的问题", i+1, d.what)
		}
		if _, ok := debtKinds[d.kind]; !ok {
			t.Errorf("第 %d 条（%s）的 kind 是 %q，不在登记的种类里（%v）", i+1, d.what, d.kind, debtKindNames())
		}
		byKind[d.kind]++
	}

	t.Logf("deferred registry: %d/%d entries — %v（'故意'是决定，'未做/未立纪律'是欠账，'已暂缓'是人的决定）",
		len(debts), debtCeiling, byKind)
}

func debtKindNames() []string {
	out := make([]string, 0, len(debtKinds))
	for k := range debtKinds {
		out = append(out, k)
	}
	return out
}
