package supplychain

import (
	"testing"
	"time"

	"github.com/idcu/ngm/internal/config"
)

// TestV139DefaultReleaseAge —— B 批判据（竞品全维度评估 B1 / ADR-050）。
//
// 三条不变量：
//  1. 写了 supplyChain 段但没写冷却 ⇒ 默认 P1D（对齐 pnpm 11+ / Deno 2.9 的 2026 默认）；
//  2. 完全没有 supplyChain 段（sc == nil）⇒ 不门禁（ADR-009 底线：没配策略不挡所有既有项目）；
//  3. --allow-fresh 的副本只关冷却、不改其他门禁（白名单/严重级别仍生效），且不泄漏回原策略。
func TestV139DefaultReleaseAge(t *testing.T) {
	// 1. 有段无字段 ⇒ 24h。
	withSection, err := FromConfig(&config.SupplyChainConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := withSection.MinimumReleaseAge(); got != 24*time.Hour {
		t.Errorf("有段无字段的默认晾晒期 = %v, want 24h", got)
	}
	if DefaultMinimumReleaseAge != 24*time.Hour {
		t.Errorf("DefaultMinimumReleaseAge 常量 = %v, want 24h", DefaultMinimumReleaseAge)
	}

	// 2. 完全无段 ⇒ 0（ADR-009 底线不变）。
	noSection, err := FromConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := noSection.MinimumReleaseAge(); got != 0 {
		t.Errorf("无 supplyChain 段的晾晒期 = %v, want 0（不门禁）", got)
	}

	// 项目级逃生口 = 完全不配 supplyChain 段（上面 noSection 已断言 0）。
	// 注意 P0D 被 ParseISODuration 有意拒绝（"配了却不生效"的错觉），所以不用它做开关；
	// 单命令逃生口是 --allow-fresh / WithoutMinimumAge（下面断言）。
	if _, err := FromConfig(&config.SupplyChainConfig{MinimumReleaseAge: "P0D"}); err == nil {
		t.Error("P0D 应仍被 ParseISODuration 拒绝——保持既有语义，不用它当逃生口")
	}

	// 3. WithoutMinimumAge：副本关冷却，其他字段不变，且原策略不受影响。
	based := &config.SupplyChainConfig{
		AllowlistRepos:      []string{"github.com/trusted/*"},
		OSVIgnoreSeverities: []string{"low"},
		PostInstallPolicy:   "deny",
	}
	orig, err := FromConfig(based)
	if err != nil {
		t.Fatal(err)
	}
	bypass := orig.WithoutMinimumAge()
	if orig.MinimumReleaseAge() != DefaultMinimumReleaseAge {
		t.Errorf("WithoutMinimumAge 泄漏回原策略: orig=%v", orig.MinimumReleaseAge())
	}
	if bypass.MinimumReleaseAge() != 0 {
		t.Errorf("副本晾晒期 = %v, want 0", bypass.MinimumReleaseAge())
	}
	// 白名单与严重级别必须保留（逃生口只关晾晒期）。
	if !bypass.IsAllowlisted("github.com", "trusted/lib") {
		t.Error("副本丢了白名单——--allow-fresh 不该放宽其他门禁")
	}
	if got := bypass.IgnoredSeverities(); len(got) != 1 || got[0] != "LOW" {
		t.Errorf("副本丢了严重级别豁免: %v", got)
	}
	if bypass.PostInstallPolicy() != "deny" {
		t.Errorf("副本丢了 postInstallPolicy: %q", bypass.PostInstallPolicy())
	}

	// nil 策略的副本仍是 nil（不能 panic，也不能凭空造出门禁）。
	if (*Policy)(nil).WithoutMinimumAge() != nil {
		t.Error("nil.WithoutMinimumAge() 应仍是 nil")
	}
}
