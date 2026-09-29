package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV02EngineAdaptersAcceptance 是 v0.2 E 组（tsc / deno / postcss）的可执行验收。
//
// 全部用**假引擎**扮演三个工具：ndm 的纪律是测试不依赖公网、也不要求机器
// 装齐所有引擎。假引擎能回显 argv 与自报版本，因此"翻译是否正确"和
// "版本门槛是否生效"两件事都能离线断言。
func TestV02EngineAdaptersAcceptance(t *testing.T) {
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	t.Run("typecheck runs the declared typescript engine with --noEmit", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeEngineCatalog(t, proj, engineEntry{
			Name: "typescript", Kind: "typeCheck", Adapter: "subprocess", Command: fake,
		})

		dump := filepath.Join(t.TempDir(), "args.txt")
		t.Setenv("FAKE_DUMP_ARGS", dump)

		code, out := runCaptureCode(t, "typecheck", "--engine=typescript", "--dir="+proj)
		if code != 0 {
			t.Fatalf("typecheck exit=%d:\n%s", code, out)
		}
		raw, err := os.ReadFile(dump)
		if err != nil {
			t.Fatalf("the engine was never invoked: %v", err)
		}
		if !strings.Contains(string(raw), "--noEmit") {
			t.Errorf("tsc must be called with --noEmit (otherwise it emits JS), got:\n%s", raw)
		}
	})

	t.Run("a declared deno older than 2.4 is refused for bundling", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeEngineCatalog(t, proj, engineEntry{
			Name: "deno", Kind: "bundle", Adapter: "subprocess", Command: fake,
		})
		t.Setenv("FAKE_VERSION", "2.3.0")

		code, out := runCaptureCode(t, "build", "--engine=deno", "--dir="+proj)
		if code != 5 {
			t.Fatalf("an unsupported deno must exit 5, got %d:\n%s", code, out)
		}
		// 提示必须可操作：说清要什么版本、以及替代方案
		for _, want := range []string{"2.4", "esbuild"} {
			if !strings.Contains(out, want) {
				t.Errorf("the message should mention %q:\n%s", want, out)
			}
		}
	})

	t.Run("a declared deno at 2.4 or newer may bundle", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeEngineCatalog(t, proj, engineEntry{
			Name: "deno", Kind: "bundle", Adapter: "subprocess", Command: fake,
		})
		t.Setenv("FAKE_VERSION", "2.4.0")

		code, out := runCaptureCode(t, "build", "--engine=deno", "--dir="+proj)
		if code != 0 {
			t.Fatalf("deno 2.4 can bundle; exit=%d:\n%s", code, out)
		}
	})

	// postcss 没有内建压缩：这条固定"不静默忽略用户传的 flag"
	t.Run("css with postcss says so when --minify cannot be honoured", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		testutils.WriteFile(t, proj, "app.css", ".a{color:red}\n")
		writeEngineCatalog(t, proj, engineEntry{
			Name: "postcss", Kind: "css", Adapter: "subprocess", Command: fake,
		})

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--minify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("css exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "minifier") {
			t.Errorf("the report must say --minify was ignored and why:\n%s", out)
		}
	})

	// 内置清单已含 tsc / postcss，但"没装"不能让 validate 失败
	t.Run("engines validate is quiet about optional engines that are not installed", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)

		_, jout := runCaptureCode(t, "engines", "validate", "--json", "--dir="+proj)
		var payload struct {
			Issues []struct {
				Entry string `json:"entry"`
				Kind  string `json:"kind"`
			} `json:"issues"`
		}
		if err := json.Unmarshal([]byte(jout), &payload); err != nil {
			t.Fatalf("validate --json is not valid JSON: %v\n%s", err, jout)
		}
		for _, is := range payload.Issues {
			// 只拦"未安装"这一类：装了的时候 validate 还会补一条版本比对信息，
			// 那是有意为之，不是本用例要固定的东西。
			if is.Kind != "unavailable" {
				continue
			}
			if strings.HasPrefix(is.Entry, "typescript/") || strings.HasPrefix(is.Entry, "postcss/") {
				t.Errorf("an optional engine missing from this machine must not be an issue: %+v", is)
			}
		}
	})
}

type engineEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Adapter string `json:"adapter"`
	Command string `json:"command"`
}

func writeEngineCatalog(t *testing.T, proj string, entries ...engineEntry) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"version": 1, "engines": entries})
	if err != nil {
		t.Fatal(err)
	}
	testutils.WriteFile(t, proj, "ngm.engines.json", string(body))
}
