package security

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

func mustPolicy(t *testing.T, allow, deny []string) *Policy {
	t.Helper()
	pol, err := NewPolicy(allow, deny, "~/.ngm/config.json")
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return pol
}

func TestParse(t *testing.T) {
	cases := []struct {
		in       string
		want     Permission
		wantFail bool
	}{
		{in: "net:github.com", want: Permission{Namespace: Net, Target: "github.com"}},
		{in: " run:esbuild ", want: Permission{Namespace: Run, Target: "esbuild"}},
		{in: "ENV:GITHUB_TOKEN", want: Permission{Namespace: Env, Target: "GITHUB_TOKEN"}},
		{in: "write:ngm.vendor", want: Permission{Namespace: Write, Target: "ngm.vendor"}},
		// 未知命名空间必须报错：静默忽略会让用户以为权限生效了
		{in: "fetch:github.com", wantFail: true},
		{in: "net", wantFail: true},
		{in: "net:", wantFail: true},
		{in: "", wantFail: true},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if tc.wantFail {
			if err == nil {
				t.Errorf("Parse(%q) should fail", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}

	// 未知命名空间的提示要列出可选项，否则用户只能猜
	_, err := Parse("fetch:github.com")
	if err == nil || !strings.Contains(errs.FormatHuman(err), "net") {
		t.Errorf("the error should list the namespaces: %v", err)
	}
}

// TestDefaultTier 固定安全模型的那张三档表。
//
// 这张表是权限模型的**全部默认语义**，改它等于改默认安全姿态，
// 因此逐条钉住而不是只测一两个代表。
func TestDefaultTier(t *testing.T) {
	cases := []struct {
		p    Permission
		want Tier
	}{
		{Permission{Read, "ngm.vendor"}, TierAllowed},
		{Permission{Write, "ngm.vendor"}, TierAllowed},
		{Permission{Write, "ngm.lock"}, TierAllowed},
		{Permission{Run, "git"}, TierAllowed},
		{Permission{Env, "GITHUB_TOKEN"}, TierAllowed}, // 只透传、不解析，默认不拦
		{Permission{Net, "github.com"}, TierConfigurable},
		{Permission{Net, "example.com"}, TierConfigurable},
		{Permission{Run, "esbuild"}, TierConfigurable},
		{Permission{Run, "tsc"}, TierConfigurable},
		{Permission{Run, "node"}, TierConfigurable},
	}
	for _, tc := range cases {
		if got := DefaultTier(tc.p); got != tc.want {
			t.Errorf("DefaultTier(%s) = %v, want %v", tc.p, got, tc.want)
		}
	}
}

// 权限矩阵：允许 / 需配置 / 默认禁止三档的实际判定。
func TestPolicy_Matrix(t *testing.T) {
	t.Run("allowed by default", func(t *testing.T) {
		pol := mustPolicy(t, nil, nil)
		for _, p := range []Permission{
			{Read, "ngm.vendor"}, {Write, "ngm.lock"}, {Run, "git"}, {Env, "GITHUB_TOKEN"},
		} {
			if err := pol.Check(p); err != nil {
				t.Errorf("%s should be allowed by default: %v", p, err)
			}
		}
	})

	t.Run("needs configuration", func(t *testing.T) {
		pol := mustPolicy(t, nil, nil)
		for _, p := range []Permission{{Net, "github.com"}, {Run, "esbuild"}} {
			if err := pol.Check(p); err == nil {
				t.Errorf("%s must be refused until it is configured", p)
			}
		}
	})

	t.Run("configured permission is granted", func(t *testing.T) {
		pol := mustPolicy(t, []string{"net:github.com", "run:esbuild"}, nil)
		if err := pol.CheckNet("github.com"); err != nil {
			t.Errorf("net:github.com was allowed: %v", err)
		}
		if err := pol.CheckRun("esbuild"); err != nil {
			t.Errorf("run:esbuild was allowed: %v", err)
		}
		// 允许的是**那一条**，不是整个命名空间
		if err := pol.CheckNet("gitee.com"); err == nil {
			t.Error("allowing github.com must not allow gitee.com")
		}
	})

	// deny 优先于 allow 是唯一不会让"我明明禁了"落空的选择。
	t.Run("deny wins over allow", func(t *testing.T) {
		pol := mustPolicy(t, []string{"env:GITHUB_TOKEN"}, []string{"env:GITHUB_TOKEN"})
		if allowed := pol.Allows(Permission{Env, "GITHUB_TOKEN"}); allowed {
			t.Error("a permission present in both lists must be denied")
		}
		// 提示要说清是 deny 造成的，而不是让他去改 allow
		err := pol.Check(Permission{Env, "GITHUB_TOKEN"})
		if err == nil || !strings.Contains(errs.FormatHuman(err), "deny") {
			t.Errorf("the hint should point at the deny list: %v", err)
		}
	})

	// 没有策略时按**默认档位**判定，而不是一律放行——
	// 否则"配置读不出来"会变成绕过门禁的通道。
	t.Run("a nil policy degrades to the default tier, not to allow-all", func(t *testing.T) {
		var pol *Policy
		if err := pol.Check(Permission{Run, "git"}); err != nil {
			t.Errorf("run:git is allowed by default: %v", err)
		}
		if err := pol.CheckNet("github.com"); err == nil {
			t.Error("without a policy, a network access must still be refused")
		}
	})
}

// 违例提示必须可操作：指出改哪个文件的哪一行。
func TestPolicy_HintIsActionable(t *testing.T) {
	pol := mustPolicy(t, nil, nil)
	err := pol.CheckNet("github.com")
	if err == nil {
		t.Fatal("expected a denial")
	}
	shown := errs.FormatHuman(err)
	for _, want := range []string{`"net:github.com"`, "permissions.allow", "~/.ngm/config.json"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the hint should contain %q:\n%s", want, shown)
		}
	}
	// 退出的语义是"配置没写"，属配置类：exit 3
	if got := errs.ExitCode(err); got != 3 {
		t.Errorf("a permission denial must exit 3, got %d: %v", got, err)
	}
}

func TestDeniedEnvVars(t *testing.T) {
	pol := mustPolicy(t, nil, []string{"env:GITHUB_TOKEN", "env:SSH_AUTH_SOCK", "run:npm"})
	got := pol.DeniedEnvVars()
	if len(got) != 2 || got[0] != "GITHUB_TOKEN" || got[1] != "SSH_AUTH_SOCK" {
		t.Errorf("DeniedEnvVars = %v (only env: entries, sorted)", got)
	}
	// 不是环境变量的拒绝项不该混进来
	for _, name := range got {
		if name == "npm" {
			t.Errorf("run:npm is not an env var: %v", got)
		}
	}

	var nilPol *Policy
	if names := nilPol.DeniedEnvVars(); len(names) != 0 {
		t.Errorf("a nil policy denies nothing: %v", names)
	}
}

func TestPolicy_Conflicts(t *testing.T) {
	pol := mustPolicy(t, []string{"net:github.com", "run:git"}, []string{"net:github.com"})
	got := pol.Conflicts()
	if len(got) != 1 || got[0] != "net:github.com" {
		t.Errorf("Conflicts = %v", got)
	}
}

func TestNewPolicy_RejectsBadEntries(t *testing.T) {
	// 配置里写错一条权限时整个策略都不成立——不能"跳过坏的那条"，
	// 否则用户会以为其余的都生效了。
	if _, err := NewPolicy([]string{"net:github.com", "bogus:1"}, nil, "src"); err == nil {
		t.Error("an unknown namespace must fail the whole policy")
	}
	if _, err := NewPolicy(nil, []string{"net"}, "src"); err == nil {
		t.Error("a missing target must fail the whole policy")
	}
}
