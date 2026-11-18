package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
		Passthrough   *struct {
			Command string         `json:"command"`
			Signals map[string]int `json:"signals"`
		} `json:"passthroughFamily"`
		Codes []struct {
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

	// 透传族**不在** 0..6 码表里：`ngm run` 返回的码属于项目脚本，不属于 ngm 的
	// 错误分类。它必须在契约里单独有一节，否则读这份 JSON 的包装器会把它当成
	// "未登记的内部码"。
	if doc.Passthrough == nil {
		t.Error("契约缺少 passthroughFamily：`ngm run` 的透传退出码没有登记")
	} else {
		if doc.Passthrough.Command != "run" {
			t.Errorf("passthroughFamily 的 command 是 %q，应为 run", doc.Passthrough.Command)
		}
		if doc.Passthrough.Signals["SIGINT"] != 130 || doc.Passthrough.Signals["SIGTERM"] != 143 {
			t.Errorf("信号映射是 %v，应为 SIGINT=130 / SIGTERM=143", doc.Passthrough.Signals)
		}
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

// TestV133ContractNamesRealTests 防止契约文件再次登记一个不存在的守门人。
//
// 缺陷形态（2026-11-18）：description 里写 TestV119ExitCodeContractMatchesCode，
// 实际测试叫 TestV119ExitCodesJsonContract——于是这条承诺在仓库里没有任何
// 判据守住，而读契约的人以为有人守。判据从 JSON 文本里抽出反引号测试名，
// 再在整个 Go 源码里找 func 定义。
func TestV133ContractNamesRealTests(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/exit-codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	names := regexp.MustCompile("`([A-Za-z][A-Za-z0-9_]*)`").FindAllStringSubmatch(doc.Description, -1)
	if len(names) == 0 {
		t.Fatal("the contract description must name at least one guard test")
	}

	// 收集整个仓库的 func 名（只需要 cmd/ngm 与 internal）。
	sources := map[string]bool{}
	for _, root := range []string{"../.."} {
		err := filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
			if werr != nil {
				return werr
			}
			if info.IsDir() {
				if info.Name() == ".git" || info.Name() == "dist" || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			for _, m := range regexp.MustCompile(`(?m)^func\s+([A-Za-z][A-Za-z0-9_]*)\s*\(`).FindAllStringSubmatch(string(b), -1) {
				sources[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range names {
		if !sources[m[1]] {
			t.Errorf("contract names test %q, but no such func exists in the repository", m[1])
		}
	}
}
