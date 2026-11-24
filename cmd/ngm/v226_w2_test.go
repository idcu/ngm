package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV226CssMinifyFailsClosed 固定 E7 的 fail-closed 契约。
//
// 变异检验：把 engines_extra.go 里 CheckCSSMinifySupport 的 errs.New(..., 3, ...)
// 改回一条 note / 返回 nil → 第 1 条与最后一条子用例立刻红；恢复 → 绿。
// 第 2 条守住"显式声明支持就放行"，防止把压缩一刀切禁掉。
func TestV226CssMinifyFailsClosed(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	t.Setenv("FAKE_ECHO_STDIN", "1")

	writeCSSCatalog := func(t *testing.T, proj, name string, supportsMinify bool) {
		t.Helper()
		entry := map[string]any{
			"name": name, "kind": "css", "adapter": "subprocess", "command": fake,
		}
		if supportsMinify {
			entry["supportsMinify"] = true
		}
		body, err := json.Marshal(map[string]any{"version": 1, "engines": []any{entry}})
		if err != nil {
			t.Fatal(err)
		}
		testutils.WriteFile(t, proj, "ngm.engines.json", string(body))
		testutils.WriteFile(t, proj, "app.css", ".a{color:red}\n")
	}

	t.Run("postcss plus minify is a config error with an explicit engine hint", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeCSSCatalog(t, proj, "postcss", false)

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--minify", "--dir="+proj)
		if code != 3 {
			t.Fatalf("exit=%d want 3 (an unsupported --minify must fail closed):\n%s", code, out)
		}
		for _, want := range []string{"esbuild", "lightningcss"} {
			if !strings.Contains(out, want) {
				t.Errorf("hint must name %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("declared minify support compiles normally", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeCSSCatalog(t, proj, "minifier", true)

		code, out := runCaptureCode(t, "css", "app.css", "--engine=minifier", "--minify", "--dir="+proj)
		if code != 0 {
			t.Fatalf("exit=%d want 0 for an engine declaring minify support:\n%s", code, out)
		}
		if !strings.Contains(out, ".a{color:red}") {
			t.Errorf("the supported path must still produce compiled CSS on stdout:\n%s", out)
		}
	})

	t.Run("postcss without minify is unchanged", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeCSSCatalog(t, proj, "postcss", false)

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--dir="+proj)
		if code != 0 {
			t.Fatalf("exit=%d want 0 on the legacy non-minify path:\n%s", code, out)
		}
		if strings.Contains(out, "does not support") {
			t.Errorf("non-minify path must not gain the new failure message:\n%s", out)
		}
	})

	t.Run("dry-run reports the same failure without spawning", func(t *testing.T) {
		isolateUserEnv(t)
		proj := newProject(t)
		writeCSSCatalog(t, proj, "postcss", false)
		marker := t.TempDir() + "/probed.txt"
		t.Setenv("FAKE_VERSION_MARKER", marker)

		code, out := runCaptureCode(t, "css", "app.css", "--engine=postcss", "--minify", "--dry-run", "--dir="+proj)
		if code != 3 {
			t.Fatalf("dry-run exit=%d want 3 (Plans shares the same preflight):\n%s", code, out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("dry-run must not probe the engine: marker stat = %v", err)
		}
	})
}
