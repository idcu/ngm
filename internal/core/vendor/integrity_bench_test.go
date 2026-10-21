package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkVerifyVendorTreeDeep 测 `ngm verify --deep` 的逐文件哈希。
//
//	v0.2 复盘 §2.3 把"单个依赖内部串行哈希"挂账，
//	这个基准是为了让"改没改得动"这件事**可复现**，而不是在复盘里写一句"更快了"。
//
// 跑法（A/B 对照就在同一份代码上，只改并发度）：
//
//	go test -run '^$' -bench VerifyVendorTreeDeep -benchtime 20x ./internal/vendor
//	GOMAXPROCS=1 go test -run '^$' -bench VerifyVendorTreeDeep -benchtime 20x ./internal/vendor
//
// **为什么用 GOMAXPROCS 做对照**：这里的成本全在 CPU（sha256）与页缓存读取上，
// 没有被测的"依赖级并发"参与，因此 GOMAXPROCS=1 等价于改回串行——
// 不需要为了测量而临时改代码（那样的数字没法被别人复现）。
//
// 树的规模刻意取"文件多、单文件小"：那正是并行有收益的形态。
// 若哪天有人把并行度调大而收益消失，这个基准会显示出来。
//
// **实测**（2026-10-01，Windows 10 / i3-10100，2000 × 8 KiB = 16 MiB）：
//
//	GOMAXPROCS=1   373.6ms/op   43.86 MB/s
//	默认（8）      178.6ms/op   91.75 MB/s     → 2.09×
//
// 与 digest 清单那条路径的 1.14× 形成对照：这里没有 git 子进程，成本全在文件读取与
// sha256 上，所以并行能拿到接近核数的倍数（8 个逻辑核拿到 2.09×，受内存带宽与
// 磁盘页缓存限制）。分配量两次相同——并行没有引入额外拷贝。
func BenchmarkVerifyVendorTreeDeep(b *testing.B) {
	const files = 2000
	const perFile = 8 << 10 // 8 KiB

	left := b.TempDir()
	right := b.TempDir()

	b.StopTimer()
	for i := 0; i < files; i++ {
		rel := filepath.Join("pkg", fmt.Sprintf("mod%03d", i%50), fmt.Sprintf("f%04d.ts", i))
		body := make([]byte, perFile)
		for j := range body {
			// 确定的内容：同一份代码反复跑得到同一棵树
			body[j] = byte('a' + (i+j)%26)
		}
		for _, root := range []string{left, right} {
			full := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(full, body, 0o644); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StartTimer()

	total := int64(files * perFile)
	b.SetBytes(total)
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		res, err := VerifyVendorTree(left, right)
		if err != nil {
			b.Fatal(err)
		}
		if !res.OK() {
			b.Fatalf("identical trees must verify clean: %v", res.Mismatches)
		}
		if res.Files != files {
			b.Fatalf("expected %d files, got %d", files, res.Files)
		}
	}
}
