package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// TestM1Acceptance 是 development/v0.1-plan.md 中 M1（Git 层）阶段的**可执行验收**。
//
// 逐条对应 M1 的验收标准：
//
//  1. 全部 git 测试在离线 fixture 上运行通过；网络类失败 → exit 4
//  2. annotated tag / branch / commit 三类解析用例正确；归一化表驱动全绿
//  3. 认证用例：token 不出现于任何输出与本地文件
//
// 本测试额外走**真实 CLI 路径**（dispatch），在 --offline 下完成
// init → add → update 的端到端闭环，证明"仅凭本地 mirror 即可解析 commit"。
//
// 这是一个普通 Go 测试（非 build tag 隔离）：它只依赖 git 与本地 fixture，
// 因此在 ubuntu / macos / windows 三平台一致运行，并随 `go test ./...` 常态化回归。
func TestM1Acceptance(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	ctx := context.Background()

	// ---------------------------------------------------------------------
	// 准备：一个"上游"仓库，含 annotated tag 与一个分支
	// ---------------------------------------------------------------------
	upstream := testutils.NewGitRepo(t)
	upstream.WriteFile("src/index.ts", "export const x = 1\n")
	headV1 := upstream.Commit("feat: initial")
	upstream.Tag("v1.0.0", true /* annotated */)

	upstream.WriteFile("src/b.ts", "export const b = 2\n")
	headMain := upstream.Commit("feat: b")

	// 断言 precondition：annotated tag 的 tag object 与 commit 不同
	if tagObj := testutils.GitTagObjectSHA(t, upstream.Dir, "v1.0.0"); tagObj == headV1 {
		t.Fatalf("fixture precondition failed: annotated tag object equals commit")
	}

	// ---------------------------------------------------------------------
	// 验收 2：归一化表驱动 + 三类 refType 解析（离线，走真实解析入口）
	// ---------------------------------------------------------------------
	t.Run("normalization table", func(t *testing.T) {
		table := []struct{ in, want string }{
			{"https://github.com/org/repo.git", "github.com/org/repo"},
			{"git@github.com:org/repo.git", "github.com/org/repo"},
			{"git://github.com/org/repo.git", "github.com/org/repo"},
			{"github:org/repo", "github.com/org/repo"},
			{"gitee:org/repo", "gitee.com/org/repo"},
			{"gitlab:group/sub/repo", "gitlab.com/group/sub/repo"},
		}
		for _, tc := range table {
			got, err := resolve.Normalize(tc.in)
			if err != nil {
				t.Errorf("Normalize(%q): %v", tc.in, err)
				continue
			}
			if got.String() != tc.want {
				t.Errorf("Normalize(%q)=%q want %q", tc.in, got.String(), tc.want)
			}
		}
	})

	t.Run("refType resolution on local fixture", func(t *testing.T) {
		repo := resolve.MustNormalize("github:org/repo")
		opts := resolve.ResolveOptions{GitURL: upstream.Dir}

		// tag（annotated）
		gotTag, err := resolve.ResolveRef(ctx, repo, "v1.0.0", resolve.RefTypeTag, opts)
		if err != nil {
			t.Fatalf("tag: %v", err)
		}
		if gotTag != headV1 {
			t.Errorf("annotated tag → %s want %s", gotTag, headV1)
		}

		// branch
		gotBranch, err := resolve.ResolveRef(ctx, repo, "main", resolve.RefTypeBranch, opts)
		if err != nil {
			t.Fatalf("branch: %v", err)
		}
		if gotBranch != headMain {
			t.Errorf("branch main → %s want %s", gotBranch, headMain)
		}

		// commit
		gotCommit, err := resolve.ResolveRef(ctx, repo, headV1, resolve.RefTypeCommit, opts)
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		if gotCommit != headV1 {
			t.Errorf("commit → %s want %s", gotCommit, headV1)
		}
	})

	// ---------------------------------------------------------------------
	// 验收 1：网络类失败 → exit 4
	// ---------------------------------------------------------------------
	t.Run("network failure maps to exit 4", func(t *testing.T) {
		repo := resolve.MustNormalize("github:org/repo")
		bogus := filepath.Join(t.TempDir(), "no-such-repo")
		_, err := resolve.ResolveRef(ctx, repo, "main", resolve.RefTypeBranch,
			resolve.ResolveOptions{GitURL: bogus})
		if err == nil {
			t.Fatal("expected failure")
		}
		var ne *errs.NgmError
		if !errors.As(err, &ne) {
			t.Fatalf("error type %T", err)
		}
		if ne.Code.ExitCode() != 4 {
			t.Errorf("exit code=%d want 4", ne.Code.ExitCode())
		}
	})

	// ---------------------------------------------------------------------
	// 验收 3：token 不出现在任何输出中
	// ---------------------------------------------------------------------
	t.Run("token never leaks", func(t *testing.T) {
		const token = "ghp_m1acceptancetoken"
		t.Setenv("GITHUB_TOKEN", token)

		secrets := git.SecretsFromEnvVars(git.MergeTokenEnvVars(nil))
		if len(secrets) == 0 {
			t.Fatal("secrets not picked up from env")
		}
		redacted := git.Redact("fatal: auth failed for ghp_m1acceptancetoken", secrets)
		if strings.Contains(redacted, token) {
			t.Errorf("token leaked after redaction: %q", redacted)
		}

		// 真实 git 调用的错误路径也不得泄露
		remote := "https://x-access-token:" + token + "@127.0.0.1:1/none.git"
		_, err := git.Run(ctx, git.Options{Secrets: secrets}, "ls-remote", remote)
		if err != nil && strings.Contains(err.Error(), token) {
			t.Errorf("token leaked in git error: %v", err)
		}
	})

	// ---------------------------------------------------------------------
	// 端到端：init → add → update --offline（仅本地 mirror，零网络）
	// ---------------------------------------------------------------------
	t.Run("offline end-to-end", func(t *testing.T) {
		layout, err := vendor.DefaultLayout()
		if err != nil {
			t.Fatal(err)
		}
		if err := layout.EnsureDirs(); err != nil {
			t.Fatal(err)
		}

		// 预置 mirror（把本地 fixture 登记为 `github:ngm-fixture/libs` 的镜像）
		seedMirror(t, "github:ngm-fixture/libs", upstream.Dir)

		proj := t.TempDir()

		// init
		if code, out := runCaptureCode(t, "init", "github.com:ngm-fixture/app", "--runtime=node", "--dir="+proj); code != 0 {
			t.Fatalf("init exit=%d out=%s", code, out)
		}

		// add（annotated tag）
		if code, out := runCaptureCode(t, "add", "github:ngm-fixture/libs@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add exit=%d out=%s", code, out)
		}

		// update --offline
		code, out := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline")
		if code != 0 {
			t.Fatalf("update --offline exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, headV1) {
			t.Errorf("update should resolve to %s (annotated tag commit):\n%s", headV1, out)
		}
		// 明确断言不是 tag object
		if tagObj := testutils.GitTagObjectSHA(t, upstream.Dir, "v1.0.0"); strings.Contains(out, tagObj) {
			t.Errorf("update resolved to tag object %s instead of commit %s", tagObj, headV1)
		}

		// --offline 状态下，ngm.json 未被破坏
		data, rerr := os.ReadFile(filepath.Join(proj, "ngm.json"))
		if rerr != nil {
			t.Fatal(rerr)
		}
		if !bytes.Contains(data, []byte(`"name": "github:ngm-fixture/libs"`)) {
			t.Errorf("ngm.json missing dependency:\n%s", data)
		}
	})

	// ---------------------------------------------------------------------
	// 端到端：--offline 但 mirror 缺失 → exit 4
	// ---------------------------------------------------------------------
	t.Run("offline without mirror exits 4", func(t *testing.T) {
		proj := t.TempDir()
		if code, out := runCaptureCode(t, "init", "github.com:x/y", "--dir="+proj); code != 0 {
			t.Fatalf("init: %s", out)
		}
		if code, out := runCaptureCode(t, "add", "github:absent/repo@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline")
		if code != 4 {
			t.Errorf("exit=%d want 4; out=%s", code, out)
		}
	})

	// ---------------------------------------------------------------------
	// 环境健壮性：开发者的"敌意"全局 git 配置不得影响验收
	//
	// 覆盖三类真实世界的本机失败源：
	//   - 全局开启 GPG 签名（缺密钥 → commit/tag 失败）
	//   - core.autocrlf=true（行尾被转换）
	//   - 残留 GIT_DIR（仓库定位被劫持）
	// ---------------------------------------------------------------------
	t.Run("survives hostile developer git config", func(t *testing.T) {
		hostile := filepath.Join(t.TempDir(), "hostile-gitconfig")
		body := strings.Join([]string{
			"[user]",
			"\tname = Hostile",
			"\temail = hostile@example.com",
			"[commit]",
			"\tgpgsign = true",
			"[tag]",
			"\tgpgsign = true",
			"[core]",
			"\tautocrlf = true",
			"",
		}, "\n")
		if err := os.WriteFile(hostile, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GIT_CONFIG_GLOBAL", hostile)
		t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
		t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "decoy.git")) // 诱饵
		t.Setenv("GIT_WORK_TREE", t.TempDir())

		// fixture 与端到端流程在敌意配置下必须照常工作
		hostileUpstream := testutils.NewGitRepo(t)
		want := hostileUpstream.WriteFile("a.ts", "1\n").Commit("feat: under hostile config")
		hostileUpstream.Tag("v1.0.0", true)

		isolateUserEnv(t)
		seedMirror(t, "github:hostile-test/libs", hostileUpstream.Dir)

		proj := t.TempDir()
		if code, out := runCaptureCode(t, "init", "github.com:hostile-test/app", "--dir="+proj); code != 0 {
			t.Fatalf("init: %s", out)
		}
		if code, out := runCaptureCode(t, "add", "github:hostile-test/libs@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "update", "--all", "--dir="+proj, "--offline")
		if code != 0 {
			t.Fatalf("update --offline under hostile config: exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, want) {
			t.Errorf("expected commit %s in output:\n%s", want, out)
		}
	})
}
