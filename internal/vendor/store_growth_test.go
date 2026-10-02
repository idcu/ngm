package vendor

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV06StoreGrowthInventory 量出"内容寻址 store 每多一个 commit 会长多少"（v0.6 C 组）。
//
// 为什么需要：E2（内容寻址 store / mirror 的 GC）自 v0.2 起挂账，理由一直是
// **缺实测的磁盘增长数据**（见 ADR-003 补录）。本用例只**测**，不做 GC——
// 先用数据决定它该不该排期，而不是因为"迟早要做"就动手。
//
// 形态刻意取真实仓库常见的样子：**一个仓库、多个 commit，每个 commit 只改一个文件**，
// 其余内容原样不动。于是"源码增量"远小于"树的总字节"——那正是去重该发力、
// 而实测要看清楚的地方。
//
// 它**不是门禁**（数字随 fixture 形态变化），只作为证据；有一条下限断言，
// 防止"Put 悄悄不干活"时这个用例还绿着（那种绿比红更糟）。
func TestV06StoreGrowthInventory(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()
	store := newStore(t)

	const (
		commits   = 12
		baseFiles = 20
		fileSize  = 8 << 10 // 8 KiB
	)

	repo := testutils.NewGitRepo(t)
	for i := 0; i < baseFiles; i++ {
		repo.WriteFile(fmt.Sprintf("src/f%02d.txt", i), strings.Repeat("a", fileSize))
	}
	repo.Commit("feat: base tree")

	treeBytes := int64(baseFiles * fileSize)
	t.Logf("每个 commit 的内容树：%d 文件 × %d B = %s；每个 commit 只改其中 **1** 个文件（%s）",
		baseFiles, fileSize, v06Bytes(treeBytes), v06Bytes(fileSize))

	var prev int64
	for i := 0; i < commits; i++ {
		if i > 0 {
			// 只动一个文件、其余原样——"源码增量 << 树"的形态
			repo.WriteFile(fmt.Sprintf("src/f%02d.txt", i%baseFiles), strings.Repeat("b", fileSize))
			repo.Commit(fmt.Sprintf("feat: change %d", i))
		}
		head := repo.Head()
		dg, err := git.BuildArchiveDigest(ctx, git.Options{}, repo.Dir, head)
		if err != nil {
			t.Fatalf("BuildArchiveDigest: %v", err)
		}
		if _, perr := store.Put(ctx, git.Options{}, repo.Dir, Meta{
			Repo:   "github:test/growth",
			Commit: head,
			Digest: dg,
		}); perr != nil {
			t.Fatalf("Put: %v", perr)
		}

		now := v06DirBytes(t, store.Root())
		t.Logf("commit %2d: store 累计 %9s（本次 +%9s）· 已存下 %.2f 棵树",
			i+1, v06Bytes(now), v06Bytes(now-prev), float64(now)/float64(treeBytes))
		prev = now
	}

	total := v06DirBytes(t, store.Root())

	// **上界**断言（v0.8 起）。它守的是布局本身：
	//
	//	v0.6 实测（v1 布局，每个 digest 一棵完整树）：12 个 commit → 1.88 MiB ≈ **20×** 树。
	//	v0.8 换成 blob 池 + 清单（ADR-019）之后，同一份 fixture 应当只在**一份树**上下浮动
	//	（唯一内容 8 KiB 的 blob + 每个 commit 一份约 3.7 KiB 的清单）。
	//
	// 所以判据从"至少要有 N 棵树"翻转成"**不得接近 N 棵树**"——同一把尺子，
	// 量的从"放大得有多严重"变成"去重是否真的生效"。
	if total >= treeBytes*int64(commits)/2 {
		t.Fatalf("store 长了 %s，接近 %d 棵树的量级（%s）—— 跨 commit 的去重看起来失效了"+
			"（写路径是否又回到了按整棵树寻址？ADR-019）",
			v06Bytes(total), commits, v06Bytes(treeBytes*int64(commits)/2))
	}

	perCommit := total / int64(commits)
	// 镜像侧（层 1）的对照：Git 对象库自己的体积。这里用 fixture 仓库的 `.git` 作**上界**——
	// 它是松散对象，而 mirror 是 `clone --mirror` 得来的（对象被打包），因此只会更小。
	// 这一格的分工很清楚：层 1 的增长由 **git 自己的 gc** 管，层 2 的增长**没有任何人管**。
	gitBytes := v06DirBytes(t, filepath.Join(repo.Dir, ".git"))
	t.Logf("结论：%d 个 commit 后 content store = %s（平均 %s/commit）；"+
		"同期源码的**真实增量**只有 %s。比值 **%.1f×** —— 跨 commit 已去重（v0.6 的同一场景是 **20.0×**）：",
		commits, v06Bytes(total), v06Bytes(perCommit), v06Bytes(commits*fileSize),
		float64(perCommit)/float64(fileSize))
	t.Logf("同一份历史的 Git 对象库（层 1 的上界估计）= %s，即 content store 是它的 **%.1f×**。"+
		"两层分工不同：层 1 的增长由 `git gc` 管（用户随时可跑），"+
		"层 2 的增长**没有任何人去回收**——它只增不减。",
		v06Bytes(gitBytes), float64(total)/float64(gitBytes))
	t.Logf("布局是 blobs/<aa>/<sha> + trees/<digest>/manifest.json（ADR-019），" +
		"内容按 sha256 只存一份，每个 commit 只多一份**清单**。" +
		"于是\"只增不减\"的性质没变（ADR-018 的两个条件仍未成立），但**斜率**从 20× 降到了 ~1×——" +
		"去重改的是斜率，不是终点。")
}

// v06DirBytes 递归求和目录里所有文件的字节数（目录自身不计）。
func v06DirBytes(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return total
}

// v06Bytes 把字节数印成人能读的形态（证据用例的输出要能直接贴进文档）。
func v06Bytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
