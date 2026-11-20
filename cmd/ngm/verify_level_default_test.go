package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV140ActionVerifyLevelDefaultsToContent —— B 批判据（竞品全维度评估 B2 / ADR-051）。
//
// CI 是供应链关键路径，所以 action 的 verify-level 默认 content（字节校验）；
// CLI 的 ngm verify 仍默认 structure（本地轻量循环）。两者不同是有意的，
// 但必须在两处都可见——否则用户会以为"CLI 与 action 行为一致"而踩坑。
//
// 本判据守三条：
//  1. action.yml 的 verify-level 默认是 content；
//  2. action.yml 的说明里点明"CLI 默认 structure"这一差异；
//  3. cmd/ngm/verify.go 的 CLI 默认仍是 structure（不被误改）。
func TestV140ActionVerifyLevelDefaultsToContent(t *testing.T) {
	root := repoRoot(t)

	action, err := os.ReadFile(filepath.Join(root, "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	act := string(action)

	// 1. 默认值必须是 content。
	block := sectionBetween(act, "  verify-level:", "  args:")
	if block == "" {
		t.Fatal("action.yml 里找不到 verify-level 段")
	}
	if !strings.Contains(block, `default: "content"`) {
		t.Errorf("action.yml verify-level 默认值不是 content:\n%s", block)
	}
	if strings.Contains(block, `default: "structure"`) {
		t.Error("action.yml verify-level 仍有 structure 默认值")
	}

	// 2. 双向可见：说明里点出 CLI 默认 structure。
	if !strings.Contains(strings.ToLower(block), "cli") ||
		!strings.Contains(strings.ToLower(block), "structure") {
		t.Error("action.yml 没说明 CLI 默认 structure 这一差异——用户会误以为两处默认一致")
	}

	// 3. CLI 默认仍是 structure（读源码断言，不靠 help 文本）。
	verifySrc, err := os.ReadFile(filepath.Join(root, "cmd", "ngm", "verify.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(verifySrc), `fs.String("level", "",`) {
		t.Error("verify.go 的 --level 默认值被改了——CLI 应保持 structure（空值走 structure 分支）")
	}
	if !strings.Contains(string(verifySrc), `fs.Bool("content", false,`) {
		t.Error("verify.go 的 --content 默认值被改成 true——CLI 应保持 structure")
	}
}

// sectionBetween 取 from 起、to 前的文本（含 from 行，不含 to 行）。
func sectionBetween(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], to)
	if j < 0 {
		return s[i:]
	}
	return s[i : i+j]
}
