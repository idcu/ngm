package resolve_test

import (
	"context"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
)

// denyRunGit 是拒绝 `run:git` 的最小策略。
type denyRunGit struct{}

func (denyRunGit) CheckRun(exe string) error {
	if exe == "git" {
		return errs.New(errs.CodeConfigInvalid, "permission denied: run:git",
			"remove \"run:git\" from permissions.deny")
	}
	return nil
}
func (denyRunGit) CheckNet(string) error   { return nil }
func (denyRunGit) DeniedEnvVars() []string { return nil }

// TestV66GraphResolutionHonorsRunGitGate 固定评估报告 P1#1。
//
// `GraphOptions.Policy` 此前没有透传给 `resolveOne` 里的 ResolveRef ⇒
// 图解析阶段的 `git ls-remote` **绕过** `run:git` 门禁：配置里 deny 了它，
// 子进程照样起来。而 reftype.go 自己写下的不变量是
// "ngm 里不应存在绕过门禁的 git 调用路径"。
//
// 断言双管齐下：报错（且 exit 3）+ spawn 计数为 0——
// 后者才是"门禁真的在一进程都没起之前拦下"的证据。
func TestV66GraphResolutionHonorsRunGitGate(t *testing.T) {
	w := newFixtureWorld(t)
	w.add("github:w/a", leafRepo(t, "v1"))

	opts := w.opts()
	opts.Policy = git.Permissions(denyRunGit{})

	git.ResetSpawnCount()
	_, err := resolve.ResolveGraph(context.Background(), []resolve.DepSpec{
		{Name: "github:w/a", Ref: "v1", RefType: resolve.RefTypeTag},
	}, opts)

	if err == nil {
		t.Fatal("deny run:git 时图解析必须失败——它此前会绕过门禁")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit code = %d, want 3（门禁拒绝属于配置类）", code)
	}
	if n := git.SpawnCount(); n != 0 {
		t.Errorf("被拒绝的路径不得启动任何 git 子进程，实际启动 %d 个", n)
	}
}
