package vendor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

// seedMirrorFixture 建一个**真的** mirror（本地克隆一个真仓库），返回 mirror、
// 源仓库的 HEAD 与那个仓库的相对路径。
//
// 为什么不用手搭目录：残骸、包、refs 这三样的形状是 git 说了算的，手搭的夹具
// 只能证明"我的代码同意我的想象"。
func seedMirrorFixture(t *testing.T) (*Mirror, string, string) {
	t.Helper()
	m, repo, _ := newFixtureMirror(t)
	src := t.TempDir()
	testutils.GitInit(t, src)
	testutils.GitWriteFile(t, src, "a.ts", "export const a = 1\n")
	head := testutils.GitCommit(t, src, "feat: a")
	if _, err := m.EnsureLocal(context.Background(), repo, src); err != nil {
		t.Fatalf("EnsureLocal: %v", err)
	}
	return m, head, repo.MirrorRelPath() + ".git"
}

// writeFileAt 写一个文件并把它的 mtime 设到指定的过去。
//
// 残骸的年龄是这一版唯一的判定输入，因此夹具必须能**精确**控制它
// （而不是靠 sleep——那测的是时钟，不是代码）。
func writeFileAt(t *testing.T, path string, content []byte, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

// makeBareShell 造一个"看起来是裸仓库、但一个 ref 都没有"的目录——
// 即一次被中断的 `git clone --mirror` 留在磁盘上的形状。
func makeBareShell(t *testing.T, dir string) {
	t.Helper()
	for _, d := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// config 照抄 v0.101 **实测**到的形状（一个被中断的 `git clone --mirror`）：
	// core.bare=true、**有** remote.origin.url、但**没有** remote.origin.fetch。
	// 那条缺失的 refspec 就是"这次克隆没走完"的指纹（见 git.IsMirror 的说明）。
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(
		"[core]\n\tbare = true\n[remote \"origin\"]\n\turl = https://example.invalid/o/r.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMirrorUsageReportsWhatTheMirrorHolds 是这张网的**小计对账**：
// 明细与合计必须逐项相等。
//
// 为什么这一条比"报了几个数"更重要：一份小计与明细对不上的报告，会让用户
// 按合计去腾空间，而真正的占用在别处——那是仪器说谎的形状（v0.6 复盘 §5.1）。
func TestMirrorUsageReportsWhatTheMirrorHolds(t *testing.T) {
	m, _, wantRepo := seedMirrorFixture(t)

	u, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if !u.Exists {
		t.Fatalf("Exists should be true after EnsureLocal")
	}
	if u.Repos != 1 || len(u.Entries) != 1 {
		t.Fatalf("want exactly one mirror row, got %d row(s) / Repos=%d", len(u.Entries), u.Repos)
	}
	e := u.Entries[0]
	if e.Repo != wantRepo {
		t.Errorf("Repo=%q want %q", e.Repo, wantRepo)
	}
	if e.Bytes <= 0 {
		t.Errorf("Bytes should be positive, got %d", e.Bytes)
	}
	if e.NotMirror {
		t.Errorf("a real mirror must not be reported as not a mirror")
	}
	if e.ResidueCount != 0 {
		t.Errorf("a clean mirror must have no residue, got %d", e.ResidueCount)
	}
	// **刚建的 mirror 可以一个包都没有**（实测：本地克隆默认落的是松散对象，
	// 于是 PackBytes=0 而 Bytes>0）。这一条钉住的是"0 不是错误"，
	// 免得后来者看到 0 就以为读数坏了、或者把它当成"没有历史"。
	if e.PackBytes != 0 {
		t.Errorf("a fresh local clone is loose, so PackBytes should be 0 (got %d)", e.PackBytes)
	}

	// 小计 = 逐行之和（三组数各对一次）。
	if u.Bytes != e.Bytes {
		t.Errorf("total Bytes=%d but the only row says %d", u.Bytes, e.Bytes)
	}
	if u.PackBytes != e.PackBytes {
		t.Errorf("total PackBytes=%d but the only row says %d", u.PackBytes, e.PackBytes)
	}
	// 历史本体不可能比整个目录还大：这条不变量能在"把别的目录算进来"时立刻红。
	if u.PackBytes > u.Bytes {
		t.Errorf("packed history (%d) cannot exceed the whole directory (%d)", u.PackBytes, u.Bytes)
	}
}

// TestMirrorUsageCountsResidueAgainstTheRightRepo 固定残骸的**归属**。
//
// 一个只报总量的读数在"哪个 mirror 需要清"这个问题上是没用的；而归属错了
// 比不报更糟——用户会去删一个干净仓库里的东西。
func TestMirrorUsageCountsResidueAgainstTheRightRepo(t *testing.T) {
	m, _, cleanRepo := seedMirrorFixture(t)

	// 第二个仓库：一次被中断的克隆（有体积、没有 ref），并在它下面留一个临时包。
	const residueBytes = 4096
	shellRepo := "github.com/org/interrupted.git"
	shellDir := filepath.Join(m.Root(), filepath.FromSlash(shellRepo))
	makeBareShell(t, shellDir)
	writeFileAt(t, filepath.Join(shellDir, "objects", "pack", "tmp_pack_XX"), make([]byte, residueBytes), 0)

	u, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	byRepo := map[string]MirrorEntry{}
	for _, e := range u.Entries {
		byRepo[e.Repo] = e
	}
	if len(byRepo) != 2 {
		t.Fatalf("want 2 rows, got %d: %+v", len(byRepo), u.Entries)
	}

	shell, ok := byRepo[shellRepo]
	if !ok {
		t.Fatalf("the interrupted mirror is missing from the report: %+v", u.Entries)
	}
	if !shell.NotMirror {
		t.Errorf("a bare shell with no refs must be reported as not a mirror")
	}
	if shell.ResidueCount != 1 || shell.ResidueBytes != residueBytes {
		t.Errorf("residue on the shell = %d file(s) / %d bytes, want 1 / %d",
			shell.ResidueCount, shell.ResidueBytes, residueBytes)
	}
	if clean, ok := byRepo[cleanRepo]; !ok {
		t.Fatalf("the clean mirror fell out of the report")
	} else if clean.ResidueCount != 0 || clean.ResidueBytes != 0 {
		t.Errorf("residue was attributed to the wrong repo: %+v", clean)
	}
	if u.NotMirrorCount != 1 {
		t.Errorf("NotMirrorCount=%d want 1", u.NotMirrorCount)
	}
	// 残骸**已经**占着磁盘，所以它必须出现在总量里——否则"清了能省多少"无从谈起。
	if u.ResidueBytes != residueBytes {
		t.Errorf("total ResidueBytes=%d want %d", u.ResidueBytes, residueBytes)
	}
	if u.Bytes < residueBytes {
		t.Errorf("total Bytes=%d must include the residue (%d)", u.Bytes, residueBytes)
	}
}

// TestMirrorPruneResidueRemovesOnlyOldResidue 是这一版的核心行为：
// 旧的残骸走，年轻的留下，别的一律不动。
func TestMirrorPruneResidueRemovesOnlyOldResidue(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	packDir := filepath.Join(m.Root(), filepath.FromSlash(rel), "objects", "pack")

	old := filepath.Join(packDir, "tmp_pack_OLD")
	young := filepath.Join(packDir, "tmp_pack_YOUNG")
	writeFileAt(t, old, make([]byte, 2048), 2*time.Hour) // 比门槛（1h）旧
	writeFileAt(t, young, make([]byte, 1024), 0)         // 刚写下

	// ---- dry-run：什么都不许删 ----
	dry, err := m.PruneResidue(MirrorResidueGrace, time.Now(), true)
	if err != nil {
		t.Fatalf("PruneResidue(dry-run): %v", err)
	}
	if len(dry.Removed) != 1 || !strings.HasSuffix(dry.Removed[0], "tmp_pack_OLD") {
		t.Errorf("dry-run should list exactly the old residue, got %v", dry.Removed)
	}
	if dry.DryRun != true || dry.Touched != 1 || dry.Scanned != 1 {
		t.Errorf("dry-run result shape: DryRun=%v Touched=%d Scanned=%d",
			dry.DryRun, dry.Touched, dry.Scanned)
	}
	if _, serr := os.Stat(old); serr != nil {
		t.Fatalf("dry-run must not remove anything: %v", serr)
	}

	// ---- 真跑 ----
	res, err := m.PruneResidue(MirrorResidueGrace, time.Now(), false)
	if err != nil {
		t.Fatalf("PruneResidue: %v", err)
	}
	if _, serr := os.Stat(old); !os.IsNotExist(serr) {
		t.Errorf("the old residue should be gone, stat err = %v", serr)
	}
	if _, serr := os.Stat(young); serr != nil {
		t.Errorf("residue younger than the grace must survive: %v", serr)
	}
	if res.BytesFreed != 2048 {
		t.Errorf("BytesFreed=%d want 2048", res.BytesFreed)
	}
	if res.KeptYoung != 1 || res.KeptYoungBytes != 1024 {
		t.Errorf("KeptYoung=%d/%d want 1/1024", res.KeptYoung, res.KeptYoungBytes)
	}
	if res.Scanned != 1 || res.Touched != 1 {
		t.Errorf("Scanned=%d Touched=%d want 1/1 (one repo scanned, one touched)", res.Scanned, res.Touched)
	}
}

// TestMirrorPruneResidueNeverTouchesPackedHistory 是**安全边界**的牙齿：
// 残骸是 git 自己的临时文件，而对象库（松散对象与包）是内容。
//
// 它对应层 2 的那条"绝不碰内容树"（ADR-018 决策 1/5）：一个清理命令如果不能
// 被证明"只清它该清的那一类"，用户就没有理由在真机器上跑它。
func TestMirrorPruneResidueNeverTouchesPackedHistory(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	repoDir := filepath.Join(m.Root(), filepath.FromSlash(rel))

	// 先把松散对象收成包，这样"内容"的两种形态同时在磁盘上（实测：本地克隆
	// 默认只落松散对象，不跑这一步就没有 .pack 可保护）。
	if _, err := m.GC(context.Background(), false); err != nil {
		t.Fatalf("GC: %v", err)
	}
	packs, err := filepath.Glob(filepath.Join(repoDir, "objects", "pack", "*.pack"))
	if err != nil || len(packs) == 0 {
		t.Fatalf("fixture has no packfile to protect (err=%v, %v)", err, packs)
	}
	// 对象库里除残骸以外的**每一个**文件都要被保护（松散对象也在里面）。
	var objects []string
	if werr := filepath.WalkDir(filepath.Join(repoDir, "objects"),
		func(p string, d os.DirEntry, e error) error {
			if e != nil || d.IsDir() {
				return e
			}
			if strings.HasPrefix(d.Name(), mirrorResiduePrefix) {
				return nil
			}
			objects = append(objects, p)
			return nil
		}); werr != nil {
		t.Fatal(werr)
	}
	if len(objects) == 0 {
		t.Fatalf("fixture has no objects at all — the net would pass vacuously")
	}

	res, err := m.PruneResidue(MirrorResidueGrace, time.Now(), false)
	if err != nil {
		t.Fatalf("PruneResidue: %v", err)
	}
	if len(res.Removed) != 0 {
		t.Fatalf("nothing in a clean mirror is residue, got %v", res.Removed)
	}
	for _, p := range objects {
		if _, serr := os.Stat(p); serr != nil {
			t.Errorf("history was touched: %s (%v)", p, serr)
		}
	}
}

// TestMirrorGCPacksLooseObjectsSoTheReportChanges 把"看得见"与"点得动"接起来：
//
// `store usage` 报的是 packfile，而一次本地克隆落的是松散对象（PackBytes=0）。
// `store gc` 之后同一个读数必须**变**——否则这条命令就只是"跑了个 git，看起来
// 做了点什么"，而用户没有任何办法验证它真的整理了东西。
func TestMirrorGCPacksLooseObjectsSoTheReportChanges(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	ctx := context.Background()

	before, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if before.PackBytes != 0 {
		t.Fatalf("fixture assumption broken: a fresh local clone should be loose, got %d", before.PackBytes)
	}

	if _, gerr := m.GC(ctx, false); gerr != nil {
		t.Fatalf("GC: %v", gerr)
	}

	after, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage(after gc): %v", err)
	}
	if after.PackBytes <= 0 {
		t.Errorf("after gc the mirror should have packed history, got %d", after.PackBytes)
	}
	if after.PackBytes > after.Bytes {
		t.Errorf("packed history (%d) cannot exceed the whole directory (%d)", after.PackBytes, after.Bytes)
	}
	if len(after.Entries) != 1 || after.Entries[0].Repo != rel {
		t.Errorf("gc must not change what the report lists: %+v", after.Entries)
	}
	if after.NotMirrorCount != 0 {
		t.Errorf("gc must not make a mirror look not a mirror")
	}
}

// TestMirrorPruneResidueRejectsZeroGrace 是年龄门槛的**负例**。
//
// 0 会让并发安全窗完全失效：另一个进程正在写的临时包会被删掉，
// 而 mirror 层没有跨进程锁（mirror.go 的 Mirror 文档如实写了这一点）。
func TestMirrorPruneResidueRejectsZeroGrace(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	old := filepath.Join(m.Root(), filepath.FromSlash(rel), "objects", "pack", "tmp_pack_OLD")
	writeFileAt(t, old, make([]byte, 16), 2*time.Hour)

	for _, d := range []time.Duration{0, -time.Minute} {
		if _, err := m.PruneResidue(d, time.Now(), false); err == nil {
			t.Fatalf("grace %v must be refused (0 removes a running clone's temporary pack)", d)
		} else if !strings.Contains(err.Error(), "positive") {
			t.Errorf("grace %v: error should say why, got %v", d, err)
		}
	}
	if _, serr := os.Stat(old); serr != nil {
		t.Errorf("a refused call must not have deleted anything: %v", serr)
	}
}

// TestMirrorGCSkipsWhatItMustAndLeavesTheRestWorking 固定 `store gc` 的两条边界：
//
//  1. **只整理能整理的**：没有 ref 的目录（中断的克隆）与不是裸仓库的目录
//     都被跳过并**点名报出**——它们的体积不是"需要整理的历史"；
//  2. **整理之后 mirror 仍然能解析**：`git gc` 不能把可证明性弄没。
func TestMirrorGCSkipsWhatItMustAndLeavesTheRestWorking(t *testing.T) {
	m, head, rel := seedMirrorFixture(t)
	ctx := context.Background()

	shellRepo := "github.com/org/interrupted.git"
	makeBareShell(t, filepath.Join(m.Root(), filepath.FromSlash(shellRepo)))
	junkRepo := "github.com/org/junk.git"
	if err := os.MkdirAll(filepath.Join(m.Root(), filepath.FromSlash(junkRepo)), 0o755); err != nil {
		t.Fatal(err)
	}

	// ---- dry-run：必须**一个 git 子进程都不起**（仪器断言，不是掐表）----
	git.ResetSpawnCount()
	dry, err := m.GC(ctx, true)
	if err != nil {
		t.Fatalf("GC(dry-run): %v", err)
	}
	if n := git.SpawnCount(); n != 0 {
		t.Errorf("dry-run started %d git process(es); it must only look", n)
	}
	if len(dry.Collected) != 1 || dry.Collected[0] != rel {
		t.Errorf("dry-run should list exactly the usable mirror, got %v", dry.Collected)
	}
	if dry.Reclaimed() != 0 {
		t.Errorf("a dry run cannot know what it would reclaim, got %d", dry.Reclaimed())
	}

	// ---- 真跑 ----
	res, err := m.GC(ctx, false)
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if len(res.Collected) != 1 || res.Collected[0] != rel {
		t.Errorf("Collected=%v want [%s]", res.Collected, rel)
	}
	if len(res.SkippedIncomplete) != 1 || res.SkippedIncomplete[0] != shellRepo {
		t.Errorf("SkippedIncomplete=%v want [%s]", res.SkippedIncomplete, shellRepo)
	}
	if len(res.SkippedNotMirror) != 1 || res.SkippedNotMirror[0] != junkRepo {
		t.Errorf("SkippedNotMirror=%v want [%s]", res.SkippedNotMirror, junkRepo)
	}
	if res.BytesBefore <= 0 || res.BytesAfter <= 0 {
		t.Errorf("前后体积都要报出来: before=%d after=%d", res.BytesBefore, res.BytesAfter)
	}
	// 被跳过的目录必须原样还在（ngm 不删不属于自己的东西）。
	for _, p := range []string{
		filepath.Join(m.Root(), filepath.FromSlash(shellRepo)),
		filepath.Join(m.Root(), filepath.FromSlash(junkRepo)),
	} {
		if _, serr := os.Stat(p); serr != nil {
			t.Errorf("a skipped directory disappeared: %s (%v)", p, serr)
		}
	}

	// **整理之后仍然可证明**：那个 commit 还得解析得出来。
	got := strings.TrimSpace(testutils.GitOutput(t, filepath.Join(m.Root(), filepath.FromSlash(rel)), "rev-parse", "main"))
	if got != head {
		t.Errorf("after gc the mirror resolves main=%q want %q", got, head)
	}
}

// TestMirrorUsageOnMissingRootIsNotEmpty 固定"还没有 mirror"不是错误。
func TestMirrorUsageOnMissingRootIsNotEmpty(t *testing.T) {
	m := NewMirror(filepath.Join(t.TempDir(), "does-not-exist"), git.Options{})

	u, err := m.Usage()
	if err != nil {
		t.Fatalf("a missing mirror root must not be an error: %v", err)
	}
	if u.Exists {
		t.Errorf("Exists must be false when the root is missing")
	}
	if u.Repos != 0 || len(u.Entries) != 0 {
		t.Errorf("nothing should be reported, got %d row(s)", len(u.Entries))
	}

	// 三个回收/整理入口在"什么都没有"时都必须是廉价的 no-op。
	if res, err := m.PruneResidue(MirrorResidueGrace, time.Now(), false); err != nil || len(res.Removed) != 0 {
		t.Errorf("PruneResidue on a missing root: %v / %v", res.Removed, err)
	}
	if res, err := m.GC(context.Background(), false); err != nil || len(res.Collected) != 0 {
		t.Errorf("GC on a missing root: %v / %v", res.Collected, err)
	}
}

// TestIsLandingDirNameClaimsOnlyOurOwnNames 固定**归属判据**的形状。
//
// 它是"ngm 只删自己建的东西"里最重的一次删除（`os.RemoveAll` 一个目录）的前提，
// 因此两个方向都要钉住：我们的名字必须认出来，别人的名字**一个都不许**认。
func TestIsLandingDirNameClaimsOnlyOurOwnNames(t *testing.T) {
	ours := []string{
		"repo.git" + CloneLandingInfix + "a1b2c3d4",
		"repo.git" + CloneLandingInfix + "12345678", // 十进制 pid 回退（十六进制字符的子集）
		"repo.git" + CloneLandingInfix + "0",
	}
	for _, n := range ours {
		if !isLandingDirName(n) {
			t.Errorf("%q is a name ngm creates — it must be claimed", n)
		}
	}

	theirs := []string{
		"repo.git",                              // 通用名字：ADR-030 决定**不删**
		"repo.git" + CloneLandingInfix,          // 没有后缀
		CloneLandingInfix + "a1b2c3d4",          // 没有 .git 基名
		".git" + CloneLandingInfix + "a1",       // 空基名
		"repo.git" + CloneLandingInfix + "A1B2", // 大写不是我们写的
		"repo.git" + CloneLandingInfix + "a1-z", // 非十六进制
		"repo.git" + CloneLandingInfix + "a1.z",
		"repo.tar" + CloneLandingInfix + "a1",          // 不是 .git 的延伸
		"repo.git.clone-a1" + CloneLandingInfix + "b2", // 后缀里再来一段（我们不会这么写）
	}
	for _, n := range theirs {
		if isLandingDirName(n) {
			t.Errorf("%q is NOT a name ngm creates — claiming it would delete a directory we did not build", n)
		}
	}
}

// TestMirrorUsageSeesUnlandedClones 是这一版的核心可见性：
// **没落地的克隆必须出现在报告里**。
//
// v0.100 的实测就是这么来的：一个 66.82 MiB 的目录躺在 mirror 根下，
// 而当时的 `usage` 谁也看不见它（它不以 `.git` 结尾，因此不是一行仓库）。
func TestMirrorUsageSeesUnlandedClones(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	landing := filepath.Join(m.Root(), filepath.FromSlash(rel)+CloneLandingInfix+"deadbeef")
	if err := os.MkdirAll(landing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(landing, "HEAD"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(u.Landings) != 1 {
		t.Fatalf("the unfinished clone must be reported, got %v", u.Landings)
	}
	if !strings.HasSuffix(u.Landings[0], CloneLandingInfix+"deadbeef") {
		t.Errorf("Landings[0]=%q", u.Landings[0])
	}
	if u.LandingBytes < 2048 {
		t.Errorf("LandingBytes=%d want >= 2048", u.LandingBytes)
	}
	// **不能混进仓库那一列**：小计的语义是"逐行之和"，而它不是一行。
	if u.Repos != 1 || u.Bytes == u.LandingBytes {
		t.Errorf("a landing dir is not a repo: Repos=%d Bytes=%d LandingBytes=%d",
			u.Repos, u.Bytes, u.LandingBytes)
	}
}

// TestPruneReclaimsUnlandedClonesAndNothingElse 是这一版最重的一次删除的边界。
//
// 三个方向：旧的落地目录走、年轻的留下、**别的任何目录都不许动**。
func TestPruneReclaimsUnlandedClonesAndNothingElse(t *testing.T) {
	m, _, rel := seedMirrorFixture(t)
	parent := filepath.Join(m.Root(), filepath.FromSlash(rel))
	oldDir := parent + CloneLandingInfix + "aaaa1111"
	youngDir := parent + CloneLandingInfix + "bbbb2222"
	// 一个"长得有点像"的目录：它是**用户的东西**，不是我们的名字。
	foreign := filepath.Join(filepath.Dir(parent), "notes.git.backup")

	for _, d := range []string{oldDir, youngDir, foreign} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "payload"), make([]byte, 1024), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * MirrorResidueGrace)
	if err := os.Chtimes(oldDir, past, past); err != nil {
		t.Fatal(err)
	}

	res, err := m.PruneResidue(MirrorResidueGrace, time.Now(), false)
	if err != nil {
		t.Fatalf("PruneResidue: %v", err)
	}
	if len(res.LandingsRemoved) != 1 || !strings.HasSuffix(res.LandingsRemoved[0], "aaaa1111") {
		t.Errorf("only the old landing dir should go, got %v", res.LandingsRemoved)
	}
	if _, serr := os.Stat(oldDir); !os.IsNotExist(serr) {
		t.Errorf("the old landing dir should be gone (%v)", serr)
	}
	if _, serr := os.Stat(youngDir); serr != nil {
		t.Errorf("a landing dir younger than the grace must survive: %v", serr)
	}
	if _, serr := os.Stat(foreign); serr != nil {
		t.Errorf("a directory that is not named like ours must NEVER be removed: %v", serr)
	}
	if res.LandingBytesFreed < 1024 {
		t.Errorf("LandingBytesFreed=%d want >= 1024", res.LandingBytesFreed)
	}
	if res.KeptYoung < 1 {
		t.Errorf("the young landing dir must be reported as kept-young, got %d", res.KeptYoung)
	}
	// 层 1 的另一类残骸（临时包）不该被这次调用碰到：那个仓库是干净的。
	if len(res.Removed) != 0 {
		t.Errorf("no temporary packs exist in this fixture, got %v", res.Removed)
	}
}

// TestMirrorUsageIsDeterministic 固定顺序：遍历顺序不能泄漏进输出。
//
// 依赖目录顺序的报告会让人误以为"内容变了"（与 ContentStoreUsage 同一条理由）。
func TestMirrorUsageIsDeterministic(t *testing.T) {
	m, _, _ := seedMirrorFixture(t)
	for _, slug := range []string{"github:zzz/last", "github:org/middle", "github:aaa/first"} {
		dir := filepath.Join(m.Root(), filepath.FromSlash(resolve.MustNormalize(slug).MirrorRelPath()+".git"))
		makeBareShell(t, dir)
	}
	u, err := m.Usage()
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if len(u.Entries) != 4 {
		t.Fatalf("want 4 rows, got %d", len(u.Entries))
	}
	for i := 1; i < len(u.Entries); i++ {
		if u.Entries[i-1].Repo >= u.Entries[i].Repo {
			t.Fatalf("rows are not sorted by repo: %q then %q",
				u.Entries[i-1].Repo, u.Entries[i].Repo)
		}
	}
}
