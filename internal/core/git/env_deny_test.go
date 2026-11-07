package git

import (
	"strings"
	"testing"
)

type denyPolicy struct{ denied []string }

func (p denyPolicy) CheckRun(string) error   { return nil }
func (p denyPolicy) CheckNet(string) error   { return nil }
func (p denyPolicy) DeniedEnvVars() []string { return p.denied }

func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		name, val, _ := strings.Cut(e, "=")
		out[strings.ToUpper(name)] = val // 后出现的覆盖先出现的（os/exec 的去重语义）
	}
	return out
}

// 逃逸形状：Windows 环境变量名大小写不敏感、GIT_CONFIG_KEY_n / VALUE_n 是
// **前缀族**而不是固定几个名字。只按大写名字精确匹配会漏掉两类继承。
func TestBuildEnvStripsHijackShapes(t *testing.T) {
	for _, name := range []string{
		"git_ssh_command",
		"git_config_parameters",
		"git_config_key_99",
		"git_config_value_12",
		"git_dir",
		"git_work_tree",
		"git_index_file",
		"git_common_dir",
	} {
		t.Setenv(name, "evil")
	}

	got := envMap(buildEnv(Options{}))
	for _, banned := range []string{
		"GIT_SSH_COMMAND", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_99", "GIT_CONFIG_VALUE_12",
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	} {
		if _, ok := got[banned]; ok {
			t.Errorf("buildEnv leaked %s through a case/prefix variation", banned)
		}
	}
}

// 策略的 env:<NAME> 拒绝是**大小写不敏感**的：用户在 ngm.json 里写小写，
// 而进程环境里是 GITHUB_TOKEN，两者必须能对上。仓库定位变量同理。
func TestBuildEnvHonorsPolicyDeniedEnvVars(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("GIT_OBJECT_DIRECTORY", "/somewhere/else")

	got := envMap(buildEnv(Options{Policy: denyPolicy{denied: []string{"github_token", "git_object_directory"}}}))
	if _, ok := got["GITHUB_TOKEN"]; ok {
		t.Error("a policy-denied credential must be removed from the child env")
	}
	if _, ok := got["GIT_OBJECT_DIRECTORY"]; ok {
		t.Error("a policy-denied env var must be removed even when it is also a locator")
	}

	// 未拒绝的变量仍然继承（凭证通道不能因为这条机制被整体掐掉）。
	t.Setenv("GITEE_TOKEN", "keep-me")
	got = envMap(buildEnv(Options{Policy: denyPolicy{denied: []string{"github_token"}}}))
	if got["GITEE_TOKEN"] != "keep-me" {
		t.Error("unrelated credentials must still be inherited")
	}
}

// 提示开关与 locale：CI 必须非交互（否则无 tty 会挂起）；locale 被钉成 C
// 以免 git 的本地化文本破坏错误解析。后出现的值生效。
func TestBuildEnvPromptAndLocale(t *testing.T) {
	got := envMap(buildEnv(Options{}))
	if got["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT=%q, want 0 by default", got["GIT_TERMINAL_PROMPT"])
	}
	if got["LC_ALL"] != "C" || got["LANG"] != "C" {
		t.Errorf("locale must be pinned to C: LC_ALL=%q LANG=%q", got["LC_ALL"], got["LANG"])
	}

	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if got := envMap(buildEnv(Options{})); got["LC_ALL"] != "C" {
		t.Errorf("inherited LC_ALL=%q must lose to the pinned value", got["LC_ALL"])
	}

	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	if got := envMap(buildEnv(Options{})); got["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("the pinned non-interactive value must win over the inherited one, got %q", got["GIT_TERMINAL_PROMPT"])
	}
	if got := envMap(buildEnv(Options{AllowPrompt: true}))["GIT_TERMINAL_PROMPT"]; got != "1" {
		t.Errorf("AllowPrompt=true must leave the inherited value alone, got %q", got)
	}
}

// ExtraEnv 是**显式**通道，追加在过滤之后；调用方（ngm 自己的代码）负责任的变量
// 才能从这里进入子进程。这条钉住"它确实优先于继承值"，避免有人把过滤器反过来用。
func TestBuildEnvExtraEnvIsExplicitAndWins(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	got := envMap(buildEnv(Options{ExtraEnv: []string{"GIT_TERMINAL_PROMPT=0", "NGM_TEST_FLAG=on"}}))
	if got["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("explicit ExtraEnv must win, got %q", got["GIT_TERMINAL_PROMPT"])
	}
	if got["NGM_TEST_FLAG"] != "on" {
		t.Error("explicit ExtraEnv must reach the child")
	}
}
