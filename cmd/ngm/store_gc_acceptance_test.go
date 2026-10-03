package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/vendor"
)

// TestV11StorePruneOrphansAcceptance 端到端验证 `ngm store prune --orphans`。
//
// 它是本项目**第一条会删掉"内容字节"**的路径（此前的 `prune` 只删解包残骸）。
// 因此这里守的不是"删得干不干净"，而是它的**边界**：
//
//  1. **不带 `--orphans` 时一个 blob 都不动**（默认行为一字不变——这是最重要的那条）；
//  2. 带 `--orphans` 时只删"没有任何清单引用、且比门槛更旧"的 blob；
//  3. `--dry-run` 一个都不删，但要说清"将会删什么"；
//  4. `--older-than` 不带 `--orphans` 是**用错**，必须报错而不是默默无效（exit 3）；
//  5. 有清单读不出来时**拒绝删除**（那时"无人引用"只是个下界）。
func TestV11StorePruneOrphansAcceptance(t *testing.T) {
	isolateUserEnv(t)

	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	store := vendor.NewContentStore(layout.ContentRoot())

	// 直接往池里放一个孤儿（没有任何清单引用它），并把它做旧。
	orphanName := fmt.Sprintf("%x", sha256.Sum256([]byte("v11-orphan")))
	orphanPath := store.BlobPath(orphanName)
	if merr := os.MkdirAll(filepath.Dir(orphanPath), 0o755); merr != nil {
		t.Fatal(merr)
	}
	if werr := os.WriteFile(orphanPath, []byte("orphaned bytes"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	old := time.Now().Add(-72 * time.Hour)
	if cerr := os.Chtimes(orphanPath, old, old); cerr != nil {
		t.Fatal(cerr)
	}

	t.Run("without --orphans nothing in the pool is touched", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if _, serr := os.Stat(orphanPath); serr != nil {
			t.Error("不带 --orphans 时**绝不能**动 blob")
		}
		if !strings.Contains(out, "untouched") {
			t.Errorf("安全声明要在输出里：\n%s", out)
		}
	})

	t.Run("--dry-run reports but removes nothing", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune", "--orphans", "--dry-run")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "would remove 1 orphaned blob") {
			t.Errorf("dry-run 应当报出 1 个将被删除的孤儿：\n%s", out)
		}
		if _, serr := os.Stat(orphanPath); serr != nil {
			t.Error("dry-run 不能真的删")
		}
	})

	t.Run("--orphans removes it, and says how many referenced bytes it left alone", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune", "--orphans")
		if code != 0 {
			t.Fatalf("exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "removed 1 orphaned blob") {
			t.Errorf("应当删掉那个孤儿：\n%s", out)
		}
		if !strings.Contains(out, "referenced blob(s) untouched") {
			t.Errorf("被引用的 blob 数也要报（安全声明）：\n%s", out)
		}
		if _, serr := os.Stat(orphanPath); serr == nil {
			t.Error("够老的孤儿应当被删掉")
		}
	})

	t.Run("--older-than without --orphans is a mistake", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune", "--older-than=1h")
		if code != 3 {
			t.Errorf("用错 flag 应当 exit 3，得到 %d：\n%s", code, out)
		}
		if !strings.Contains(out, "--orphans") {
			t.Errorf("错误要指出正确的用法：\n%s", out)
		}
	})

	t.Run("refuses when a manifest is unreadable", func(t *testing.T) {
		// 造一份读不出的清单：它的 blob 会被算成孤儿，而它引用了什么看不见。
		dg := "sha256:" + fmt.Sprintf("%064x", 1)
		mpath := store.ManifestPath(dg)
		if merr := os.MkdirAll(filepath.Dir(mpath), 0o755); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(mpath, []byte("{not json"), 0o644); werr != nil {
			t.Fatal(werr)
		}
		young := fmt.Sprintf("%x", sha256.Sum256([]byte("v11-young")))
		ypath := store.BlobPath(young)
		if merr := os.MkdirAll(filepath.Dir(ypath), 0o755); merr != nil {
			t.Fatal(merr)
		}
		if werr := os.WriteFile(ypath, []byte("z"), 0o644); werr != nil {
			t.Fatal(werr)
		}

		code, out := runCaptureCode(t, "store", "prune", "--orphans")
		if code == 0 {
			t.Errorf("有清单读不出来时必须拒绝回收，exit=%d：\n%s", code, out)
		}
		if _, serr := os.Stat(ypath); serr != nil {
			t.Error("拒绝时一个 blob 都不能删")
		}
	})
}
