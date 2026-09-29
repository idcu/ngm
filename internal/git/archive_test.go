package git

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

func TestParseListTreeZ(t *testing.T) {
	// 记录格式：`<mode> SP <type> SP <object> TAB <path> NUL`
	const sha1 = "1111111111111111111111111111111111111111"
	const sha2 = "2222222222222222222222222222222222222222"
	in := "100644 blob " + sha1 + "\tplain.txt\x00" +
		"100755 blob " + sha2 + "\tscripts/run.sh\x00" +
		"120000 blob " + sha1 + "\tlink with space.txt\x00"

	got := parseListTreeZ([]byte(in))
	if len(got) != 3 {
		t.Fatalf("parsed %d entries, want 3: %+v", len(got), got)
	}
	if got[0].Mode != "100644" || got[0].Path != "plain.txt" || got[0].SHA != sha1 {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if got[1].Mode != "100755" || got[1].Type != "blob" || got[1].Path != "scripts/run.sh" {
		t.Errorf("entry 1 = %+v", got[1])
	}
	// -z 模式下路径不被转义，空格原样保留
	if got[2].Path != "link with space.txt" {
		t.Errorf("entry 2 path = %q", got[2].Path)
	}
}

func TestParseListTreeZ_Malformed(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	in := "garbage\x00" + // 无 TAB
		"100644 blob " + sha + "\t\x00" + // 空路径
		"100644 blob\t\t\x00" + // 缺 object 字段
		" blob " + sha + "\tempty-mode.txt\x00" + // 缺 mode
		"100644 blob " + sha + "\tok.txt\x00" // 唯一合法记录

	got := parseListTreeZ([]byte(in))
	if len(got) != 1 {
		t.Fatalf("expected only the valid record, got %+v", got)
	}
	if got[0].Path != "ok.txt" {
		t.Errorf("path=%q", got[0].Path)
	}
}

// TestValidateTreeEntry 覆盖模式校验的全部分支。
//
// 为什么是单元测试而非端到端：`git update-index --cacheinfo` 会把非标准 mode
// （如 100664）规范化为 100644，无法通过真实仓库构造出"非法 mode"的 tree。
func TestValidateTreeEntry(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		mode    string
		wantErr bool
		wantMsg string
	}{
		{digest.ModeRegular, false, ""},
		{digest.ModeExecutable, false, ""},
		{digest.ModeSymlink, false, ""},
		{digest.ModeTree, false, ""}, // 目录由调用方跳过
		{digest.ModeGitlink, true, "gitlink"},
		{"100664", true, "100664"}, // 历史仓库可能出现
		{"040000", true, "040000"}, // 带前导零的 tree 写法
		{"", true, `""`},
	}
	for _, tc := range cases {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			err := validateTreeEntry(TreeEntry{Mode: tc.mode, Path: "x.txt"}, commit)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateTreeEntry(%q) err=%v wantErr=%v", tc.mode, err, tc.wantErr)
			}
			if tc.wantErr {
				assertErrCode(t, err, errs.CodeConfigInvalid)
				if !strings.Contains(err.Error(), tc.wantMsg) {
					t.Errorf("error %q should mention %q", err.Error(), tc.wantMsg)
				}
			}
		})
	}
	// gitlink 错误的 Hint 必须可操作
	err := validateTreeEntry(TreeEntry{Mode: digest.ModeGitlink, Path: "vendor/sub"}, commit)
	var ne *errs.NgmError
	if !errors.As(err, &ne) || ne.Hint == "" {
		t.Errorf("gitlink error must carry an actionable hint")
	}
}

func TestListTree_KitchenSink(t *testing.T) {
	repo := testutils.BuildKitchenSinkRepo(t)
	head := repo.Head()

	entries, err := ListTree(context.Background(), Options{}, repo.Dir, head)
	if err != nil {
		t.Fatalf("ListTree: %v", err)
	}

	byPath := map[string]TreeEntry{}
	for _, e := range entries {
		byPath[e.Path] = e
	}

	want := map[string]string{
		"plain.txt":              digest.ModeRegular,
		"empty.txt":              digest.ModeRegular,
		"crlf.txt":               digest.ModeRegular,
		"with space.txt":         digest.ModeRegular,
		"中文目录/文件.txt":            digest.ModeRegular,
		"nested/deep/dir/mod.ts": digest.ModeRegular,
		"big.bin":                digest.ModeRegular,
		"scripts/run.sh":         digest.ModeExecutable,
		"link-to-plain.txt":      digest.ModeSymlink,
		"dir/link-up.txt":        digest.ModeSymlink,
	}
	for path, mode := range want {
		e, ok := byPath[path]
		if !ok {
			t.Errorf("missing entry %q", path)
			continue
		}
		if e.Mode != mode {
			t.Errorf("%s: mode=%s want %s", path, e.Mode, mode)
		}
	}
	if len(entries) != len(want) {
		t.Errorf("entry count = %d, want %d\n%+v", len(entries), len(want), entries)
	}
	// 不应包含目录条目（-r 已递归展开）
	for _, e := range entries {
		if e.Mode == digest.ModeTree {
			t.Errorf("directory entry leaked into -r listing: %+v", e)
		}
	}
}

func TestCatFileBatch(t *testing.T) {
	repo := testutils.BuildKitchenSinkRepo(t)
	head := repo.Head()
	ctx := context.Background()

	entries, err := ListTree(ctx, Options{}, repo.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	var shas []string
	for _, e := range entries {
		shas = append(shas, e.SHA)
	}

	contents, err := CatFileBatch(ctx, Options{}, repo.Dir, shas)
	if err != nil {
		t.Fatalf("CatFileBatch: %v", err)
	}
	if len(contents) != len(shas) {
		t.Fatalf("got %d contents for %d shas", len(contents), len(shas))
	}

	// 抽查：plain.txt、空文件、大文件、symlink 目标
	byPath := map[string][]byte{}
	for i, e := range entries {
		byPath[e.Path] = contents[i]
	}
	if got := string(byPath["plain.txt"]); got != "hello ngm\n" {
		t.Errorf("plain.txt = %q", got)
	}
	if got := len(byPath["empty.txt"]); got != 0 {
		t.Errorf("empty.txt len = %d", got)
	}
	if got := len(byPath["big.bin"]); got != 8192 {
		t.Errorf("big.bin len = %d want 8192", got)
	}
	// symlink 的 blob 内容就是目标字符串
	if got := string(byPath["link-to-plain.txt"]); got != "plain.txt" {
		t.Errorf("symlink content = %q want %q", got, "plain.txt")
	}
	if got := string(byPath["dir/link-up.txt"]); got != "../plain.txt" {
		t.Errorf("symlink content = %q want %q", got, "../plain.txt")
	}
	// CRLF 原样保留
	if got := string(byPath["crlf.txt"]); got != "line1\r\nline2\r\n" {
		t.Errorf("crlf.txt = %q (CRLF must be preserved)", got)
	}
}

func TestCatFileBatch_Empty(t *testing.T) {
	got, err := CatFileBatch(context.Background(), Options{}, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("empty batch should not fail: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestCatFileBatch_MissingObject(t *testing.T) {
	repo := testutils.NewGitRepo(t)
	head := repo.WriteFile("a.ts", "1\n").Commit("feat")
	_ = head

	_, err := CatFileBatch(context.Background(), Options{}, repo.Dir,
		[]string{strings.Repeat("f", 40)})
	if err == nil {
		t.Fatalf("expected failure for missing object")
	}
	assertErrCode(t, err, errs.CodeGitFetch)
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention missing object: %v", err)
	}
}

// ---------------------------------------------------------------------------
// M2 冻结测试向量
// ---------------------------------------------------------------------------

// TestBuildArchive_KitchenSinkVectors 是 M2 的核心向量：
// 覆盖 ADR-008 要求的所有条目类型，把**清单字节流**与 **digest** 双双 golden 化。
//
// 为什么两个都 golden：
//   - digest 是产品契约（进入 lock）
//   - 清单是 digest 的输入；单独 golden 能在规则微调时直接指出哪一行变了
//
// 任何 golden 变化都必须在本 PR 说明，并重新评估规范版本（ADR-008 §关键说明 5）。
func TestBuildArchive_KitchenSinkVectors(t *testing.T) {
	repo := testutils.BuildKitchenSinkRepo(t)
	head := repo.Head()

	manifest, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}

	// 清单：渲染为可读形式做 golden（原始字节含 NUL，不便审查）
	testutils.GoldenString(t, "vectors/archive-kitchen-sink.manifest", digest.RenderManifest(manifest))
	// digest：产品契约
	testutils.GoldenString(t, "vectors/archive-kitchen-sink.digest", digest.Digest(manifest)+"\n")
}

// TestBuildArchive_CRLFNotConverted 是 ADR-008「不做 CRLF → LF 转换」的向量级证明。
//
// 构造两个仅换行不同的仓库；若实现做了换行归一，两者 digest 会相同——那就错了。
func TestBuildArchive_CRLFNotConverted(t *testing.T) {
	ctx := context.Background()

	lfRepo := testutils.NewGitRepo(t)
	lfRepo.WriteFileRaw("f.txt", []byte("a\nb\n"))
	lfHead := lfRepo.Commit("lf")

	crlfRepo := testutils.NewGitRepo(t)
	crlfRepo.WriteFileRaw("f.txt", []byte("a\r\nb\r\n"))
	crlfHead := crlfRepo.Commit("crlf")

	lfDigest, err := BuildArchiveDigest(ctx, Options{}, lfRepo.Dir, lfHead)
	if err != nil {
		t.Fatal(err)
	}
	crlfDigest, err := BuildArchiveDigest(ctx, Options{}, crlfRepo.Dir, crlfHead)
	if err != nil {
		t.Fatal(err)
	}

	if lfDigest == crlfDigest {
		t.Fatalf("LF and CRLF variants produced the same digest (%s) — newline conversion must not happen", lfDigest)
	}
}

// TestBuildArchive_Idempotent 锁定"重复生成同一 digest"（可复现性要求）。
func TestBuildArchive_Idempotent(t *testing.T) {
	repo := testutils.BuildKitchenSinkRepo(t)
	head := repo.Head()
	ctx := context.Background()

	first, err := BuildArchiveDigest(ctx, Options{}, repo.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := BuildArchiveDigest(ctx, Options{}, repo.Dir, head)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("digest changed on repeat %d: %s vs %s", i, again, first)
		}
	}
}

// TestBuildArchive_EmptyCommit 空 tree 是合法的（清单只有头部）。
func TestBuildArchive_EmptyCommit(t *testing.T) {
	repo := testutils.NewGitRepo(t)
	head := repo.Head() // GitInit 只做了空 commit

	manifest, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err != nil {
		t.Fatalf("BuildArchive(empty): %v", err)
	}
	want := digest.ManifestVersion + "\x00"
	if string(manifest) != want {
		t.Errorf("empty manifest = %q want %q", manifest, want)
	}
	if got := digest.Digest(manifest); !strings.HasPrefix(got, "sha256:") {
		t.Errorf("digest = %q", got)
	}
}

// TestBuildArchive_ChangedContentChangesDigest 内容变 → digest 必变。
func TestBuildArchive_ChangedContentChangesDigest(t *testing.T) {
	ctx := context.Background()

	r1 := testutils.NewGitRepo(t)
	r1.WriteFile("a.txt", "one\n")
	h1 := r1.Commit("one")

	r2 := testutils.NewGitRepo(t)
	r2.WriteFile("a.txt", "two\n")
	h2 := r2.Commit("two")

	d1, err := BuildArchiveDigest(ctx, Options{}, r1.Dir, h1)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := BuildArchiveDigest(ctx, Options{}, r2.Dir, h2)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Errorf("content change did not change digest")
	}
}

// TestBuildArchive_PathRenameChangesDigest 路径变 → digest 必变
// （否则"改名"会成为不可检测的变更）。
func TestBuildArchive_PathRenameChangesDigest(t *testing.T) {
	ctx := context.Background()

	r1 := testutils.NewGitRepo(t)
	r1.WriteFile("old/name.ts", "same\n")
	h1 := r1.Commit("old name")

	r2 := testutils.NewGitRepo(t)
	r2.WriteFile("new/name.ts", "same\n")
	h2 := r2.Commit("new name")

	d1, _ := BuildArchiveDigest(ctx, Options{}, r1.Dir, h1)
	d2, _ := BuildArchiveDigest(ctx, Options{}, r2.Dir, h2)
	if d1 == d2 {
		t.Errorf("path rename did not change digest")
	}
}

// TestBuildArchive_ModeChangeChangesDigest 可执行位变 → digest 必变。
func TestBuildArchive_ModeChangeChangesDigest(t *testing.T) {
	ctx := context.Background()

	plain := testutils.NewGitRepo(t)
	plain.WriteFile("run.sh", "#!/bin/sh\n")
	hPlain := plain.Commit("plain")

	exec := testutils.NewGitRepo(t)
	exec.AddExecutable("run.sh", "#!/bin/sh\n")
	hExec := exec.Commit("exec")

	dPlain, err := BuildArchiveDigest(ctx, Options{}, plain.Dir, hPlain)
	if err != nil {
		t.Fatal(err)
	}
	dExec, err := BuildArchiveDigest(ctx, Options{}, exec.Dir, hExec)
	if err != nil {
		t.Fatal(err)
	}
	if dPlain == dExec {
		t.Errorf("mode change (100644 → 100755) did not change digest")
	}
}

// ---------------------------------------------------------------------------
// 异常路径：必须报错，不得静默哈希
// ---------------------------------------------------------------------------

func TestBuildArchive_RejectsLFSPointer(t *testing.T) {
	repo := testutils.BuildLFSRepo(t)
	head := repo.Head()

	_, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err == nil {
		t.Fatalf("LFS pointer must be rejected, not silently hashed")
	}
	assertErrCode(t, err, errs.CodeConfigInvalid)
	msg := err.Error()
	if !strings.Contains(msg, "LFS") {
		t.Errorf("error should mention LFS: %v", err)
	}
	if !strings.Contains(msg, "assets/logo.bin") {
		t.Errorf("error should name the offending path: %v", err)
	}
	if !strings.Contains(msg, "4d7a2146") {
		t.Errorf("error should include the LFS oid: %v", err)
	}
	// Hint 必须可操作
	var ne *errs.NgmError
	if !errors.As(err, &ne) || ne.Hint == "" {
		t.Errorf("error must carry an actionable hint")
	}
}

func TestBuildArchive_RejectsGitlink(t *testing.T) {
	repo := testutils.BuildGitlinkRepo(t)
	head := repo.Head()

	_, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err == nil {
		t.Fatalf("gitlink must be rejected")
	}
	assertErrCode(t, err, errs.CodeConfigInvalid)
	msg := err.Error()
	if !strings.Contains(msg, "gitlink") && !strings.Contains(msg, "submodule") {
		t.Errorf("error should mention gitlink/submodule: %v", err)
	}
	if !strings.Contains(msg, "vendor/sub") {
		t.Errorf("error should name the offending path: %v", err)
	}
}

func TestBuildArchive_UnknownModeRejected(t *testing.T) {
	// git 会把非标准 mode（100664）规范化为 100644，因此这里改为验证
	// "git 规范化后我们接受它，且不会静默错误"——真正的非法 mode 由
	// TestValidateTreeEntry 的单元测试覆盖。
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("a.txt", "x\n")
	sha := testutils.GitHashObject(t, repo.Dir, []byte("y\n"))
	repo.Exec("update-index", "--add", "--cacheinfo", "100664,"+sha+",legacy.txt")
	head := repo.Commit("legacy mode")

	// 记录 git 的实际规范化结果，作为文档化的事实
	out := repo.Exec("ls-tree", "-r", head)
	if strings.Contains(out, "100664") {
		// 若某版本 git 保留了 100664，则必须被拒绝
		_, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
		if err == nil {
			t.Fatalf("git preserved mode 100664 but BuildArchive accepted it (must reject)")
		}
		assertErrCode(t, err, errs.CodeConfigInvalid)
		return
	}
	// 规范化路径：应正常产出 digest
	if _, err := BuildArchiveDigest(context.Background(), Options{}, repo.Dir, head); err != nil {
		t.Fatalf("normalized mode should be accepted: %v", err)
	}
}

// TestBuildArchive_SymlinkHashIsTargetString 锁定 symlink 的哈希对象是**目标字符串**。
func TestBuildArchive_SymlinkHashIsTargetString(t *testing.T) {
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("target.txt", "content\n")
	repo.Commit("target")
	repo.AddSymlinkEntry("link.txt", "target.txt")
	head := repo.Commit("link")

	manifest, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	rendered := digest.RenderManifest(manifest)

	wantSHA := digest.HashBytes([]byte("target.txt"))
	if !strings.Contains(rendered, "link.txt\t120000\t"+wantSHA) {
		t.Errorf("symlink record should hash the target string %q (sha %s)\n%s",
			"target.txt", wantSHA, rendered)
	}
}

// TestBuildArchive_SortedByPath 清单必须按路径字节序排列（不依赖 git 的 tree 序）。
func TestBuildArchive_SortedByPath(t *testing.T) {
	repo := testutils.NewGitRepo(t)
	// 故意以"非字典序"的提交顺序写入
	for _, p := range []string{"zzz.txt", "aaa.txt", "MMM.txt", "000.txt"} {
		repo.WriteFile(p, "x\n")
	}
	head := repo.Commit("mixed order")

	manifest, err := BuildArchive(context.Background(), Options{}, repo.Dir, head)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(digest.RenderManifest(manifest), "\n"), "\n")
	var paths []string
	for _, l := range lines[1:] {
		paths = append(paths, strings.SplitN(l, "\t", 2)[0])
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("manifest not sorted by byte order: %v", paths)
	}
	// 字节序：数字 < 大写 < 小写
	want := []string{"000.txt", "MMM.txt", "aaa.txt", "zzz.txt"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v want %v", paths, want)
	}
}

// assertErrCode 断言错误是 NgmError 且退出码符合预期。
func assertErrCode(t *testing.T, err error, want errs.Code) {
	t.Helper()
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error is not *errs.NgmError: %T (%v)", err, err)
	}
	if ne.Code != want {
		t.Errorf("exit code=%d want %d; err=%v", ne.Code.ExitCode(), want.ExitCode(), err)
	}
}
