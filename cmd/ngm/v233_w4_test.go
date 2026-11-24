package main

import (
	"strings"
	"testing"
)

// TestV233ActionHasReceiptMode 固定 E14：action.yml 有第四种 mode=receipt，
// 其命令序列先 `build --provenance` 写旁车回执、再 `verify --receipt` 只读复验；
// 原有三 mode 与 verify 的 content 默认不得被顺手改掉。
//
// 变异检验：删掉 receipt 分支的 `ngm verify --receipt` 那一行 → "复验步骤"子测试
// 立刻红；恢复 → 绿。把 mode 默认值改成 receipt → "默认值仍为 verify"红；恢复 → 绿。
func TestV233ActionHasReceiptMode(t *testing.T) {
	act := readRepoFile(t, "action.yml")

	// ① 四种 mode 齐全，默认仍是 verify。
	inputBlock := sectionBetween(act, "  mode:", "  dir:")
	for _, mode := range []string{"verify", "adopt", "adopt-check", "receipt"} {
		if !strings.Contains(inputBlock, mode) {
			t.Errorf("action.yml 的 mode 输入未列出 %q:\n%s", mode, inputBlock)
		}
	}
	if !strings.Contains(inputBlock, `default: "verify"`) {
		t.Errorf("mode 默认值被改动，必须仍是 verify:\n%s", inputBlock)
	}

	// ② 现有 verify mode 的默认内容不变（content，不是 structure）。
	levelBlock := sectionBetween(act, "  verify-level:", "  args:")
	if !strings.Contains(levelBlock, `default: "content"`) {
		t.Errorf("verify-level 默认值不再是 content:\n%s", levelBlock)
	}
	if strings.Contains(levelBlock, `default: "structure"`) {
		t.Error("verify mode 默认仍是 structure——E14 不得改动现有默认值")
	}

	blocks := v233CaseBlocks(act)
	if len(blocks) < 5 {
		t.Fatalf("case 分支解析异常，只拿到 %d 个: %v", len(blocks), v233KeysOf(blocks))
	}

	// ③ 原有三 mode 的调用保持原样。
	old := map[string]string{
		"verify":      "ngm verify --dir",
		"adopt":       "ngm adopt --dir",
		"adopt-check": `--check="$NGM_PROOF"`,
	}
	for mode, want := range old {
		body, ok := blocks[mode]
		if !ok {
			t.Fatalf("case 里缺少原有 mode %q", mode)
		}
		if !strings.Contains(body, want) {
			t.Errorf("mode %q 的调用被改动，缺少 %q:\n%s", mode, want, body)
		}
	}
	if strings.Contains(blocks["verify"], "--receipt") {
		t.Error("verify mode 不得掺入 --receipt——默认仍是依赖证明")
	}

	// ④ 新 mode：先构建出可复验产物，再只读复验；两步顺序与缺一不可。
	receipt, ok := blocks["receipt"]
	if !ok {
		t.Fatal("case 里缺少 mode=receipt")
	}
	for _, want := range []string{
		"ngm build --provenance",
		`--outfile="$NGM_BUILDOUT"`,
		"ngm verify --receipt",
	} {
		if !strings.Contains(receipt, want) {
			t.Errorf("receipt mode 命令序列缺少 %q:\n%s", want, receipt)
		}
	}
	buildAt := strings.Index(receipt, "ngm build --provenance")
	verifyAt := strings.Index(receipt, "ngm verify --receipt")
	if buildAt < 0 || verifyAt <= buildAt {
		t.Errorf("receipt mode 必须先 build --provenance 再 verify --receipt:\n%s", receipt)
	}
	if strings.Contains(receipt, "--level=") {
		t.Error("receipt 复验不注入 --level=，它是旁车复验通道，不是依赖 verify")
	}

	// ⑤ 新输入给出落盘路径；未知 mode 提示也同步列出四种。
	outBlock := sectionBetween(act, "  build-outfile:", "  verify-level:")
	if !strings.Contains(outBlock, `default: "dist/index.js"`) {
		t.Errorf("build-outfile 必须默认落盘路径:\n%s", outBlock)
	}
	if !strings.Contains(act, "adopt-check or receipt") {
		t.Error("unknown mode 的报错未列出 receipt")
	}
}

// v233CaseBlocks 把 action.yml 里 `case "$NGM_MODE" in ... esac` 的每个分支
// 切成 mode → 分支正文；只认 10 空格缩进的 `  <mode>)` 行，注释与命令都在正文里。
func v233CaseBlocks(act string) map[string]string {
	blocks := map[string]string{}
	cur := ""
	var body []string
	for _, line := range strings.Split(act, "\n") {
		if strings.HasPrefix(line, "          ") {
			rest := line[10:]
			if rest != "" && strings.HasSuffix(rest, ")") && !strings.ContainsAny(rest, " \t") {
				if cur != "" {
					blocks[cur] = strings.Join(body, "\n")
				}
				cur = strings.TrimSuffix(rest, ")")
				body = nil
				continue
			}
		}
		if cur == "" {
			continue
		}
		body = append(body, line)
		if strings.TrimSpace(line) == "esac" {
			blocks[cur] = strings.Join(body, "\n")
			cur = ""
			body = nil
		}
	}
	return blocks
}

func v233KeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
