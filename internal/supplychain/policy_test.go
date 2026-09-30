package supplychain

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

func boolPtr(b bool) *bool { return &b }

// TestPolicy_CheckRepo 覆盖判定语义：两个白名单字段各自生效、同时配置时是 AND。
func TestPolicy_CheckRepo(t *testing.T) {
	t.Run("no policy allows everything", func(t *testing.T) {
		p, err := FromConfig(nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.IsEmpty() != true {
			t.Error("a nil config must yield an empty (inactive) policy")
		}
		if err := p.CheckRepo("gitlab.example.com", "anyone/anything"); err != nil {
			t.Errorf("an unconfigured policy must not gate anything; got: %v", err)
		}
	})

	t.Run("allowedGitHosts rejects other hosts", func(t *testing.T) {
		p, err := FromConfig(&config.SupplyChainConfig{AllowedGitHosts: []string{"github.com"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.CheckRepo("github.com", "o/r"); err != nil {
			t.Errorf("listed host must pass: %v", err)
		}
		err = p.CheckRepo("evil.example.com", "o/r")
		if err == nil {
			t.Fatal("unlisted host must be rejected")
		}
		if code := errs.ExitCode(err); code != 3 {
			t.Errorf("a policy rejection must be exit 3 (config/policy error), got %d", code)
		}
		if !strings.Contains(err.Error(), "allowedGitHosts") {
			t.Errorf("error should name allowedGitHosts; got: %v", err)
		}
	})

	t.Run("host matching is case-insensitive", func(t *testing.T) {
		p, err := FromConfig(&config.SupplyChainConfig{AllowedGitHosts: []string{"GitHub.com"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.CheckRepo("github.com", "o/r"); err != nil {
			t.Errorf("host matching must ignore case; got: %v", err)
		}
	})

	t.Run("allowlistRepos rejects unlisted repos", func(t *testing.T) {
		p, err := FromConfig(&config.SupplyChainConfig{AllowlistRepos: []string{"github.com/my-org/*"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.CheckRepo("github.com", "my-org/utils"); err != nil {
			t.Errorf("listed repo must pass: %v", err)
		}
		err = p.CheckRepo("github.com", "other/utils")
		if err == nil {
			t.Fatal("unlisted repo must be rejected")
		}
		// 拒绝必须是可被 CI 消费的 NgmError：错误码决定退出码，Hint 给出可照抄的修法。
		// 注意 Hint **不在** Error() 里（正文由 errs.FormatHuman 渲染），因此按字段断言——
		// 这里最初写成 strings.Contains(err.Error(), ...) 就漏掉了 Hint。
		var ne *errs.NgmError
		if !errors.As(err, &ne) {
			t.Fatalf("policy rejections must be *errs.NgmError; got %T", err)
		}
		if ne.Code.ExitCode() != 3 {
			t.Errorf("exit code = %d, want 3 (config/policy error)", ne.Code.ExitCode())
		}
		if !strings.Contains(ne.Message, "allowlistRepos") {
			t.Errorf("message should name allowlistRepos; got %q", ne.Message)
		}
		if !strings.Contains(ne.Hint, "github.com/other/*") {
			t.Errorf("hint should suggest the pattern to add; got %q", ne.Hint)
		}
	})

	t.Run("both fields are ANDed", func(t *testing.T) {
		p, err := FromConfig(&config.SupplyChainConfig{
			AllowedGitHosts: []string{"github.com"},
			AllowlistRepos:  []string{"github.com/my-org/*"},
		})
		if err != nil {
			t.Fatal(err)
		}
		// host 通过但 repo 不通过 → 仍然拒绝
		if err := p.CheckRepo("github.com", "other/utils"); err == nil {
			t.Error("repo not in allowlist must be rejected even when the host is allowed")
		}
		// 两者都通过
		if err := p.CheckRepo("github.com", "my-org/utils"); err != nil {
			t.Errorf("both rules satisfied must pass; got: %v", err)
		}
		// host 不通过 → 拒绝（此时可能同时违反两条，先报 host）
		err = p.CheckRepo("evil.example.com", "my-org/utils")
		if err == nil {
			t.Fatal("host not in allowedGitHosts must be rejected")
		}
		if !strings.Contains(err.Error(), "allowedGitHosts") {
			t.Errorf("host violation should be reported first; got: %v", err)
		}
	})
}

// TestPolicy_PostInstallIsActive 固定 ADR-009 决策 5 的**修订后**语义。
//
// 这条测试的来历值得记一笔：v0.2 它断言"永远不执行"，作用是在**代码层面留痕**——
// 谁接上执行入口而没修订 ADR-009，这里就会红并指向那篇 ADR。v0.3 沙箱就位后
// 决策 5 被修订（先改文档再改代码），于是这条测试也随之改写。
func TestPolicy_PostInstallIsActive(t *testing.T) {
	cases := map[string]bool{
		"":       false, // 未配置 → deny
		"deny":   false,
		"prompt": false, // 非交互工具："询问"没有真实形态，语义是不自动执行
		"allow":  true,
	}
	for v, want := range cases {
		p, err := FromConfig(&config.SupplyChainConfig{PostInstallPolicy: v})
		if err != nil {
			t.Fatal(err)
		}
		if got := p.PostInstallIsActive(); got != want {
			t.Errorf("PostInstallPolicy=%q: active=%v, want %v (ADR-009 决策 5，v0.3 修订)", v, got, want)
		}
		if v == "" && p.PostInstallPolicy() != "deny" {
			t.Errorf("an unset postInstallPolicy must read back as deny, got %q", p.PostInstallPolicy())
		}
	}
}

// TestPolicy_MinimumReleaseAge / VerifyOnLock：取值归一化。
func TestPolicy_MinimumReleaseAgeAndVerifyOnLock(t *testing.T) {
	p, err := FromConfig(&config.SupplyChainConfig{
		MinimumReleaseAge: "PT12H",
		VerifyOnLock:      boolPtr(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.MinimumReleaseAge().Hours() != 12 {
		t.Errorf("minimumReleaseAge = %v, want 12h", p.MinimumReleaseAge())
	}
	if !p.VerifyOnLock() {
		t.Error("verifyOnLock should be true")
	}
	if p.IsEmpty() {
		t.Error("a policy with a release-age gate is not empty")
	}
}

func TestPolicy_FromConfigRejectsInvalidDuration(t *testing.T) {
	if _, err := FromConfig(&config.SupplyChainConfig{MinimumReleaseAge: "P1M"}); err == nil {
		t.Fatal("FromConfig must reject a duration that ParseISODuration rejects")
	}
}
