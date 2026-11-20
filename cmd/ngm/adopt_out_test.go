package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAdoptCreatesTheOutputParentDirectory：adopt 与 sbom / attest / transform
// 走同一条产物纪律——相对 --out 落在 --dir 下，父目录缺失时由命令建出来。
func TestAdoptCreatesTheOutputParentDirectory(t *testing.T) {
	isolateUserEnv(t)
	p := newProject(t)
	seedNodeModules(t, p)

	out := filepath.Join(p, "dist", "nested", "proof.json")
	code, stdout, stderr := runSplit(t, "adopt", "--dir="+p, "--out="+out)
	if code != 0 {
		t.Fatalf("adopt --out with a missing parent exited %d\n%s\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("the proof must exist after creating its parent: %v", err)
	}
}
