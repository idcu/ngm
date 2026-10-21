package main

import (
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV68InstallFreshSpendsFewerGitSpawns 是"6 spawn → 2"的端到端读数。
//
// 仪器是现成的（`git.SpawnCount`，git/spawn.go 的理由：掐表测的是噪声加信号，
// 计数只测信号）。这里断言的是**上界**而不是精确值——解析阶段的 spawn 数
// 依赖 mirror 状态与上游形状，把它钉死会让测试对无关变化敏感；
// 而"回到两位数"正是这个优化被撤销的形状（每个依赖 4 个重复 spawn）。
func TestV68InstallFreshSpendsFewerGitSpawns(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	scUpstream(t, "github:v68/dep", "export const d = 1\n", "")
	p := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:v68/dep@v1", "--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add: %s", out)
	}

	git.ResetSpawnCount()
	code, out := runCaptureCode(t, "install", "--dir="+p)
	if code != 0 {
		t.Fatalf("install: %s", out)
	}
	n := git.SpawnCount()
	t.Logf("install (fresh, 1 dependency) spent %d git spawn(s)", n)

	// 实测（2026-10-09，本机）：**6**。
	//
	// 构成：解析 = EnsureMirror fetch(1) + ls-remote(1) + 上游 ngm.json 读取(1)；
	// 其余 = 晾晒期的 commit 日期读取(1) + digest 阶段 **2**（ls-tree + cat-file）。
	// 修复前 digest 阶段是 6（digest 两遍 + tree + cat-file），即本例 10。
	if n > 8 {
		t.Errorf("install(fresh) 花了 %d 个 git spawn——本版把 digest 阶段压到 2 个，"+
			"回到两位数说明它又被算了两遍（评估报告 P1#4）", n)
	}
}
