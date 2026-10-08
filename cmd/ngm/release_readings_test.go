package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// v0.58：**散文里的发布读数必须对得上事实源**。
//
// 起点是上一轮的一个真实误差：同步发布读数时（手抄）把 `docs/README.md` 里那行
// "57 个 tag 里，GitHub 上有 53 个 release" 写成了 "55 个 tag 里 … 51 个"——
// 偏了一个版本，而**没有任何判据看着它**：
//
//	· `scripts/check-release-status.sh` 核对的是 tag 与 release **是否存在**；
//	· go 测试里没有一条读过散文里的**计数**。
//
// 这一版把"读数的来源"也立起来。本地可得的事实源有两个（都不需要网络）：
//
//	· **已交付版本数** = `docs/development/` 里的复盘文件数（`v*-retrospective.md`）
//	  ——项目纪律是"每个版本一份复盘"，而 `check-release-status.sh` 另有门禁
//	  要求"每个 tag 都有复盘"，两条合起来让这个数就是"已交付版本数"。
//	  计划文件数（`v*-plan.md`）作为**第二个来源**交叉核对：两者必须相等。
//	· **最新版本** = 那些文件名里最大的版本号。
//
// 其余读数（GitHub 有多少个 release、Gitee 有几个）只在网络上可得，所以判据
// 不做"对不对"，只做**两件仍能离线做完的事**：
//
//	① 每一处出现的版本总数都必须等于派生值（中英文、数字与汉字都算）；
//	② 同一个事实在多处出现时必须**处处相同**，且彼此的**算术关系**成立
//	   （`缺 N 个` == 版本数 − Gitee 数；`成功 N 次` == GitHub 数 − 从未发布的 4 个；
//	   `fifty-four historical artifacts` == 版本数 − Gitee 数）。
//
// 判据 ① 就能抓住上一轮那行错：它写的 55 不等于派生的 57。

// neverReleased 是从未发布过 GitHub release 的 tag 数（v0.5.0 ~ v0.8.0）：
// 那四个 tag 是镜像转发过来的，**没有触发** tag 推送的工作流（见根 README 的发布状态表）。
// 它是一处**写清楚的事实**，不是可以随手改的数。
const neverReleased = 4

var (
	// 中文的**总数声明**：`五十七个 tag 均已打` / `五十七个 tag 全部已打`。
	//
	// 必须带"均已/全部"这两个词：同一个仓库里还有**历史陈述**（例如"那四个 tag
	// 没有触发工作流"），它们说的是别的范围，不是总数声明——第一版没带这两个词，
	// 于是在发布清单里报了三处误报。
	reZhTagCount = regexp.MustCompile(`([一二三四五六七八九十]+)个 tag (?:均已|全部)`)
	// 英文：`all fifty-seven tags`
	reEnTagCount = regexp.MustCompile(`all ([a-z]+(?:-[a-z]+)?) tags`)
	// 中文那一行：`57 个 tag 里，GitHub 上有 **53 个** release`
	reZhTagline = regexp.MustCompile(`(\d+) 个 tag 里，GitHub 上有 \*{0,2}(\d+) 个`)
	// 英文那两行：`| GitHub | **53 of 57** |` / `| Gitee | **3 of 57** |`
	reEnOfN = regexp.MustCompile(`\| (GitHub|Gitee) \| \*{0,2}(\d+) of (\d+)\*{0,2} \|`)
	// 还有第三种写法（roadmap 那句）：`GitHub 有 57 个里的 53 个`
	//
	// 填充类必须**把数字排除在外**：第一版写作 `[^，。\n]{0,6}`，于是它贪婪地
	// 吃掉了" 有 5"，`(\d+)` 只匹配到 "7"——正则读出来的数与文件里的不一样，
	// 而这条判据的整个价值就在"读出来的数要对"。探针一条就照出来了。
	reZhOfN = regexp.MustCompile(`(GitHub|Gitee)[^，。\n\d]{0,6}(\d+) 个里的 (\d+) 个`)
	// 暂缓那几处：`缺 54 个`
	reZhGap = regexp.MustCompile(`缺 \*{0,2}(\d+) 个`)
	// `Gitee 发行版（54 个）` 与 `共五十四个版本`
	reZhGiteeGap = regexp.MustCompile(`Gitee 发行版（(\d+) 个）`)
	reZhVersionC = regexp.MustCompile(`共([一二三四五六七八九十]+)个版本`)
	// `worked forty-nine times`
	reEnTimes = regexp.MustCompile(`worked ([a-z]+(?:-[a-z]+)?) times`)
	// `those fifty-four historical artifacts`
	reEnArtifacts = regexp.MustCompile(`those ([a-z]+(?:-[a-z]+)?) historical artifacts`)
	// 行内的版本号（`v0.57` 或 `v0.57.0`）——取"交付"那句话里的**最后一个**，
	// 它就是"当前最新的已交付版本"。
	reVersionToken = regexp.MustCompile(`v[0-9]+\.[0-9]+(?:\.[0-9]+)?`)
)

func TestV58PublishedReadingsAgreeWithTheirSource(t *testing.T) {
	delivered, latest := deliveredVersions(t)
	if delivered < 10 {
		t.Fatalf("派生的已交付版本数只有 %d——这条判据的范围缩了", delivered)
	}

	readings := 0
	deliveryLines := 0
	githubCount, giteeCount := -1, -1

	// record 记下某处声明的"某个源上有多少个"，并要求**处处相同**：
	// 同一个事实在两处出现却写成两个数，是这类误差最典型的形态（上一轮那行就是）。
	record := func(display, what string, n int) {
		switch what {
		case "GitHub":
			if githubCount < 0 {
				githubCount = n
			} else if githubCount != n {
				t.Errorf("%s: GitHub 的 release 数这里写 %d，别处写 %d——同一件事有两份读数",
					display, n, githubCount)
			}
		case "Gitee":
			if giteeCount < 0 {
				giteeCount = n
			} else if giteeCount != n {
				t.Errorf("%s: Gitee 的发行版数这里写 %d，别处写 %d——同一件事有两份读数",
					display, n, giteeCount)
			}
		}
	}

	docs := livingDocs(t)
	// `docs/development/` 整体是**快照**（复盘与计划不随之后的删改修订），
	// 但那个目录的**索引**（发布清单）是活的：头部写着交付区间、第 4/6 步写着缺口数，
	// 而这两处正是每次发布要手动同步的地方——它必须在判据的范围内。
	docs = append(docs, "../../docs/development/README.md")

	for _, doc := range docs {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		display := strings.TrimPrefix(filepath.ToSlash(doc), "../../")
		text := string(body)

		// 两类模式的扫描面不同，因为它们的栖息地不同：
		//
		//	· **计数短语**（"五十九个 tag 均已打" / "缺 N 个" / "N 个里的 M 个" …）
		//	  活在**正文**里 ⇒ 只扫正文 ✓。表格单元会把它们当**例句**引用
		//	  （逐版表里写着"中文汉字「五十八个 tag 均已打」"），第一版在那儿误报过。
		//	· **`N of M`** 只活在**表格**里（`| GitHub | **54 of 59** |`）⇒ 扫全文 ✓。
		var prose strings.Builder
		for _, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "> |") {
				continue
			}
			prose.WriteString(line)
			prose.WriteString("\n")
		}
		proseText := prose.String()

		// ① 版本总数：中文写法
		for _, m := range reZhTagCount.FindAllStringSubmatch(proseText, -1) {
			n, ok := zhNumber(m[1])
			if !ok {
				t.Errorf("%s: 读不出汉字数字 %q（判据只认到五十九）", display, m[1])
				continue
			}
			readings++
			if n != delivered {
				t.Errorf("%s: 写着 %s 个 tag，而事实源（docs/development 的复盘文件）是 %d 个",
					display, m[1], delivered)
			}
		}

		// ① 版本总数：英文写法
		for _, m := range reEnTagCount.FindAllStringSubmatch(proseText, -1) {
			n, ok := enNumber(m[1])
			if !ok {
				t.Errorf("%s: 读不出英文数词 %q", display, m[1])
				continue
			}
			readings++
			if n != delivered {
				t.Errorf("%s: 写着 all %s tags，而事实源是 %d 个", display, m[1], delivered)
			}
		}

		// ② `N of M`：M 必须等于版本总数；同一个事实多处出现必须相同
		for _, m := range reEnOfN.FindAllStringSubmatch(text, -1) {
			what, n, total := m[1], mustAtoi(t, m[2]), mustAtoi(t, m[3])
			readings++
			if total != delivered {
				t.Errorf("%s: `%s | %d of %d` 里的总数是 %d，而事实源是 %d",
					display, what, n, total, total, delivered)
			}
			record(display, what, n)
		}

		// ② 第三种写法（roadmap 那句）：`GitHub 有 57 个里的 53 个`
		for _, m := range reZhOfN.FindAllStringSubmatch(proseText, -1) {
			what, total, n := m[1], mustAtoi(t, m[2]), mustAtoi(t, m[3])
			readings++
			if total != delivered {
				t.Errorf("%s: `%s 有 %d 个里的 %d 个` 里的总数是 %d，而事实源是 %d",
					display, what, total, n, total, delivered)
			}
			record(display, what, n)
		}

		// ② 中文那一行里的两个数
		for _, m := range reZhTagline.FindAllStringSubmatch(proseText, -1) {
			total, gh := mustAtoi(t, m[1]), mustAtoi(t, m[2])
			readings++
			if total != delivered {
				t.Errorf("%s: `%d 个 tag 里，GitHub 上有 %d 个` 里的总数是 %d，而事实源是 %d",
					display, total, gh, total, delivered)
			}
			record(display, "GitHub", gh)
		}

		// ② 交付区间的右端必须是**最新版本**——**只在"交付"那句话里查**。
		//
		// 别的行也会出现两个版本号，而它们说的是别的事（例如"从未发布的那四个 tag"、
		// "Gitee 侧只有 v0.1.0 与 v0.5.0 ~ v0.57.0"）——按全文匹配会把这些也当成
		// 交付声明（这条判据的第一版就是这么错的）。
		for _, line := range strings.Split(proseText, "\n") {
			if !strings.Contains(line, "均已交付") && !strings.Contains(line, "delivered in source") {
				continue
			}
			// 表格单元里的"均已交付"说的是**某一组工作**做完了，不是版本区间
			// （发布清单里"两组均已交付 ✅"）；而**引用**旧句子也会带上这个词
			// （逐版表的行会写出被修掉的那句"v0.1 ~ v0.11 均已交付"）。
			// 两者的共同点是：它们是**表格行**（含块引用里的 `> | …`）。
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "|") || strings.HasPrefix(trimmed, "> |") {
				continue
			}
			last := lastVersionOnLine(line)
			if last == "" {
				// 有的句子里"均已交付"说的是**某一组工作**（例如发布清单里
				// "两组均已交付 ✅"），它不含版本区间——那是另一件事，跳过。
				continue
			}
			deliveryLines++
			readings++
			if last != latest {
				t.Errorf("%s: 交付区间的右端写着 %s，而事实源里最新的版本是 %s",
					display, last, latest)
			}
		}

		// ② 暂缓那几处（缺 N 个 / Gitee 发行版（N 个）/ 共 N 个版本）：
		//    它们都等于"版本数 − Gitee 数"，但 Gitee 数可能出现在别的文档里，
		//    所以先收集，最后统一核对。
		for _, m := range reZhGap.FindAllStringSubmatch(proseText, -1) {
			readings++
			gaps = append(gaps, gapReading{display, mustAtoi(t, m[1]), "缺 N 个"})
		}
		for _, m := range reZhGiteeGap.FindAllStringSubmatch(proseText, -1) {
			readings++
			gaps = append(gaps, gapReading{display, mustAtoi(t, m[1]), "Gitee 发行版（N 个）"})
		}
		for _, m := range reZhVersionC.FindAllStringSubmatch(proseText, -1) {
			n, ok := zhNumber(m[1])
			if !ok {
				t.Errorf("%s: 读不出汉字数字 %q", display, m[1])
				continue
			}
			readings++
			gaps = append(gaps, gapReading{display, n, "共 N 个版本"})
		}
		for _, m := range reEnArtifacts.FindAllStringSubmatch(proseText, -1) {
			n, ok := enNumber(m[1])
			if !ok {
				t.Errorf("%s: 读不出英文数词 %q", display, m[1])
				continue
			}
			readings++
			gaps = append(gaps, gapReading{display, n, "N historical artifacts"})
		}

		// ② 成功次数 == GitHub 数 − 从未发布的 4 个（延后核对，同上）
		for _, m := range reEnTimes.FindAllStringSubmatch(proseText, -1) {
			n, ok := enNumber(m[1])
			if !ok {
				t.Errorf("%s: 读不出英文数词 %q", display, m[1])
				continue
			}
			readings++
			times = append(times, timesReading{display, n})
		}
	}

	if readings == 0 {
		t.Fatal("一处读数都没扫到——这条判据的范围缩到零了")
	}
	if githubCount < 0 {
		t.Fatal("没有扫到 GitHub 的 release 读数——分不清它是没写还是写错了")
	}
	if giteeCount < 0 {
		t.Fatal("没有扫到 Gitee 的发行版读数——缺口的算术关系没法核对")
	}
	if deliveryLines == 0 {
		t.Fatal("一条带版本的交付声明都没扫到——这条判据的范围缩到零了")
	}

	// ② 缺口 == 版本数 − Gitee 数（四处联动：中文那行、安装指南、状态页、开发总览）
	wantGap := delivered - giteeCount
	for _, g := range gaps {
		if g.n != wantGap {
			t.Errorf("%s: `%s` 写着 %d，而 版本数(%d) − Gitee(%d) = %d",
				g.doc, g.kind, g.n, delivered, giteeCount, wantGap)
		}
	}

	// ② 成功次数 == GitHub 数 − 从未发布的 4 个
	for _, tr := range times {
		if tr.n != githubCount-neverReleased {
			t.Errorf("%s: 写着工作流成功 %d 次，而 GitHub(%d) − 从未发布的 %d = %d",
				tr.doc, tr.n, githubCount, neverReleased, githubCount-neverReleased)
		}
	}

	t.Logf("release readings: %d reading(s) (incl. %d delivery line(s)) · delivered=%d · latest=%s · github=%d · gitee=%d · gap=%d",
		readings, deliveryLines, delivered, latest, githubCount, giteeCount, wantGap)
}

type gapReading struct {
	doc  string
	n    int
	kind string
}

type timesReading struct {
	doc string
	n   int
}

var (
	gaps  []gapReading
	times []timesReading
)

// lastVersionOnLine 取一行里**最后一个**版本号，并把 `v0.57.0` 与 `v0.57` 归一
// （交付那句话里两种写法都出现，而它们说的是同一个版本）。
func lastVersionOnLine(line string) string {
	toks := reVersionToken.FindAllString(line, -1)
	if len(toks) == 0 {
		return ""
	}
	return strings.TrimSuffix(toks[len(toks)-1], ".0")
}

// deliveredVersions 从 `docs/development/` **派生**已交付版本数与最新版本。
//
// 两个来源交叉核对：复盘文件（`v*-retrospective.md`）与计划文件（`v*-plan.md`）
// ——项目纪律是每个版本两份都写，所以两边的数目必须相等。任何一个为零即 Fatal。
func deliveredVersions(t *testing.T) (int, string) {
	t.Helper()
	counts := map[string]int{}
	for _, kind := range []string{"retrospective", "plan"} {
		files, err := filepath.Glob("../../docs/development/v*-" + kind + ".md")
		if err != nil {
			t.Fatal(err)
		}
		counts[kind] = len(files)
	}
	if counts["retrospective"] == 0 || counts["plan"] == 0 {
		t.Fatalf("派生的版本数有一边是零：%v", counts)
	}
	if counts["retrospective"] != counts["plan"] {
		t.Fatalf("复盘 %d 份、计划 %d 份——两个来源对不上，事实源本身不可信",
			counts["retrospective"], counts["plan"])
	}

	files, _ := filepath.Glob("../../docs/development/v*-retrospective.md")
	versions := make([]string, 0, len(files))
	for _, f := range files {
		base := filepath.Base(f)
		versions = append(versions, strings.TrimSuffix(strings.TrimPrefix(base, "v"), "-retrospective.md"))
	}
	sort.Slice(versions, func(i, j int) bool { return versionLess(versions[i], versions[j]) })
	return len(files), "v" + versions[len(versions)-1]
}

// versionLess 比较 `0.9` 与 `0.10` 这种版本号（按每一段数字比，不按字符串）。
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, bi := mustAtoiOpt(as[i]), mustAtoiOpt(bs[i])
		if ai != bi {
			return ai < bi
		}
	}
	return len(as) < len(bs)
}

func mustAtoiOpt(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	if s == "" {
		t.Fatalf("空数字")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("不是纯数字：%q", s)
		}
	}
	return mustAtoiOpt(s)
}

// zhNumber 读"一二三…十…十九…二十…五十七"这类数字（判据只用到 59 以内）。
func zhNumber(s string) (int, bool) {
	digits := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	if s == "十" {
		return 10, true
	}
	total, cur := 0, 0
	for _, r := range s {
		if r == '十' {
			if cur == 0 {
				cur = 1
			}
			total += cur * 10
			cur = 0
			continue
		}
		d, ok := digits[r]
		if !ok {
			return 0, false
		}
		cur = d
	}
	return total + cur, true
}

// enNumber 读"one…twenty…fifty-seven"这类数词（判据只用到 60 以内）。
func enNumber(s string) (int, bool) {
	ones := map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
		"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11,
		"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
		"seventeen": 17, "eighteen": 18, "nineteen": 19}
	tens := map[string]int{"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50,
		// `sixty` 是 v0.60 那个版本数（六十）教它补上的：判据的解析器也会不够用。
		"sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90}
	if n, ok := ones[s]; ok {
		return n, true
	}
	if n, ok := tens[s]; ok {
		return n, true
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 2 {
		if t, ok := tens[parts[0]]; ok {
			if o, ok := ones[parts[1]]; ok && o < 10 {
				return t + o, true
			}
		}
	}
	return 0, false
}
