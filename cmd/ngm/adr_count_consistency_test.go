package main

import (
	"regexp"
	"strconv"
	"testing"
)

// TestV142AdrCountsAgreeWithTheAdrTable —— 台账判据（指向已发生的真实缺陷，符合 ADR-049 门槛）。
//
// 起因（2026-11-21，第三次全量推进时发现）：上一轮新增 ADR-050/051 后，
// 根 README.md 仍写"49 个决策记录"（实际 51），docs/README.md 仍写"ADR-001 ~ ADR-045"（实际 051）。
// 数字写死在文档里就会漂移——这正是项目反复遇到的失效模式。
//
// 单篇 `docs/adr/adr-*.md` 已随历史文档清理移出工作树，`docs/adr/README.md` 的汇总表
// 成为唯一事实源。本判据改为：
//   - 表内恰好 58 行、最大编号 058（与 V71 的顺序对账互补：这里守规模与末项）；
//   - 根 README.md / docs/README.md 不再写旧的"N 个决策记录"计数（该读数已撤），
//     若写 `ADR-001 ~ ADR-NNN` 范围，末项必须等于表内末项。
func TestV142AdrCountsAgreeWithTheAdrTable(t *testing.T) {
	const wantRows = 58
	const wantMax = 58

	doc := readDoc(t, "../../docs/adr/README.md")
	rows := reAdrIndexRow.FindAllStringSubmatch(doc, -1)
	if len(rows) != wantRows {
		t.Fatalf("ADR 汇总表有 %d 行，要求 %d 行——单篇文件已删除，表是唯一事实源", len(rows), wantRows)
	}
	maxNum := 0
	for _, row := range rows {
		n, err := strconv.Atoi(row[1])
		if err != nil {
			t.Fatalf("ADR 编号不是数字: %q: %v", row[1], err)
		}
		if n > maxNum {
			maxNum = n
		}
	}
	if maxNum != wantMax {
		t.Errorf("汇总表末项是 ADR-%03d，要求 ADR-%03d", maxNum, wantMax)
	}

	// ① 根 README：旧的"N 个决策记录"读数已撤，不许复活。
	rootReadme := readDoc(t, "../../README.md")
	if m := regexp.MustCompile(`(\d+) 个决策记录`).FindStringSubmatch(rootReadme); m != nil {
		t.Errorf("根 README.md 又写回了写死的决策计数 %q——ADR 数量只从汇总表读", m[0])
	}

	// ② docs/README：若写范围，末项必须等于表内末项（不再接受旧范围 ADR-045）。
	docsReadme := readDoc(t, "../../docs/README.md")
	if m := regexp.MustCompile(`ADR-\d{3} ~ ADR-(\d{3})`).FindStringSubmatch(docsReadme); m != nil {
		got, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		if got != maxNum {
			t.Errorf("docs/README.md 写 ADR 范围末项 %03d，汇总表末项 %03d——范围读数漂移", got, maxNum)
		}
	}
}
