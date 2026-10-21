package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestIsAncestor 覆盖快进判定的三种退出码语义。
//
// 这是 `ngm verify` 区分"预期更新（branch 前进）"与"非预期漂移（历史被改写）"
// 的唯一信息来源：两种现象都是"ref 指向了别的 commit"，区别只在祖先关系。
func TestIsAncestor(t *testing.T) {
	testutils.MustHaveGit(t)
	r := testutils.NewGitRepo(t)
	ctx := context.Background()
	opts := Options{}

	r.WriteFile("a.txt", "1\n")
	first := r.Commit("first")
	r.WriteFile("a.txt", "2\n")
	second := r.Commit("second")

	got, err := IsAncestor(ctx, opts, r.Dir, first, second)
	if err != nil {
		t.Fatalf("IsAncestor(first, second): %v", err)
	}
	if !got {
		t.Errorf("first should be an ancestor of second")
	}

	got, err = IsAncestor(ctx, opts, r.Dir, second, first)
	if err != nil {
		t.Fatalf("IsAncestor(second, first): %v", err)
	}
	if got {
		t.Errorf("second must not be an ancestor of first")
	}

	// git 把"自己"视为自己的祖先（快进的定义包含"没有移动"）
	got, err = IsAncestor(ctx, opts, r.Dir, first, first)
	if err != nil {
		t.Fatalf("IsAncestor(self): %v", err)
	}
	if !got {
		t.Errorf("a commit must be its own ancestor")
	}

	// 未知对象必须是**错误**而不是 false：调用方要能区分
	// "证明不是祖先"（unexpected 漂移）与"查不到"（保守上报）
	unknown := strings.Repeat("0", 40)
	if _, err := IsAncestor(ctx, opts, r.Dir, unknown, second); err == nil {
		t.Errorf("an unknown object must be reported as an error, not as `false`")
	}

	// 空参数是调用方的编程错误，应被立即拒绝
	if _, err := IsAncestor(ctx, opts, r.Dir, "", second); err == nil {
		t.Errorf("empty commit must be rejected")
	}
}

// TestIsAncestor_RewrittenHistory 复现"历史被改写"（force push 后的分支顶端）。
//
// 场景：对分支做 amend —— 新 tip 的父提交与旧 tip 相同，但旧 tip 不再是
// 新 tip 的祖先。verify 必须据此判为 expected（快进）之外的情况。
func TestIsAncestor_RewrittenHistory(t *testing.T) {
	testutils.MustHaveGit(t)
	r := testutils.NewGitRepo(t)
	ctx := context.Background()

	r.WriteFile("a.txt", "1\n")
	base := r.Commit("base")
	r.WriteFile("a.txt", "2\n")
	oldTip := r.Commit("old tip")

	r.WriteFile("a.txt", "3\n")
	r.Exec("commit", "--amend", "-q", "-m", "amended tip")
	newTip := r.RevParse("HEAD")
	if newTip == oldTip {
		t.Skip("amend produced an identical commit (unexpected git behaviour)")
	}

	got, err := IsAncestor(ctx, Options{}, r.Dir, oldTip, newTip)
	if err != nil {
		t.Fatalf("IsAncestor(oldTip, newTip): %v", err)
	}
	if got {
		t.Errorf("a rewritten tip must not have the old tip as an ancestor")
	}

	// 共同祖先仍然是祖先：这类"前进"才是 expected
	got, err = IsAncestor(ctx, Options{}, r.Dir, base, newTip)
	if err != nil {
		t.Fatalf("IsAncestor(base, newTip): %v", err)
	}
	if !got {
		t.Errorf("base should remain an ancestor of the amended tip")
	}
}
