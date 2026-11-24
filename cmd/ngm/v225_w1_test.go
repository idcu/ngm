package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/receipt"
	"github.com/idcu/ngm/internal/testutils"
)

func w1BuildArgs(proj string) []string {
	return []string{"build", "--outfile=dist/app.js", "--dir=" + proj, "--provenance", "--cache"}
}

// TestV225BuildCacheKeyAndSource 钉住 E6：
// 键的每一项变化都必须 miss（输入 / argv / mappings 走完整命令，
// engine digest / 平台走 CacheKey 直接对照）；命中写 source=cache；
// 污染缓存失效并重建一次，重建仍不符则 exit 2，不静默返回错误产物。
func TestV225BuildCacheKeyAndSource(t *testing.T) {
	testutils.AllowEngines(t, "fake", "fake-engine")
	isolateUserEnv(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := w1Fixture(t, fake)
	t.Setenv("FAKE_OUT", "/* v225 bundle */\n")
	dump := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv("FAKE_DUMP_ARGS", dump)

	// 首次：必然现场构建，并调用一次引擎。
	code, out := runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 0 {
		t.Fatalf("first build exit=%d:\n%s", code, out)
	}
	_, r1 := w1ReadReceipt(t, proj)
	if r1.Source != receipt.SourceBuilt {
		t.Fatalf("first build source=%q, want built", r1.Source)
	}
	if _, err := os.Stat(dump); err != nil {
		t.Fatalf("first build did not invoke the engine: %v", err)
	}

	// 同输入第二次：命中，source=cache，且引擎根本没被启动。
	if err := os.Remove(dump); err != nil {
		t.Fatal(err)
	}
	code, out = runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 0 {
		t.Fatalf("cache-hit build exit=%d:\n%s", code, out)
	}
	_, r2 := w1ReadReceipt(t, proj)
	if r2.Source != receipt.SourceCache {
		t.Fatalf("second build source=%q, want cache", r2.Source)
	}
	if _, err := os.Stat(dump); !os.IsNotExist(err) {
		t.Errorf("cache hit must not start the engine (argv dump reappeared): %v", err)
	}

	// 改输入 → miss。
	testutils.WriteFile(t, proj, "src/index.ts", "export const x = 2 // changed\n")
	if err := os.Remove(dump); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	code, out = runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 0 {
		t.Fatalf("input-change build exit=%d:\n%s", code, out)
	}
	_, r3 := w1ReadReceipt(t, proj)
	if r3.Source != receipt.SourceBuilt {
		t.Errorf("changed input must miss (source=%q)", r3.Source)
	}
	if _, err := os.Stat(dump); err != nil {
		t.Errorf("missing build must invoke the engine: %v", err)
	}

	// 改 argv → miss（--production 改变了传给引擎的参数）。
	argsWithProduction := append(w1BuildArgs(proj), "--production")
	code, out = runCaptureCode(t, argsWithProduction...)
	if code != 0 {
		t.Fatalf("argv-change build exit=%d:\n%s", code, out)
	}
	_, r4 := w1ReadReceipt(t, proj)
	if r4.Source != receipt.SourceBuilt {
		t.Errorf("changed argv must miss (source=%q)", r4.Source)
	}

	// 改 mappings → miss。
	testutils.WriteFile(t, proj, "ngm.mappings.json", `{"version":1,"mappings":[]}`)
	code, out = runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 0 {
		t.Fatalf("mappings-change build exit=%d:\n%s", code, out)
	}
	_, r5 := w1ReadReceipt(t, proj)
	if r5.Source != receipt.SourceBuilt {
		t.Errorf("changed mappingsDigest must miss (source=%q)", r5.Source)
	}

	// 剩下两个键成分（engine digest / 平台三元组）用同一份回执直接对照：
	// 它们分别是"换了工具字节"与"换了平台"，不需要再 spawn 另一个引擎。
	baseKey := r5.CacheKey(buildPlatformTriple())
	mutated := r5
	mutated.Engine.Digest = "sha256:" + strings.Repeat("a", 64)
	if got := mutated.CacheKey(buildPlatformTriple()); got == baseKey {
		t.Error("changing engine.digest did not change the cache key")
	}
	mutated = r5
	mutated.MappingsDigest = "sha256:" + strings.Repeat("b", 64)
	if got := mutated.CacheKey(buildPlatformTriple()); got == baseKey {
		t.Error("changing mappingsDigest did not change the cache key")
	}
	if got := r5.CacheKey("another-os-another-arch-go"); got == baseKey {
		t.Error("changing the platform triple did not change the cache key")
	}

	// 污染：改坏缓存里的字节后，重建一次应自愈（source=built，exit 0）。
	cacheEntry := filepath.Join(buildCacheDir(), receipt.CacheEntryName(baseKey), "artifact")
	if _, err := os.Stat(cacheEntry); err != nil {
		t.Fatalf("cache entry missing after a built run: %v", err)
	}
	if err := os.WriteFile(cacheEntry, []byte("polluted cache bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out = runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 0 {
		t.Fatalf("polluted cache should be rebuilt once, got exit %d:\n%s", code, out)
	}
	_, r6 := w1ReadReceipt(t, proj)
	if r6.Source != receipt.SourceBuilt {
		t.Errorf("polluted cache must be rebuilt, source=%q", r6.Source)
	}

	// 再污染一次，但让引擎这次产出**不同的**字节：重建仍不符 → exit 2，不能静默。
	if err := os.WriteFile(cacheEntry, []byte("polluted again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_OUT", "/* v225 changed output — not the cached bytes */\n")
	code, out = runCaptureCode(t, w1BuildArgs(proj)...)
	if code != 2 {
		t.Fatalf("irreparable cache pollution must exit 2, got %d:\n%s", code, out)
	}
}
