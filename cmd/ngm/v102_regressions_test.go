package main

import (
	"encoding/json"
	"os"
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
