package main

import (
	"encoding/json"
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

// writeGlobalPerms 把 `permissions` 写进**全局配置**（`<HOME>/.ngm/config.json`）。
//
// 路径必须用 `HOME`，不是 `NGM_HOME`：前者是全局配置的根（`config.GlobalPath`），
// 后者只覆盖 mirror / content / cache 那一层（`vendor.Layout`）。
// 写错地方的后果很隐蔽——配置读不到，于是"默认拒绝"会让判据**假绿**：
// 它拒绝的理由不是你配的 deny，而是"什么都没配"。第一版就踩了这个坑。
func writeGlobalPerms(t *testing.T, allow, deny []string) {
	t.Helper()
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("isolateUserEnv did not set HOME")
	}
	dir := filepath.Join(home, ".ngm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	type perms struct {
		Allow []string `json:"allow,omitempty"`
		Deny  []string `json:"deny,omitempty"`
	}
	body, err := json.Marshal(map[string]perms{"permissions": {Allow: allow, Deny: deny}})
	if err != nil {
		t.Fatal(err)
	}
	if werr := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o644); werr != nil {
		t.Fatalf("write global config: %v", werr)
	}
}

// allowGitee 是"用户按提示配好之后"的状态：放行该主机。
//
// 每个远端判据都要先做这一步——否则它测的不是被测行为，而是**默认拒绝**。
func allowGitee(t *testing.T) {
	t.Helper()
	writeGlobalPerms(t, []string{"net:gitee.com"}, nil)
}

// fixtureResolve 走"声明 + 解析"两步：`ngm add` 只改 ngm.json，真正碰网络的是 `update`。
//
// 这个区分本身值得写下来：如果判据只跑 `add` 就断言 commit，它会在**没有联网**
// 的情况下通过（lock 根本不存在），于是"远端解析对不对"从来没被测过。
func fixtureResolve(t *testing.T, dir, spec string) {
	t.Helper()
	if code, out := runCaptureCode(t, "add", spec, "--ref-type=tag", "--dir="+dir); code != 0 {
		t.Fatalf("add %s: %s", spec, out)
	}
	if code, out := runCaptureCode(t, "update", "--all", "--dir="+dir); code != 0 {
		t.Fatalf("update after add %s (this is the step that talks to the host): %s", spec, out)
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
	allowGitee(t)
	p := newProject(t)
	fixtureResolve(t, p, "gitee:idcu/ngm-fixture-basic@v1.0.0")

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
	allowGitee(t)
	p := newProject(t)
	fixtureResolve(t, p, "gitee:idcu/ngm-fixture-basic@v1.1.0-light")
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

	if code, out := runCaptureCode(t, "add", "gitee:idcu/ngm-fixture-basic@v1.0.0",
		"--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add should not need the network: %s", out)
	}

	// 第一段：**放行**该 host 时必须成功（否则这一段什么都没证明——
	// 默认档位本来就是"需配置"，任何配置都能让它失败）。
	//
	// 第一版把 deny 写进了 `NGM_HOME/config.json`，而全局配置在
	// `<HOME>/.ngm/config.json`：配置根本没被读到，判据却是绿的，
	// 因为它拒绝的理由是"什么都没配"。**假的绿比红危险**。
	allowGitee(t)
	if code, out := runCaptureCode(t, "update", "--all", "--dir="+p); code != 0 {
		t.Fatalf("with the host allowed, update must reach gitee.com:\n%s", out)
	}

	// 第二段：allow 与 deny 同时在场 ⇒ **deny 胜**，且必须在出网之前。
	// 用一个新的项目与一份新的用户态，避免上一段的 mirror 让第二段短路。
	isolateUserEnv(t)
	p2 := newProject(t)
	writeGlobalPerms(t, []string{"net:gitee.com"}, []string{"net:gitee.com"})
	if code, out := runCaptureCode(t, "add", "gitee:idcu/ngm-fixture-basic@v1.0.0",
		"--ref-type=tag", "--dir="+p2); code != 0 {
		t.Fatalf("add should not need the network: %s", out)
	}
	code, out := runCaptureCode(t, "update", "--all", "--dir="+p2)
	if code == 0 {
		t.Fatalf("`deny` must win over `allow` — the host was denied and the update succeeded:\n%s", out)
	}
	if !strings.Contains(out, "gitee.com") {
		t.Errorf("the refusal must name the host it would have contacted:\n%s", out)
	}
	// 没有留下 mirror：连"悄悄连了一下再失败"都不允许。
	ngmHome := os.Getenv("NGM_HOME")
	if _, err := os.Stat(filepath.Join(ngmHome, "mirror", "gitee.com")); err == nil {
		t.Error("a denied network permission must not leave a partially cloned mirror behind")
	}
}
