package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV18RealUpstreamFirstRun 是**可选的联网复核**：对真实的 GitHub 跑一次
// "第一次上手"的完整路径（init → add → install → verify → why → tree）。
//
// **为什么需要它**：本项目**所有**测试都用本地 mirror（这是对的——门禁必须确定性、离线），
// 代价是"第一次真实上手能不能走通"这条**最影响采用**的路径，在 CI 里没有任何证据。
// 这一版把它补上，但**不**把它做成门禁：联网测试会因别人的服务而红，
// 那种红会训练人忽略红色。因此它默认跳过，只在 `NGM_REAL_UPSTREAM=1` 时运行。
//
// 它验的是四类只有真实远端才暴露的东西：
//
//	① 真实 ref 解析（`master` 这种会移动的分支）与真实 clone
//	② 真实仓库**没有 `ngm.json`** 时的行为（上游清单缺席是最常见的情形）
//	③ **可复现性**：两个独立项目装同一个 commit，`archiveDigest` 必须逐字相同
//	④ 真实错误路径：不存在的仓库必须给出**点名该仓库**的失败，而不是含混的报错
//
// 跑法：
//
//	NGM_REAL_UPSTREAM=1 go test -count=1 -timeout 900s -run TestV18RealUpstreamFirstRun -v ./cmd/ngm
func TestV18RealUpstreamFirstRun(t *testing.T) {
	if os.Getenv("NGM_REAL_UPSTREAM") != "1" {
		t.Skip("联网复核：设 NGM_REAL_UPSTREAM=1 才运行（默认跳过，它不进离线门禁）")
	}

	home := isolateUserEnv(t)
	testutils.MustHaveGit(t)

	// 真实 clone 走 https://github.com，因此要显式授权那个 host。
	// 这与 audit 的 OSV 替身用 grantNetFor 是同一条纪律：**默认拒绝**，
	// 想联网就得把 host 写进配置。
	writeGlobalConfig(t, home, `{"permissions":{"allow":["net:github.com"]}}`)

	const (
		slug  = "github:octocat/Hello-World"
		ref   = "master"
		badup = "github:octocat/definitely-not-a-repo-ngm-probe"
	)

	// step 跑一条命令并计时。**联网的部分只记录、不判定**：
	// 它会因别人的服务而红，而那种红不属于本项目——但它的**耗时**是本项目的事。
	step := func(t *testing.T, mustSucceed bool, args ...string) (int, string, time.Duration) {
		t.Helper()
		start := time.Now()
		code, out := runCaptureCode(t, args...)
		dur := time.Since(start)
		t.Logf("%-58s exit=%d  %s", strings.Join(args[:min(2, len(args))], " "), code, dur.Round(time.Millisecond))
		if mustSucceed && code != 0 {
			t.Fatalf("%v exit=%d after %s:\n%s", args, code, dur.Round(time.Millisecond), out)
		}
		return code, out, dur
	}

	firstRun := func(t *testing.T) (dir, digest string) {
		t.Helper()
		dir = newProject(t)

		code, out, _ := step(t, true, "add", slug+"@"+ref, "--ref-type=branch", "--dir="+dir)
		_ = code
		t.Logf("add: %s", firstLine(out))

		// install 需要真实 clone。远端不可达时（本机实测过一次 Connection reset）
		// **这次复核就没有证明任何东西**——如实判为未证明，而不是记成通过或失败。
		// 只有 exit 4（GitFetch）走这条；别的退出码是真失败。
		code, out, installDur := step(t, false, "install", "--dir="+dir)
		if code == 4 {
			t.Skipf("环境无法访问 %s（install exit=4，%s）：这一次没有证明任何东西 —— %s",
				slug, installDur.Round(time.Millisecond), firstLine(out))
		}
		if code != 0 {
			t.Fatalf("install exit=%d:\n%s", code, out)
		}
		t.Logf("install: %s", firstLine(out))

		// **离线** verify：这是可以在任何网络条件下断言的路径（本地 lock/vendor/store）。
		step(t, true, "verify", "--offline", "--dir="+dir)
		code, _, _ = step(t, true, "verify", "--offline", "--deep", "--dir="+dir)
		_ = code

		// **在线** verify：只记录。它的成败取决于远端可达性（实测过一次
		// Connection reset：ngm 如实报 check incomplete 并退 4，没有谎报通过）。
		onlineCode, onlineOut, onlineDur := step(t, false, "verify", "--dir="+dir)
		t.Logf("在线 verify：exit=%d 耗时=%s —— %s", onlineCode, onlineDur.Round(time.Millisecond), firstLine(onlineOut))
		if installDur < onlineDur {
			t.Logf("（本轮网络等待占了大头：install=%s，在线 verify=%s）",
				installDur.Round(time.Millisecond), onlineDur.Round(time.Millisecond))
		}

		code, out, _ = step(t, true, "why", slug, "--json", "--dir="+dir)
		_ = code
		var why struct {
			Commit string     `json:"commit"`
			Paths  [][]string `json:"paths"`
			Locked struct {
				Commit        string `json:"commit"`
				ArchiveDigest string `json:"archiveDigest"`
			} `json:"locked"`
		}
		if err := json.Unmarshal([]byte(out), &why); err != nil {
			t.Fatalf("why --json: %v\n%s", err, out)
		}
		t.Logf("why: commit=%s locked=%s digest=%s paths=%d",
			short(why.Commit), short(why.Locked.Commit), why.Locked.ArchiveDigest, len(why.Paths))

		code, out, _ = step(t, true, "tree", "--dir="+dir)
		_ = code
		t.Logf("tree:\n%s", out)

		if !reDigest.MatchString(why.Locked.ArchiveDigest) {
			t.Errorf("archiveDigest=%q is not a sha256 digest", why.Locked.ArchiveDigest)
		}
		if why.Commit == "" || why.Commit != why.Locked.Commit {
			t.Errorf("the resolved commit and the locked commit must agree: %q vs %q", why.Commit, why.Locked.Commit)
		}
		return dir, why.Locked.ArchiveDigest
	}

	// ③ 可复现性：两个**互不相干**的项目装同一个 commit，digest 必须逐字相同。
	_, digestA := firstRun(t)
	_, digestB := firstRun(t)
	if digestA != digestB {
		t.Errorf("the same upstream commit produced two digests:\n  %s\n  %s\n"+
			"(reproducibility is the whole point of archiveDigest)", digestA, digestB)
	}
	t.Logf("可复现性：两个独立项目得到同一个 digest %s", digestA)

	// ④ 真实错误路径：不存在的仓库必须点名它自己。
	proj := newProject(t)
	code, out, dur := step(t, false, "add", badup+"@main", "--ref-type=branch", "--dir="+proj)
	if code == 0 {
		t.Fatalf("adding a repo that does not exist must fail:\n%s", out)
	}
	if !strings.Contains(out, "definitely-not-a-repo-ngm-probe") {
		t.Errorf("the failure must name the repository it could not reach:\n%s", out)
	}
	t.Logf("不存在的仓库：exit=%d 耗时=%s，报错点名了它自己", code, dur.Round(time.Millisecond))
}

var reDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func short(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}
