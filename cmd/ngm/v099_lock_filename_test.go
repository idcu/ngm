package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/mappings"
)

// v0.99：`mappings.LockFileName` 与 `lock.FileName` 必须**是同一个名字**——
// 而这条一致性以前**谁也验不了**。
//
// `internal/mappings/validate.go` 里原来写着："两者口径一致由 CLI 的集成测试保证"。
// 那句话做不到：常量没导出，仓库里没有任何一个包能同时看见这两个名字
// （mappings 刻意不依赖 lock）。于是那句话只是……一句话。
//
// 修法两步：① 把常量导出（依赖方向不变）；② 在能同时 import 两个包的地方
// （本目录）把它变成判据。这就是本项目一路在用的口径：**承诺要么有网，要么别写**。
func TestV99LockFileNameHasOneSource(t *testing.T) {
	if mappings.LockFileName != lock.FileName {
		t.Errorf("锁文件名两处不一致：mappings.LockFileName = %q，lock.FileName = %q —— "+
			"一个包读 ngm.lock、另一个去校验别的名字，报错会说\"不在 A 里\"而实际文件叫 B",
			mappings.LockFileName, lock.FileName)
	}
	// 两个空串也"相等"——把这种退化情形单独挡掉。
	if mappings.LockFileName == "" || lock.FileName == "" {
		t.Fatalf("锁文件名不能是空串（mappings=%q lock=%q）", mappings.LockFileName, lock.FileName)
	}

	// 常量必须真的被用在工作里：一个导出了却没人用的常量只是把承诺挪了个地方。
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "mappings", "validate.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "LockFileName") {
		t.Error("`LockFileName` 在 validate.go 里没有被用到——那这条判据比的就是两个常量，而不是两处行为")
	}
}
