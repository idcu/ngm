package git

import (
	"strings"
	"testing"
)

// 判据（S2/C3）：能执行另一段程序、或覆盖 ngm 显式参数的环境变量不得被继承。
func TestV113BuildEnvStripsHijackVars(t *testing.T) {
	for _, name := range []string{
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_ASKPASS", "SSH_ASKPASS",
		"GIT_EXTERNAL_DIFF", "GIT_PROXY_COMMAND", "GIT_PAGER",
	} {
		t.Setenv(name, "evil-value")
	}
	// 凭证本身仍必须继承：ngm 不解析它，只让 git 自己用。
	t.Setenv("GITHUB_TOKEN", "keep-me")

	env := buildEnv(Options{})
	names := map[string]string{}
	for _, e := range env {
		name, val, _ := strings.Cut(e, "=")
		names[strings.ToUpper(name)] = val
	}
	for _, banned := range []string{
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_ASKPASS", "SSH_ASKPASS",
		"GIT_EXTERNAL_DIFF", "GIT_PROXY_COMMAND", "GIT_PAGER",
	} {
		if _, ok := names[banned]; ok {
			t.Errorf("buildEnv leaked %s", banned)
		}
	}
	if names["GITHUB_TOKEN"] != "keep-me" {
		t.Errorf("credential variables must still be inherited: %v", names["GITHUB_TOKEN"])
	}
	if names["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("the non-interactive default must remain")
	}
}

// 判据（C3）：`git clone` 只接收有形状的地址；helper 形式与选项形态都拒绝。
func TestV113ValidateRemoteURL(t *testing.T) {
	ok := []string{
		"https://github.com/org/repo.git",
		"ssh://git@github.com/org/repo.git",
		"git://github.com/org/repo.git",
		"file:///srv/git/repo.git",
		"git@github.com:org/repo.git",
		`C:\repos\repo.git`,
		"/srv/git/repo.git",
		"./fixtures/repo.git",
	}
	for _, s := range ok {
		if err := ValidateRemoteURL(s); err != nil {
			t.Errorf("%q should be accepted: %v", s, err)
		}
	}

	bad := []string{
		"",
		"-c core.sshCommand=evil",
		"ext::sh -c 'touch /tmp/pwn'",
		"fd::/tmp/sock",
		"ftp://example.com/repo.git",
		"custom://example.com/repo.git",
	}
	for _, s := range bad {
		if err := ValidateRemoteURL(s); err == nil {
			t.Errorf("%q should be refused", s)
		}
	}
}
