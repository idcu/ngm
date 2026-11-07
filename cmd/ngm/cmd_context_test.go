package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

type stubReport struct {
	data []byte
	err  error
}

func (s stubReport) Marshal() ([]byte, error) { return s.data, s.err }

type failingWriter struct{ err error }

func (w failingWriter) Write(p []byte) (int, error) { return 0, w.err }

// P2-3 的骨架把 12 条命令的"解析失败 → 信封 + 退出码 3"收成一份实现。
// 这条判据钉住三件事：成功时 `--json` 只在实际声明了 jsonOut 时才标记 ctx；
// 失败时人读/机器读两条通道都给出提示；`c.usage` 的文本确实被挂到 fs 上。
func TestCmdContextParsePaths(t *testing.T) {
	t.Run("success marks json only when jsonOut is declared", func(t *testing.T) {
		var out, errb bytes.Buffer
		fs := newFlagSet("probe", &errb)
		jsonOut := fs.Bool("json", false, "machine-readable report")
		c := newCmdContext(context.Background(), []string{"--json"}, &out, &errb, fs)
		if !c.parse([]flagSpec{{Name: "json", Bool: true}}, jsonOut) {
			t.Fatalf("parse failed: stderr=%q", errb.String())
		}
		if !wantsJSON(c.ctx) {
			t.Error("--json was requested and jsonOut is non-nil, so the context must carry the mark")
		}
	})

	t.Run("nil jsonOut keeps the context unmarked", func(t *testing.T) {
		var out, errb bytes.Buffer
		fs := newFlagSet("probe", &errb)
		fs.Bool("json", false, "machine-readable report")
		c := newCmdContext(context.Background(), []string{"--json"}, &out, &errb, fs)
		if !c.parse([]flagSpec{{Name: "json", Bool: true}}, nil) {
			t.Fatalf("parse failed: stderr=%q", errb.String())
		}
		if wantsJSON(c.ctx) {
			t.Error("without a jsonOut pointer the command opted out of the JSON mark (verify does this)")
		}
	})

	t.Run("failure emits the JSON envelope only when asked early", func(t *testing.T) {
		var out, errb bytes.Buffer
		fs := newFlagSet("probe", &errb)
		fs.Bool("json", false, "machine-readable report")
		c := newCmdContext(context.Background(), []string{"--json", "--bogus"}, &out, &errb, fs)
		c.usage("probe usage\n")
		if c.parse([]flagSpec{{Name: "json", Bool: true}}, nil) {
			t.Fatal("--bogus must not parse")
		}
		if !strings.Contains(out.String(), `"Usage"`) {
			t.Errorf("stdout=%q: `--json` asked for a machine-readable error, so the envelope must be written", out.String())
		}
		if !strings.Contains(errb.String(), "probe usage") {
			t.Errorf("stderr=%q: the command's usage text must be reachable on failure", errb.String())
		}
	})

	t.Run("failure without json writes no envelope", func(t *testing.T) {
		var out, errb bytes.Buffer
		fs := newFlagSet("probe", &errb)
		fs.Bool("json", false, "machine-readable report")
		c := newCmdContext(context.Background(), []string{"--bogus"}, &out, &errb, fs)
		c.usage("probe usage\n")
		if c.parse([]flagSpec{{Name: "json", Bool: true}}, nil) {
			t.Fatal("--bogus must not parse")
		}
		if out.Len() != 0 {
			t.Errorf("stdout=%q: without --json a parse failure must stay on the human channel", out.String())
		}
		if !strings.Contains(errb.String(), "flag provided but not defined") ||
			!strings.Contains(errb.String(), "probe usage") {
			t.Errorf("stderr=%q: the human channel must name the bad flag and the usage text", errb.String())
		}
	})
}

func TestCmdContextMarkJSON(t *testing.T) {
	var out, errb bytes.Buffer
	c := newCmdContext(context.Background(), nil, &out, &errb, newFlagSet("probe", &errb))
	if wantsJSON(c.markJSON(false)) {
		t.Error("markJSON(false) must not mark")
	}
	if !wantsJSON(c.markJSON(true)) {
		t.Error("markJSON(true) must mark (verify checks before the flag is effective)")
	}
}

// env 失败时：ctx 已被标记（命令先看了 `--json`）就必须给信封，否则只给耳机通道；
// 两条通道的退出码都来自同一个错误模型，不能各写各的数字。
func TestCmdContextEnvFailureKeepsTheEnvelope(t *testing.T) {
	// newProjectEnv 只读**全局**配置；用 NGM_HOME 把它指到临时目录，
	// 再放一份坏掉的 config.json（不碰真实 home）。
	home := t.TempDir()
	t.Setenv("NGM_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir() // 项目侧只要是个可解析的路径；失败来自全局配置

	for _, tc := range []struct {
		name string
		ctx  context.Context
		json bool
	}{
		{name: "json", ctx: withJSON(context.Background()), json: true},
		{name: "human", ctx: context.Background(), json: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			c := newCmdContext(tc.ctx, nil, &out, &errb, newFlagSet("probe", &errb))
			env, code := c.env(projectDir)
			if env != nil {
				t.Fatal("a broken config must not yield an env")
			}
			if code != errs.CodeConfigInvalid.ExitCode() {
				t.Errorf("code=%d want %d", code, errs.CodeConfigInvalid.ExitCode())
			}
			if tc.json {
				if !strings.Contains(out.String(), `"Usage"`) && !strings.Contains(out.String(), `"code"`) {
					t.Errorf("stdout=%q: JSON mode must carry the error envelope", out.String())
				}
				// runErr 的契约是"信封 + 人读那行都写"（stdout 给机器、stderr 给人），
				// 所以 JSON 模式并没有把 stderr 静默掉——这里钉住这个既有行为。
				if !strings.Contains(errb.String(), "ConfigInvalid") {
					t.Errorf("stderr=%q: the human line is still written in JSON mode (runErr contract)", errb.String())
				}
			} else {
				if out.Len() != 0 {
					t.Errorf("stdout=%q: human mode must not write an envelope", out.String())
				}
				if errb.Len() == 0 {
					t.Error("human mode must say what went wrong")
				}
			}
		})
	}
}

func TestCmdContextWriteReport(t *testing.T) {
	t.Run("success returns the caller code and writes the document", func(t *testing.T) {
		var out, errb bytes.Buffer
		c := newCmdContext(context.Background(), nil, &out, &errb, newFlagSet("probe", &errb))
		if code := c.writeReport(stubReport{data: []byte(`{"ok":true}`)}, 3); code != 3 {
			t.Errorf("code=%d want 3", code)
		}
		if got := out.String(); got != `{"ok":true}` {
			t.Errorf("stdout=%q", got)
		}
	})

	t.Run("marshal failure goes through the error model", func(t *testing.T) {
		var out, errb bytes.Buffer
		c := newCmdContext(context.Background(), nil, &out, &errb, newFlagSet("probe", &errb))
		if code := c.writeReport(stubReport{err: errors.New("boom")}, 0); code != errs.CodeInternal.ExitCode() {
			t.Errorf("code=%d want %d (internal failure, ADR-026)", code, errs.CodeInternal.ExitCode())
		}
		if out.Len() != 0 {
			t.Errorf("stdout=%q: nothing could be produced", out.String())
		}
	})

	t.Run("write failure is an internal failure, not a policy failure", func(t *testing.T) {
		var errb bytes.Buffer
		c := newCmdContext(context.Background(), nil, failingWriter{err: errors.New("closed pipe")}, &errb, newFlagSet("probe", &errb))
		if code := c.writeReport(stubReport{data: []byte("x")}, 0); code != errs.CodeInternal.ExitCode() {
			t.Errorf("code=%d want %d: the conclusion was reached but could not be delivered", code, errs.CodeInternal.ExitCode())
		}
		if !strings.Contains(errb.String(), "write report:") {
			t.Errorf("stderr=%q: the delivery failure must be named", errb.String())
		}
	})
}
