package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.102 的 P0 回归：每条都走**命令 + 退出码 + 关键输出**，而不是只测函数。

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLockFixture(t *testing.T, dir string) {
	t.Helper()
	const hex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lib := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	dep := "cccccccccccccccccccccccccccccccccccccccc"
	body := `{
  "version": 1,
  "lockfileVersion": "2.0.0",
  "dependencies": [
    {
      "name": "github:acme/lib",
      "ref": "v1.0.0",
      "refType": "tag",
      "commit": "` + lib + `",
      "archiveDigest": "sha256:` + hex + `",
      "resolvedAt": "2026-11-03T00:00:00Z",
      "vendorPath": "github.com/acme/lib",
      "requiredBy": ["(root)"]
    },
    {
      "name": "github:acme/dep",
      "ref": "v1.0.0",
      "refType": "tag",
      "commit": "` + dep + `",
      "archiveDigest": "sha256:` + hex + `",
      "resolvedAt": "2026-11-03T00:00:00Z",
      "vendorPath": "github.com/acme/dep",
      "requiredBy": ["github:acme/lib"]
    }
  ]
}`
	writeFile(t, filepath.Join(dir, "ngm.lock"), body)
}

// TestV102PrereleaseTagsDoNotRequireARetrospective 钉住发布链路的一个边界：
// `v0.102.0-dev.1` 是给下游（ngm-repo 的 `.ngm-version`）钉住的预发布点，
// 不是"一个版本交付了"；release 门禁因此不能拿它去要 v0.102 的复盘。
// 例外只放开这一条：附注 tag 规则仍然适用（轻量预发布仍须红）。
func TestV102PrereleaseTagsDoNotRequireARetrospective(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-release-status.sh")
	dir := t.TempDir()
	initSyntheticReleaseRepo(t, dir) // v0.1 复盘 + v0.1.0 附注 tag
	gitIn(t, dir, "tag", "-a", "v0.2.0-dev.1", "-m", "v0.2.0-dev.1")

	run := func() (string, int) {
		cmd := exec.Command(bash, filepath.ToSlash(script))
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("run script: %v", err)
			}
		}
		return string(out), code
	}

	out, code := run()
	if code != 0 {
		t.Fatalf("预发布 tag 不该被判成缺复盘，got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "pre-release") || !strings.Contains(out, "v0.2.0-dev.1") {
		t.Errorf("报告要单独列出预发布 tag（说明为什么不要求复盘）:\n%s", out)
	}

	// 牙齿：删掉附注 tag、换成一个轻量 tag ⇒ 仍须红（豁免只针对规则 2）。
	gitIn(t, dir, "tag", "-d", "v0.2.0-dev.1")
	gitIn(t, dir, "tag", "v0.2.0-dev.1")
	out, code = run()
	if code != 1 {
		t.Fatalf("轻量预发布 tag 必须 exit 1，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "lightweight") {
		t.Errorf("要点名轻量 tag:\n%s", out)
	}
}

// TestV102ShellVariablesBeforeNonASCIIUseBraces 钉住一个只在 bash 3.2 上现形的解析坑：
// `"$entry_main（"` 里的中文括号紧贴变量名，bash 3.2 在 C locale 下会把多字节
// 字节当成名字的一部分 ⇒ `entry_main<garbage>: unbound variable`（`set -u` 直接退出）。
// bash 5 / Windows 全绿，macOS CI 才红——所以这条网在**所有平台**读源码，而不是等 CI。
func TestV102ShellVariablesBeforeNonASCIIUseBraces(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "scripts", "*.sh"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scripts/*.sh (err=%v) —— 这条网会静默地什么都不守", err)
	}
	ident := func(c byte) bool {
		return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			for j := 0; j < len(line); j++ {
				if line[j] != '$' {
					continue
				}
				k := j + 1
				if k >= len(line) {
					continue
				}
				c := line[k]
				// 这些前缀已经有明确边界，或者不是变量。
				if c == '{' || c == '(' || c == '!' || c == '#' {
					continue
				}
				if !(c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) {
					continue
				}
				k++
				for k < len(line) && ident(line[k]) {
					k++
				}
				if k < len(line) && line[k] >= 0x80 {
					name := line[j+1 : k]
					t.Errorf("%s:%d: `$%s` 紧贴非 ASCII 字符；bash 3.2 会把后面的字节当成变量名的一部分（改成 `${%s}`）",
						filepath.Base(f), i+1, name, name)
				}
			}
		}
	}
}

// TestV102SchemaVersionTwoIsReadEverywhere 钉住决策 1：
// 读取器接受 v1/v2；modules validate 与 config validate 使用同一判据。
func TestV102SchemaVersionTwoIsReadEverywhere(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ngm.json"), `{
  "schemaVersion": 2,
  "name": "github.com:demo/v2app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": []
}`)

	if code, out := runCaptureCode(t, "config", "validate", "--dir="+dir); code != 0 {
		t.Fatalf("config validate must accept schemaVersion 2: exit=%d\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+dir); code != 0 {
		t.Fatalf("install must accept schemaVersion 2: exit=%d\n%s", code, out)
	}
	if !strings.Contains(readLockFile(t, dir), `"lockfileVersion": "2.0.0"`) {
		t.Fatalf("install should have written a v2 lock")
	}

	// 集合里的模块清单同样接受 v2，并且 modules validate 复用 ProjectFile.Validate。
	module := filepath.Join(dir, "packages", "mod")
	writeFile(t, filepath.Join(module, "ngm.json"), `{
  "schemaVersion": 2,
  "name": "@demo/mod",
  "version": "0.1.0",
  "runtime": "node",
  "dependencies": []
}`)
	if code, out := runCaptureCode(t, "modules", "validate", "--dir="+dir, "--strict"); code != 0 {
		t.Fatalf("modules validate must accept a v2 module manifest: exit=%d\n%s", code, out)
	}

	// 超出 [1,2] 的版本必须被拒绝，并说出支持区间。
	writeFile(t, filepath.Join(dir, "ngm.json"), `{
  "schemaVersion": 3,
  "name": "github.com:demo/v3app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": []
}`)
	code, out := runCaptureCode(t, "config", "validate", "--dir="+dir)
	if code != 3 || !strings.Contains(out, "supports v1..v2") {
		t.Fatalf("schemaVersion 3 must be rejected with the supported range; exit=%d\n%s", code, out)
	}
}

// TestV102SelectiveVerifyTrimsRequiredBy 钉住决策 2：
// 过滤 lock 时裁剪 requiredBy，不再把"父节点不在子集"报成幽灵边。
func TestV102SelectiveVerifyTrimsRequiredBy(t *testing.T) {
	dir := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:demo/selective", "--dir="+dir); code != 0 {
		t.Fatalf("init: exit=%d\n%s", code, out)
	}
	writeLockFixture(t, dir)

	code, stdout, stderr := runSplit(t, "verify", "github:acme/dep", "--offline", "--json", "--dir="+dir)
	if code != 4 {
		t.Fatalf("a selected transitive dependency without local resources is operational (exit 4), got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "ghost edge") {
		t.Fatalf("trimmed edges must not be reported as ghost edges:\n%s\n%s", stdout, stderr)
	}
	var rep struct {
		Dependencies []struct {
			Name string `json:"name"`
		} `json:"dependencies"`
		Summary struct {
			Operational int `json:"operational"`
			Critical    int `json:"critical"`
			ExitCode    int `json:"exitCode"`
		} `json:"summary"`
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if len(rep.Dependencies) != 1 || rep.Dependencies[0].Name != "github:acme/dep" {
		t.Fatalf("selected set must contain exactly dep, got %+v", rep.Dependencies)
	}
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "requiredBy was trimmed") {
		t.Fatalf("the trimmed edge must be reported in notes, got %v", rep.Notes)
	}
	if rep.Summary.Operational != 1 || rep.Summary.Critical != 0 || rep.Summary.ExitCode != 4 {
		t.Fatalf("summary must be operational=1 critical=0 exit=4, got %+v", rep.Summary)
	}

	_, human := runCaptureCode(t, "verify", "github:acme/dep", "--offline", "--dir="+dir)
	if !strings.Contains(human, "requiredBy was trimmed") {
		t.Fatalf("human output must mention the trimmed edges:\n%s", human)
	}
}

// TestV102IncompleteIsOperationalNotCritical 钉住决策 3：
// 未安装 / 缺 mirror / 缺 store 都是 operational（exit 4），不是 critical（exit 2）。
func TestV102IncompleteIsOperationalNotCritical(t *testing.T) {
	dir := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:demo/operational", "--dir="+dir); code != 0 {
		t.Fatalf("init: exit=%d\n%s", code, out)
	}
	writeLockFixture(t, dir)

	code, stdout, stderr := runSplit(t, "verify", "--offline", "--json", "--dir="+dir)
	if code != 4 {
		t.Fatalf("cold mirror + missing vendor must be exit 4, got %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	var rep struct {
		Summary struct {
			Total       int `json:"total"`
			OK          int `json:"ok"`
			Critical    int `json:"critical"`
			Operational int `json:"operational"`
			ExitCode    int `json:"exitCode"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if rep.Summary.Total != 2 || rep.Summary.Critical != 0 || rep.Summary.Operational != 2 || rep.Summary.ExitCode != 4 {
		t.Fatalf("summary must count 2 operational and 0 critical, got %+v", rep.Summary)
	}
	if strings.Contains(stdout, `"driftKind": "critical"`) {
		t.Fatalf("resource gaps must not be classified as critical:\n%s", stdout)
	}
}

// TestV102CriticalStillWinsWhenBytesDiffer 证明"只有内容确证不符才 critical"：
// 一次真实安装后删掉 vendor 里的文件，verify 必须 exit 2。
func TestV102CriticalStillWinsWhenBytesDiffer(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("lib/index.ts", "export const x = 1\n")
	head := up.Commit("feat: lib")
	seedMirror(t, "github:v102/lib", up.Dir)

	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:demo/critical", "--dir="+proj); code != 0 {
		t.Fatalf("init: exit=%d\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "add", "github:v102/lib@"+head,
		"--ref-type=commit", "--dir="+proj); code != 0 {
		t.Fatalf("add: exit=%d\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: exit=%d\n%s", code, out)
	}

	// 安装后删掉 vendor 树里的一个文件：content store 仍在，vendor 与它不符。
	victim := filepath.Join(proj, "ngm.vendor", "github.com", "v102", "lib", "lib", "index.ts")
	if err := os.Remove(victim); err != nil {
		t.Fatalf("remove vendored file: %v", err)
	}

	code, stdout, stderr := runSplit(t, "verify", "--offline", "--json", "--dir="+proj)
	if code != 2 {
		t.Fatalf("a vendored tree that differs from the store is critical (exit 2), got %d\n%s\n%s", code, stdout, stderr)
	}
	var rep struct {
		Summary struct {
			Critical    int `json:"critical"`
			Operational int `json:"operational"`
			ExitCode    int `json:"exitCode"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if rep.Summary.Critical != 1 || rep.Summary.Operational != 0 || rep.Summary.ExitCode != 2 {
		t.Fatalf("summary must be critical=1 operational=0 exit=2, got %+v", rep.Summary)
	}
}

// TestV102DuplicateRootDeclarationsAreRejected 钉住决策 4：
// config validate 与 install 都必须报出冲突的两条声明，而不是"最后一条赢"。
func TestV102DuplicateRootDeclarationsAreRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ngm.json"), `{
  "schemaVersion": 1,
  "name": "github.com:demo/duplicate",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {"name": "github:acme/lib", "ref": "v1.0.0", "refType": "tag"},
    {"name": "github:acme/lib", "ref": "v2.0.0", "refType": "tag"}
  ]
}`)

	code, out := runCaptureCode(t, "config", "validate", "--dir="+dir)
	if code != 3 {
		t.Fatalf("duplicate roots must fail validation, exit=%d\n%s", code, out)
	}
	for _, want := range []string{"duplicate dependency declaration", "dependencies[0]", "dependencies[1]", "v1.0.0", "v2.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("validation output missing %q:\n%s", want, out)
		}
	}

	code, out = runCaptureCode(t, "install", "--dir="+dir)
	if code != 3 || !strings.Contains(out, "duplicate dependency declaration") {
		t.Fatalf("install must fail on duplicate roots, exit=%d\n%s", code, out)
	}
}

// TestV102ModulesIndexRefusesToMoveTheCurrentBranch 钉住 P2 硬化：
// 目标索引分支等于当前分支时必须拒绝，否则 checkout 会指向一个只含索引的 commit。
func TestV102ModulesIndexRefusesToMoveTheCurrentBranch(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("ngm.json", `{"schemaVersion":1,"name":"demo","version":"0.1.0","runtime":"node"}`)
	repo.WriteFile("packages/a/ngm.json", `{"schemaVersion":2,"name":"a","version":"1.0.0","runtime":"node"}`)
	repo.Commit("init")
	repo.Exec("checkout", "-b", "ngm-index")

	code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--branch=ngm-index")
	if code != 3 || !strings.Contains(out, "refusing to move the current branch") {
		t.Fatalf("moving the current branch must be refused, exit=%d\n%s", code, out)
	}
}

// TestV102TreePrefersTheLockGraph 钉住决策 5：
// lock 有 requiredBy 时 tree 不读 mirror，并在人读/JSON 输出标注来源。
func TestV102TreePrefersTheLockGraph(t *testing.T) {
	dir := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:demo/tree", "--dir="+dir); code != 0 {
		t.Fatalf("init: exit=%d\n%s", code, out)
	}
	writeLockFixture(t, dir)

	code, stdout, stderr := runSplit(t, "tree", "--offline", "--json", "--dir="+dir)
	if code != 0 {
		t.Fatalf("tree must use the lock graph without a mirror, exit=%d\n%s\n%s", code, stdout, stderr)
	}
	var rep struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if rep.Source != "ngm.lock" {
		t.Fatalf("source = %q, want ngm.lock", rep.Source)
	}

	_, human := runCaptureCode(t, "tree", "--offline", "--dir="+dir)
	if !strings.Contains(human, "source: ngm.lock") {
		t.Fatalf("human output must name the source:\n%s", human)
	}
	if strings.Contains(human, "GitFetch") || strings.Contains(human, "no local mirror") {
		t.Fatalf("tree must not touch the mirror when the lock graph is complete:\n%s", human)
	}
}
