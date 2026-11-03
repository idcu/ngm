package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.119：退出码多了一份**机器可读的对外契约**（`contracts/exit-codes.json`）。
//
// 在此之前，退出码只存在于两处：`internal/core/errs` 的常量（给 Go 看）与
// `docs/architecture/observability.md` 的表格（给人看）。脚本与 CI 要判断
// "这个码该不该重试"，只能 grep 人类可读输出——那是**用展示层做契约**，措辞一改就断。
//
// 查三件事：
//  1. 契约里的码与 `errs` 常量逐条对得上（名字取自 `Code.String()`，不是我另起的名）；
//  2. 0..6 恰好各一条（多出来的码在代码里不存在，就是空承诺）；
//  3. 每条都有 meaning（≥10 字）与 retryable —— 这两栏才是契约的用处；
//     缺了它，契约退化成一张码表。
func TestV119ExitCodesJsonContract(t *testing.T) {
	body, err := os.ReadFile("../../contracts/exit-codes.json")
	if err != nil {
		t.Fatalf("缺少退出码契约：%v", err)
	}
	var doc struct {
		SchemaVersion int `json:"schemaVersion"`
		Codes         []struct {
			Code      int    `json:"code"`
			Name      string `json:"name"`
			Meaning   string `json:"meaning"`
			Retryable *bool  `json:"retryable"`
			Source    string `json:"source"`
		} `json:"codes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("契约不是合法 JSON：%v", err)
	}
	if doc.SchemaVersion != 1 {
		t.Errorf("schemaVersion 应为 1，实际 %d", doc.SchemaVersion)
	}

	seen := map[int]bool{}
	for _, c := range doc.Codes {
		if seen[c.Code] {
			t.Errorf("退出码 %d 在契约里重复", c.Code)
		}
		seen[c.Code] = true

		if got := errs.Code(c.Code).String(); got != c.Name {
			t.Errorf("退出码 %d 的 name 是 %q，代码里是 %q", c.Code, c.Name, got)
		}
		if len([]rune(c.Meaning)) < 10 {
			t.Errorf("退出码 %d 的 meaning 太短（要写清什么时候会看到它）", c.Code)
		}
		if c.Retryable == nil {
			t.Errorf("退出码 %d 缺少 retryable —— 这一栏才是契约的用处", c.Code)
		}
		if c.Source == "" {
			t.Errorf("退出码 %d 缺少 source（代码里的常量名）", c.Code)
		}
	}

	for code := 0; code <= 6; code++ {
		if !seen[code] {
			t.Errorf("契约缺少退出码 %d（0..6 必须齐全）", code)
		}
	}
	if len(doc.Codes) != 7 {
		t.Errorf("契约有 %d 条，代码里只有 0..6 七个码", len(doc.Codes))
	}
}
