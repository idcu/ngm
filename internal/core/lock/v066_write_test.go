package lock

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.66：lock 的三连加固（评估报告 P1#3 / P2#6-#8）。
//
//	① Write 前必须 Validate——非法条目不得落盘
//	② 短 commit 是"写得进去、读不回来"的典型，单独钉一条
//	③ 临时文件改 0644——ngm.lock 要提交进 Git、供团队共享

func TestV66WriteRefusesInvalidLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ngm.lock")

	f := NewFile()
	f.Dependencies = []Dependency{{
		Name: "github:org/utils", Ref: "v1.2.3", RefType: "tag",
		Commit:        "abc1234", // 7 位短 hash：解析层接受、lock 层不接受
		ArchiveDigest: "sha256:" + strings.Repeat("aa", 32),
		ResolvedAt:    "2026-10-09T00:00:00Z",
		VendorPath:    "github.com/org/utils",
	}}

	err := Write(path, f)
	if err == nil {
		t.Fatal("短 commit 的 lock 不得落盘（写进去之后下一次读会直接失败）")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("拒绝时必须说清是 lock 非法: %v", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Errorf("被拒绝的写入不得留下文件（原子写的意义）: %v", serr)
	}
}

// 对照：合法的 lock 写出来必须能原样读回。
func TestV66WriteProducesReadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ngm.lock")
	if err := Write(path, sampleLock()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := Read(path); err != nil {
		t.Fatalf("写出来的 lock 必须能被读回: %v", err)
	}
}

// TestV66WriteUsesSharedPermissions：0600 的 lock 在团队协作里读不到、
// 与仓库其余文件（0644）不一致。
func TestV66WriteUsesSharedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的权限位语义不同（只影响只读位），该断言只在 POSIX 上有意义")
	}
	path := filepath.Join(t.TempDir(), "ngm.lock")
	if err := Write(path, sampleLock()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o644 {
		t.Errorf("ngm.lock 的权限 = %o, want 644（它是要提交进 Git 的共享文件）", got)
	}
}
