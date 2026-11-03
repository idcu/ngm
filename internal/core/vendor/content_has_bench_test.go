package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
)

// BenchmarkContentStoreHas 测"一个 digest 是否就绪"的代价。
//
// 为什么需要它（v0.102）：`Has` 从"看清单与 meta"改成了"逐个 blob os.Stat"，
// 因为只查清单会把"清单在、blob 被删"当成就绪，随后 install 短路、verify 才报资源缺失。
// 这个修正让代价从 O(1) 次读变成 O(files) 次 stat——必须有个可复现的读数，
// 否则"大依赖树到底贵多少"只能靠猜。
//
// 跑法：
//
//	go test -run '^$' -bench ContentStoreHas -benchmem ./internal/core/vendor
//
// 实测（2026-11-03，Windows 10 / i3-10100 / NTFS，每个文件 28 B，全部命中页缓存，
// -benchtime 200x）：
//
//	files=20      929µs/op     ~21k files/s    37 KiB/op     177 allocs/op
//	files=200    8.25ms/op    ~24k files/s   316 KiB/op   1,568 allocs/op
//	files=2000   77.1ms/op    ~26k files/s   3.01 MiB/op  16,072 allocs/op
//
// 线性成立（每多一个文件约 +38µs，也就是一次 stat + 两次路径拼装/JSON 解析里的
// 分摊），没有隐藏的 O(n²)。这个数比"一次 os.Stat 约 1µs"的经验值高，是这台机器的
// 实测结果，换机器请重跑本基准，不要引用这里的绝对值。
//
// 结论：**只留 benchmark，不引入缓存**。缓存要能省下一趟线性遍历，就必须在
// "blob 被外部删除"时失效；而 ngm 没有任何跨进程的文件监听，唯一可用的失效信号
// （清单 mtime）恰好不随 blob 删除而变。install 里同一 digest 确实会被 Has 两次
// （就绪判定 + Materialize 的防御性检查），但把这一点做成进程级缓存会把
// "检查"变成"猜"——与 `Has` 上一轮修掉的失败形态同源。等有真实的重复调用热点再说。
func BenchmarkContentStoreHas(b *testing.B) {
	for _, files := range []int{20, 200, 2000} {
		b.Run(fmt.Sprintf("files=%d", files), func(b *testing.B) {
			store := NewContentStore(filepath.Join(b.TempDir(), "content"))
			dg := "sha256:" + strings.Repeat("ab", 32)

			b.StopTimer()
			entries := make([]ManifestEntry, 0, files)
			var total int64
			for i := 0; i < files; i++ {
				sha := fmt.Sprintf("%064x", i+1)
				body := []byte(fmt.Sprintf("export const f%04d = %d;\n", i, i))
				blob := store.BlobPath(sha)
				if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(blob, body, 0o644); err != nil {
					b.Fatal(err)
				}
				entries = append(entries, ManifestEntry{
					Path: fmt.Sprintf("src/f%04d.ts", i),
					Mode: digest.ModeRegular,
					SHA:  sha,
					Size: int64(len(body)),
				})
				total += int64(len(body))
			}
			mf := Manifest{
				SchemaVersion:   layoutSchemaVersion,
				ManifestVersion: digest.ManifestVersion,
				Digest:          dg,
				Entries:         entries,
			}
			meta := Meta{
				SchemaVersion:   MetaSchemaVersion,
				Repo:            "github:test/many",
				Commit:          strings.Repeat("a", 40),
				Digest:          dg,
				ManifestVersion: digest.ManifestVersion,
			}
			if err := store.publishManifest(mf, meta); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(total)
			b.ReportAllocs()
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				if !store.Has(dg) {
					b.Fatal("Has returned false for a complete entry")
				}
			}
		})
	}
}
