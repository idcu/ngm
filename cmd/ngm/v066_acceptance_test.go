package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.66：承诺修复版的端到端断言。
//
//	① install / update 的 --json（评估报告 P3：CI 最需要的两个写命令没有机器可读输出）
//	② installFromLock 的按需取物（对象已在本地时不得 fetch，ADR-010）
//	③ guardNoSecrets 接线（AssertNoSecrets 此前零调用）
//	④ store prune 的 CLI 语义（--older-than 的显式判定与 0 的拒绝）

// runSplit 分开捕获 stdout / stderr——"--json 时 stdout 恰好一份 JSON"
// 这条规矩必须在这个粒度上判（runCaptureCode 会把两者合并）。
func runSplit(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := dispatch(args, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

type writeReportProbe struct {
	Version      int    `json:"version"`
	Command      string `json:"command"`
	Offline      bool   `json:"offline"`
	Count        int    `json:"count"`
	FromStore    int    `json:"fromStore"`
	Dependencies []struct {
		Name          string `json:"name"`
		Ref           string `json:"ref"`
		RefType       string `json:"refType"`
		Commit        string `json:"commit"`
		ArchiveDigest string `json:"archiveDigest"`
		VendorPath    string `json:"vendorPath"`
		Source        string `json:"source"`
	} `json:"dependencies"`
}

// TestV66InstallJSONOnTheLockPath：lock 尊重路径（store 命中）的报告。
func TestV66InstallJSONOnTheLockPath(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p) // 第一次 install 已把 store 填好

	code, out, errOut := runSplit(t, "install", "--dir="+p, "--json")
	if code != 0 {
		t.Fatalf("install --json exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}

	var rep writeReportProbe
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("stdout 不是恰好一份 JSON 文档: %v\n%s", err, out)
	}
	if rep.Version != 1 || rep.Command != "install" || rep.Offline {
		t.Errorf("报告头 = version:%d command:%q offline:%v", rep.Version, rep.Command, rep.Offline)
	}
	if rep.Count != 1 || len(rep.Dependencies) != 1 {
		t.Fatalf("count=%d deps=%d, want 1/1", rep.Count, len(rep.Dependencies))
	}
	d := rep.Dependencies[0]
	if d.Name != "github:x/dep" {
		t.Errorf("name=%q", d.Name)
	}
	if len(d.Commit) != 40 {
		t.Errorf("commit=%q，必须是完整 40 位", d.Commit)
	}
	if !strings.HasPrefix(d.ArchiveDigest, "sha256:") {
		t.Errorf("archiveDigest=%q", d.ArchiveDigest)
	}
	if d.Source != "store" {
		t.Errorf("store 命中路径的 source 应为 store，got %q", d.Source)
	}
	if rep.FromStore != 1 {
		t.Errorf("fromStore=%d, want 1", rep.FromStore)
	}

	// 人类文本改走 stderr（全局约定），stdout 只留下 JSON。
	if !strings.Contains(errOut, "installed") {
		t.Errorf("人类文本应改走 stderr，stderr 里没有安装摘要:\n%s", errOut)
	}
	if strings.Contains(out, "installed") {
		t.Errorf("人类文本混进了 stdout:\n%s", out)
	}
}

// TestV66InstallJSONOnTheFreshPath：fresh 路径（无 lock → 重新解析）的报告。
func TestV66InstallJSONOnTheFreshPath(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	scUpstream(t, "github:x/fresh", "export const f = 1\n", "")
	p := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:x/fresh@v1", "--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add: %s", out)
	}

	code, out, errOut := runSplit(t, "install", "--dir="+p, "--json")
	if code != 0 {
		t.Fatalf("install --json exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}

	var rep writeReportProbe
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("stdout 不是恰好一份 JSON 文档: %v\n%s", err, out)
	}
	if rep.Command != "install" || rep.Count != 1 {
		t.Fatalf("报告头不对: command=%q count=%d", rep.Command, rep.Count)
	}
	if got := rep.Dependencies[0].Source; got != "resolved" {
		t.Errorf("fresh 路径的 source 应为 resolved，got %q", got)
	}
}

// TestV66UpdateJSON：update 的报告与退出码对照。
func TestV66UpdateJSON(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	code, out, errOut := runSplit(t, "update", "--all", "--dir="+p, "--json")
	if code != 0 {
		t.Fatalf("update --json exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	var rep writeReportProbe
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("stdout 不是恰好一份 JSON 文档: %v\n%s", err, out)
	}
	if rep.Command != "update" || rep.Count != 1 {
		t.Fatalf("报告头不对: command=%q count=%d", rep.Command, rep.Count)
	}

	// 对照：不带 --json 时退出码相同（v0.16 第②条规矩）。
	humanCode, _ := runCaptureCode(t, "update", "--all", "--dir="+p)
	if humanCode != code {
		t.Errorf("`--json` 改变了退出码：带 %d / 不带 %d", code, humanCode)
	}
}

// TestV66InstallFromLockFetchesOnlyWhenNeeded 固定按需取物（评估报告 P3）：
// mirror 已存在且已含 lock 的 commit 时，installFromLock **不得 fetch**。
//
// 探针用 FETCH_HEAD 文件（与 v0.03 的 verify 懒取物断言同一技术）：
// 它只在 fetch 之后出现，因此是"发生过 fetch"的直接证据。
func TestV66InstallFromLockFetchesOnlyWhenNeeded(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	// 清空 content store：让 lock 路径**未命中**，从而走到 mirror 那一段。
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(layout.ContentRoot()); err != nil {
		t.Fatal(err)
	}

	mirror := mirrorDirForTest(t, "github:x/dep")
	fh := fetchHeadCleared(t, mirror)

	code, out := runCaptureCode(t, "install", "--dir="+p)
	if code != 0 {
		t.Fatalf("对象已在 mirror 里，install 必须成功（不允许因此触网）: %s", out)
	}
	if _, serr := os.Stat(fh); !os.IsNotExist(serr) {
		t.Errorf("对象已在本地时不得 fetch：FETCH_HEAD 出现了（stat err=%v）", serr)
	}
}

// TestV66GuardNoSecrets：接线后的落盘前凭证检查。
func TestV66GuardNoSecrets(t *testing.T) {
	if err := guardNoSecrets([]byte(`{"ref":"v1"}`), nil, "ngm.lock"); err != nil {
		t.Errorf("没有配置任何 token 时不得拦截: %v", err)
	}
	if err := guardNoSecrets([]byte(`{"ok":true}`), []string{"ghp_abc"}, "ngm.lock"); err != nil {
		t.Errorf("不含 token 的内容应放行: %v", err)
	}
	err := guardNoSecrets([]byte(`{"url":"https://x:ghp_abc@h/r.git"}`), []string{"ghp_abc"}, "ngm.lock")
	if err == nil {
		t.Fatal("含 token 字面量的内容必须被拒绝落盘")
	}
	if strings.Contains(err.Error(), "ghp_abc") {
		t.Errorf("错误消息不得回显 token 本身: %v", err)
	}
}

// TestV66EmptyDirFlagCarriesAnEnvelope：`--dir=` 空值的拒绝发生在**命令行
// 分流层**（早于命令自己的 flag 解析），因此 v0.54 的信封覆盖不到它——
// 实测 `ngm verify --json --dir=` 的 stdout 是 0 字节（v0.66 修）。
func TestV66EmptyDirFlagCarriesAnEnvelope(t *testing.T) {
	isolateUserEnv(t)

	code, out, _ := runSplit(t, "verify", "--json", "--dir=")
	if code != 3 {
		t.Fatalf("空 --dir 必须 exit 3，got %d", code)
	}
	var env jsonErrorEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("用户提了 --json 时失败必须给一份信封：%v\nstdout:\n%s", err, out)
	}
	if env.Error.Code == "" || env.Error.ExitCode != code || env.Error.Message == "" {
		t.Errorf("信封不合格：%+v（进程退 %d）", env.Error, code)
	}

	// 对照：没提 --json ⇒ stdout 仍为空（不许给没要的人塞 JSON）。
	code2, out2, _ := runSplit(t, "verify", "--dir=")
	if code2 != 3 {
		t.Fatalf("对照：空 --dir 也必须 exit 3，got %d", code2)
	}
	if strings.TrimSpace(out2) != "" {
		t.Errorf("用户没提 --json 时 stdout 必须为空，got:\n%s", out2)
	}
}

// TestV66StorePruneCLISemantics：两个 CLI 语义（评估报告 P2#16 与 P3）。
func TestV66StorePruneCLISemantics(t *testing.T) {
	isolateUserEnv(t)

	// ① 显式给了 --older-than（哪怕等于默认值）却不带 --orphans：
	//    用户以为它会做点什么，因此必须拒绝而不是静默忽略。
	//    （修复前比较 Duration 的字符串形态，"24h" 与默认值归一化后相同 ⇒ 静默放行。）
	code, out := runCaptureCode(t, "store", "prune", "--older-than=24h")
	if code != 3 {
		t.Errorf("`--older-than` 不带 `--orphans` 必须 exit 3，got %d:\n%s", code, out)
	}

	// ② --older-than=0 让安全窗失效，必须拒绝。
	code, out = runCaptureCode(t, "store", "prune", "--orphans", "--older-than=0")
	if code != 3 {
		t.Errorf("`--older-than=0` 必须 exit 3（0 会删掉刚写入的 blob），got %d:\n%s", code, out)
	}

	// ③ 默认调用（不给 --older-than）仍然正常。
	code, out = runCaptureCode(t, "store", "prune", "--dry-run")
	if code != 0 {
		t.Errorf("`store prune --dry-run` 应 exit 0，got %d:\n%s", code, out)
	}
}
