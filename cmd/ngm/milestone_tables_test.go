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
// 从外面看不出区别；把起点写下来，缺口才是可查的。四张表的起点都是**量出来的现状**。
var reMilestoneRow = regexp.MustCompile(`^>? ?\| (?:> )?\*{0,2}(v0\.\d+)\*{0,2} `)

// milestoneTableStartsAt：活文档 → 它的逐版表起点（仓库相对路径为键）。
//
//   - `docs/README.md` 从 v0.1 起（中文全量里程碑表）；
//   - `README.md` 从 **v0.9** 起（英文表只回溯到 v0.9——v0.5~v0.8 只有中文行，这是现状）；
//   - `docs/development/README.md` 从 **v0.5** 起（v0.1~v0.4 的复盘在仓库里，但表从 v0.5 起）；
//   - `docs/internals/project-state.md` 从 **v0.56** 起（§2 的机制登记：更早那些机制
//     变更在 v0.56 之前的表里另有记法，这里只认这一段）。
var milestoneTableStartsAt = map[string]string{
	"docs/README.md":                  "v0.1",
	"README.md":                       "v0.9",
	"docs/development/README.md":      "v0.5",
	"docs/internals/project-state.md": "v0.56",
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

	docs := livingDocs(t)
	// `docs/development/` 整体是**快照**（复盘与计划不随之后的删改修订），
	// 但那个目录的**索引**是活的——它的逐版表与 `docs/README.md` 是同一种东西，
	// 因此和 v0.58 那条判据一样单独加进来。
	docs = append(docs, "../../docs/development/README.md")

	declared := 0
	for _, doc := range docs {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		display := strings.TrimPrefix(strings.ReplaceAll(doc, "\\", "/"), "../../")

		rows := []int{}
		for _, line := range strings.Split(string(body), "\n") {
			if m := reMilestoneRow.FindStringSubmatch(line); m != nil {
				rows = append(rows, parseVersion(m[1]))
			}
		}
		if len(rows) == 0 {
			continue
		}
		max := 0
		for _, n := range rows {
			if n > max {
				max = n
			}
		}
		startDoc, isDeclared := milestoneTableStartsAt[display]

		// ② 够得到最新版本的文档**必须**有声明（新表不会漏网）
		if max < latestN {
			continue // 只到更早版本的表（例如 roadmap）不归这条管
		}
		if !isDeclared {
			t.Errorf("%s 的版本行够得到最新版本（v0.%d），却没有在 `milestoneTableStartsAt` 里"+
				"声明起点——逐版表要写下它从哪个版本起，缺口才是可查的", display, max)
			continue
		}
		declared++
		start := parseVersion(startDoc)

		// ③ 起点之后的每一行：恰好一次、升序、连续到最多那一行
		seen := map[int]bool{}
		ordered := []int{}
		for _, n := range rows {
			if n < start {
				continue // 起点之前的行不归这张表管（roadmap 之类）
			}
			if seen[n] {
				t.Errorf("%s: v0.%d 出现了两次——逐版表里每个版本只该有一行", display, n)
			}
			seen[n] = true
			ordered = append(ordered, n)
		}
		if len(ordered) == 0 {
			t.Errorf("%s: 声明了起点 %s，却没有任何一行落在这之后——登记指向空气", display, startDoc)
			continue
		}
		if max != latestN {
			t.Errorf("%s: 表写到 v0.%d 就停了，而最新版本是 %s——"+
				"逐版表要一路写到最新（少了行 = 登记的动作发生了、结果却没落地）",
				display, max, latest)
		}
		for i := 1; i < len(ordered); i++ {
			if ordered[i] <= ordered[i-1] {
				t.Errorf("%s: v0.%d 出现在 v0.%d 之后——逐版表要按版本升序"+
					"（v0.63 修过一次：四行是倒着插进去的）",
					display, ordered[i], ordered[i-1])
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
			t.Errorf("%s: 缺 %d 行（%s）——从 %s 起要一路写到 %s",
				display, len(missing), strings.Join(missing, " "), startDoc, latest)
		}
	}

	if declared < 4 {
		t.Fatalf("只核对到 %d 张逐版表——这条判据的范围缩了", declared)
	}
	t.Logf("milestone tables: %d table(s) · %d version(s) · latest %s", declared, total, latest)
}
