package vendor

import (
	"os"
	"strings"
	"testing"
)

// TestFsyncDirHasOneImplementation 钉住 v0.136：content_v2.go 曾有一份与
// atomicfile.fsyncDir 逐行相同的私有实现——正是 atomicfile 包文件头批评的
// "只差一点的复制"。现在两处共用 atomicfile.FsyncDir。
func TestFsyncDirHasOneImplementation(t *testing.T) {
	raw, err := os.ReadFile("content_v2.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	if !strings.Contains(src, "var fsyncDir = atomicfile.FsyncDir") {
		t.Error("content store must delegate directory fsync to atomicfile.FsyncDir")
	}
	// 空跑保护：不能只是引了 atomicfile 却仍留着另一份函数体
	if strings.Contains(src, "func fsyncDir(dir string) {") {
		t.Error("the duplicate fsyncDir implementation must be gone")
	}
}
