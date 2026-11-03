package main

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// v0.63：**逐版表必须每个版本恰好一行、而且在正确的顺序上**。
//
// 起点是一处**已经发生、且没有任何判据看见的损坏**（v0.63 量到）：
// 我每版往逐版表里"插一行"的手法，在 v0.55~v0.62 那一段把这四张活表弄坏了——
//
//	docs/README.md              倒序插进四行 · v0.58~v0.61 整段消失（散文并进邻居）· 两行重复
//	README.md（英文）            同病（v0.59~v0.61 的散文并进 v0.62 那行）
//	docs/development/README.md  v0.49~v0.62 那一段变成降序
//	docs/internals/project-state.md  v0.58~v0.61 插错位置 · 缺 v0.62/v0.63
//
// 而 v0.58 那条"发布读数"判据**一直是绿的**：它查数字（`N of M`、缺口、成功次数），
// 不查**行的结构与顺序**。这与 v0.62 那处"钩子是死的"是同一族缺陷：
// **登记的动作发生了，登记的结果没人核对。**
//
// 判据的覆盖集是**派生的**：版本集来自 `docs/development/` 的复盘文件名
// （与 v0.58 同源：复盘数 == 已交付版本数），而"哪些文档算逐版表"由**扫描活文档**发现
// ——任何一份活文档，只要它的版本行里够得到**最新版本**，就必须在
// `milestoneTableStartsAt` 里声明起点（没声明即红，新表不会漏网）。
//
// 起点为什么要声明：表不是从 v0.1 起时，"少了开头几行"与"本来就从那里起"
// 从外面看不出区别；把起点写下来，缺口才是可查的。三张表的起点都是**量出来的现状**。
//
// 2026-11-03：根 README.md 重写为面向使用者的说明，不再承载逐版表；
// 逐版账本保留在三份开发文档中（本判据的覆盖集随之从四张变为三张）。
var reMilestoneRow = regexp.MustCompile(`^>? ?\| (?:> )?\*{0,2}(v0\.\d+)\*{0,2} `)

// milestoneLedger：一本逐版账本 —— 起点 + 构成它的文件（按卷顺序）。
//
// 2026-11-03：账本**允许分卷**。单卷 80–130KB 的账本没人会在评审里打开，
// 而账本的价值全在"有人读"。分卷后判据跨卷合并，仍然要求每版恰好一行、
// 升序、连续到最新——分卷只改放置方式，不改核对口径。
type milestoneLedger struct {
	start string
	files []string
}

var milestoneLedgers = []milestoneLedger{
	{"v0.1", []string{"../../docs/milestones.md", "../../docs/milestones-b.md"}},
	{"v0.5", []string{"../../docs/development/ledger.md", "../../docs/development/ledger-b.md"}},
	{"v0.56", []string{"../../docs/internals/progress.md", "../../docs/internals/progress-b.md", "../../docs/internals/progress-c.md"}},
}

// parseVersion 把 `v0.57` 读成 57（只支持这条判据用到的形状）。
func parseVersion(s string) int {
	n := 0
	for _, r := range strings.TrimPrefix(s, "v0.") {
		n = n*10 + int(r-'0')
	}
	return n
}

func TestV63MilestoneTablesListEveryVersionExactlyOnce(t *testing.T) {
	total, latest := deliveredVersions(t) // 与 v0.58 同源
	if total < 10 {
		t.Fatalf("派生的版本数只有 %d——这条判据的范围缩了", total)
	}
	latestN := parseVersion(latest)
	if latestN != total {
		t.Fatalf("最新版本 %s 与版本数 %d 对不上——事实源自己不可信", latest, total)
	}

	declared := 0
	registered := map[string]bool{}

	for _, led := range milestoneLedgers {
		start := parseVersion(led.start)
		var rows []int
		label := ""

		for _, f := range led.files {
			display := strings.TrimPrefix(strings.ReplaceAll(f, "\\", "/"), "../../")
			registered[display] = true
			body, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("%s：%v（账本分卷少了文件 = 账本被截断）", display, err)
			}
			if label == "" {
				label = display
			}
			for _, line := range strings.Split(string(body), "\n") {
				if m := reMilestoneRow.FindStringSubmatch(line); m != nil {
					rows = append(rows, parseVersion(m[1]))
				}
			}
		}

		if len(rows) == 0 {
			t.Errorf("%s：声明了起点 %s，却没有任何版本行——登记指向空气", label, led.start)
			continue
		}
		declared++

		max := 0
		for _, n := range rows {
			if n > max {
				max = n
			}
		}
		if max != latestN {
			t.Errorf("%s：账本写到 v0.%d 就停了，而最新版本是 %s——"+
				"逐版账本要一路写到最新（少了行 = 登记的动作发生了、结果却没落地）",
				label, max, latest)
		}

		// 起点之后的每一行：恰好一次、升序、连续到最新（跨卷合并后核对）
		seen := map[int]bool{}
		var ordered []int
		for _, n := range rows {
			if n < start {
				continue // 起点之前的行不归这本账管（roadmap 之类）
			}
			if seen[n] {
				t.Errorf("%s: v0.%d 出现了两次——逐版账本里每个版本只该有一行（分卷也不能重叠）", label, n)
			}
			seen[n] = true
			ordered = append(ordered, n)
		}
		for i := 1; i < len(ordered); i++ {
			if ordered[i] <= ordered[i-1] {
				t.Errorf("%s: v0.%d 出现在 v0.%d 之后——逐版账本要按版本升序"+
					"（分卷按卷顺序拼接；v0.63 修过一次：四行是倒着插进去的）",
					label, ordered[i], ordered[i-1])
			}
		}
		var missing []string
		for n := start; n <= latestN; n++ {
			if !seen[n] {
				missing = append(missing, "v0."+strconv.Itoa(n))
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s：缺 %d 行（%s）——从 %s 起要一路写到 %s",
				label, len(missing), strings.Join(missing, " "), led.start, latest)
		}
	}

	// 未登记检测：任何活文档只要够得到最新版本，就必须是某本账本的卷（新表不会漏网）。
	for _, doc := range livingDocs(t) {
		display := strings.TrimPrefix(strings.ReplaceAll(doc, "\\", "/"), "../../")
		if registered[display] {
			continue
		}
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		max := 0
		for _, line := range strings.Split(string(body), "\n") {
			if m := reMilestoneRow.FindStringSubmatch(line); m != nil {
				if n := parseVersion(m[1]); n > max {
					max = n
				}
			}
		}
		if max >= latestN {
			t.Errorf("%s 的版本行够得到最新版本（v0.%d），却不是任何一本账本的卷——"+
				"要么把它登记进 milestoneLedgers，要么删掉它（它现在是没人核对的历史）", display, max)
		}
	}

	if declared < 3 {
		t.Fatalf("只核对到 %d 本账本——这条判据的范围缩了", declared)
	}
	t.Logf("milestone ledgers: %d book(s) · %d version(s) · latest %s", declared, total, latest)
}
