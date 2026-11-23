package config

import "testing"

// TestV205MergeProjectDeepCopiesSupplyChainSlices 固定 2026-11-23 审计 P2：
// v0.66 承诺"改 Effective 不回写来源"，但 SupplyChain 只做了结构体浅拷贝，
// 三个白名单切片仍共享底层数组。
func TestV205MergeProjectDeepCopiesSupplyChainSlices(t *testing.T) {
	project := &ProjectFile{
		Name: "app", Version: "1.0.0",
		SupplyChain: &SupplyChainConfig{
			AllowedGitHosts:     []string{"github.com"},
			AllowlistRepos:      []string{"github:org/repo"},
			OSVIgnoreSeverities: []string{"low"},
		},
	}
	eff := mergeProject(DefaultBuiltin(), project)
	eff.SupplyChain.AllowedGitHosts[0] = "evil.example"
	eff.SupplyChain.AllowlistRepos[0] = "evil:org/repo"
	eff.SupplyChain.OSVIgnoreSeverities[0] = "critical"

	if project.SupplyChain.AllowedGitHosts[0] != "github.com" ||
		project.SupplyChain.AllowlistRepos[0] != "github:org/repo" ||
		project.SupplyChain.OSVIgnoreSeverities[0] != "low" {
		t.Errorf("Effective 的切片改动回写了 Project: %#v", project.SupplyChain)
	}
}

// TestV205bDuplicateLocalRootsAreRejected 固定同批次的 local 身份漏检：
// validateUniqueRootDependencies 此前对 ParseSlug 失败的 local 条目直接 continue，
// 手写清单里两条相同 local:<name> + 相同 path 不会被重复根规则拒绝。
func TestV205bDuplicateLocalRootsAreRejected(t *testing.T) {
	deps := []Dependency{
		{Name: "local:kernel", Ref: "auto", RefType: "local-dir", Path: "pkg", SourcePath: "./kernel"},
		{Name: "local:kernel", Ref: "auto", RefType: "local-dir", Path: "pkg", SourcePath: "./kernel"},
	}
	if err := validateUniqueRootDependencies(deps); err == nil {
		t.Fatal("duplicate local roots must be rejected like duplicate Git roots")
	}
}
