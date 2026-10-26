package main

import (
	"strings"
	"testing"
)

func TestModulesValidateStrictSharedDependencyAlignment(t *testing.T) {
	root := t.TempDir()
	writeModuleFixture(t, root, "packages/a", `{
  "name":"a","version":"1.0.0",
  "dependencies":[{"name":"github:acme/lib","ref":"v1.0.0","refType":"tag"}]
}`)
	writeModuleFixture(t, root, "packages/b", `{
  "name":"b","version":"1.0.0",
  "dependencies":[{"name":"github:acme/lib","ref":"v2.0.0","refType":"tag"}]
}`)
	writeModuleFixture(t, root, "packages/c", `{
  "name":"c","version":"1.0.0",
  "dependencies":[{"name":"github:acme/lib","ref":"v1.0.0","refType":"tag"}]
}`)

	// 默认 validate 只检查清单/重名，不因集合内 ref 不一致失败。
	if code, out := runCaptureCode(t, "modules", "validate", "--dir="+root); code != 0 {
		t.Fatalf("default validate should pass, got %d:\n%s", code, out)
	}

	code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict")
	if code != 3 {
		t.Fatalf("strict validate should fail on divergent shared ref, got %d:\n%s", code, out)
	}
	for _, want := range []string{"shared dependency", "github:acme/lib", "v1.0.0", "v2.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("strict output missing %q:\n%s", want, out)
		}
	}
}

func TestModulesValidateStrictSharedDependencyAligned(t *testing.T) {
	root := t.TempDir()
	writeModuleFixture(t, root, "packages/a", `{
  "name":"a","version":"1.0.0",
  "dependencies":[{"name":"github:acme/lib","ref":"v1.0.0","refType":"tag"}]
}`)
	writeModuleFixture(t, root, "packages/b", `{
  "name":"b","version":"1.0.0",
  "dependencies":[{"name":"github:acme/lib","ref":"v1.0.0","refType":"tag"}]
}`)

	if code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict"); code != 0 {
		t.Fatalf("aligned shared ref should pass strict validate, got %d:\n%s", code, out)
	}
}
