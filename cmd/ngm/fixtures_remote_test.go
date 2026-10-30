package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
)

// 远端夹具的 opt-in 判据（v0.112）。
//
// **默认跳过**，因为本仓库的纪律是"测试不依赖公网"。但有三件事本地 mirror
// 假不出来，只有真实宿主的 ref 广播能证明：
//
//  1. annotated tag 在 `ls-remote` 里出现**两行**（`refs/tags/x` 与 `refs/tags/x^{}`），
//     而 ngm 必须锁到剥开后的那个 commit；
//  2. 真实 `git clone`（pack 传输、auth、redirect）；
//  3. `net:<host>` 门禁在**出网之前**生效——本地路径根本不走这条检查。
//
// 跑法：
//
//	NGM_NET_FIXTURES=1 go test ./cmd/ngm -run TestNetFixture -v
//
// 夹具仓库在 https://gitee.com/idcu/ngm-fixture-{basic,monorepo,content}，
// 由 TestV112PushInstructions 生成后人工推送一次（见 docs/internals/remote-fixtures.md）。

const fixtureOrgURL = "https://gitee.com/idcu/"

// expectedFixtureRefs 是**推送时**的 ref → sha 快照，同时充当腐坏守卫。
//
// 这些 SHA 是确定的：生成器固定了作者、时间、消息与内容，任何机器重跑都得到
// 同一批值（见 TestV112FixtureGeneratorReproducesTheSameSHAs）。
//
// 它们也是**唯一**能发现"夹具被改写过"的东西：远端 tag 被移动之后，夹具仍然
// "能用"，只是那些依赖它的断言开始以难以归因的方式失败。
var expectedFixtureRefs = map[string]map[string]string{
	"ngm-fixture-basic": {
		// annotated tag：对象本身是 tag，剥开后是 c1。
		"refs/tags/v1.0.0":    "14ae2ef6731a35a561677eb4de569beaed878450",
		"refs/tags/v1.0.0^{}": "05701ae7e09d2716cf02824d4dfa56c4595392f5",
		// lightweight tag：直接指向 commit（= main 的 tip）。
		"refs/tags/v1.1.0-light": "725cb2ebfb3da6209371a37ebe47677b2821cd70",
		"refs/heads/main":        "725cb2ebfb3da6209371a37ebe47677b2821cd70",
		"refs/heads/legacy":      "6530ad09ab1f3f7065e8df34154f981bfc0622e9",
	},
	"ngm-fixture-monorepo": {
		"refs/tags/packages-a-v1.0.0":    "03e8dfce08ac4fca3f9cf449c9f6881f4c3916b2",
		"refs/tags/packages-a-v1.0.0^{}": "978c1d43f504e23817731c92ff5aee9630376348",
		"refs/tags/packages-b-v1.0.0":    "75d1cc68583fba41ed41fb406b5b8aa16de5c879",
		"refs/tags/packages-b-v1.0.0^{}": "a17e0c3b084a6b396c1bf3ef72833646faf2e056",
		"refs/heads/main":                "a17e0c3b084a6b396c1bf3ef72833646faf2e056",
	},
	"ngm-fixture-content": {
		"refs/tags/v1.0.0":    "7a97e7512d50ddff20a64c1f51db3e8f4b9f4581",
		"refs/tags/v1.0.0^{}": "8d2b7a6cf5fb9c2e16960632bc8e9f41987ba827",
		"refs/heads/main":     "8d2b7a6cf5fb9c2e16960632bc8e9f41987ba827",
	},
}

// requireNetFixtures 是这几个判据的唯一闸门。
func requireNetFixtures(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("NGM_NET_FIXTURES")) != "1" {
		t.Skip("network fixture test — set NGM_NET_FIXTURES=1; needs gitee.com and the " +
			"three ngm-fixture-* repositories (see docs/internals/remote-fixtures.md)")
	}
	if testing.Short() {
		t.Skip("network fixture test skipped in -short mode")
	}
}

// lsRemote 返回 ref → sha。annotated tag 的 `^{}` 行会原样保留——
// 那两行的存在本身就是这条判据要看的东西之一。
func lsRemote(t *testing.T, repo string) map[string]string {
	t.Helper()
	cmd := exec.Command("git", "ls-remote", fixtureOrgURL+repo+".git")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-remote %s: %v\n%s", repo, err, out)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		refs[fields[1]] = fields[0]
	}
	return refs
}

// TestNetFixturesHaveNotBeenRewritten 是腐坏守卫，必须排在其他远端判据之前看。
//
// 它回答的问题与"ngm 能不能用"无关：**这些仓库还是当初那三个吗**。
// 一个被移动过的 tag 会让下游断言以"解析出来的 commit 不对"失败，
// 而那听起来像 ngm 的 bug。
func TestNetFixturesHaveNotBeenRewritten(t *testing.T) {
	requireNetFixtures(t)

	for repo, want := range expectedFixtureRefs {
		got := lsRemote(t, repo)
		if len(got) == 0 {
			t.Fatalf("%s is empty — the fixtures have not been pushed yet "+
				"(generate them with NGM_FIXTURES_OUT=<dir> and `git push --mirror`)", repo)
		}
		for ref, wantSHA := range want {
			gotSHA, ok := got[ref]
			if !ok {
				t.Errorf("%s: %s is missing on the remote (was the fixture rewritten?)", repo, ref)
				continue
			}
			if gotSHA != wantSHA {
				t.Errorf("%s: %s = %s, want %s — the fixture was rewritten.\n"+
					"    Either restore it, or regenerate and update BOTH this table and\n"+
					"    docs/internals/remote-fixtures.md in the same commit: a fixture whose\n"+
					"    recorded SHA is stale turns every downstream failure into a mystery.",
					repo, ref, gotSHA, wantSHA)
			}
		}
	}
}

// TestNetFixtureAnnotatedTagPeelsToTheCommit 是本轮最想证明的那一条：
// 真实宿主把 annotated tag 广播成**两行**，而 ngm 必须锁到剥开后的 commit，
// 而不是 tag 对象本身的 sha。
func TestNetFixtureAnnotatedTagPeelsToTheCommit(t *testing.T) {
	requireNetFixtures(t)

	isolateUserEnv(t)
	p := newProject(t)
	if code, out := runCaptureCode(t, "add", "gitee:idcu/ngm-fixture-basic@v1.0.0",
		"--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add annotated tag: %s", out)
	}

	lf, err := lock.Read(filepath.Join(p, "ngm.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if lf == nil || len(lf.Dependencies) != 1 {
		t.Fatalf("lock has no dependency: %+v", lf)
	}
	got := lf.Dependencies[0].Commit
	want := expectedFixtureRefs["ngm-fixture-basic"]["refs/tags/v1.0.0^{}"]
	if got != want {
		t.Errorf("locked commit = %s, want the peeled commit %s.\n"+
			"    If it equals the tag object's sha (%s), the `^{}` line is not being used —\n"+
			"    which on a real host means every annotated tag resolves wrongly.",
			got, want, expectedFixtureRefs["ngm-fixture-basic"]["refs/tags/v1.0.0"])
	}
}

// 同一棵树，lightweight tag 走另一条路径（ls-remote 只给一行）。
// 两种语义必须落到各自正确的 commit——否则"两种 tag 都能用"只是文档里的说法。
func TestNetFixtureLightweightTagResolvesToItsCommit(t *testing.T) {
	requireNetFixtures(t)

	isolateUserEnv(t)
	p := newProject(t)
	if code, out := runCaptureCode(t, "add", "gitee:idcu/ngm-fixture-basic@v1.1.0-light",
		"--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add lightweight tag: %s", out)
	}
	lf, err := lock.Read(filepath.Join(p, "ngm.lock"))
	if err != nil || lf == nil || len(lf.Dependencies) != 1 {
		t.Fatalf("read lock: %v (%+v)", err, lf)
	}
	got := lf.Dependencies[0].Commit
	want := expectedFixtureRefs["ngm-fixture-basic"]["refs/tags/v1.1.0-light"]
	if got != want {
		t.Errorf("locked commit = %s, want %s", got, want)
	}
}

// `net:<host>` 门禁必须在**出网之前**生效。
//
// 这条只有真实远端能测：本地 mirror 走的是文件路径，压根不经过 net: 检查。
// 判据三项：拒绝、理由点名 host、以及**没有留下任何下载产物**。
func TestNetFixtureNetworkGateRefusesBeforeConnecting(t *testing.T) {
	requireNetFixtures(t)

	isolateUserEnv(t)
	p := newProject(t)

	// 全局配置里拒掉该 host（项目级 ngm.json 的 permissions 也可以，但全局更贴近
	// "CI 机器统一收紧"的真实用法）。
	home := os.Getenv("NGM_HOME")
	if home == "" {
		t.Fatal("isolateUserEnv did not set NGM_HOME")
	}
	cfg := `{"permissions":{"deny":["net:gitee.com"]}}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	code, out := runCaptureCode(t, "add", "gitee:idcu/ngm-fixture-basic@v1.0.0",
		"--ref-type=tag", "--dir="+p)
	if code == 0 {
		t.Fatalf("a denied host must not succeed:\n%s", out)
	}
	if !strings.Contains(out, "gitee.com") {
		t.Errorf("the refusal must name the host it would have contacted:\n%s", out)
	}
	// 没有留下 mirror：连"悄悄连了一下再失败"都不允许。
	if _, err := os.Stat(filepath.Join(home, "mirror", "gitee.com")); err == nil {
		t.Error("a denied network permission must not leave a partially cloned mirror behind")
	}
}
