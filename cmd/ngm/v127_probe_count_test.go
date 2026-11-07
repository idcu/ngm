package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// markerCalls 返回假引擎被 `--version` 调用过几次（文件不存在即 0）。
func markerCalls(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read marker: %v", err)
	}
	return strings.Count(string(data), "\n")
}

// TestV127EnginesListProbesOncePerProgram 固定 list 对同一程序的多个条目只探测一次。
//
// 条目按 (adapter, program, args) 去重：esbuild 同时提供 bundle 与 transform，
// 拿它当"两条清单"看待会让一次 list 探测两遍版本（P2-2）。
func TestV127EnginesListProbesOncePerProgram(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	isolateUserEnv(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := m6Project(t, `{"bundle":"fake"}`)
	m6Catalog(t, proj, fake, "bundle", "transform")

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	if code := dispatch([]string{"engines", "list", "--json", "--dir=" + proj}, &out, &errb); code != 0 {
		t.Fatalf("engines list exit = %d, stderr:\n%s", code, errb.String())
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if n := markerCalls(t, marker); n != 1 {
		t.Fatalf("engines list probed the same program %d times, want 1", n)
	}
}

// TestV127ValidateProbesOncePerEntry 固定 validate 对每条清单只探测一次。
//
// 内置引擎也要授权：validate 是 fail-closed 的（探针被拒绝 → 3），
// 而本用例关心的是"探测次数"，不是权限本身。
func TestV127ValidateProbesOncePerEntry(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine", "esbuild", "tsc", "postcss")
	isolateUserEnv(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := m6Project(t, `{"bundle":"fake"}`)
	m6Catalog(t, proj, fake, "bundle")

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	if code := dispatch([]string{"engines", "validate", "--dir=" + proj}, &out, &errb); code != 0 {
		t.Fatalf("engines validate exit = %d, stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	if n := markerCalls(t, marker); n != 1 {
		t.Fatalf("validate probed %d times, want exactly 1", n)
	}
}

// TestV127DenoBundleProbesExactlyOnce 是"探测/执行单一路径"的回归判据。
//
// deno bundle 的版本门槛要求一次 `--version`。此前 preflight 与引擎执行
// 各自生成一次 argv，于是同一次构建探测两遍；现在 preflight 只做 NoProbe
// 预检，真正的探测留给闸门之后的执行路径。
func TestV127DenoBundleProbesExactlyOnce(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := m6Project(t, `{"bundle":"deno"}`)
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "deno", Kind: "bundle", Adapter: "subprocess",
		Command: `"` + fake + `" bundle`,
	})

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)
	t.Setenv("FAKE_VERSION", "2.4.0")

	var out, errb bytes.Buffer
	if code := dispatch([]string{"build", "src/index.ts", "--engine=deno", "--dir=" + proj}, &out, &errb); code != 0 {
		t.Fatalf("build exit = %d, stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	if n := markerCalls(t, marker); n != 1 {
		t.Fatalf("deno bundle probed %d times, want exactly 1", n)
	}
}
