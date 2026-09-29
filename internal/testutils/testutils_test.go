package testutils

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateGolden 把 golden 根目录指向临时目录。
//
// 必要性：testutils 自己的测试若把 roundtrip.golden 写进仓库 testdata/，
// 会在开发者机器上留下未跟踪文件并污染 `git status`（甚至被误提交）。
func isolateGolden(t *testing.T) {
	t.Helper()
	t.Setenv("NGM_TESTDATA_DIR", t.TempDir())
}

func TestGolden_UpdateAndMatch(t *testing.T) {
	isolateGolden(t)

	*updateGolden = true
	t.Cleanup(func() { *updateGolden = false })

	sub := &testing.T{}
	GoldenString(sub, "roundtrip.golden", "hello\n")
	if sub.Failed() {
		t.Fatalf("update should not fail")
	}
	// 确认文件写到了隔离目录而不是仓库
	path := GoldenPath(t, "roundtrip.golden")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("file content mismatch: %q", string(data))
	}
	if strings.Contains(path, filepath.Join("internal", "testutils")) {
		t.Errorf("golden leaked into repo testdata: %s", path)
	}

	// match 模式
	*updateGolden = false
	sub2 := &testing.T{}
	GoldenString(sub2, "roundtrip.golden", "hello\n")
	if sub2.Failed() {
		t.Fatalf("match should pass")
	}
}

func TestGolden_MissingShowsHint(t *testing.T) {
	isolateGolden(t)
	*updateGolden = false
	t.Cleanup(func() { *updateGolden = false })

	sub := &testing.T{}
	GoldenString(sub, "absent.golden", "x\n")
	if !sub.Failed() {
		t.Fatalf("missing golden should fail the assertion")
	}
}

func TestGolden_Mismatch(t *testing.T) {
	isolateGolden(t)

	*updateGolden = true
	t.Cleanup(func() { *updateGolden = false })

	subA := &testing.T{}
	GoldenString(subA, "mismatch.golden", "abc\n")
	if subA.Failed() {
		t.Fatalf("seed failed")
	}

	*updateGolden = false
	subB := &testing.T{}
	GoldenString(subB, "mismatch.golden", "xyz\n")
	if !subB.Failed() {
		t.Fatalf("expected mismatch to fail")
	}
}

func TestHead(t *testing.T) {
	in := bytes.Repeat([]byte("a\n"), 100)
	got := head(in, 5)
	if !strings.HasPrefix(got, "a\n") {
		t.Errorf("head prefix wrong")
	}
	if !strings.Contains(got, "more lines") {
		t.Errorf("head should mention truncation")
	}
}

func TestMustHaveGit(t *testing.T) {
	MustHaveGit(t) // 若 git 不存在会被 Skip
}

// TestIsolateUserEnv 确认隔离生效：ngm 的用户态路径随 NGM_HOME 变化。
func TestIsolateUserEnv(t *testing.T) {
	home := IsolateUserEnv(t)
	if got := os.Getenv("NGM_HOME"); !strings.HasPrefix(got, home) {
		t.Errorf("NGM_HOME=%q not under %q", got, home)
	}
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if os.Getenv(k) != home {
			t.Errorf("%s=%q want %q", k, os.Getenv(k), home)
		}
	}
}

// TestGitInit_EnvironmentNeutral 锁定 GitInit 的仓库级配置，
// 确保 fixture 不受开发者全局 git 配置（GPG 签名、autocrlf）影响。
func TestGitInit_EnvironmentNeutral(t *testing.T) {
	MustHaveGit(t)
	dir := t.TempDir()
	GitInit(t, dir)

	want := map[string]string{
		"commit.gpgsign": "false",
		"tag.gpgsign":    "false",
		"core.autocrlf":  "false",
		"gc.auto":        "0",
	}
	for key, val := range want {
		got := strings.TrimSpace(GitOutput(t, dir, "config", "--get", key))
		if got != val {
			t.Errorf("config %s=%q want %q", key, got, val)
		}
	}
}

// TestGitInit_AnnotatedTagWorksWithGlobalGpgSign 的等价保护：
// 即使模拟全局 tag.gpgsign=true，仓库级 false 仍应让 annotated tag 创建成功。
func TestGitInit_AnnotatedTagWorksWithGlobalGpgSign(t *testing.T) {
	MustHaveGit(t)
	dir := t.TempDir()
	GitInit(t, dir)
	// 仓库级配置已覆盖全局；这里直接验证 annotated tag 可创建且解引用到 commit
	head := strings.TrimSpace(GitOutput(t, dir, "rev-parse", "HEAD"))
	got := GitTag(t, dir, "v1.0.0", true)
	if got != head {
		t.Errorf("annotated tag resolved to %s want commit %s", got, head)
	}
}

// TestGitInit_SurvivesHostileGlobalConfig 是"开发者本机失败"的回归防线。
//
// 真实世界场景：开发者全局开启了 GPG 提交/打标签签名（`commit.gpgsign` /
// `tag.gpgsign = true`），或设了 `core.autocrlf = true`。若 fixture 不做仓库级
// 覆盖，annotated tag 创建会因缺少可用签名密钥而**失败**，导致 M1 的
// annotated tag 用例、乃至整个验收全线报错。
//
// 本测试用 GIT_CONFIG_GLOBAL 指向一个"敌意"配置来复现该环境
// （不改动开发者的真实全局配置），断言 GitInit 仍然产出可用的 fixture。
func TestGitInit_SurvivesHostileGlobalConfig(t *testing.T) {
	MustHaveGit(t)

	hostile := filepath.Join(t.TempDir(), "hostile-global-gitconfig")
	body := strings.Join([]string{
		"[user]",
		"\tname = Hostile Global",
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
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1") // 隔离 system 层，聚焦 global 层

	dir := t.TempDir()
	head := GitInit(t, dir) // 仓库级配置必须压过 global 的 gpgsign=true

	got := GitTag(t, dir, "v1.0.0", true) // 若未加固，这里会因 GPG 签名失败而 t.Fatal
	if got != head {
		t.Errorf("annotated tag resolved to %s want commit %s", got, head)
	}

	// 行尾纪律：写入 LF 的文件必须原样入库（autocrlf=true 不得生效）
	GitWriteFile(t, dir, "lf.txt", "a\nb\n")
	blob := GitOutput(t, dir, "hash-object", "lf.txt")
	if strings.Contains(blob, "CRLF") {
		t.Errorf("unexpected CRLF conversion reported: %s", blob)
	}
	commit := GitCommit(t, dir, "chore: lf")
	content := GitOutput(t, dir, "show", commit+":lf.txt")
	if strings.Contains(content, "\r\n") {
		t.Errorf("autocrlf converted LF to CRLF in the blob:\n%q", content)
	}
}

func TestGitWriteFileAndCommit(t *testing.T) {
	MustHaveGit(t)
	dir := t.TempDir()
	GitInit(t, dir)
	GitWriteFile(t, dir, "src/index.ts", "export const x = 1\n")
	head := GitCommit(t, dir, "add index")
	if len(head) < 7 {
		t.Errorf("commit hash too short: %q", head)
	}
}

func TestGitTag(t *testing.T) {
	MustHaveGit(t)
	dir := t.TempDir()
	GitInit(t, dir)
	GitWriteFile(t, dir, "a.txt", "hi\n")
	GitCommit(t, dir, "a")
	h := GitTag(t, dir, "v1.0.0", false)
	if len(h) < 7 {
		t.Errorf("tag rev too short: %q", h)
	}
}
