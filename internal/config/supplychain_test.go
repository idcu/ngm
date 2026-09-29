package config

import (
	"strings"
	"testing"
)

// ptr 用于构造 VerifyOnLock 这类 *bool 字段。
func ptr(b bool) *bool { return &b }

// TestSupplyChainConfig_Validate 表驱动覆盖六个字段的合法与非法取值。
//
// 关注点有两条，缺一不可：
//  1. 非法取值必须被拒绝（这是"策略 schema 非法 → exit 3"的地基）
//  2. 错误信息必须指出**哪个字段、第几个条目**，否则用户面对一长串配置无从下手
func TestSupplyChainConfig_Validate(t *testing.T) {
	valid := &SupplyChainConfig{
		AllowedGitHosts:     []string{"github.com", "gitee.com"},
		AllowlistRepos:      []string{"github.com/my-org/*", "gitee.com/trusted/**"},
		MinimumReleaseAge:   "P3D",
		OSVIgnoreSeverities: []string{"LOW", "medium"}, // 大小写不敏感
		PostInstallPolicy:   "deny",
		VerifyOnLock:        ptr(true),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a fully populated, valid policy must pass; got: %v", err)
	}

	// 空配置（字段全零值）也必须通过：不配置策略 = 不启用门禁
	if err := (&SupplyChainConfig{}).Validate(); err != nil {
		t.Fatalf("an empty policy must pass; got: %v", err)
	}

	cases := []struct {
		name    string
		sc      SupplyChainConfig
		wantSub string // 错误里必须出现这个片段（字段路径）
	}{
		// allowedGitHosts
		{"host empty", SupplyChainConfig{AllowedGitHosts: []string{""}}, "allowedGitHosts[0]"},
		{"host with wildcard", SupplyChainConfig{AllowedGitHosts: []string{"github.*"}}, "allowedGitHosts[0]"},
		{"host with scheme", SupplyChainConfig{AllowedGitHosts: []string{"https://github.com"}}, "allowedGitHosts[0]"},
		{"host with slash", SupplyChainConfig{AllowedGitHosts: []string{"github.com/org"}}, "allowedGitHosts[0]"},
		{"host leading dot", SupplyChainConfig{AllowedGitHosts: []string{".github.com"}}, "allowedGitHosts[0]"},

		// allowlistRepos
		{"pattern no slash", SupplyChainConfig{AllowlistRepos: []string{"github.com"}}, "allowlistRepos[0]"},
		{"pattern matches everything", SupplyChainConfig{AllowlistRepos: []string{"*"}}, "allowlistRepos[0]"},
		{"pattern with space", SupplyChainConfig{AllowlistRepos: []string{"github.com/a b/*"}}, "allowlistRepos[0]"},
		{"pattern with scheme", SupplyChainConfig{AllowlistRepos: []string{"https://github.com/o/*"}}, "allowlistRepos[0]"},
		{"second pattern invalid", SupplyChainConfig{AllowlistRepos: []string{"github.com/ok/*", "bad"}}, "allowlistRepos[1]"},

		// minimumReleaseAge
		{"duration months", SupplyChainConfig{MinimumReleaseAge: "P1M"}, "minimumReleaseAge"},
		{"duration without P", SupplyChainConfig{MinimumReleaseAge: "3D"}, "minimumReleaseAge"},
		{"duration zero", SupplyChainConfig{MinimumReleaseAge: "P0D"}, "minimumReleaseAge"},

		// osvIgnoreSeverities
		{"severity bogus", SupplyChainConfig{OSVIgnoreSeverities: []string{"LOW", "SEVERE"}}, "osvIgnoreSeverities[1]"},
		{"severity empty", SupplyChainConfig{OSVIgnoreSeverities: []string{""}}, "osvIgnoreSeverities[0]"},

		// postInstallPolicy
		{"policy bogus", SupplyChainConfig{PostInstallPolicy: "yes"}, "postInstallPolicy"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.sc.Validate()
			if err == nil {
				t.Fatalf("want an error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error should name the offending field (%q); got: %v", c.wantSub, err)
			}
		})
	}
}

// TestValidateAllowedHost_WildcardHint 固定一条常见误解的处理方式：
// 把 `*` 写进 host。报错必须告诉用户该改用什么，而不是只说"非法"。
func TestValidateAllowedHost_WildcardHint(t *testing.T) {
	err := validateAllowedHost("github.*")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "allowlistRepos") {
		t.Errorf("the error should point at allowlistRepos as the way to express a wildcard; got: %v", err)
	}
}

// TestValidateRepoPattern_HostOnlyHint 固定另一条常见误解：
// 把"整个 host"写进 allowlistRepos。应引导到 allowedGitHosts。
func TestValidateRepoPattern_HostOnlyHint(t *testing.T) {
	err := validateRepoPattern("github.com")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "allowedGitHosts") {
		t.Errorf("the error should point at allowedGitHosts; got: %v", err)
	}
}
