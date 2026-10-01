package main

import (
	"fmt"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV06VerifySpawnBudget 把"次数优先于秒数"从结论变成**门禁**（v0.6 B 组）。
//
// v0.5 证明了 3s 目标在噪声里不可判别：同一台机器上未优化的对照组最大 2.433s
// 也过线，而跨机器状态的方差（≥0.3s）大于那轮收益（0.45s）。因此本版让
// **每次在线 verify 起几个 git** 成为唯一会被断言的性能量。
//
// 预算（一次完整在线 verify，每个依赖）：
//
//	commit 型 → 2 次：`ls-tree -r -z`（列清单）+ `cat-file --batch`（取字节）
//	tag 型    → 3 次：以上两次 + `ls-remote`（它必须问远端，ADR-010）
//
// 为什么是"上界"而不是"恰好"：更少是**好事**（例如将来把多个依赖的 cat-file 合并成
// 一个长驻进程），门禁不该拦住改进。而"更多"一定是退步——每一次多出来的 spawn
// 都是每个依赖都付的成本（100 依赖就是 100 次）。
//
// 为什么这条用例值一次 CI 时间：它开门第一天就抓到了一次**真实的记账错误**。
// v0.5 的 C 组把 `CatFileBatch` 收进唯一的构造点 `newGitCommand`（那里计数），
// 却漏删了它自己那句 `noteSpawn()`，于是每个 `cat-file --batch` 被记两次账：
// 本用例会看到 commit 3 次/依赖、tag 4 次/依赖（真实值是 2 与 3）。
// 见 `TestSpawn_OnlyOneFileCountsSpawns`（守"计账只有一处"）。
//
// 依赖规模取 8：偏大的 N 让"每依赖多一次"这种劣化无法被常数项吸收掉。
func TestV06VerifySpawnBudget(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	const deps = 8

	budgets := []struct {
		refType string
		perDep  int64
	}{
		{"commit", 2},
		{"tag", 3},
	}

	for _, b := range budgets {
		b := b
		t.Run(b.refType, func(t *testing.T) {
			proj := newProject(t)
			for i := 0; i < deps; i++ {
				slug := fmt.Sprintf("github:v06/%s%02d", b.refType, i)
				r := testutils.NewGitRepo(t)
				r.WriteFile("index.ts", fmt.Sprintf("export const v = %d\n", i))
				r.Commit("feat: dep")
				r.Tag("v1.0.0", false)
				seedMirror(t, slug, r.Dir)

				ref := "v1.0.0"
				if b.refType == "commit" {
					ref = r.Head()
				}
				if code, out := runCaptureCode(t, "add", slug+"@"+ref, "--ref-type="+b.refType, "--dir="+proj); code != 0 {
					t.Fatalf("add %s: %s", slug, out)
				}
			}
			// install 自己也会起子进程（解析 + 落锁 + 取对象），因此计数器要在它**之后**
			// 清零——本用例测的是 verify 的成本，不是 install 的。
			if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
				t.Fatalf("install: %s", out)
			}

			git.ResetSpawnCount()
			code, out := runCaptureCode(t, "verify", "--dir="+proj)
			if code != 0 {
				t.Fatalf("verify(%s) exit=%d:\n%s", b.refType, code, out)
			}

			got := git.SpawnCount()
			want := b.perDep * deps
			t.Logf("online verify, %d %s dependencies: %d git spawns (%.2f per dependency; budget %d)",
				deps, b.refType, got, float64(got)/deps, b.perDep)

			if got > want {
				t.Errorf("online verify spent %d git spawns for %d %s dependencies (%.2f per dependency), "+
					"over the budget of %d (%.2f per dependency).\n"+
					"Every extra spawn is paid once per dependency (100 dependencies = 100 processes).\n"+
					"If this is a deliberate change, raise the budget in this test *and* record why "+
					"(see docs/development/v0.6-plan.md, group B).",
					got, deps, b.refType, float64(got)/deps, want, float64(b.perDep))
			}

			// 下限：计数器必须真的在数。只写上界的话，"计数器坏了、永远返回 1"
			// 这类实现会让门禁一路绿——那正是本版要治的病（仪器说谎比没有仪器更糟）。
			if got < deps {
				t.Errorf("only %d git spawns for %d dependencies: the counter looks dead, "+
					"and a dead counter makes this whole gate meaningless", got, deps)
			}
		})
	}
}
