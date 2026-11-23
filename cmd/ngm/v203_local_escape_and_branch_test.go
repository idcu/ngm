package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV203LocalSourceSymlinkEscapeIsRejected 固定 2026-11-23 审计 P2：
// local-dir 来源的逃逸软链不得绕进 vendor。git 来源有 EscapesPackageTree，
// localfs 此前只做词法路径校验、CopyTree 原样重建链接目标。
func TestV203LocalSourceSymlinkEscapeIsRejected(t *testing.T) {
	dir := newProject(t)
	src := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("x=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "outside"), filepath.Join(src, "escape")); err != nil {
		t.Skipf("symlinks are not available in this environment: %v", err)
	}
	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {"name":"local:kernel","ref":"auto","refType":"local-dir","sourcePath":"./kernel"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	code, out := runCaptureCode(t, "install", "--dir="+dir)
	if code != 3 || !strings.Contains(out, "escapes") {
		t.Fatalf("escaping symlink must be rejected with exit 3; got exit=%d\n%s", code, out)
	}
}

// TestV204LocalGitBranchRefResolves 固定 2026-11-23 审计 P2：
// local-git 的 refType 不区分 tag/branch，CLI 却承诺三选一；
// 此前非 hex ref 只按 tag 查，branch 名必然 not found。
func TestV204LocalGitBranchRefResolves(t *testing.T) {
	dir := newProject(t)
	repo := filepath.Join(dir, "gitmod")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q")
	gitRun(t, repo, "checkout", "-qb", "main")
	if err := os.WriteFile(filepath.Join(repo, "index.js"), []byte("module.exports='b';\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-qm", "init")

	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {"name":"local:gitmod","ref":"main","refType":"local-git","sourcePath":"./gitmod","localRepo":"./gitmod"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"install", "--dir=" + dir},
		{"install", "--dir=" + dir},
	} {
		code, out := runCaptureCode(t, args...)
		if code != 0 {
			t.Fatalf("%v: branch ref should resolve; exit=%d\n%s", args, code, out)
		}
	}
}

// TestV206VerifyJSONExposesGateResults 固定 2026-11-23 审计 P2：
// 附加门禁能把 summary.exitCode 抬到 2，但依赖全是 ok —— 报告必须说明是哪一层。
func TestV206VerifyJSONExposesGateResults(t *testing.T) {
	dir := newProject(t)
	if err := os.MkdirAll(filepath.Join(dir, "kernel"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kernel", "index.js"), []byte("x=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {"name":"local:kernel","ref":"auto","refType":"local-dir","sourcePath":"./kernel"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+dir); code != 0 {
		t.Fatalf("install failed: %d\n%s", code, out)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if code := dispatch([]string{"verify", "--json", "--signatures", "--dir=" + dir}, stdout, stderr); code != 0 {
		t.Fatalf("verify failed: %d\n%s%s", code, stdout.String(), stderr.String())
	}
	var rep struct {
		Gates []struct {
			Name     string `json:"name"`
			Enabled  bool   `json:"enabled"`
			Status   string `json:"status"`
			ExitCode int    `json:"exitCode"`
		} `json:"gates"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("verify --json is not valid JSON: %v\n%s", err, stdout.String())
	}
	found := false
	for _, g := range rep.Gates {
		if g.Name == "signatures" && g.Enabled && g.Status == "pass" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verify --json --signatures must expose an enabled signature gate: %s", stdout.String())
	}
}
