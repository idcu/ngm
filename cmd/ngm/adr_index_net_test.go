package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// v0.71：ADR 索引的机械判据。
//
// 起因（v0.70 复盘 §4 缺陷 1）：`docs/adr/README.md` 的索引行靠**手抄**，
// 而 ADR-025 的行整行缺失、ADR-024 重复了两行、ADR-026 的行与前者粘连成
// 一个多余的单元格——这套缺陷**挂了数版**没有任何东西变红。
// 根因与 v0.64 的 `schemaFiles` 同类："清单活在人的脑子里，必然漂移"。
//
// 本判据把索引变成**可推导的对账**：
//
//	文件集   docs/adr/adr-*.md（os.ReadDir，事实源一）
//	索引行   docs/adr/README.md 的表格（事实源二）
//	① 每个 ADR 文件**恰好一行**：缺失 = 漏登记；≥2 = 重复（v0.70 修掉的那处）
//	② 每行的链接必须指向真实存在的文件（不许指向空气）
//	③ 每行必须恰好 3 列（4 个竖线）——"两行粘成一格"最先坏的就是列数
//	④ 编号连续（001..N 无缺口）：ADR 的纪律是"被取代也不删除"（README §流程），
//	  所以缺口只会是文件丢失，不存在合法的缺口
func TestV71AdrIndexCoversEveryAdrFile(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "docs", "adr")

	// ① 文件集（跳过 README 之类；只认 adr-*.md）。
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	files := map[string]bool{}
	nums := map[int]string{}
	for _, e := range dirEntries {
		name := e.Name()
		if !strings.HasPrefix(name, "adr-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		files[name] = true
		m := reAdrFileNum.FindStringSubmatch(name)
		if m == nil {
			t.Errorf("ADR 文件名不符合 adr-<编号>-<slug>.md 的形状: %s", name)
			continue
		}
		n, cerr := strconv.Atoi(m[1])
		if cerr != nil {
			t.Errorf("ADR 编号不是数字: %s", name)
			continue
		}
		if prev, dup := nums[n]; dup {
			t.Errorf("ADR 编号 %03d 被两个文件使用: %s 与 %s", n, prev, name)
		}
		nums[n] = name
	}
	if len(files) == 0 {
		t.Fatal("docs/adr 下没有 adr-*.md——判据的范围缩到零了，先确认目录没被挪走")
	}

	// ② 索引行。
	doc := readDoc(t, "../../docs/adr/README.md")
	rows := map[string]int{}
	for _, line := range strings.Split(doc, "\n") {
		m := reAdrIndexRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		link := m[2]
		if got := strings.Count(line, "|"); got != 4 {
			t.Errorf("索引行必须是 3 列（4 个竖线），%s 那行有 %d 个——列数先坏，行就会粘连: %s",
				link, got, strings.TrimSpace(line))
		}
		if !files[link] {
			t.Errorf("索引指向不存在的文件: ./%s（要么文件被挪走，要么链接拼错）", link)
		}
		rows[link]++
	}
	if len(rows) == 0 {
		t.Fatal("索引里没有认出任何一行——先看 docs/adr/README.md 的表格形状是不是变了")
	}

	// ③ 双向对账。
	var missing []string
	for name := range files {
		switch rows[name] {
		case 0:
			missing = append(missing, name)
		case 1:
		default:
			t.Errorf("%s 在索引里出现了 %d 次——v0.70 修掉的正是这类重复行", name, rows[name])
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("这些 ADR 文件在索引里没有行（漏登记的后果：读者按索引找不到已定的决策）：%s",
			strings.Join(missing, ", "))
	}

	// ④ 编号连续。
	have := make([]int, 0, len(nums))
	for n := range nums {
		have = append(have, n)
	}
	sort.Ints(have)
	for i, n := range have {
		if n != i+1 {
			t.Errorf("ADR 编号在 %03d 处出现缺口（前一个是 %03d）——纪律是「被取代也不删除」，缺口只该来自文件丢失",
				n, i)
			break
		}
	}
}

// reAdrFileNum 抓文件名里的编号（`adr-027-attestation.md` → "027"）。
var reAdrFileNum = regexp.MustCompile(`^adr-(\d+)-`)

// reAdrIndexRow 抓索引表的一行：`| [ADR-027](./adr-027-attestation.md) | … | … |`。
//
// 只认行首的链接单元格：README 其余段落（流程 / 模板 / 约定）不含这种形状，
// 模板代码块里的 `[ADR-YYY]` 是占位字母、不会被 \d+ 命中。
var reAdrIndexRow = regexp.MustCompile(`(?m)^\|\s*\[ADR-(\d+)\]\(\./([^)]+)\)\s*\|`)
