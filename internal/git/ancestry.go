package git

import (
	"context"
	"fmt"

	"github.com/idcu/ngm/internal/errs"
)

// IsAncestor 报告 ancestor 是否为 descendant 的祖先（**含两者相等**）。
//
// 用途（verify 的漂移分类，architecture/observability.md §"预期更新" vs "非预期漂移"）：
//
//	locked 是 resolved 的祖先 → branch 快进（前进 N 个 commit）→ driftKind: expected
//	否则                     → 历史被改写（force push）→ driftKind: unexpected
//
// 两者的可观测现象完全相同（branch 的 ref 指向了别的 commit），区别只在
// **新旧 commit 的祖先关系**——因此这个判定是 expected/unexpected 分类的唯一依据。
//
// 实现：`git merge-base --is-ancestor <ancestor> <descendant>`，其退出码语义为
// 0 = 是祖先、1 = 不是、其他（128）= 出错（对象缺失等）。因为"非零"包含两种
// 完全不同的含义，这里用 RunAllowFailure 观察退出码，而不是让 Run 统一报错。
func IsAncestor(ctx context.Context, opts Options, repoPath, ancestor, descendant string) (bool, error) {
	if ancestor == "" || descendant == "" {
		return false, errs.New(errs.CodeConfigInvalid,
			"IsAncestor: empty commit", "")
	}
	opts.Dir = repoPath

	res, err := RunAllowFailure(ctx, opts, "merge-base", "--is-ancestor", ancestor, descendant)
	if err != nil {
		// 启动类失败（git 不在 PATH）
		return false, err
	}

	switch res.ExitCode {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		// 128：对象缺失 / 不是 commit / 仓库损坏。
		// 调用方必须把"无法证明"与"证明为否"区别对待——这里返回错误而非 false，
		// 避免把"查不到"静默当成"不是祖先"（那会误报成非预期漂移）。
		return false, errs.Wrap(errs.CodeGitFetch,
			fmt.Sprintf("merge-base --is-ancestor %s %s failed in %s",
				ShortSHA(ancestor), ShortSHA(descendant), repoPath),
			"the local mirror may be incomplete; re-run without --offline to refresh it",
			fmt.Errorf("git exit %d: %s", res.ExitCode, StderrText(res, opts.Secrets)))
	}
}
