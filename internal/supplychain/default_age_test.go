package supplychain

import (
	"testing"
	"time"

	"github.com/idcu/ngm/internal/config"
)

// TestV211DefaultReleaseAge —— ADR-053 判据，取代历史 TestV139（ADR-050 的"无段不门禁"）。
//
// 四种形态：
//  1. 无 supplyChain 段 ⇒ 全局默认 24h（覆盖 ADR-009 的空策略底线，仅限 minimumReleaseAge）；
//  2. 有段无字段 ⇒ 24h；
//  3. 显式 minimumReleaseAge: "PT0S" ⇒ 0（项目级关闭，不是"配了没生效"）；
//  4. --allow-fresh 副本只关冷却、不改其他门禁，且不泄漏回原策略。
func TestV211DefaultReleaseAge(t *testing.T) {
	// 1. 无段 ⇒ 24h。
	noSection, err := FromConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := noSection.MinimumReleaseAge(); got != 24*time.Hour {
		t.Errorf("无 supplyChain 段的晾晒期 = %v, want 24h", got)
	}
	// IsEmpty 只描述"有没有显式配置"，不能再当作"门禁是否执行"的判据。
	if !noSection.IsEmpty() {
		t.Error("无段仍应 IsEmpty=true（历史判据读法）")
	}
	// 默认值的 CLI 强制执行切换依赖夹具迁移（ADR-053 接线事项）；此处只钉住 Policy 默认值。
	if noSection.AgeGateActive() {
		t.Error("无段的 CLI 门禁尚未切换，AgeGateActive 应为 false")
	}

	// 2. 有段无字段 ⇒ 24h。
	withSection, err := FromConfig(&config.SupplyChainConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if got := withSection.MinimumReleaseAge(); got != 24*time.Hour {
		t.Errorf("有段无字段的默认晾晒期 = %v, want 24h", got)
	}
	if !withSection.AgeGateActive() {
		t.Error("显式写了 supplyChain 段的项目应继续执行晾晒期门禁")
	}
	if DefaultMinimumReleaseAge != 24*time.Hour {
		t.Errorf("DefaultMinimumReleaseAge 常量 = %v, want 24h", DefaultMinimumReleaseAge)
	}

	// 3. 显式 PT0S 关闭；P0D 仍被拒（避免"配了零却没生效"的错觉）。
	off, err := FromConfig(&config.SupplyChainConfig{MinimumReleaseAge: "PT0S"})
	if err != nil {
		t.Fatalf("PT0S 应作为显式关闭被接受: %v", err)
	}
	if got := off.MinimumReleaseAge(); got != 0 {
		t.Errorf("PT0S 的晾晒期 = %v, want 0", got)
	}
	if off.AgeGateActive() {
		t.Error("PT0S 是显式关闭，不应再执行门禁")
	}
	if _, err := FromConfig(&config.SupplyChainConfig{MinimumReleaseAge: "P0D"}); err == nil {
		t.Error("P0D 应仍被 ParseISODuration 拒绝——不用它当逃生口")
	}

	// 4. WithoutMinimumAge：副本关冷却，其他字段不变，且原策略不受影响。
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

	// 全局默认策略上做 --allow-fresh 也不泄漏（无段项目同样能单命令放行）。
	implicit := noSection.WithoutMinimumAge()
	if implicit.MinimumReleaseAge() != 0 || noSection.MinimumReleaseAge() != 24*time.Hour {
		t.Errorf("无段项目的 --allow-fresh 副本泄漏: copy=%v orig=%v",
			implicit.MinimumReleaseAge(), noSection.MinimumReleaseAge())
	}

	// nil 指针的副本仍是 nil（不能 panic，也不能凭空造出门禁）。
	if (*Policy)(nil).WithoutMinimumAge() != nil {
		t.Error("nil.WithoutMinimumAge() 应仍是 nil")
	}
}
