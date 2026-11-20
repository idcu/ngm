package vendor

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/security"
	"github.com/idcu/ngm/internal/testutils"
)

// TestModuleCacheFirstCloneHonorsNetGate 钉住 v0.136 的一条安全边界：
// ModuleCache.Ensure 的**首次 clone** 与增量 fetch 一样触网，必须在起 git 之前
// 过 net 门禁。此前只有 fetch 分支判 net，deny net: 下首次 clone 仍会真实联网，
// 违反 security-model.md「未显式放行的 net: 按拒绝处理」。
//
// 用 https 远端 + 空策略（net 需显式授权）+ 空 module cache：断言在 clone 之前
// 就被拒、没有留下任何目录、也没有 spawn 任何 git。
func TestModuleCacheFirstCloneHonorsNetGate(t *testing.T) {
	pol, err := security.NewPolicy(nil, nil, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	c := NewModuleCache(root, git.Options{Policy: pol})
	repo := resolve.MustNormalize("github.com/acme/modules")
	path := c.PathFor(repo)

	git.ResetSpawnCount()
	res, err := c.Ensure(context.Background(), repo,
		"https://github.com/acme/modules.git", resolve.ProtocolHTTPS,
		"0123456789012345678901234567890123456789", "v1.0.0", "tag")

	if err == nil {
		t.Fatal("first clone under a deny-by-default net policy must be refused")
	}
	if errs.ExitCode(err) != 3 && errs.ExitCode(err) != 4 {
		t.Errorf("permission refusal should be a config/git-class error, got exit %d: %v", errs.ExitCode(err), err)
	}
	if !strings.Contains(err.Error(), "net:github.com") {
		t.Errorf("the error must name the exact permission that was denied: %v", err)
	}
	if n := git.SpawnCount(); n != 0 {
		t.Errorf("the gate must fire before any git spawn, got %d spawns", n)
	}
	if _, serr := os.Stat(path); serr == nil {
		t.Errorf("a refused clone must not leave a module cache directory at %s", path)
	}
	// 没在 Ensure 里创建文件，但 t.TempDir 下的 root 应仍为空
	if entries, rerr := os.ReadDir(root); rerr != nil || len(entries) != 0 {
		t.Errorf("refused clone should leave the cache root empty: %v (entries=%d)", rerr, len(entries))
	}
	if res.Created {
		t.Error("a refused clone must not report Created")
	}
}

// TestModuleCacheCachedCommitOfflineDoesNotNeedNet 是上一条的反向守卫：
// 门禁只加在「首次 clone」与「缺失 commit 的 fetch」两条触网路径上，
// 缓存已存在且 commit 命中时是纯离线路径，不该被 net 门禁挡住（verify --offline 依赖它）。
func TestModuleCacheCachedCommitOfflineDoesNotNeedNet(t *testing.T) {
	// 明确 deny 该 host：离线命中仍应成功（门禁只在触网路径上）。
	pol, err := security.NewPolicy(nil, []string{"net:github.com"}, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}

	remote := testutils.NewBareRemote(t)
	src := testutils.NewGitRepo(t)
	src.SetRemote("origin", remote)
	src.WriteFile("packages/a/index.ts", "export const a = 1\n")
	commit := src.Commit("feat: a")
	src.Tag("packages/a/v1.0.0", false)
	src.Push("origin", "main")
	src.Push("origin", "refs/tags/packages/a/v1.0.0")

	// 先用宽松策略建立缓存（首次 clone 需要 net）
	warm := NewModuleCache(t.TempDir(), git.Options{Policy: nil})
	repo := resolve.MustNormalize("github.com/acme/modules")
	if _, err := warm.Ensure(context.Background(), repo, vFileURL(t, remote), resolve.ProtocolHTTPS,
		commit, "packages/a/v1.0.0", "tag"); err != nil {
		t.Fatalf("warm-up Ensure: %v", err)
	}

	// 再用 deny net: 策略、但复用同一个缓存目录与同一个 commit——应走离线命中
	cold := NewModuleCache(warm.Root(), git.Options{Policy: pol})
	git.ResetSpawnCount()
	res, err := cold.Ensure(context.Background(), repo, "https://github.com/acme/modules.git",
		resolve.ProtocolHTTPS, commit, "packages/a/v1.0.0", "tag")
	if err != nil {
		t.Fatalf("an already-cached commit must not need net: %v", err)
	}
	if res.Created {
		t.Error("a cache hit must not report Created")
	}
}
