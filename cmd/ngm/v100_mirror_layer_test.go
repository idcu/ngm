package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// layerOneResiduePath 在隔离 home 的 mirror 布局下建一个"被中断的克隆"：
// 有体积、没有 ref，并在它下面留一个临时包。
//
// 形状照抄实测：本机探到一个 66.82 MiB 的 `objects/pack/tmp_pack_*`，
// 对象库为空、`packed-refs` 不存在（见 ADR-030 的读数）。
func layerOneResiduePath(t *testing.T, rel, name string, bytes int, age time.Duration) string {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(layout.MirrorRoot(), filepath.FromSlash(rel))
	for _, d := range []string{"objects/pack", "refs"} {
		if merr := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); merr != nil {
			t.Fatal(merr)
		}
	}
	if werr := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if werr := os.WriteFile(filepath.Join(dir, "config"), []byte("[core]\n\tbare = true\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	p := filepath.Join(dir, "objects", "pack", name)
	if werr := os.WriteFile(p, make([]byte, bytes), 0o644); werr != nil {
		t.Fatal(werr)
	}
	when := time.Now().Add(-age)
	if cerr := os.Chtimes(p, when, when); cerr != nil {
		t.Fatal(cerr)
	}
	return p
}

// TestV100LayerOneIsReportedAndReclaimed 是 v0.100 三件事的端到端闭环：
//
//	① 残骸可回收：`store prune` 清掉被中断的克隆留下的临时包（并且只清够旧的）；
//	② 占用可见：`store usage` 说出 mirror 层有什么、哪个仓库一个 ref 都没有；
//	③ 历史可整理：`store gc` 跑 `git gc`，且不碰它不该碰的目录。
//
// 它守的边界与层 2 那条同源：一个删除类命令最危险的形态不是"没删干净"，
// 而是"顺手删了别的东西"——这里"别的东西"就是**可证明性**（mirror 里的历史）。
func TestV100LayerOneIsReportedAndReclaimed(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}

	// 一个**真的** mirror（本地克隆），它必须活到最后。
	src := testutils.NewGitRepo(t)
	head := src.WriteFile("a.ts", "export const a = 1\n").Commit("feat: a")
	seedMirror(t, "github:v100/real", src.Dir)
	realMirror := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).
		PathFor(resolve.MustNormalize("github:v100/real"))

	// 一次被中断的克隆：1 KiB 的旧临时包 + 512 B 的新临时包。
	const interrupted = "github.com/v100/interrupted.git"
	interruptedDir := filepath.Join(layout.MirrorRoot(), filepath.FromSlash(interrupted))
	old := layerOneResiduePath(t, interrupted, "tmp_pack_OLD", 1024, 2*vendor.MirrorResidueGrace)
	young := layerOneResiduePath(t, interrupted, "tmp_pack_YOUNG", 512, 0)

	t.Run("usage reports layer one", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "usage")
		if code != 0 {
			t.Fatalf("store usage exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "mirror (layer 1): ") {
			t.Errorf("layer 1 must be reported (it is the root of everything else):\n%s", out)
		}
		if !strings.Contains(out, "2 repo(s)") {
			t.Errorf("both mirrors must be counted:\n%s", out)
		}
		// 这是这一版最值钱的一条读数：有体积、却解析不出任何 commit 的目录。
		// （v0.101 把判据与措辞换成了成因口径——`NOT A MIRROR`，理由见
		// `vendor.MirrorEntry.NotMirror`：**从空上游成功克隆**的 mirror 也没有 ref。）
		if !strings.Contains(out, "NOT A MIRROR") {
			t.Errorf("the interrupted clone must be flagged:\n%s", out)
		}
		if !strings.Contains(out, "1 path(s) are NOT mirrors") {
			t.Errorf("the note must say how many and what to do:\n%s", out)
		}
		if !strings.Contains(out, interrupted) {
			t.Errorf("the flagged repo must be named:\n%s", out)
		}
		if !strings.Contains(out, "2 temporary pack(s)") {
			t.Errorf("both residue files must be counted:\n%s", out)
		}
	})

	t.Run("prune removes only the old residue", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune")
		if code != 0 {
			t.Fatalf("store prune exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "removed 1 mirror residue file(s)") {
			t.Errorf("exactly one file should have been reclaimed:\n%s", out)
		}
		if !strings.Contains(out, "tmp_pack_OLD") {
			t.Errorf("the removed file must be named:\n%s", out)
		}
		// 措辞是 item(s)：v0.101 起这个数**两类都算**（临时包与没落地的克隆目录），
		// 因为保护它们的是同一个东西——一次进行中的克隆。
		if !strings.Contains(out, "kept 1 mirror residue item(s) younger than "+vendor.MirrorResidueGrace.String()) {
			t.Errorf("the young residue must be explained, not silently kept:\n%s", out)
		}
		if _, serr := os.Stat(old); !os.IsNotExist(serr) {
			t.Errorf("the old residue should be gone, stat err = %v", serr)
		}
		if _, serr := os.Stat(young); serr != nil {
			t.Errorf("the young residue must survive a concurrent clone's window: %v", serr)
		}
	})

	t.Run("gc dry-run says what it would do without doing it", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "gc", "--dry-run")
		if code != 0 {
			t.Fatalf("store gc --dry-run exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "would collect 1 mirror(s)") {
			t.Errorf("only the usable mirror is collectable:\n%s", out)
		}
		if !strings.Contains(out, "skipped 1 mirror(s) whose clone never finished") {
			t.Errorf("the skip must be explained:\n%s", out)
		}
	})

	t.Run("gc collects the usable mirror and leaves the history provable", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "gc")
		if code != 0 {
			t.Fatalf("store gc exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "collected 1 mirror(s)") {
			t.Errorf("the usable mirror must be collected:\n%s", out)
		}
		if !strings.Contains(out, "unreachable objects for 2 weeks") {
			t.Errorf("保留窗口要说出来（它正是 ngm 不传 --prune=now 的理由）：\n%s", out)
		}
		// **整理之后仍然可证明**：那个 commit 还得解析得出来。
		got := strings.TrimSpace(testutils.GitOutput(t, realMirror, "rev-parse", "main"))
		if got != head {
			t.Errorf("after gc the mirror resolves main=%q want %q", got, head)
		}
	})

	t.Run("the interrupted clone is still there for the user to decide", func(t *testing.T) {
		// ngm 不删它：那是一个目录，不是残骸文件。报出来 + 给出路（usage 的 note）。
		if _, serr := os.Stat(interruptedDir); serr != nil {
			t.Errorf("ngm must not remove a directory it did not create: %v", serr)
		}
	})
}

// TestV100StoreGCReportsGitFailureInsteadOfPretending 测量 `store gc` 的**码 4**。
//
// 夹具是一个**真实存在**的破损形状：refs 指向一个不存在的对象（部分恢复/损坏的镜像）。
// 它通过了"是不是裸仓库"与"有没有 ref"两道快速判定，因此会真的走到 git 面前——
// 而 git 会拒绝它。这一条同时钉住两件事：
//
//  1. git 的失败**不伪装成成功**（退出码 4，不是 0）；
//  2. 已经整理过的那部分**照样报出来**（用户要不要重跑取决于它）。
func TestV100StoreGCReportsGitFailureInsteadOfPretending(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// 一个能用的 mirror（它必须被整理）
	src := testutils.NewGitRepo(t)
	src.WriteFile("a.ts", "export const a = 1\n").Commit("feat: a")
	seedMirror(t, "github:v100/good", src.Dir)

	// 一个"能用但坏了"的 mirror：**先用真克隆建出来**（因此它有镜像 refspec，
	// 会真的走到 git 面前），再把一个 ref 指向不存在的对象。
	//
	// v0.101 改了这里：此前它是手搭的裸仓库（只有 `core.bare=true`），而那形状
	// 在 v0.101 起会被判成"克隆没走完"从而**被 `store gc` 跳过**——于是这条测量
	// 量到的不再是"git 失败"，而是"跳过"。手搭的形状与"真 mirror 坏掉"不是一回事。
	seedMirror(t, "github:v100/broken", src.Dir)
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	brokenRepo := "github.com/v100/broken.git"
	broken := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).
		PathFor(resolve.MustNormalize("github:v100/broken"))
	// 40 个十六进制字符，但那个对象不存在。
	missing := strings.Repeat("deadbeef", 5)
	if werr := os.MkdirAll(filepath.Join(broken, "refs", "heads"), 0o755); werr != nil {
		t.Fatal(werr)
	}
	if werr := os.WriteFile(filepath.Join(broken, "refs", "heads", "main"), []byte(missing+"\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}

	// 子测试名**写成字面量**：`exitCodeElsewhere` 里的指针是
	// `TestV100StoreGCReportsGitFailureInsteadOfPretending/store gc`，
	// 而 TestV56 从源码核对那个名字真的存在（表驱动的名字它看不见）。
	t.Run("store gc", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "gc")
		if code != 4 {
			t.Fatalf("exit=%d want 4 (git failed on one mirror):\n%s", code, out)
		}
		if !strings.Contains(out, "git gc failed on "+brokenRepo) {
			t.Errorf("the failing mirror must be named:\n%s", out)
		}
		// 已做完的那部分照样报出来——否则用户不知道要不要重跑。
		if !strings.Contains(out, "collected 1 mirror(s)") {
			t.Errorf("the mirrors that were collected must still be reported:\n%s", out)
		}
		if !strings.Contains(out, "github.com/v100/good.git") {
			t.Errorf("the collected mirror must be named:\n%s", out)
		}
	})
}
