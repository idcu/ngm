package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestV142AdrCountsAgreeWithFiles —— 台账判据（指向已发生的真实缺陷，符合 ADR-049 门槛）。
//
// 起因（2026-11-21，第三次全量推进时发现）：上一轮新增 ADR-050/051 后，
// 根 README.md 仍写"49 个决策记录"（实际 51），docs/README.md 仍写"ADR-001 ~ ADR-045"（实际 051）。
// V71 只守 docs/adr/README.md 的索引表，不守这两处**写死的读数**——于是数字漂移挂了两版没人发现。
// 这正是项目反复遇到的失效模式："数字写死在文档里就会漂移"。
//
// 本判据把两处读数变成可推导的对账（事实源 = docs/adr/adr-*.md 文件集）：
//   - 根 README.md 的"N 个决策记录"必须等于文件数；
//   - docs/README.md 的"ADR-001 ~ ADR-NNN"范围末项必须等于最大文件编号。
func TestV142AdrCountsAgreeWithFiles(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "docs", "adr")

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	reNum := regexp.MustCompile(`^adr-(\d+)-.*\.md$`)
	count := 0
	maxNum := 0
	for _, e := range ents {
		m := reNum.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		count++
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("ADR 编号不是数字: %s: %v", e.Name(), err)
		}
		if n > maxNum {
			maxNum = n
		}
	}
	if count == 0 {
		t.Fatal("docs/adr 下没有 adr-*.md——判据范围缩到零了")
	}

	// ① 根 README："N 个决策记录"。
	rootReadme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	reCount := regexp.MustCompile(`(\d+) 个决策记录`)
	mCount := reCount.FindSubmatch(rootReadme)
	if mCount == nil {
		t.Fatal("根 README.md 里找不到 N 个决策记录 那行——判据先确认读数行还在")
	}
	got, err := strconv.Atoi(string(mCount[1]))
	if err != nil {
		t.Fatal(err)
	}
	if got != count {
		t.Errorf("README.md 写 %d 个决策记录，实际 %d 个 adr-*.md——新增 ADR 时漏同步了读数", got, count)
	}

	// ② docs/README："ADR-001 ~ ADR-NNN"范围。
	docsReadme, err := os.ReadFile(filepath.Join(root, "docs", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	reRange := regexp.MustCompile(`ADR-\d{3} ~ ADR-(\d{3})`)
	mRange := reRange.FindSubmatch(docsReadme)
	if mRange == nil {
		t.Fatal("docs/README.md 里找不到 ADR-001 ~ ADR-NNN 那行——判据先确认范围行还在")
	}
	gotMax, err := strconv.Atoi(string(mRange[1]))
	if err != nil {
		t.Fatal(err)
	}
	if gotMax != maxNum {
		t.Errorf("docs/README.md 写 ADR 范围末项 %03d，实际最大编号 %03d——新增 ADR 时漏同步了范围",
			gotMax, maxNum)
	}

}
