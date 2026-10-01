package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v4SignerKeys 是一次性 SSH 密钥对（签名端）与 allowed_signers（验证端）。
type v4SignerKeys struct {
	priv           string
	allowedSigners string
}

// v4NewSignerKeys 生成一次性密钥。缺 ssh-keygen 时跳过（与"缺 deno"同类）。
func v4NewSignerKeys(t *testing.T) v4SignerKeys {
	t.Helper()
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not installed; the classifier is covered in internal/git")
	}
	dir := t.TempDir()
	priv := filepath.Join(dir, "signing_key")
	cmd := exec.Command(keygen, "-t", "ed25519", "-N", "", "-C", "ngm-test", "-f", priv)
	if out, kerr := cmd.CombinedOutput(); kerr != nil {
		t.Fatalf("ssh-keygen: %v\n%s", kerr, out)
	}
	pub, rerr := os.ReadFile(priv + ".pub")
	if rerr != nil {
		t.Fatal(rerr)
	}
	allowed := filepath.Join(dir, "allowed_signers")
	// principal 必须是仓库的 committer email：git 会比对签名者身份与提交者。
	line := "ngm@test.local " + strings.TrimSpace(string(pub)) + "\n"
	if werr := os.WriteFile(allowed, []byte(line), 0o600); werr != nil {
		t.Fatal(werr)
	}
	return v4SignerKeys{priv: priv, allowedSigners: allowed}
}

// v4PointGitAt 让**本进程里跑的 git** 用这份信任配置。
//
// 判定用的必须是用户自己的密钥配置（ADR-014 决策 2）——测试通过 GIT_CONFIG_GLOBAL
// 扮演"用户已经配好了 keyring"这一前提。
func v4PointGitAt(t *testing.T, allowedSigners string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	// 正斜杠：git 配置里反斜杠是转义字符，Windows 路径会被判为非法配置。
	body := "[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tallowedSignersFile = " +
		filepath.ToSlash(allowedSigners) + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
}

// v4SignedUpstream 建一个 commit 已签名的上游 fixture。
//
// 签名必须发生在 seedMirror **之前**：镜像里存的是 commit 对象本身，
// 之后补签不会改变已经镜像的那一个。
func v4SignedUpstream(t *testing.T, slug string, keys v4SignerKeys) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.Exec("config", "gpg.format", "ssh")
	r.Exec("config", "user.signingkey", filepath.ToSlash(keys.priv)+".pub")
	r.Exec("config", "commit.gpgsign", "true")
	r.WriteFile("index.ts", "export const signed = 1\n")
	r.Exec("add", "-A")
	r.Exec("commit", "-q", "-S", "-m", "feat: signed")
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
	return r
}

// v4UnsignedUpstream 建一个**没有签名**的上游——绝大多数依赖的真实形态。
func v4UnsignedUpstream(t *testing.T, slug string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const plain = 1\n")
	r.Commit("feat: unsigned")
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
	return r
}

// v4ProjectWithDep 建一个项目并安装指定依赖。
func v4ProjectWithDep(t *testing.T, slug string) string {
	t.Helper()
	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", slug+"@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add exit=%d:\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install exit=%d:\n%s", code, out)
	}
	return proj
}

// TestV04VerifySignaturesAcceptance 是签名检查的端到端验收。
func TestV04VerifySignaturesAcceptance(t *testing.T) {
	isolateUserEnv(t)

	// 这一条是本功能最重要的性质：**未签名默认不是失败**。
	// 绝大多数依赖没有签名，把它当错误会让 verify 对所有人变红——那是假警报，
	// 而假警报会让真的警报失效。
	t.Run("an unsigned dependency is reported but does not fail", func(t *testing.T) {
		v4UnsignedUpstream(t, "github:v4/plain")
		proj := v4ProjectWithDep(t, "github:v4/plain")

		code, out := runCaptureCode(t, "verify", "--signatures", "--dir="+proj)
		if code != 0 {
			t.Fatalf("an unsigned dependency must not fail verify; exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "no signature") {
			t.Errorf("the report must say it is unsigned:\n%s", out)
		}
		// 报告要说明判定来自哪里——否则用户不知道 ngm 凭什么这么说。
		if !strings.Contains(out, "your own Git key configuration") {
			t.Errorf("the report must say whose keys decide:\n%s", out)
		}
	})

	t.Run("--require-signed turns it into a gate", func(t *testing.T) {
		// 每个子测试用**各自的 slug**：mirror 以 slug 为键，而 fixture 仓库是临时目录，
		// 复用一个已 seed 过的 slug 会让后来者去 fetch 一个已经消失的上游。
		v4UnsignedUpstream(t, "github:v4/gated")
		proj := v4ProjectWithDep(t, "github:v4/gated")

		code, out := runCaptureCode(t, "verify", "--require-signed", "--dir="+proj)
		if code != 2 {
			t.Fatalf("an unsigned dependency under --require-signed must exit 2; got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:v4/gated") {
			t.Errorf("the failure must name the dependency:\n%s", out)
		}
	})

	// 有了密钥与信任配置，同一个依赖应当通过——这是"功能真的可用"的那一半。
	t.Run("a signed dependency passes the gate", func(t *testing.T) {
		keys := v4NewSignerKeys(t)
		v4SignedUpstream(t, "github:v4/signed", keys)
		v4PointGitAt(t, keys.allowedSigners)

		proj := v4ProjectWithDep(t, "github:v4/signed")

		code, out := runCaptureCode(t, "verify", "--require-signed", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a signature from a key you list must pass; exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "signed by") {
			t.Errorf("the report must say who signed it:\n%s", out)
		}
	})

	// --json 是给 CI 解析的（usage 里就这么写）。因此 stdout 必须是**纯 JSON**：
	// 附加段落一律走 stderr。这曾经是错的——`--json --sandbox` 会把沙箱段落
	// 混进 JSON 里。本用例是那处修复的回归防线。
	t.Run("--json keeps stdout pure", func(t *testing.T) {
		v4UnsignedUpstream(t, "github:v4/jsonpure")
		proj := v4ProjectWithDep(t, "github:v4/jsonpure")

		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		code := dispatch([]string{"verify", "--json", "--signatures", "--dir=" + proj}, stdout, stderr)
		if code != 0 {
			t.Fatalf("verify --json exit=%d:\n%s", code, stderr.String())
		}

		var rep map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
			t.Fatalf("stdout is not valid JSON (the additive section leaked into it): %v\n%s",
				err, stdout.String())
		}
		if !strings.Contains(stderr.String(), "git signatures") {
			t.Errorf("the additive section belongs on stderr under --json:\n%s", stderr.String())
		}
	})
}
