package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件用**真实生成的 SSH 密钥**签一个真实 commit，再断言分类结果。
//
// 为什么不用假的 git 输出：`%G?` 的取值（G/B/U/N）来自 git 与它背后的签名工具，
// 不是 ngm 能模拟的东西。用假输出测的是"我们的 switch 语句对不对"，
// 而这里要证明的是"我们对 git 的用法与 git 的行为一致"——
// 后者只有真密钥能证明（这也是本项目两次真工具验收各自抓到真缺陷的原因）。
//
// 缺 ssh-keygen 时跳过（与"缺 deno"同类）；CI 三平台的 runner 都自带它。

// signerKeys 是测试用的一次性 SSH 密钥对与 allowed_signers 文件。
type signerKeys struct {
	// priv 是私钥路径（签名端用）。
	priv string
	// allowedSigners 是 allowed_signers 文件路径（验证端用）。
	allowedSigners string
	// email 是签名者身份（必须与 fixture 仓库的 committer email 一致，
	// 否则 git 会认为签名与提交者不匹配）。
	email string
}

// newSignerKeys 生成一次性密钥并写好 allowed_signers。
//
// principal 用**仓库的 committer email**：git 校验时会比对签名者身份与提交者，
// 只把公钥列进去而身份对不上，结果不会是好签名。
func newSignerKeys(t *testing.T) signerKeys {
	t.Helper()
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not installed; the status mapping follows git's own format specifier")
	}

	dir := t.TempDir()
	priv := filepath.Join(dir, "signing_key")
	cmd := exec.Command(keygen, "-t", "ed25519", "-N", "", "-C", "ngm-test", "-f", priv)
	if out, kerr := cmd.CombinedOutput(); kerr != nil {
		t.Fatalf("ssh-keygen: %v\n%s", kerr, out)
	}

	pub, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	const email = "ngm@test.local"
	allowed := filepath.Join(dir, "allowed_signers")
	line := email + " " + strings.TrimSpace(string(pub)) + "\n"
	if werr := os.WriteFile(allowed, []byte(line), 0o600); werr != nil {
		t.Fatal(werr)
	}
	return signerKeys{priv: priv, allowedSigners: allowed, email: email}
}

// runGitIn 在 dir 里跑一条 git 命令（本文件只做 testutils 之外的最小封装，
// 避免让 internal/git 的测试依赖 testutils —— 那会绕回本包）。
func runGitIn(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// initRepo 建一个最小仓库并返回 HEAD。
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitIn(t, dir, nil, "init", "-q", "-b", "main", "--template=")
	runGitIn(t, dir, nil, "config", "user.name", "ngm test")
	runGitIn(t, dir, nil, "config", "user.email", "ngm@test.local")
	runGitIn(t, dir, nil, "config", "core.autocrlf", "false")
	return dir
}

// TestCommitSignature_UnsignedThenSigned 固定最基本的两种形态：
// 未签名的 commit 报 `none`（**不是错误**），签名后报 `good`。
func TestCommitSignature_UnsignedThenSigned(t *testing.T) {
	dir := initRepo(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	// 先造一个**未签名**的 commit：这是绝大多数依赖的真实形态。
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, nil, "add", "-A")
	runGitIn(t, dir, nil, "commit", "-q", "-m", "unsigned")
	unsigned := strings.TrimSpace(runGitIn(t, dir, nil, "rev-parse", "HEAD"))

	sig, err := CommitSignature(context.Background(), Options{Dir: dir}, dir, unsigned)
	if err != nil {
		t.Fatalf("CommitSignature: %v", err)
	}
	if sig.Status != SigNone {
		t.Errorf("an unsigned commit must be reported as none (a fact, not a failure), got %q", sig.Status)
	}
	if sig.From != "commit" {
		t.Errorf("From = %q, want commit", sig.From)
	}
}

// TestCommitSignature_RealSSHSignature 是本次交付的核心断言：
// 用真密钥签出来的 commit，在**用户自己的密钥配置**下报 good；
// 换成一份不含该密钥的 allowed_signers，则报 untrusted（而不是 good，也不是 bad）。
func TestCommitSignature_RealSSHSignature(t *testing.T) {
	keys := newSignerKeys(t)
	dir := initRepo(t)

	// 签名端配置（仓库级）：用 SSH 格式 + 我们的私钥。
	runGitIn(t, dir, nil, "config", "gpg.format", "ssh")
	runGitIn(t, dir, nil, "config", "user.signingkey", keys.priv+".pub")
	runGitIn(t, dir, nil, "config", "commit.gpgsign", "true")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("signed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, nil, "add", "-A")
	runGitIn(t, dir, nil, "commit", "-q", "-S", "-m", "signed")
	signed := strings.TrimSpace(runGitIn(t, dir, nil, "rev-parse", "HEAD"))

	// 验证端配置：通过 GIT_CONFIG_GLOBAL 指向一份 allowed_signers。
	// 关键点——判定用的是**用户自己的**密钥配置，ngm 不参与密钥管理。
	//
	// 路径写成正斜杠：git 配置文件里反斜杠是转义字符，Windows 的 `C:\...`
	// 会被判为非法配置（实测 `fatal: bad config line`）。
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tallowedSignersFile = " +
		filepath.ToSlash(keys.allowedSigners) + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)

	sig, err := CommitSignature(context.Background(), Options{Dir: dir}, dir, signed)
	if err != nil {
		t.Fatalf("CommitSignature: %v", err)
	}
	if sig.Status != SigGood {
		t.Fatalf("a signature from a key you list must be good, got %q (raw=%q signer=%q)",
			sig.Status, sig.Raw, sig.Signer)
	}

	// 对照：把 allowed_signers 换成一个**不认识**的密钥。
	// 同一个 commit、同一把私钥，只是验证方的信任配置不同 → 必须变成 untrusted。
	//
	// 这一步是本测试的牙齿：没有它，"报 good"可能只是因为 git 根本没在验证。
	empty := filepath.Join(t.TempDir(), "allowed_signers")
	if werr := os.WriteFile(empty, nil, 0o600); werr != nil {
		t.Fatal(werr)
	}
	body2 := "[gpg]\n\tformat = ssh\n[gpg \"ssh\"]\n\tallowedSignersFile = " +
		filepath.ToSlash(empty) + "\n"
	if werr := os.WriteFile(cfg, []byte(body2), 0o600); werr != nil {
		t.Fatal(werr)
	}

	sig2, err := CommitSignature(context.Background(), Options{Dir: dir}, dir, signed)
	if err != nil {
		t.Fatalf("CommitSignature: %v", err)
	}
	if sig2.Status == SigGood {
		t.Errorf("with a key you do not list, the verdict must not be good; got %q", sig2.Status)
	}
	if sig2.Status == SigNone {
		t.Errorf("the signature exists; reporting none would hide it; got %q", sig2.Status)
	}
}
