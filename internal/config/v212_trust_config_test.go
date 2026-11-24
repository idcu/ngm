package config

import "testing"

// TestV212TrustPolicyIsWiredFromConfig 固定 2026-11-24 竞品改进 P0：
// supplyChain.trustPolicy 是配置字段而不是只存在于 Policy 构造函数的内部开关；
// PT0S 是显式项目级关闭，P0D 仍按既有语义拒绝。
func TestV212TrustPolicyIsWiredFromConfig(t *testing.T) {
	sc := &SupplyChainConfig{MinimumReleaseAge: "PT0S", TrustPolicy: "no-downgrade"}
	if err := sc.Validate(); err != nil {
		t.Fatalf("PT0S + no-downgrade must be valid config: %v", err)
	}
	bad := &SupplyChainConfig{TrustPolicy: "always-trust"}
	if err := bad.Validate(); err == nil {
		t.Fatal("an unknown trustPolicy must be rejected")
	}
	legacy := &SupplyChainConfig{MinimumReleaseAge: "P0D"}
	if err := legacy.Validate(); err == nil {
		t.Fatal("P0D must stay rejected (explicit no-op is PT0S, not P0D)")
	}
}
