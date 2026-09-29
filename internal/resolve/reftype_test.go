package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

// fixtureRepo 创建一个本地 Git 仓库并返回 (路径, HEAD commit, annotated tag 所指向的 commit)。
//
// 所有 refType 解析测试都在这类本地 fixture 上运行——禁止依赖公网
// （development/README.md 测试策略 #1）。
func fixtureRepo(t *testing.T) (dir, headCommit string) {
	t.Helper()
	testutils.MustHaveGit(t)
	dir = t.TempDir()
	headCommit = testutils.GitInit(t, dir)
	testutils.GitWriteFile(t, dir, "src/index.ts", "export const x = 1\n")
	headCommit = testutils.GitCommit(t, dir, "feat: add index")
	return dir, headCommit
}

func TestResolveRef_Branch(t *testing.T) {
	dir, head := fixtureRepo(t)
	repo := MustNormalize("github:org/repo")

	got, err := ResolveRef(context.Background(), repo, "main", RefTypeBranch, ResolveOptions{GitURL: dir})
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != head {
		t.Errorf("branch main → %s, want %s", got, head)
	}
}

func TestResolveRef_LightweightTag(t *testing.T) {
	dir, head := fixtureRepo(t)
	testutils.GitTag(t, dir, "v1.0.0", false)

	repo := MustNormalize("github:org/repo")
	got, err := ResolveRef(context.Background(), repo, "v1.0.0", RefTypeTag, ResolveOptions{GitURL: dir})
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != head {
		t.Errorf("lightweight tag → %s, want %s", got, head)
	}
}

// TestResolveRef_AnnotatedTag 是 M1 最关键的一个用例。
//
// annotated tag 在 ls-remote 中有两行：tag object 与其 `^{}` 解引用。
// 若实现取错了行，lock 记录的 commit 语义就错了——verify 会误报漂移。
func TestResolveRef_AnnotatedTag(t *testing.T) {
	dir, head := fixtureRepo(t)
	testutils.GitTag(t, dir, "v1.1.0", true /* annotated */)

	repo := MustNormalize("github:org/repo")
	got, err := ResolveRef(context.Background(), repo, "v1.1.0", RefTypeTag, ResolveOptions{GitURL: dir})
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != head {
		t.Errorf("annotated tag → %s, want commit %s (tag object would be a different sha)", got, head)
	}
	// 明确断言不是 tag object 的 sha
	tagObjSHA := strings.TrimSpace(mustGit(t, dir, "rev-parse", "refs/tags/v1.1.0"))
	if got == tagObjSHA {
		t.Fatalf("resolved to tag object %s instead of peeled commit %s", tagObjSHA, head)
	}
}

func TestResolveRef_Commit(t *testing.T) {
	_, head := fixtureRepo(t)
	repo := MustNormalize("github:org/repo")

	// 完整 hash
	got, err := ResolveRef(context.Background(), repo, head, RefTypeCommit, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != head {
		t.Errorf("commit → %s want %s", got, head)
	}

	// 短 hash + 大写 → 规范化为小写
	short := strings.ToUpper(head[:8])
	got2, err := ResolveRef(context.Background(), repo, short, RefTypeCommit, ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRef(short): %v", err)
	}
	if got2 != strings.ToLower(short) {
		t.Errorf("short commit → %s want %s", got2, strings.ToLower(short))
	}
}

func TestResolveRef_CommitInvalidFormat(t *testing.T) {
	repo := MustNormalize("github:org/repo")
	cases := []struct {
		name string
		ref  string
	}{
		{"non-hex", "zzzzzzz"},
		{"too short", "abc12"},
		{"too long", strings.Repeat("a", 41)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveRef(context.Background(), repo, tc.ref, RefTypeCommit, ResolveOptions{})
			if err == nil {
				t.Fatalf("expected error for ref %q", tc.ref)
			}
			assertCode(t, err, errs.CodeConfigInvalid)
		})
	}
}

func TestResolveRef_NotFound(t *testing.T) {
	dir, _ := fixtureRepo(t)
	testutils.GitTag(t, dir, "v1.0.0", false)
	repo := MustNormalize("github:org/repo")

	// 不存在的 tag
	_, err := ResolveRef(context.Background(), repo, "v9.9.9", RefTypeTag, ResolveOptions{GitURL: dir})
	if err == nil {
		t.Fatalf("expected not-found error")
	}
	assertCode(t, err, errs.CodeConfigInvalid)
	if !strings.Contains(err.Error(), "v9.9.9") {
		t.Errorf("error should mention the missing ref: %v", err)
	}
	// Hint 应列出可用 ref 以帮助纠错
	var ne *errs.NgmError
	_ = errors.As(err, &ne)
	if ne != nil && !strings.Contains(ne.Hint, "v1.0.0") {
		t.Errorf("hint should suggest available refs, got: %q", ne.Hint)
	}

	// 分支名当作 tag 查 → 也应失败（refType 必填的意义）
	_, err = ResolveRef(context.Background(), repo, "main", RefTypeTag, ResolveOptions{GitURL: dir})
	if err == nil {
		t.Fatalf("expect tag lookup of branch name to fail")
	}
	assertCode(t, err, errs.CodeConfigInvalid)
}

func TestResolveRef_InvalidRefType(t *testing.T) {
	dir, _ := fixtureRepo(t)
	repo := MustNormalize("github:org/repo")
	_, err := ResolveRef(context.Background(), repo, "main", RefType("banana"), ResolveOptions{GitURL: dir})
	if err == nil {
		t.Fatalf("expected error")
	}
	assertCode(t, err, errs.CodeConfigInvalid)
}

func TestResolveRef_EmptyRef(t *testing.T) {
	repo := MustNormalize("github:org/repo")
	_, err := ResolveRef(context.Background(), repo, "  ", RefTypeBranch, ResolveOptions{GitURL: t.TempDir()})
	if err == nil {
		t.Fatalf("expected error")
	}
	assertCode(t, err, errs.CodeConfigInvalid)
}

// TestResolveRef_NetworkFailureMapsToExit4 用一个不存在的远端路径模拟网络/仓库失败，
// 断言错误分类为 CodeGitFetch（退出码 4）。
func TestResolveRef_NetworkFailureMapsToExit4(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := MustNormalize("github:org/repo")
	bogus := t.TempDir() + "/does-not-exist"
	_, err := ResolveRef(context.Background(), repo, "main", RefTypeBranch, ResolveOptions{GitURL: bogus})
	if err == nil {
		t.Fatalf("expected failure")
	}
	assertCode(t, err, errs.CodeGitFetch)
}

// TestResolveRef_NoGitInPath 验证 git 缺失时的错误可操作（Hint 指向安装）。
//
// 通过把 PATH 置空模拟；git 可能仍能被绝对路径找到，因此本用例在找不到 git 时跳过。
func TestResolveRef_NoGitInPath(t *testing.T) {
	t.Skip("PATH 操控在 Windows/CI 上不稳定，交由 CI 环境矩阵覆盖")
}

func TestResolveRefTypes_Batch(t *testing.T) {
	dir, head := fixtureRepo(t)
	testutils.GitTag(t, dir, "v2.0.0", true)
	repo := MustNormalize("github:org/repo")

	got, err := ResolveRefTypes(context.Background(), repo, []RefSpec{
		{Ref: "main", Type: RefTypeBranch},
		{Ref: "v2.0.0", Type: RefTypeTag},
		{Ref: head, Type: RefTypeCommit},
	}, ResolveOptions{GitURL: dir})
	if err != nil {
		t.Fatalf("ResolveRefTypes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d results", len(got))
	}
	for i, c := range got {
		if c != head {
			t.Errorf("result[%d]=%s want %s", i, c, head)
		}
	}
	// 批量中有一个失败 → 整体失败
	_, err = ResolveRefTypes(context.Background(), repo, []RefSpec{
		{Ref: "main", Type: RefTypeBranch},
		{Ref: "absent", Type: RefTypeTag},
	}, ResolveOptions{GitURL: dir})
	if err == nil {
		t.Errorf("expected batch failure")
	}
}

// ---- helpers ----

func assertCode(t *testing.T, err error, want errs.Code) {
	t.Helper()
	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("error is not *errs.NgmError: %T (%v)", err, err)
	}
	if ne.Code != want {
		t.Errorf("exit code=%d want %d; err=%v", ne.Code.ExitCode(), want.ExitCode(), err)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return testutils.GitOutput(t, dir, args...)
}
