package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRun 在 dir 里执行 git 命令。
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=ngm-test", "GIT_AUTHOR_EMAIL=test@local",
		"GIT_COMMITTER_NAME=ngm-test", "GIT_COMMITTER_EMAIL=test@local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// TestLocalGitInstallVerifyE2E 覆盖 local-git 来源（ADR-052）：
// 本地 git 仓 + tag → install（复用 commit 证明链）→ verify 通过。
func TestLocalGitInstallVerifyE2E(t *testing.T) {
	dir := newProject(t)
	repo := filepath.Join(dir, "gitmod")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "index.js"), []byte("module.exports='lg1';\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-qm", "init")
	gitRun(t, repo, "tag", "v1.0.0")

	manifest := `{
  "schemaVersion": 3,
  "name": "github.com:my-org/app",
  "version": "1.0.0",
  "runtime": "node",
  "dependencies": [
    {
      "name": "local:gitmod",
      "ref": "v1.0.0",
      "refType": "local-git",
      "sourcePath": "./gitmod",
      "localRepo": "./gitmod"
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "install", "--dir="+dir)
	if code != 0 {
		t.Fatalf("install local-git failed: exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "local-git") {
		t.Fatalf("install output should mention local-git: %s", out)
	}

	lockData, err := os.ReadFile(filepath.Join(dir, "ngm.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lf struct {
		Dependencies []struct {
			Name       string `json:"name"`
			RefType    string `json:"refType"`
			Commit     string `json:"commit"`
			SourcePath string `json:"sourcePath"`
			LocalRepo  string `json:"localRepo"`
			VendorPath string `json:"vendorPath"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(lockData, &lf); err != nil {
		t.Fatal(err)
	}
	if len(lf.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d: %s", len(lf.Dependencies), lockData)
	}
	d := lf.Dependencies[0]
	if d.RefType != "local-git" || len(d.Commit) != 40 || d.LocalRepo != "./gitmod" {
		t.Fatalf("local-git lock entry wrong: %#v", d)
	}
	if !strings.HasPrefix(d.VendorPath, "local/") {
		t.Fatalf("local-git vendor path must use local/ namespace: %q", d.VendorPath)
	}

	code, out = runCaptureCode(t, "verify", "--dir="+dir)
	if code != 0 {
		t.Fatalf("verify local-git failed: exit=%d\n%s", code, out)
	}
}
