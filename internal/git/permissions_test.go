package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/security"
)

func policyFrom(t *testing.T, allow, deny []string) *security.Policy {
	t.Helper()
	pol, err := security.NewPolicy(allow, deny, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}
	return pol
}

// TestBuildEnv_StripsDeniedVariables 固定 `deny: ["env:X"]` 的施加方式：
// 从子进程环境里**剔除**，而不是读取它的值再置空。
//
// 这个区别关系到 v0.1 就承诺的性质——ngm 不读取、不解析、不缓存 token。
// 如果为了"禁用它"而先读出它的值，那条承诺就破了。
func TestBuildEnv_StripsDeniedVariables(t *testing.T) {
	t.Setenv("NGM_KEEP_ME", "1")
	t.Setenv("GITHUB_TOKEN", "ghp_should_not_be_passed")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")

	t.Run("denied variables are not passed", func(t *testing.T) {
		pol := policyFrom(t, nil, []string{"env:GITHUB_TOKEN", "env:SSH_AUTH_SOCK"})
		env := buildEnv(Options{Policy: pol})

		if containsEnv(env, "GITHUB_TOKEN=ghp_should_not_be_passed") {
			t.Error("a denied token was passed to the child process")
		}
		if containsEnv(env, "SSH_AUTH_SOCK=/tmp/agent.sock") {
			t.Error("a denied variable was passed to the child process")
		}
		// 只剔除被拒绝的那些：把整个环境清掉会让 git 在别处莫名其妙地失败
		if !containsEnv(env, "NGM_KEEP_ME=1") {
			t.Error("unrelated variables must survive")
		}
		// 其它纪律不受影响
		if !containsEnv(env, "GIT_TERMINAL_PROMPT=0") || !containsEnv(env, "LC_ALL=C") {
			t.Error("the existing env discipline must be preserved")
		}
	})

	t.Run("name matching is case insensitive", func(t *testing.T) {
		pol := policyFrom(t, nil, []string{"env:github_token"})
		if containsEnv(buildEnv(Options{Policy: pol}), "GITHUB_TOKEN=ghp_should_not_be_passed") {
			t.Error("Windows 环境变量名不区分大小写，拒绝匹配也应如此")
		}
	})

	t.Run("without a policy nothing is stripped", func(t *testing.T) {
		env := buildEnv(Options{})
		if !containsEnv(env, "GITHUB_TOKEN=ghp_should_not_be_passed") {
			t.Error("no policy means the environment is passed through unchanged")
		}
	})
}

// `run:git` 的拒绝必须在**启动之前**生效：否则"我的配置不允许执行 git"
// 会以"git 失败了"的样子出现，用户会去查网络或凭证。
func TestRun_DeniedRunGitDoesNotStart(t *testing.T) {
	pol := policyFrom(t, nil, []string{"run:git"})

	_, err := Run(context.Background(), Options{Policy: pol}, "version")
	if err == nil {
		t.Fatal("running git must be refused")
	}
	if got := errs.ExitCode(err); got != 3 {
		t.Errorf("exit=%d, want 3", got)
	}
	if !strings.Contains(errs.FormatHuman(err), "run:git") {
		t.Errorf("the error should name the permission:\n%s", errs.FormatHuman(err))
	}
}

// 默认档位下 git 是被允许的：否则 ngm 的基本功能（mirror / cat-file / ls-remote）
// 会在没有任何配置的项目里全部失效。
func TestRun_GitIsAllowedByDefault(t *testing.T) {
	pol := policyFrom(t, nil, nil)
	if err := pol.CheckRun("git"); err != nil {
		t.Errorf("run:git must be allowed by default: %v", err)
	}
}
