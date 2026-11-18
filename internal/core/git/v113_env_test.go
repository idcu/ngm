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

// 判据（2026-11-18 B2）：GIT_EXEC_PATH / LD_PRELOAD / DYLD_INSERT_LIBRARIES
// 能改变 git 实际执行的程序，必须与 GIT_SSH_COMMAND 同等待遇。
func TestV132BuildEnvStripsExecAndLoaderVars(t *testing.T) {
	for _, name := range []string{
		"GIT_EXEC_PATH", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES",
		"git_exec_path", "ld_preload", // Windows 大小写不敏感
	} {
		t.Setenv(name, "evil-value")
	}
	t.Setenv("GITHUB_TOKEN", "keep-me")

	env := buildEnv(Options{})
	names := map[string]string{}
	for _, e := range env {
		name, val, _ := strings.Cut(e, "=")
		names[strings.ToUpper(name)] = val
	}
	for _, banned := range []string{"GIT_EXEC_PATH", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES"} {
		if _, ok := names[banned]; ok {
			t.Errorf("buildEnv leaked %s", banned)
		}
	}
	if names["GITHUB_TOKEN"] != "keep-me" {
		t.Error("credential variables must still be inherited")
	}
}

// 判据（2026-11-18 B2）：ExtraEnv 走与 os.Environ() 同一套过滤，"追加"
// 不能成为绕过 deny 与 hijack 表的通道。同时确认内部安全开关仍被保留。
func TestV132ExtraEnvCannotResurrectDeniedVar(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")

	env := envMap(buildEnv(Options{
		Policy: denyPolicy{denied: []string{"github_token"}},
		ExtraEnv: []string{
			"GITHUB_TOKEN=smuggled-back",
			"GIT_SSH_COMMAND=evil",
			"GIT_EXEC_PATH=/evil",
			"GIT_CONFIG_PARAMETERS=x",
			"GIT_NO_LAZY_FETCH=1", // 内部开关，必须保留
		},
	}))
	for _, banned := range []string{"GITHUB_TOKEN", "GIT_SSH_COMMAND", "GIT_EXEC_PATH", "GIT_CONFIG_PARAMETERS"} {
		if _, ok := env[banned]; ok {
			t.Errorf("ExtraEnv resurrected %s", banned)
		}
	}
	if env["GIT_NO_LAZY_FETCH"] != "1" {
		t.Error("legitimate internal ExtraEnv must survive the filter")
	}
}
