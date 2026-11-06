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

// writeDenyConfig 把 `deny: ["run:<target>"]` 写进隔离 HOME 的全局配置。
//
// isolateUserEnv 会先按 testutils.WriteUserConfig 写入 allow 列表；这里覆盖它，
// 是刻意的：本组用例要证明**deny 优先**，而不是"没授权"。
func writeDenyConfig(t *testing.T, home, target string) {
	t.Helper()
	path := config.EffectiveGlobalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"permissions":{"deny":["run:` + target + `"]}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 判据：`--dry-run` 承诺 "exit without spawning anything"，因此
// 引擎的 `--version` 探测也不能发生。
//
// 这是 S0-1 的负向判据：probe.exe 收到 `--version` 会写 marker；
// dry-run 之后 marker 必须不存在。测试不允许 t.Skip——它守的就是边界本身。
func TestV109DryRunSpawnsNothing(t *testing.T) {
	home := isolateUserEnv(t)
	_ = home

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := m6Project(t, `{"bundle": "fake"}`)
	m6Catalog(t, proj, fake, "bundle")

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	code := dispatch([]string{"build", "src/index.ts", "--dry-run", "--dir=" + proj}, &out, &errb)
	if code != 0 {
		t.Fatalf("dry-run exit = %d, stderr:\n%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "would bundle") {
		t.Fatalf("dry-run did not render the plan:\n%s", out.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("dry-run spawned the engine: %s exists (output:\n%s)", marker, out.String())
	}
}

// 判据：`engines list` 对未授权的引擎**不探测**，并如实标明
// "not probed"，而不是把"没问"说成"没装"。
//
// 退出码保持 0：list 的职责是呈现事实，不是中断；把它报成 3 会让
// 每个没配 `run:` 的用户在默认档位下拿到一条红。
func TestV109EnginesListDeniedDoesNotSpawn(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		name := "human"
		if asJSON {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			home := isolateUserEnv(t)
			fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
			proj := m6Project(t, `{"bundle": "fake"}`)
			m6Catalog(t, proj, fake, "bundle")
			writeDenyConfig(t, home, "fake-engine")

			marker := filepath.Join(t.TempDir(), "probed.txt")
			t.Setenv("FAKE_VERSION_MARKER", marker)

			args := []string{"engines", "list", "--dir=" + proj}
			if asJSON {
				args = append(args, "--json")
			}
			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			if code != 0 {
				t.Fatalf("engines list exit = %d, stderr:\n%s", code, errb.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("engines list spawned a denied engine")
			}

			if asJSON {
				var rep []struct {
					Name        string `json:"name"`
					ProbeDenied bool   `json:"probeDenied"`
					RunTarget   string `json:"runTarget"`
					Version     string `json:"version"`
				}
				if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
					t.Fatalf("invalid json: %v\n%s", err, out.String())
				}
				var found bool
				for _, e := range rep {
					if e.Name == "fake" {
						found = true
						if !e.ProbeDenied || e.RunTarget != "fake-engine" || e.Version != "" {
							t.Fatalf("row = %+v; want probeDenied=true, runTarget=fake-engine, empty version", e)
						}
					}
				}
				if !found {
					t.Fatalf("engine `fake` missing from report:\n%s", out.String())
				}
			} else if !strings.Contains(out.String(), "fake-engine denied") {
				t.Fatalf("human output did not name the denied target:\n%s", out.String())
			}
		})
	}
}

// 判据（fail-closed）：`engines validate` 的职责是**给结论**。探针被
// `run:<target>` 拒绝时，版本无法核对——"拿不到结论"不能报成功，因此
// 必须显式产出 skipped/unknown 条目（`probe-denied`）并计入摘要，退出 3。
//
// 与 `engines list` 的区别是本用例的另一半：list 对同一场景退出 0
// （见 TestV109EnginesListDeniedDoesNotSpawn），因为它只负责呈现事实。
func TestV109EnginesValidateDeniedIsFailClosed(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		name := "human"
		if asJSON {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			home := isolateUserEnv(t)
			fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
			proj := m6Project(t, `{"bundle": "fake"}`)
			m6Catalog(t, proj, fake, "bundle")
			writeDenyConfig(t, home, "fake-engine")

			marker := filepath.Join(t.TempDir(), "probed.txt")
			t.Setenv("FAKE_VERSION_MARKER", marker)

			args := []string{"engines", "validate", "--dir=" + proj}
			if asJSON {
				args = append(args, "--json")
			}
			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			if code != 3 {
				t.Fatalf("engines validate exit = %d, want 3 (fail-closed); stdout:\n%s\nstderr:\n%s",
					code, out.String(), errb.String())
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("engines validate spawned a denied engine")
			}

			if asJSON {
				var rep struct {
					OK     bool `json:"ok"`
					Issues []struct {
						Entry       string `json:"entry"`
						Kind        string `json:"kind"`
						Message     string `json:"message"`
						Remediation string `json:"remediation"`
					} `json:"issues"`
				}
				if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
					t.Fatalf("invalid json: %v\n%s", err, out.String())
				}
				if rep.OK {
					t.Fatalf("ok must be false when a probe was refused:\n%s", out.String())
				}
				var foundFake bool
				for _, is := range rep.Issues {
					if is.Kind != "probe-denied" {
						continue
					}
					// 机器可读的条目也要能照着做：每条都得点名 `run:`，
					// 并且 remediation 说明去哪里授予。
					if !strings.Contains(is.Message, "run:") {
						t.Fatalf("issue message must name the permission to grant: %+v", is)
					}
					if !strings.Contains(is.Remediation, "permissions.allow") {
						t.Fatalf("issue remediation must say where to grant it: %+v", is)
					}
					// 本用例关心的条目必须点名它自己的 `run:` 名字；
					// 其余内置条目（机器装了什么就探测什么）各点各的。
					if strings.HasPrefix(is.Entry, "fake/") {
						foundFake = true
						if !strings.Contains(is.Message, "run:fake-engine") {
							t.Fatalf("issue message must name the permission to grant: %+v", is)
						}
					}
				}
				if !foundFake {
					t.Fatalf("the refused `fake` probe must be an explicit issue:\n%s", out.String())
				}
				return
			}

			text := out.String()
			if !strings.Contains(text, "probe-denied") {
				t.Fatalf("human output must name the skipped kind:\n%s", text)
			}
			if !strings.Contains(text, "run:fake-engine") {
				t.Fatalf("human output must name the permission to grant:\n%s", text)
			}
			if !strings.Contains(text, "permissions.allow") {
				t.Fatalf("human next-step must say where to grant it:\n%s", text)
			}
			if !strings.Contains(text, "issue(s) found") {
				t.Fatalf("the skipped probe must be counted in the summary:\n%s", text)
			}
		})
	}
}
