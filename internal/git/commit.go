package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// CommitTime 读取某个 commit 的 **committer date**。
//
// 用途：`supplyChain.minimumReleaseAge` 的时间源（见 ADR-009 与
// docs/architecture/supply-chain.md）。取 committer date 而不是 author date，
// 是因为它才是"这个提交进入仓库"的时刻，也是本地 mirror 唯一能直接回答的值。
//
// 完全本地：只在 mirror 上执行 `git show`，不触网——因此 `--offline` 下同样可用，
// 这与 OSV 查询（需要网络）形成对比，是"晾晒期"门禁能离线工作的前提。
//
// 诚实边界：Git **没有可信时间戳**，committer date 由提交者控制、可被回填。
// 本函数只负责如实读出这个值；能否据此信任，由上游文档说明，不由这里保证。
func CommitTime(ctx context.Context, opts Options, mirrorPath, commit string) (time.Time, error) {
	o := opts
	o.Dir = mirrorPath

	res, err := Run(ctx, o, "show", "-s", "--format=%cI", commit)
	if err != nil {
		return time.Time{}, err
	}
	raw := strings.TrimSpace(string(res.Stdout))
	if raw == "" {
		return time.Time{}, errs.New(errs.CodeGitFetch,
			fmt.Sprintf("git produced no committer date for %s", ShortSHA(commit)),
			"the commit may be missing from the local mirror; run install once with network access")
	}
	t, perr := time.Parse(time.RFC3339, raw)
	if perr != nil {
		return time.Time{}, errs.Wrap(errs.CodeGitFetch,
			"parse committer date "+strconv.Quote(raw),
			"git returned an unexpected date format; check the Git version (>= 2.30 required)", perr)
	}
	return t, nil
}
