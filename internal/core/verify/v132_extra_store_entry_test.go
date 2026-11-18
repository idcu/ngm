package verify

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// 判据（2026-11-18 B1）：content store 清单里"多出来"的条目必须被发现。
//
// 缺陷形态：replayDigestFromEntries 按 Git tree 正向取值，只遍历权威 tree 的
// 路径；往 v2 清单塞一个指向合法 blob 的额外条目后，重放 digest 仍与 lock
// 相符，而 Materialize 会把这个条目落地进 vendor，第 3 步 vendor↔store 比对
// 用的是同一份清单——两侧一致，`--deep` 全绿。
//
// 本测试直接盖住反向集合比对这一层（extraStoreEntries），覆盖三种形状：
// 正常、多余条目、tree 无法读取（必须返回 nil = "没查成"而不是"没问题"）。
func TestV132ExtraStoreEntries(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("index.ts", "export const x = 1\n").WriteFile("sub/a.txt", "a\n").Commit("fixture")

	// 取最新 commit 作为权威 tree。
	head := strings.TrimSpace(testutils.GitOutput(t, repo.Dir, "rev-parse", "HEAD"))
	d := &lock.Dependency{Commit: head, Name: "github:acme/lib"}
	opts := Options{GitOpts: git.Options{}}

	t.Run("honest manifest has no extra entries", func(t *testing.T) {
		got := extraStoreEntries(t.Context(), repo.Dir, []vendor.ContentEntry{
			{Path: "index.ts", Mode: "100644"},
			{Path: "sub/a.txt", Mode: "100644"},
		}, d, opts)
		if len(got) != 0 {
			t.Fatalf("honest entries reported as extra: %v", got)
		}
	})

	t.Run("one extra entry is reported", func(t *testing.T) {
		got := extraStoreEntries(t.Context(), repo.Dir, []vendor.ContentEntry{
			{Path: "index.ts", Mode: "100644"},
			{Path: "sub/a.txt", Mode: "100644"},
			{Path: "injected/evil.js", Mode: "100644"},
		}, d, opts)
		if len(got) != 1 || got[0] != "injected/evil.js" {
			t.Fatalf("extra entries = %v, want [injected/evil.js]", got)
		}
	})

	t.Run("unreadable tree is operational, not clean", func(t *testing.T) {
		bad := &lock.Dependency{Commit: "0000000000000000000000000000000000000000", Name: d.Name}
		// 无法重放的 tree 必须返回 nil（调用方另有 operational 分支报 exit 4）；
		// 绝不能因为"没找到多余条目"就当作检查通过。
		got := extraStoreEntries(t.Context(), repo.Dir, []vendor.ContentEntry{
			{Path: "injected/evil.js", Mode: "100644"},
		}, bad, opts)
		if len(got) != 0 {
			t.Fatalf("an unreadable tree must not produce an 'extra' conclusion: %v", got)
		}
	})
}
