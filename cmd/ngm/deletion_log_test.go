package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// v0.118：结构性删除必须留账，而且账上写的路径**不能还在版本库里**。
//
// 起点是一处已经发生、没有任何判据看见的事实：`docs/handover/` 六份文件在工作树里被删掉，
// 三处链接一直指着不存在的文件；而"删了什么、为什么、谁批准、影响哪条判据"一个字都没留下。
// 删除记录的价值不在记录本身，而在它逼人回答最后两栏——**影响**与**验证**。
//
// 查三件事：
//  1. 每条记录六个字段齐全（删除 / 日期 / 批准 / 为什么 / 影响 / 验证）；
//  2. 日期能解析、不晚于今天；
//  3. 记录的路径不在 `git ls-files` 里（否则是**假删除**：账上删了、库里还在）。
func TestV118DeletionLogIsComplete(t *testing.T) {
	const logPath = "../../docs/development/deletions.md"
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("缺少删除记录 %s：%v", logPath, err)
	}

	tracked := trackedFiles(t)
	today := time.Now().Format("2006-01-02")
	fields := []string{"- 删除：", "- 日期：", "- 批准：", "- 为什么：", "- 影响：", "- 验证："}

	var records int
	var title string
	var block []string
	inFence := false

	flush := func() {
		if title == "" {
			return
		}
		records++
		text := strings.Join(block, "\n")

		for _, f := range fields {
			if !strings.Contains(text, f) {
				t.Errorf("「%s」缺少字段 %s", title, strings.TrimSpace(f))
			}
		}

		if d := fieldValue(text, "- 日期："); d != "" {
			parsed, perr := time.Parse("2006-01-02", d)
			if perr != nil {
				t.Errorf("「%s」日期不可解析：%q（要 YYYY-MM-DD）", title, d)
			} else if parsed.Format("2006-01-02") > today {
				t.Errorf("「%s」日期 %s 在未来（今天 %s）——记录不是计划", title, d, today)
			}
		}

		for _, p := range backticked(fieldValue(text, "- 删除：")) {
			if tracked[p] {
				t.Errorf("「%s」记录的 %s 还在版本库里（假删除：账上删了、git ls-files 里还在）", title, p)
			}
		}
	}

	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue // 模板块里的 `## YYYY-MM-DD` 不算记录
		}
		if strings.HasPrefix(line, "## ") {
			flush()
			title = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			block = nil
			if title == "模板" {
				title = "" // 模板段不是账
			}
			continue
		}
		if title != "" {
			block = append(block, line)
		}
	}
	flush()

	if records == 0 {
		t.Fatalf("%s 里没有任何删除记录——空账本等于没有纪律（或者这个仓库从没删过东西，那就删掉这条判据）", logPath)
	}
	t.Logf("deletion log: %d 条记录，%d 个受跟踪文件", records, len(tracked))
}

func trackedFiles(t *testing.T) map[string]bool {
	t.Helper()
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files 失败：%v", err)
	}
	set := make(map[string]bool, 1024)
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			set[line] = true
		}
	}
	return set
}

func fieldValue(text, field string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), field) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), field))
		}
	}
	return ""
}

func backticked(s string) []string {
	var out []string
	parts := strings.Split(s, "`")
	for i := 1; i < len(parts); i += 2 {
		if p := strings.TrimSpace(parts[i]); p != "" {
			out = append(out, p)
		}
	}
	return out
}
