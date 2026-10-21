package git

import (
	"context"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// GC 在一个既有仓库（裸仓库或工作区）里执行 `git gc --quiet`。
//
// # 它做什么
//
// 把松散对象与多个 packfile 合并整理（`git repack`），这是**历史本体**的整理：
// 层 1 的 mirror 是 `git clone --mirror` 得来的，随着反复 fetch，对象会散成
// 松散对象与多个包——`git gc` 是 git 自己的收敛手段（[ADR-030]）。
//
// # 它**不**做什么，以及为什么
//
//   - **不用 `--prune=now`**：默认的"不可达对象保留 2 周"是 git 的安全窗口
//     （上游删掉的分支/回退的提交在那段时间里仍可恢复）。ngm 没有理由为了
//     "数字好看"去缩短它。
//   - **不是 `tmp_*` 残骸的回收路径**：实测（见 ADR-030 的读数）`git gc` 确实会删
//     `objects/pack/tmp_*`，但**只删超过 14 天**的那些——13 天存活、15 天被清，
//     阈值正是 `gc.pruneExpire` 的默认值 `2.weeks`。于是一次被中断的
//     `git clone --mirror` 留下的临时包（本机实测到一个 66.82 MiB 的）
//     **两周之内不会被 git 自己收走**。那条路径归 `ngm store prune`
//     （`Mirror.PruneResidue`，门槛是 `MirrorResidueGrace`）。
//
// # 网络
//
// 不触网：`git gc` 只在本地的对象库上工作，因此这里**不**做 `net:` 判定
// （与 `FetchMirror` 的区别正在于此）。`run:git` 的判定照旧在唯一的构造点
// （`newGitCommand`）里，任何绕过它的路都过不了 `spawn_gate_test.go`。
//
// 幂等：已整理过的仓库再跑一次是廉价的 no-op；失败时**不做任何补救**，
// 原样把 git 的话交给调用方（"mirror 的内容不由 ngm 解释"，与 FetchMirror 同一条纪律）。
func GC(ctx context.Context, opts Options, localPath string) error {
	if strings.TrimSpace(localPath) == "" {
		return errs.New(errs.CodeGitFetch,
			"gc: empty repository path",
			"this is an internal wiring error — the mirror layout produced no path")
	}
	o := opts
	o.Dir = localPath
	_, err := Run(ctx, o, "gc", "--quiet")
	return err
}
