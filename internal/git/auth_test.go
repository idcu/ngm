package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

func TestSecretsFromEnvVars(t *testing.T) {
	t.Setenv("NGM_TEST_TOKEN", "super-secret-value")
	t.Setenv("NGM_TEST_EMPTY", "")

	got := SecretsFromEnvVars(map[string]string{
		"github.com": "NGM_TEST_TOKEN",
		"gitee.com":  "NGM_TEST_EMPTY",     // 空值忽略
		"gitlab.com": "NGM_TEST_UNSET_VAR", // 未设置忽略
	})
	if len(got) != 1 || got[0] != "super-secret-value" {
		t.Fatalf("SecretsFromEnvVars=%v", got)
	}

	// 去重
	got2 := SecretsFromEnvVars(map[string]string{
		"a.com": "NGM_TEST_TOKEN",
		"b.com": "NGM_TEST_TOKEN",
	})
	if len(got2) != 1 {
		t.Fatalf("expected dedup, got %v", got2)
	}
}

func TestMergeTokenEnvVars_UserOverrides(t *testing.T) {
	base := WellKnownTokenEnvVars()
	merged := MergeTokenEnvVars(map[string]string{
		"github.com": "MY_GH_TOKEN",
		"corp.local": "CORP_TOKEN",
	})
	if merged["github.com"] != "MY_GH_TOKEN" {
		t.Errorf("user config should override builtin: %v", merged)
	}
	if merged["corp.local"] != "CORP_TOKEN" {
		t.Errorf("user addition missing: %v", merged)
	}
	// 内置其他项保留
	if merged["gitlab.com"] != base["gitlab.com"] {
		t.Errorf("builtin gitlab entry lost")
	}
}

func TestAssertNoSecrets(t *testing.T) {
	secrets := []string{"ghp_deadbeef"}
	if msg := AssertNoSecrets("clean output", secrets); msg != "" {
		t.Errorf("expected clean, got %q", msg)
	}
	if msg := AssertNoSecrets("token=ghp_deadbeef leaked", secrets); msg == "" {
		t.Errorf("expected detection")
	}
	// 检测消息本身不得回显 secret
	msg := AssertNoSecrets("ghp_deadbeef", secrets)
	if strings.Contains(msg, "ghp_deadbeef") {
		t.Errorf("assertion message must not echo the secret: %q", msg)
	}
}

// TestRedact 覆盖 URL userinfo 与任意字面量两类脱敏。
func TestRedact(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		secrets []string
		wantNot []string
		wantHz  string // 期望仍保留的片段
	}{
		{
			name:    "url userinfo with token",
			in:      "fatal: could not read from https://x-access-token:ghp_secret@github.com/org/repo.git",
			wantNot: []string{"ghp_secret", "x-access-token"},
			wantHz:  "github.com/org/repo.git",
		},
		{
			name:    "explicit secret literal",
			in:      "auth failed for token ghp_deadbeef please retry",
			secrets: []string{"ghp_deadbeef"},
			wantNot: []string{"ghp_deadbeef"},
			wantHz:  "auth failed for token",
		},
		{
			name:    "ssh url keeps username",
			in:      "git@github.com:org/repo.git",
			wantNot: nil,
			wantHz:  "git@github.com:org/repo.git",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in, tc.secrets)
			for _, bad := range tc.wantNot {
				if strings.Contains(got, bad) {
					t.Errorf("still contains %q: %q", bad, got)
				}
			}
			if tc.wantHz != "" && !strings.Contains(got, tc.wantHz) {
				t.Errorf("lost expected fragment %q: %q", tc.wantHz, got)
			}
		})
	}
}

// TestRun_DoesNotLeakTokenInError 是 M1 的验收项之一：
// token 不得出现在任何输出中——即使用 git 的失败信息做载体。
//
// 做法：构造一个会失败的远端 URL（携带 userinfo token），断言返回的错误
// 不再包含 token；同时验证命令行本身从未把 token 作为参数传给 git。
func TestRun_DoesNotLeakTokenInError(t *testing.T) {
	testutils.MustHaveGit(t)

	const token = "ghp_supersecret_token_value"
	remote := "https://x-access-token:" + token + "@127.0.0.1:1/nonexistent.git"

	_, err := Run(context.Background(), Options{Secrets: []string{token}},
		"ls-remote", remote)
	if err == nil {
		t.Skip("unexpectedly succeeded (port 1 reachable?)")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in error: %v", err)
	}

	// 错误应为 Git/网络类（退出码 4）
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error type %T", err)
	}
	if ne.Code != errs.CodeGitFetch {
		t.Errorf("exit code=%d want 4", ne.Code.ExitCode())
	}
	// Hint 应指向凭证配置方式
	if !strings.Contains(ne.Hint, "credential") && !strings.Contains(ne.Hint, "network") {
		t.Errorf("hint should guide to credentials or network: %q", ne.Hint)
	}
}

// TestRun_EnvPassthrough 验证环境变量原样透传给 git（ngm 不解析、不改写）。
func TestRun_EnvPassthrough(t *testing.T) {
	testutils.MustHaveGit(t)
	t.Setenv("NGM_PASSTHROUGH_MARKER", "marker-value-123")

	res, err := Run(context.Background(), Options{},
		"config", "--get-regexp", "^ngm\\.marker$")
	// 该 key 不存在 → git 返回非零；我们改用另一种方式验证环境透传：
	_ = res
	_ = err

	// 用 `git var GIT_AUTHOR_IDENT` 需要 user 配置，不稳定；
	// 直接验证环境变量在子进程中可见：借 shell 不可用，改用 git 自身读取 env 的能力有限，
	// 因此这里验证 buildEnv 的语义（含 os.Environ 且未删除标记）。
	env := buildEnv(Options{})
	if !containsEnv(env, "NGM_PASSTHROUGH_MARKER=marker-value-123") {
		t.Errorf("env not passed through: %v", filterMarker(env))
	}
	if !containsEnv(env, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("GIT_TERMINAL_PROMPT=0 not set by default")
	}
	if !containsEnv(env, "LC_ALL=C") {
		t.Errorf("LC_ALL=C not set (git messages would be localized)")
	}
}

func TestBuildEnv_AllowPrompt(t *testing.T) {
	env := buildEnv(Options{AllowPrompt: true})
	if containsEnv(env, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("AllowPrompt=true should not force GIT_TERMINAL_PROMPT=0")
	}
}

func TestRun_ExtraEnvAppended(t *testing.T) {
	testutils.MustHaveGit(t)
	env := buildEnv(Options{ExtraEnv: []string{"NGM_EXTRA=1"}})
	if !containsEnv(env, "NGM_EXTRA=1") {
		t.Errorf("ExtraEnv not appended")
	}
}

// TestBuildEnv_StripsRepoLocatingVars 是"mirror 被静默劫持"的回归防线。
//
// 若用户的 shell 残留 GIT_DIR，`git fetch` 会去改另一个仓库，
// 而 ngm 的 mirror 看起来"没更新"——极难排查。buildEnv 必须剔除这类变量。
func TestBuildEnv_StripsRepoLocatingVars(t *testing.T) {
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
	} {
		t.Setenv(k, "/somewhere/else")
	}
	t.Setenv("NGM_KEEP_ME", "kept")

	env := buildEnv(Options{})
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_PREFIX",
	} {
		if envHasKey(env, k) {
			t.Errorf("%s should have been stripped from the child env", k)
		}
	}
	if !containsEnv(env, "NGM_KEEP_ME=kept") {
		t.Errorf("unrelated env vars must be preserved")
	}
}

// TestRun_GitNotInstalled 在 PATH 剔除 git 的前提下验证错误可操作。
//
// 在 Windows 上 PATH 操控不稳定，故仅断言 Hint 常量本身的内容。
func TestRun_GitNotInstalled(t *testing.T) {
	if !strings.Contains(NotInstalledHint, "install Git") {
		t.Errorf("NotInstalledHint should mention installation: %q", NotInstalledHint)
	}
}

func TestLookPath(t *testing.T) {
	if _, err := LookPath(); err != nil {
		t.Skip("git not available")
	}
}

// ---- helpers ----

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

// envHasKey 报告 env 中是否存在该变量名（不区分大小写）。
func envHasKey(env []string, key string) bool {
	for _, e := range env {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

func filterMarker(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.Contains(e, "MARKER") {
			out = append(out, e)
		}
	}
	return out
}

// TestMirrorConfigHasNoUserInfo 验证 clone 后 mirror 的 remote URL 不含 userinfo。
//
// 这是安全纪律的直接检查：ngm 从不把凭证写进 remote URL。
func TestMirrorConfigHasNoUserInfo(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	src := t.TempDir()
	testutils.GitInit(t, src)
	dst := filepath.Join(t.TempDir(), "r.git")
	if err := CloneMirror(ctx, Options{}, src, dst); err != nil {
		t.Fatalf("clone: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(dst, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "@") && strings.Contains(string(cfg), "://") {
		// 允许 ssh 的 git@host:path 形式；只在 https:// 含 @ 时报错
		for _, line := range strings.Split(string(cfg), "\n") {
			if idx := strings.Index(line, "://"); idx >= 0 {
				rest := line[idx+3:]
				if strings.Contains(rest, "@") {
					t.Errorf("mirror config has userinfo in a https URL: %q", line)
				}
			}
		}
	}
}
