package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV136StaleLockExportIsRefused 钉住 ADR-046：过期 lock 的审计产物默认拒绝导出，
// 显式 --allow-stale 才放行。此前两条命令只写 stderr warning 就照常产出——
// 而 attest 的产物是要被签名的，签名后那份"描述过期状态却结构合法"的语句再也无法被发现。
func TestV136StaleLockExportIsRefused(t *testing.T) {
	testutils.MustHaveGit(t)

	for _, cmd := range []string{"sbom", "attest"} {
		t.Run(cmd, func(t *testing.T) {
			isolateUserEnv(t)
			p := newProject(t)
			seedDep(t, p)

			// 把 ngm.json 的 ref 改掉（不重装），制造 lock 过期。
			path := filepath.Join(p, "ngm.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var mf config.ProjectFile
			if err := json.Unmarshal(raw, &mf); err != nil {
				t.Fatal(err)
			}
			if len(mf.Dependencies) != 1 {
				t.Fatalf("fixture should have exactly one dependency, got %d", len(mf.Dependencies))
			}
			mf.Dependencies[0].Ref = "v9.9.9"
			edited, err := json.MarshalIndent(mf, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(edited, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}

			// 默认：exit 3、stdout 无产物、消息点名刷新 lock
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			code := dispatch([]string{cmd, "--dir=" + p}, stdout, stderr)
			if code != 3 {
				t.Fatalf("%s with stale lock exit=%d, want 3\nstdout:\n%s\nstderr:\n%s", cmd, code, stdout, stderr)
			}
			if strings.TrimSpace(stdout.String()) != "" {
				t.Errorf("%s must not emit an artifact on refusal, got %q", cmd, stdout.String())
			}
			if !strings.Contains(stderr.String(), "ngm install") {
				t.Errorf("refusal must give the next step: %s", stderr.String())
			}

			// 显式 --allow-stale：产出、且 stderr 标明原因
			stdout.Reset()
			stderr.Reset()
			code = dispatch([]string{cmd, "--dir=" + p, "--allow-stale"}, stdout, stderr)
			if code != 0 {
				t.Fatalf("%s --allow-stale exit=%d\nstderr:\n%s", cmd, code, stderr)
			}
			if strings.TrimSpace(stdout.String()) == "" {
				t.Errorf("%s --allow-stale should emit the artifact", cmd)
			}
			if !strings.Contains(stderr.String(), "--allow-stale") {
				t.Errorf("--allow-stale export must still say why: %s", stderr.String())
			}
		})
	}
}
