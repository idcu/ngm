package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V217：集合约束轻量版——同 ref 绿、不同 ref 红并点名模块、显式豁免生效、
// 过期豁免红。只做一致性判定，不做版本求解。
func TestV217CollectionConstraints(t *testing.T) {
	writeIndex := func(t *testing.T, root, ignore string) {
		t.Helper()
		body := fmt.Sprintf(`{"schemaVersion":1,"modules":[],"constraints":{"ignoreSharedRefs":%s}}`, ignore)
		if err := os.WriteFile(filepath.Join(root, "ngm.collection.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writePair := func(t *testing.T, root, refA, refB string) {
		t.Helper()
		writeModuleFixture(t, root, "packages/a", fmt.Sprintf(`{
  "name":"a","version":"1.0.0","runtime":"node",
  "dependencies":[{"name":"github:acme/lib","ref":%q,"refType":"tag"}]
}`, refA))
		writeModuleFixture(t, root, "packages/b", fmt.Sprintf(`{
  "name":"b","version":"1.0.0","runtime":"node",
  "dependencies":[{"name":"github:acme/lib","ref":%q,"refType":"tag"}]
}`, refB))
	}

	t.Run("same ref green", func(t *testing.T) {
		root := t.TempDir()
		writePair(t, root, "v1.0.0", "v1.0.0")
		writeIndex(t, root, `[]`)
		if code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict"); code != 0 {
			t.Fatalf("same ref must pass strict validate, got %d:\n%s", code, out)
		}
	})

	t.Run("different refs red and names modules", func(t *testing.T) {
		root := t.TempDir()
		writePair(t, root, "v1.0.0", "v2.0.0")
		code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict")
		if code != 3 {
			t.Fatalf("divergent refs must fail with exit 3, got %d:\n%s", code, out)
		}
		for _, want := range []string{
			"shared dependency", "github:acme/lib", "packages/a", "packages/b", "v1.0.0", "v2.0.0",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("explicit exemption green", func(t *testing.T) {
		root := t.TempDir()
		writePair(t, root, "v1.0.0", "v2.0.0")
		writeIndex(t, root, `["github:acme/lib"]`)
		if code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict"); code != 0 {
			t.Fatalf("registered exemption must pass strict validate, got %d:\n%s", code, out)
		}
	})

	t.Run("stale exemption red", func(t *testing.T) {
		root := t.TempDir()
		writePair(t, root, "v1.0.0", "v1.0.0")
		writeIndex(t, root, `["github:acme/retired"]`)
		code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict")
		if code != 3 {
			t.Fatalf("stale exemption must fail with exit 3, got %d:\n%s", code, out)
		}
		for _, want := range []string{"stale exemption", "github:acme/retired"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("explicit --index path", func(t *testing.T) {
		root := t.TempDir()
		writePair(t, root, "v1.0.0", "v2.0.0")
		if err := os.WriteFile(filepath.Join(root, "custom.collection.json"), []byte(
			`{"schemaVersion":1,"modules":[],"constraints":{"ignoreSharedRefs":["github:acme/lib"]}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		// --index 同时触发索引对账；这里只验证豁免确实从指定文件读到。
		code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict", "--index=custom.collection.json")
		if !strings.Contains(out, "missing from the collection index") {
			t.Fatalf("expected index drift report, got %d:\n%s", code, out)
		}
		if strings.Contains(out, "shared dependency") || strings.Contains(out, "stale exemption") {
			t.Errorf("exemption from --index file was ignored:\n%s", out)
		}
	})
}
