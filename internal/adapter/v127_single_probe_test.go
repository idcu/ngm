package adapter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// markerLines 返回假引擎被 `--version` 调用过几次。
func markerLines(t *testing.T, path string) int {
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

// TestV127ValidateProbesEachEntryOnce 固定 validate 只调一次探测。
//
// 此前"可用性"（programAvailable / os.Stat）与"版本"是两条独立的检查，
// 各自实现了一遍"what counts as available"；现在它只能来自 Catalog.Probe
// 的一次结论——否则两处可以在同一份清单上给出不同答案（P2-2）。
func TestV127ValidateProbesEachEntryOnce(t *testing.T) {
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	cat := fakeCatalog(t, fake)

	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	if issues := cat.Validate(); len(issues) != 0 {
		t.Fatalf("an installed, version-less entry must validate cleanly: %+v", issues)
	}
	if n := markerLines(t, marker); n != 1 {
		t.Fatalf("validate probed %d times, want exactly 1", n)
	}

	// 可用性判定必须不 spawn：它在 --dry-run 的规划路径上。
	entry, _ := cat.Find(KindBundle, "fake")
	if !cat.Available(entry) {
		t.Fatal("the fake engine is installed; Available must say so")
	}
	if n := markerLines(t, marker); n != 1 {
		t.Fatalf("Available() spawned a probe (%d calls, want 1)", n)
	}
}

// TestV127ProbeVersionUsesProjectDir 固定"探测与执行用同一个解析器"。
//
// deno bundle 的版本门槛会 spawn 一次 `--version`。若它用进程 CWD 解析程序，
// 而执行用项目目录，就可能探测到 PATH 上的 A、执行项目 `node_modules/.bin` 里的 B。
func TestV127ProbeVersionUsesProjectDir(t *testing.T) {
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := t.TempDir()

	name := "deno"
	if runtime.GOOS == "windows" {
		name = "deno.exe"
	}
	bin := filepath.Join(proj, "node_modules", ".bin", name)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}

	// 把 PATH 指向一个空目录：只有项目里的那份能被解析到。
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty"))
	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)
	t.Setenv("FAKE_VERSION", "2.4.0")

	entry := Entry{Name: "deno", Kind: KindBundle, Adapter: AdapterSubprocess, Program: "deno", Args: []string{"bundle"}}
	v, err := probeVersion(entry, proj, nil)
	if err != nil {
		t.Fatalf("probe must resolve through the project directory: %v", err)
	}
	if v != "2.4.0" {
		t.Fatalf("probed version = %q, want 2.4.0", v)
	}
	if n := markerLines(t, marker); n != 1 {
		t.Fatalf("probe ran %d times, want exactly 1", n)
	}
}
