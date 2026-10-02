package vendor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

// contentFixture 建一个 fixture 仓库、写入 content store，返回 (digest, store)。
func contentFixture(t *testing.T, build func(t *testing.T) *testutils.GitRepo) (string, *ContentStore) {
	t.Helper()
	store := newStore(t)
	_, dg := putFixture(t, store, build)
	return dg, store
}

// storeEntries 取某 digest 的条目集合（换布局后"内容侧"只能这样拿）。
func storeEntries(t *testing.T, store *ContentStore, dg string) []ContentEntry {
	t.Helper()
	entries, err := store.Entries(dg)
	if err != nil {
		t.Fatalf("Entries(%s): %v", dg, err)
	}
	return entries
}

// contentPathOf 返回某个树内路径在**当前布局**下的字节位置
// （v2：blob；v1：树目录里的文件）。hardlink 之类的 inode 断言需要它。
func contentPathOf(t *testing.T, store *ContentStore, dg, rel string) string {
	t.Helper()
	for _, e := range storeEntries(t, store, dg) {
		if e.Path == rel {
			if e.Full == "" {
				t.Fatalf("%s has no byte location (symlink?)", rel)
			}
			return e.Full
		}
	}
	t.Fatalf("%s not found in the content store for %s", rel, dg)
	return ""
}

// verifyAgainstStore 用条目版入口做 vendor ↔ store 的一致性校验
// （与 `verify` 走的是同一条路径，见 ADR-019）。
func verifyAgainstStore(t *testing.T, store *ContentStore, dg, vendorTree string, deep bool) VerifyResult {
	t.Helper()
	vr, err := VerifyVendorAgainstEntries(vendorTree, storeEntries(t, store, dg), deep)
	if err != nil {
		t.Fatalf("VerifyVendorAgainstEntries: %v", err)
	}
	return vr
}

func basicRepo(t *testing.T) *testutils.GitRepo {
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const x = 1\n")
	r.WriteFile("src/deep/mod.ts", "export const m = 2\n")
	r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho hi\n")
	r.Commit("feat: basic")
	return r
}

func TestLinkTree_AutoUsesHardlink(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkAuto)

	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if res.Mode != LinkHardlink {
		t.Errorf("auto mode should use hardlink on a normal filesystem, got %s", res.Mode)
	}
	if res.Degraded {
		t.Errorf("unexpected degradation")
	}
	if res.Files != 3 {
		t.Errorf("files=%d want 3", res.Files)
	}

	// 真实存在且内容正确
	body, err := os.ReadFile(filepath.Join(res.Path, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "export const x = 1\n" {
		t.Errorf("content=%q", body)
	}

	// hardlink 语义：vendor 文件与 store 里的字节是同一个 inode
	// （v2 下"store 里的字节"是 blob，不再是树目录里的文件）
	vendorFile := filepath.Join(res.Path, "index.ts")
	if !SameFile(vendorFile, contentPathOf(t, store, dg, "index.ts")) {
		t.Errorf("auto mode should hardlink; %s is not the same file as the stored blob", vendorFile)
	}
}

func TestLinkTree_CopyModeProducesIndependentFiles(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkCopy)

	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if res.Mode != LinkCopy {
		t.Errorf("mode=%s want copy", res.Mode)
	}

	vendorFile := filepath.Join(res.Path, "index.ts")
	contentFile := contentPathOf(t, store, dg, "index.ts")
	if SameFile(vendorFile, contentFile) {
		t.Errorf("copy mode must NOT share an inode with the content store")
	}

	// 内容仍必须一致
	vr := verifyAgainstStore(t, store, dg, res.Path, true)
	if !vr.OK() {
		t.Errorf("copy output should match content: %v", vr.Mismatches)
	}
}

func TestLinkTree_HardlinkModeReportsFailure(t *testing.T) {
	// 强制 hardlink 在跨卷时才失败，测试环境难以构造。
	// 这里只锁定"配置被正确解析"，失败路径由 auto 的降级逻辑覆盖。
	if ParseLinkMode("hardlink") != LinkHardlink {
		t.Errorf("ParseLinkMode(hardlink) failed")
	}
	if ParseLinkMode("") != LinkAuto || ParseLinkMode("bogus") != LinkAuto {
		t.Errorf("invalid linkMode should fall back to auto")
	}
	for _, m := range ValidLinkModes() {
		if !m.IsValid() {
			t.Errorf("%s should be valid", m)
		}
	}
	if LinkMode("nope").IsValid() {
		t.Errorf("bogus mode reported valid")
	}
}

// TestLinkTree_SymlinkModeFallsBackObservably 锁定 v2 下 symlink 模式的**行为变化**：
// 没有"一棵已物化的树"可整目录链接，于是逐条目落地——但**必须如实报告实际用的 mode**
// （vendor-layers.md：所有链接失败路径必须可观测）。
//
// 磁盘收益不变：hardlink 与目录 symlink 同样不复制内容。
func TestLinkTree_SymlinkModeFallsBackObservably(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkSymlink)

	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	// 请求的是 symlink，实际用的是 hardlink——而且**说得出来**。
	if res.Mode == LinkSymlink {
		t.Fatalf("v2 has no materialized tree to symlink; mode must not be reported as symlink")
	}
	if lt.Mode() != LinkSymlink {
		t.Errorf("the *requested* mode must still be visible: Mode()=%s", lt.Mode())
	}
	if res.Files != 3 {
		t.Errorf("fallback lands entry by entry: files=%d want 3", res.Files)
	}

	// 落地结果本身必须是正确的（内容、可执行位都在）
	if vr := verifyAgainstStore(t, store, dg, res.Path, true); !vr.OK() {
		t.Errorf("fallback output must still match the store: %v", vr.Mismatches)
	}
	if !SameFile(filepath.Join(res.Path, "index.ts"), contentPathOf(t, store, dg, "index.ts")) {
		t.Errorf("the fallback should hardlink (no content copies)")
	}
}

func TestLinkTree_SymlinkEntriesAreRebuilt(t *testing.T) {
	repo := func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("target.txt", "content\n")
		r.Commit("target")
		r.AddSymlinkEntry("link.txt", "target.txt")
		r.Commit("link")
		return r
	}
	dg, store := contentFixture(t, repo)

	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	// 用 copy 模式：symlink 条目在任何模式下都应被重建为 symlink
	lt := NewLinkTree(vendorRoot, store, LinkCopy)
	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	linkPath := filepath.Join(res.Path, "link.txt")
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink entry must be rebuilt as a symlink, mode=%v", info.Mode())
	}
	target, _ := os.Readlink(linkPath)
	if target != "target.txt" {
		t.Errorf("symlink target=%q", target)
	}
}

func TestLinkTree_ExecutableBitPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec bits are not expressible on Windows")
	}
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)

	for _, mode := range []LinkMode{LinkAuto, LinkCopy} {
		t.Run(string(mode), func(t *testing.T) {
			lt := NewLinkTree(filepath.Join(vendorRoot, string(mode)), store, mode)
			res, err := lt.Materialize(dg, "github.com/test/repo", "")
			if err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(filepath.Join(res.Path, "scripts", "run.sh"))
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s: exec bit not preserved, mode=%v", mode, st.Mode())
			}
		})
	}
}

func TestLinkTree_IdempotentAndClean(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkAuto)

	first, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatal(err)
	}

	// 手工塞入一个"已删除依赖"的残留文件，验证二次落地会清理它
	stale := filepath.Join(first.Path, "stale-file.txt")
	if err := os.WriteFile(stale, []byte("leftover\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("re-materialize must clean leftover files (stale file still present)")
	}
	if second.Path != first.Path {
		t.Errorf("path changed: %s vs %s", second.Path, first.Path)
	}
}

func TestLinkTree_MonorepoSubPath(t *testing.T) {
	repo := func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("packages/core/index.ts", "export const core = 1\n")
		r.WriteFile("packages/web/index.ts", "export const web = 1\n")
		r.WriteFile("README.md", "root\n")
		r.Commit("monorepo")
		return r
	}
	dg, store := contentFixture(t, repo)
	vendorRoot := filepath.Join(t.TempDir(), VendorDirName)
	lt := NewLinkTree(vendorRoot, store, LinkAuto)

	res, err := lt.Materialize(dg, "github.com/test/mono", "packages/core")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	wantPath := filepath.Join(vendorRoot, "github.com", "test", "mono", "packages", "core")
	if res.Path != wantPath {
		t.Errorf("path=%q want %q", res.Path, wantPath)
	}
	// vendor 里只放该子目录
	if _, err := os.Stat(filepath.Join(res.Path, "index.ts")); err != nil {
		t.Errorf("sub-path content missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "..", "web")); err == nil {
		t.Errorf("sibling package must not be materialized in this sub-path")
	}
	if _, err := os.Stat(filepath.Join(res.Path, "README.md")); err == nil {
		t.Errorf("repo root files must not leak into the sub-path vendor dir")
	}

	// 另一个子包落地后两者并存
	if _, err := lt.Materialize(dg, "github.com/test/mono", "packages/web"); err != nil {
		t.Fatalf("second sub-path: %v", err)
	}
	for _, sub := range []string{"core", "web"} {
		p := filepath.Join(vendorRoot, "github.com", "test", "mono", "packages", sub, "index.ts")
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s after materializing both sub-paths: %v", sub, err)
		}
	}
}

func TestLinkTree_MissingSubPathFails(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkAuto)

	_, err := lt.Materialize(dg, "github.com/test/repo", "does/not/exist")
	if err == nil {
		t.Fatalf("nonexistent sub-path must fail")
	}
	if !strings.Contains(err.Error(), "does/not/exist") {
		t.Errorf("error should name the sub-path: %v", err)
	}
}

func TestLinkTree_MissingContentFails(t *testing.T) {
	store := newStore(t)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkAuto)

	_, err := lt.Materialize("sha256:"+strings.Repeat("0", 64), "github.com/test/repo", "")
	if err == nil {
		t.Fatalf("missing content must fail")
	}
	// 这是完整性类失败：内容不在 store 里
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error type %T", err)
	}
	if ne.Code != errs.CodeDigestMismatch {
		t.Errorf("exit code=%d want 2 (DigestMismatch)", ne.Code.ExitCode())
	}
	// Hint 不参与 Error()（供 CLI 的 FormatHuman 使用），须单独断言
	if !strings.Contains(ne.Hint, "ngm install") {
		t.Errorf("hint should point at install, got %q", ne.Hint)
	}
}

func TestVendorPathFor(t *testing.T) {
	cases := []struct{ canon, sub, want string }{
		{"github.com/org/repo", "", "github.com/org/repo"},
		{"github.com/org/repo", "packages/core", "github.com/org/repo/packages/core"},
		{"/github.com/org/repo/", "packages/core/", "github.com/org/repo/packages/core"},
		{"github.com/org/repo", "/pkg/", "github.com/org/repo/pkg"},
	}
	for _, tc := range cases {
		if got := VendorPathFor(tc.canon, tc.sub); got != tc.want {
			t.Errorf("VendorPathFor(%q,%q)=%q want %q", tc.canon, tc.sub, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 一致性校验（M4.6）
// ---------------------------------------------------------------------------

func TestVerifyVendorTree_Matches(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkAuto)
	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatal(err)
	}

	vr := verifyAgainstStore(t, store, dg, res.Path, true)
	if !vr.OK() {
		t.Errorf("freshly materialized tree should match content: %v", vr.Mismatches)
	}
	if vr.Files != 3 {
		t.Errorf("verified files=%d want 3", vr.Files)
	}
}

func TestVerifyVendorTree_DetectsContentChange(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkCopy)
	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatal(err)
	}

	// 篡改 vendor 里的一个文件，且**保持字节数不变**。
	//
	// 为什么必须等长：verifyTrees 先用"大小不同"作廉价判定，长度变了的篡改会在
	// 那一层就被拦下，从而绕过哈希比对——那样本用例就无法证明"逐文件哈希真的执行了"。
	target := filepath.Join(res.Path, "src", "deep", "mod.ts")
	orig, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(orig) == 0 {
		t.Fatal("fixture file is empty; cannot tamper in place")
	}
	tampered := append([]byte(nil), orig...)
	if tampered[0] == 'X' {
		tampered[0] = 'Y'
	} else {
		tampered[0] = 'X'
	}
	if err := os.WriteFile(target, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	vr := verifyAgainstStore(t, store, dg, res.Path, true)
	if vr.OK() {
		t.Fatalf("tampered vendor file must be detected")
	}
	joined := strings.Join(vr.Mismatches, "\n")
	if !strings.Contains(joined, "src/deep/mod.ts") {
		t.Errorf("mismatch should name the file:\n%s", joined)
	}
	if !strings.Contains(joined, "content differs") {
		t.Errorf("mismatch should describe the difference:\n%s", joined)
	}
}

func TestVerifyVendorTree_DetectsMissingAndExtra(t *testing.T) {
	dg, store := contentFixture(t, basicRepo)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkCopy)
	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatal(err)
	}

	// 删一个文件、加一个文件
	if err := os.Remove(filepath.Join(res.Path, "index.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res.Path, "extra.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	vr := verifyAgainstStore(t, store, dg, res.Path, true)
	joined := strings.Join(vr.Mismatches, "\n")
	if !strings.Contains(joined, "index.ts") || !strings.Contains(joined, "missing from vendor") {
		t.Errorf("missing file not reported:\n%s", joined)
	}
	if !strings.Contains(joined, "extra.txt") || !strings.Contains(joined, "missing from the content store") {
		t.Errorf("extra file not reported:\n%s", joined)
	}
}

func TestVerifyVendorTree_DetectsSymlinkTargetChange(t *testing.T) {
	repo := func(t *testing.T) *testutils.GitRepo {
		r := testutils.NewGitRepo(t)
		r.WriteFile("target.txt", "c\n")
		r.Commit("t")
		r.AddSymlinkEntry("link.txt", "target.txt")
		r.Commit("l")
		return r
	}
	dg, store := contentFixture(t, repo)
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkCopy)
	res, err := lt.Materialize(dg, "github.com/test/repo", "")
	if err != nil {
		t.Fatal(err)
	}

	// 替换 symlink 指向
	linkPath := filepath.Join(res.Path, "link.txt")
	if err := os.Remove(linkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linkPath, []byte("now a regular file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	vr := verifyAgainstStore(t, store, dg, res.Path, true)
	joined := strings.Join(vr.Mismatches, "\n")
	if !strings.Contains(joined, "link.txt") {
		t.Errorf("symlink type change not reported:\n%s", joined)
	}
}

// ---------------------------------------------------------------------------
// cache 层（M4.2）
// ---------------------------------------------------------------------------

func TestCache_EnsureAndClean(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	c := NewCache(root)

	if err := c.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range []string{c.Root(), c.MetadataRoot(), c.OSVRoot(), c.TmpRoot()} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("missing dir %s: %v", d, err)
		}
	}

	// 写入一些缓存内容
	if err := os.WriteFile(filepath.Join(c.MetadataRoot(), "refs.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.OSVRoot(), "query.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := c.Clean()
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !res.Existed {
		t.Errorf("Existed should be true")
	}
	if len(res.RemovedEntries) != 3 {
		t.Errorf("removed entries=%v want 3 (metadata/osv/tmp)", res.RemovedEntries)
	}
	// 内容被清空
	if _, err := os.Stat(filepath.Join(c.MetadataRoot(), "refs.json")); !os.IsNotExist(err) {
		t.Errorf("cache content survived clean")
	}
	// 根与标准子目录被重建（后续命令无需各自 EnsureDirs）
	if st, err := os.Stat(c.Root()); err != nil || !st.IsDir() {
		t.Errorf("cache root should survive clean")
	}
	if st, err := os.Stat(c.TmpRoot()); err != nil || !st.IsDir() {
		t.Errorf("tmp dir should be recreated")
	}
}

func TestCache_CleanIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-created")
	c := NewCache(root)

	res, err := c.Clean()
	if err != nil {
		t.Fatalf("Clean on missing cache must succeed: %v", err)
	}
	if res.Existed {
		t.Errorf("Existed should be false when the cache was never created")
	}
	// 设计取舍：clean 不应有"创建目录"的副作用——不存在就什么都不做。
	// 需要目录的命令自行调用 EnsureDirs。
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("clean must not create the cache directory as a side effect")
	}

	// 二次调用同样安全
	if _, err := c.Clean(); err != nil {
		t.Fatalf("second Clean: %v", err)
	}
}

// TestCache_DoesNotTouchMirrorOrContent 锁定"cache 可丢弃、其他层不可"的边界。
func TestCache_DoesNotTouchMirrorOrContent(t *testing.T) {
	home := t.TempDir()
	layout := Layout{Home: home}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	dg, store := contentFixture(t, basicRepo)
	_ = dg

	// 在 mirror / content 里放一些"不可丢弃"的内容
	mirrorFile := filepath.Join(layout.MirrorRoot(), "keep.git")
	if err := os.MkdirAll(mirrorFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if !store.Has(dg) {
		t.Fatal("fixture precondition")
	}

	// 让 content store 指向 layout 的 content root，再 clean cache
	c := NewCache(layout.CacheRoot())
	if err := c.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Clean(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(mirrorFile); err != nil {
		t.Errorf("cache clean must not touch the mirror layer: %v", err)
	}

	// 端到端：用 content store 继续工作
	lt := NewLinkTree(filepath.Join(t.TempDir(), VendorDirName), store, LinkAuto)
	if _, err := lt.Materialize(dg, "github.com/test/repo", ""); err != nil {
		t.Errorf("content store should still work after a cache clean: %v", err)
	}
}
