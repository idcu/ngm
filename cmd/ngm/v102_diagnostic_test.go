package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.102：公开 artifact 的脱敏判据。
//
// 上传的完整日志会对公开仓库可见，因此环境值、HOME/临时目录与常见 token 形态
// 必须先被替换。这条判据只测**脱敏脚本本身**；上传路径由 ci.yml 守住。
func TestV102SanitizeGoTestLog(t *testing.T) {
	py := ""
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			py = p
			break
		}
	}
	if py == "" {
		t.Skip("no python3/python on this machine — the sanitizer is not exercised here")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.log")
	out := filepath.Join(dir, "out.log")
	input := strings.Join([]string{
		"home=/home/runner",
		`user=C:\Users\runner`,
		"temp=/tmp/runner",
		"token=secret12345",
		"auth=Bearer abcdefghijklmnop",
		"gh=ghp_abcdefghijklmnop",
		"normal line",
	}, "\n") + "\n"
	if err := os.WriteFile(in, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot(t), "scripts", "sanitize-go-test-log.py")
	cmd := exec.Command(py, script, in, out)
	env := []string{}
	for _, kv := range os.Environ() {
		key := strings.SplitN(kv, "=", 2)[0]
		switch key {
		case "HOME", "USERPROFILE", "RUNNER_TEMP", "GITEE_TOKEN", "GITHUB_TOKEN":
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME=/home/runner",
		`USERPROFILE=C:\Users\runner`,
		"RUNNER_TEMP=/tmp/runner",
		"GITEE_TOKEN=secret12345",
		"GITHUB_TOKEN=ghp_abcdefghijklmnop",
	)
	cmd.Env = env
	if got, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sanitizer failed: %v\n%s", err, got)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, leaked := range []string{
		"/home/runner", `C:\Users\runner`, "/tmp/runner",
		"secret12345", "abcdefghijklmnop",
	} {
		if strings.Contains(text, leaked) {
			t.Errorf("sanitized log still contains %q:\n%s", leaked, text)
		}
	}
	for _, want := range []string{"<HOME>", "<USERPROFILE>", "<RUNNER_TEMP>", "<redacted>"} {
		if !strings.Contains(text, want) {
			t.Errorf("sanitized log should contain %q:\n%s", want, text)
		}
	}
}
