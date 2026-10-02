package vendor

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"
)

// TestV09BlobPoolScale 量 blob 池在规模化之后的两个问题（[v0.9 计划](../../docs/development/v0.9-plan.md) C 组）：
//
//  1. **两位 hex 分片够不够**：256 个目录，10 万 blob → 单目录约 400 个文件；
//  2. **`Usage()` 还跑得动吗**：它要遍历整个池 + 读所有清单，是这一层唯一的读数入口。
//
// **默认跳过**（`t.Skip`）：它要写几万个文件，不该塞进日常测试。
// 跑法：
//
//	NGM_STORE_SCALE_BLOBS=100000 go test -count=1 -run TestV09BlobPoolScale -v ./internal/vendor
//
// 为什么它是"只测不做"：量出来只进 [metrics](../../docs/internals/metrics.md)，
// 改分片策略（例如三位）是一个需要数据的决策，不在本版。
func TestV09BlobPoolScale(t *testing.T) {
	raw := os.Getenv("NGM_STORE_SCALE_BLOBS")
	if raw == "" {
		t.Skip("set NGM_STORE_SCALE_BLOBS=100000 to run the scale measurement")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("NGM_STORE_SCALE_BLOBS=%q 不是正整数", raw)
	}

	store := newStore(t)
	// 一份**真实**的内容树：让池里不是清一色的孤儿（否则量的是另一条路径）。
	putFixture(t, store, simpleRepo)

	start := time.Now()
	payload := []byte("0123456789abcdef0123456789abcdef") // 32 B：比目录项小得多
	for i := 0; i < n; i++ {
		// 名字必须是 64 位 hex（`BlobPath` 用前两位做分片），而且**必须均匀分布**：
		// 真实 blob 名是 `sha256(mode‖content)`，前两位是均匀的。
		//
		// 这一点是量出来的：第一版夹具用 `%064x`（一个计数器），于是 2 万个名字的
		// 前两位全是 `00`——**全落进同一个分片**，量到的是夹具的偏差而不是真实分布
		// （`max files/shard=20000, shards=3`）。计数器不是内容寻址的名字。
		name := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("blob-%d", i))))
		bp := store.BlobPath(name)
		if merr := os.MkdirAll(filepath.Dir(bp), 0o755); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(bp, payload, 0o644); werr != nil {
			t.Fatal(werr)
		}
	}
	writeTook := time.Since(start)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start = time.Now()
	u, err := store.Usage()
	usageTook := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}

	// 单目录最多多少文件（分片是否均衡）。
	perShard := map[string]int{}
	shards, serr := os.ReadDir(store.blobsRoot())
	if serr != nil {
		t.Fatal(serr)
	}
	for _, sh := range shards {
		if !sh.IsDir() {
			continue
		}
		ents, derr := os.ReadDir(filepath.Join(store.blobsRoot(), sh.Name()))
		if derr != nil {
			t.Fatal(derr)
		}
		perShard[sh.Name()] = len(ents)
	}
	counts := make([]int, 0, len(perShard))
	for _, c := range perShard {
		counts = append(counts, c)
	}
	sort.Ints(counts)
	// 结论要能被复核：这些数字就是 metrics 页要引用的那一组。
	// 池里的 blob 总数 = 合成的那 n 个 + 那份真实 fixture 自己的。
	t.Logf("blob pool scale: synthetic=%d, pool blobs=%d, shards=%d, max files/shard=%d, min=%d",
		n, u.Blobs, len(perShard), counts[len(counts)-1], counts[0])
	t.Logf("wrote %d blobs in %s (%.0f/s); Usage() over the whole pool took %s",
		n, writeTook.Round(time.Millisecond),
		float64(n)/writeTook.Seconds(), usageTook.Round(time.Millisecond))
	t.Logf("memory: Usage() allocated %s (heap in use %s → %s)",
		byteCount(int64(after.TotalAlloc-before.TotalAlloc)),
		byteCount(int64(before.HeapInuse)), byteCount(int64(after.HeapInuse)))
	t.Logf("usage split: shared=%s exclusive=%s orphans=%d (%s)",
		byteCount(u.BlobSharedBytes), byteCount(u.BlobExclusiveBytes),
		u.BlobOrphans, byteCount(u.BlobOrphanBytes))

	// 最起码的一致性（否则上面的数字没有意义）：
	// 合成的那些 blob 没有一个被清单引用，因此它们必须**都在孤儿里**。
	if u.BlobOrphans < n {
		t.Errorf("合成 %d 个孤儿 blob，而 Usage 只报 %d 个——读数与写入对不上", n, u.BlobOrphans)
	}
	if got := u.BlobSharedBytes + u.BlobExclusiveBytes + u.BlobOrphanBytes; got != u.BlobBytes {
		t.Errorf("三段之和 %d != BlobBytes %d", got, u.BlobBytes)
	}
	if usageTook > 30*time.Second {
		t.Errorf("Usage() 在 %d 个 blob 上花了 %s —— 这一层的唯一读数入口已经不好用了",
			u.Blobs, usageTook)
	}
}

func byteCount(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
