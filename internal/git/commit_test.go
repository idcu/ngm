package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestCommitTime_MatchesGitItself(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const x = 1\n")
	head := r.Commit("feat: x")

	// 期望值取自 git 自己的输出，避免测试依赖任何时区或格式假设
	want := strings.TrimSpace(testutils.GitOutput(t, r.Dir, "show", "-s", "--format=%cI", head))

	got, err := CommitTime(ctx, Options{}, r.Dir, head)
	if err != nil {
		t.Fatalf("CommitTime: %v", err)
	}
	if got.Format("2006-01-02T15:04:05Z07:00") != want {
		t.Errorf("CommitTime = %s, want %s", got.Format("2006-01-02T15:04:05Z07:00"), want)
	}
	if got.IsZero() {
		t.Error("CommitTime must not return the zero time for a real commit")
	}
}

// TestCommitTime_UnknownCommitFails 固定"读不到就该报错"这件事。
//
// 静默返回零值会让一道安全门禁悄悄失效——那正是我们最不愿发生的事。
func TestCommitTime_UnknownCommitFails(t *testing.T) {
	testutils.MustHaveGit(t)
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "x\n")
	r.Commit("init")

	const missing = "0000000000000000000000000000000000000000"
	got, err := CommitTime(context.Background(), Options{}, r.Dir, missing)
	if err == nil {
		t.Fatalf("want an error for an unknown commit, got time %v", got)
	}
	if !got.IsZero() {
		t.Errorf("on failure the returned time must be zero, got %v", got)
	}
}
