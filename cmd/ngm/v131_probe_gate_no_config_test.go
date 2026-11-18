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

// 判据：`~/.ngm/config.json` **完全不存在**时，探测闸门仍然必须注册。
//
// 这是 2026-11-18 S0 的负向判据。此前 `cat.WithProbeGate` 被放进
// `if env.Policy != nil`，于是"从未配过权限"的用户反而比"配过权限"的用户
// 更宽松：`engines list` / `validate` 会真实 spawn 被克隆仓库
// `ngm.engines.json` 声明的程序，环境变量全继承。
//
// isolateUserEnv 在没有 AllowEngines 声明时**不写**全局配置（见 testenv_test.go），
// 这正是本用例要的场景。禁止 t.Skip。
func TestV131NoGlobalConfigDoesNotSpawnProbe(t *testing.T) {
	home := isolateUserEnv(t)
	// 前置条件：确认这份 HOME 里确实没有全局配置文件；否则本用例根本没测到边界。
	cfg := config.EffectiveGlobalPath(home)
	if _, err := os.Stat(cfg); err == nil {
		t.Fatalf("test precondition failed: global config exists at %s", cfg)
	}

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := m6Project(t, `{"bundle": "fake"}`)
	m6Catalog(t, proj, fake, "bundle")

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{name: "list", args: []string{"engines", "list"}, code: 0},
		{name: "validate", args: []string{"engines", "validate"}, code: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{}, tc.args...)
			args = append(args, "--dir="+proj, "--json")
			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			if code != tc.code {
				t.Fatalf("%s exit = %d, want %d; stdout:\n%s\nstderr:\n%s",
					tc.name, code, tc.code, out.String(), errb.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("%s spawned an engine with no global config", tc.name)
			}

			// 两路输出都必须点名要授予的权限，而不是把"没问"说成"没装"。
			text := out.String()
			if tc.name == "list" {
				var rows []struct {
					Name        string `json:"name"`
					ProbeDenied bool   `json:"probeDenied"`
					RunTarget   string `json:"runTarget"`
					Version     string `json:"version"`
				}
				if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
					t.Fatalf("invalid json: %v\n%s", err, text)
				}
				var found bool
				for _, r := range rows {
					if r.Name == "fake" {
						found = true
						if !r.ProbeDenied || r.RunTarget != "fake-engine" || r.Version != "" {
							t.Fatalf("row = %+v; want probeDenied=true, runTarget=fake-engine, empty version", r)
						}
					}
				}
				if !found {
					t.Fatalf("engine `fake` missing from report:\n%s", text)
				}
			} else {
				var rep struct {
					OK     bool `json:"ok"`
					Issues []struct {
						Kind    string `json:"kind"`
						Message string `json:"message"`
					} `json:"issues"`
				}
				if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
					t.Fatalf("invalid json: %v\n%s", err, text)
				}
				if rep.OK {
					t.Fatalf("ok must be false when a probe was refused:\n%s", text)
				}
				var found bool
				for _, is := range rep.Issues {
					if is.Kind == "probe-denied" && strings.Contains(is.Message, "run:fake-engine") {
						found = true
					}
				}
				if !found {
					t.Fatalf("refused probe must be an explicit issue:\n%s", text)
				}
			}
		})
	}
}
