package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// v0.71：ADR 汇总表的机械判据。
//
// 起因（v0.70 复盘 §4 缺陷 1）：`docs/adr/README.md` 的索引行靠**手抄**，
// 而 ADR-025 的行整行缺失、ADR-024 重复了两行、ADR-026 的行与前者粘连成
// 一个多余的单元格——这套缺陷**挂了数版**没有任何东西变红。
// 根因与 v0.64 的 `schemaFiles` 同类："清单活在人的脑子里，必然漂移"。
//
// 单篇 `docs/adr/adr-*.md` 已随历史文档清理移出工作树；现在**唯一的 ADR 事实源**
// 就是 `docs/adr/README.md` 的三列表 `| ADR-NNN | 决策 | 结论 |`。因此本判据
// 不再扫文件系统，改为对这张表做**表内自洽**：
//
//	① 恰好 58 行（ADR-001 ~ ADR-058 的恒定规模）；
//	② 编号 001..058 连续、无缺口、无重复；
//	③ 每行恰好三列（4 个竖线）——"两行粘成一格"最先坏的就是列数；
//	④ 第一列严格是 `ADR-NNN` 形状。
//
// `58` 是历史决策条目的定数：ADR 只补录、不删除（见汇总表"如何使用"），
// 新增决策用下一个编号，因此行数与末项编号同步增长；缺口只可能来自漏抄。
func TestV71AdrIndexCoversEveryAdrFile(t *testing.T) {
	const wantRows = 58

	doc := readDoc(t, "../../docs/adr/README.md")
	rows := reAdrIndexRow.FindAllStringSubmatch(doc, -1)
	if len(rows) != wantRows {
		t.Fatalf("ADR 汇总表有 %d 行，要求恰好 %d 行（ADR-001 ~ ADR-058）——"+
			"单篇文件已删除，这张表是唯一事实源，漏抄一行就是决策丢失", len(rows), wantRows)
	}

	seen := make(map[int]int, wantRows)
	for _, row := range rows {
		line := row[0]
		if got := strings.Count(line, "|"); got != 4 {
			t.Errorf("索引行必须是 3 列（4 个竖线），%s 那行有 %d 个——列数先坏，行就会粘连: %s",
				row[1], got, strings.TrimSpace(line))
		}
		n, err := strconv.Atoi(row[1])
		if err != nil {
			t.Errorf("ADR 编号不是数字: %q", row[1])
			continue
		}
		if prev := seen[n]; prev > 0 {
			t.Errorf("ADR-%03d 出现了两次——v0.70 修掉的正是这类重复行", n)
		}
		seen[n]++
	}

	var missing []string
	for n := 1; n <= wantRows; n++ {
		if seen[n] == 0 {
			missing = append(missing, "ADR-"+pad3(n))
		}
	}
	if len(missing) > 0 {
		t.Errorf("汇总表缺编号（连续 001..058 是纪律，缺口只来自漏抄）：%s", strings.Join(missing, ", "))
	}
}

// pad3 把 7 写成 "007"，只服务于报错文案。
func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// reAdrIndexRow 抓汇总表的一行：`| ADR-027 | 远程 adapter 的信任边界 | 结论… |`。
//
// 只认行首的 `| ADR-NNN |`，把该行余下部分整行吃进来，列数由上面的
// `strings.Count("|") == 4` 单独对账——这样"两行粘成一格"（多一列）与
// "少写一列"都能被点名，而不是让正则静默漏掉那一行。
var reAdrIndexRow = regexp.MustCompile(`(?m)^\|\s*ADR-(\d{3})\s*\|.*$`)
