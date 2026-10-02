package vendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// 这个文件测的是"**store 不完整**"这条路：清单还在（`Has()` 仍为真），但某个 blob 被删了
// ——用户手工清理过 `~/.ngm/content`、磁盘坏了、或别的工具动过它。
//
// 为什么它值得两条测试：这时 `ngm install` 会因为 `Has` 为真而**短路**（那是"无网络也能安装"
// 的承诺），于是失败发生在落地那一刻。于是这条路径上有两个承诺要守住：
//   1. 报错要**点名缺的是哪个条目**、并给出出路（否则用户会以为是**自己项目**的问题）；
//   2. 失败留下的残骸**必须能被 `verify` 认出来是不完整的**（否则就是"看起来通过"）。
// 第 1 条测的是错误的**呈现**（`errs.FormatHuman`，即用户真正看到的那几行）。

// TestV10MissingBlobIsReportedWithAWayOut 守住第 1 条承诺。
func TestV10MissingBlobIsReportedWithAWayOut(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)

	victim := "src/deep/mod.ts"
	if err := os.Remove(contentPathOf(t, store, dg, victim)); err != nil {
		t.Fatalf("删掉 blob 以模拟 store 不完整: %v", err)
	}

	// 不完整**不该**被误判成"没有这份内容"：清单在，所以 Has 仍为真——
	// 测试要照着真实链路走（install 就是被这条 `Has` 短路的）。
	if !store.Has(dg) {
		t.Fatal("清单还在时 Has 应当为真；否则测的不是这条路径")
	}

	// 落地：报出**哪一个条目**缺失，以及出路。
	lt := NewLinkTree(t.TempDir(), store, LinkHardlink)
	_, err := lt.Materialize(dg, "github.com/demo/basic", "")
	if err == nil {
		t.Fatal("blob 缺失时落地必须失败")
	}
	// 断言**用户看到的那几行**，不是 `err.Error()`：hint 由 FormatHuman 渲染
	// （`NgmError.Error()` 按设计只给 message + cause）。
	human := errs.FormatHuman(err)
	for _, want := range []string{victim, "is missing", "ngm install"} {
		if !strings.Contains(human, want) {
			t.Errorf("错误里必须有 %q：\n%s", want, human)
		}
	}
	if strings.Contains(human, "unreadable") {
		t.Errorf("这不是'清单不可读'（另一类故障）：\n%s", human)
	}

	// 读路径同样要给出路（`ngm mappings` 之类的消费方走这里）。
	_, _, rerr := store.ReadFile(dg, victim)
	if rerr == nil {
		t.Fatal("blob 缺失时 ReadFile 必须失败")
	}
	if rh := errs.FormatHuman(rerr); !strings.Contains(rh, "ngm install") {
		t.Errorf("ReadFile 的错误也要给出出路：\n%s", rh)
	}
}

// TestV10MissingBlobResidueIsDetectable 守住第 2 条承诺。
//
// **不测"失败不留残骸"**——代码不承诺那个：`Materialize` 先清空目标再重建，
// 重建中途失败时**已落地的那些文件会留下**（`links.go` 的注释只承诺"下次重建前清空"）。
// 真正要守的是：那份残骸**认得出是不完整的**，而且**下一次 install 能让它恢复**。
func TestV10MissingBlobResidueIsDetectable(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	entries := storeEntries(t, store, dg)

	// 删掉最后一个条目的 blob：它前面的文件会先落地，才测得出"残骸"。
	last := entries[len(entries)-1]
	if last.Full == "" {
		t.Skip("最后一条是 symlink，没有 blob 可删")
	}
	blobCopy, rerr := os.ReadFile(last.Full)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if err := os.Remove(last.Full); err != nil {
		t.Fatalf("删掉 blob: %v", err)
	}

	vendorRoot := t.TempDir()
	lt := NewLinkTree(vendorRoot, store, LinkHardlink)
	dst := filepath.Join(vendorRoot, "github.com", "demo", "basic")
	if _, err := lt.Materialize(dg, "github.com/demo/basic", ""); err == nil {
		t.Fatal("必须失败")
	}

	// 承诺：**不许看起来通过**。残骸（如果有）必须被 verify 报成不完整。
	vr, verr := VerifyVendorAgainstEntries(dst, entries, false)
	if verr != nil {
		t.Fatalf("VerifyVendorAgainstEntries: %v", verr)
	}
	if vr.OK() {
		t.Errorf("store 不完整时 vendor 校验**不能**通过；否则它就是'看起来通过'：%+v", vr)
	}

	// 承诺：**下一次 install 先清空**——所以恢复 blob 之后再落地，残骸不会残留。
	if err := os.WriteFile(last.Full, blobCopy, 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dst, "STALE-FROM-A-FAILED-RUN")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lt.Materialize(dg, "github.com/demo/basic", ""); err != nil {
		t.Fatalf("恢复 blob 后落地应当成功: %v", err)
	}
	if _, serr := os.Stat(stale); serr == nil {
		t.Error("重新落地必须先清空目标目录（残留会让下一次 install 误判）")
	}
	if vr2, _ := VerifyVendorAgainstEntries(dst, entries, true); !vr2.OK() {
		t.Errorf("恢复后应当验得过（deep）：%+v", vr2)
	}
}
