package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV201LocalSourcesReachEveryLockConsumer 固定 P1-1~P1-4：
// 首次 install 写进 lock 的 local 条目必须能被后续 install / gc / sbom /
// attest / 按名 verify 再次读取。ae310a8 时这些命令全部 exit 3 unknown host local。
func TestV201LocalSourcesReachEveryLockConsumer(t *testing.T) {
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
    {"name":"local:gitmod","ref":"v1.0.0","refType":"local-git","sourcePath":"./gitmod","localRepo":"./gitmod"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"install-1", []string{"install", "--dir=" + dir}, "local-git"},
		{"install-2", []string{"install", "--dir=" + dir}, "local-git"},
		{"install-frozen", []string{"install", "--dir=" + dir, "--frozen-lockfile"}, "local-git"},
		{"gc-dry", []string{"gc", "--dir=" + dir, "--dry-run"}, ""},
		{"sbom", []string{"sbom", "--dir=" + dir}, "local:gitmod"},
		{"sbom-spdx", []string{"sbom", "--dir=" + dir, "--format=spdx"}, "local:gitmod"},
		{"attest", []string{"attest", "--dir=" + dir}, "pkg:generic/local/gitmod@"},
		{"verify-target", []string{"verify", "local:gitmod", "--dir=" + dir}, "local:gitmod"},
		{"signatures", []string{"verify", "--signatures", "--dir=" + dir}, "N/A (local source)"},
	} {
		code, out := runCaptureCode(t, tc.args...)
		outputs[tc.name] = out
		if code != 0 {
			t.Errorf("%s: exit=%d\n%s", tc.name, code, out)
			continue
		}
		if tc.want != "" && !strings.Contains(out, tc.want) {
			t.Errorf("%s: output missing %q\n%s", tc.name, tc.want, out)
		}
	}
	// attest 的 local 材料不能用 git 身份表达：必须是 local: URI + sha256 digest。
	stmt := outputs["attest"]
	if !strings.Contains(stmt, "local:gitmod@") || !strings.Contains(stmt, `"sha256"`) {
		t.Fatalf("attest must describe a local source with a local URI and sha256 digest:\n%s", stmt)
	}
	if strings.Contains(stmt, "git+@sha256") {
		t.Fatalf("attest fabricated a git URI for a local source:\n%s", stmt)
	}
}

// TestV202LocalReportCountsMatchLock 固定 P1-7 / P1-5：
// local-dir 的 --json 报告必须：stdout 恰好一份 JSON、count 与依赖数一致。
// 此前 local 分支不写 rep.Dependencies；update 的人读进度行还直写原始 stdout。
func TestV202LocalReportCountsMatchLock(t *testing.T) {
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
	for _, args := range [][]string{
		{"install", "--dir=" + dir, "--json"},
		{"install", "--dir=" + dir, "--json"},
		{"update", "--all", "--dir=" + dir, "--json"},
	} {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		code := dispatch(args, stdout, stderr)
		if code != 0 {
			t.Fatalf("%v: exit=%d\nstdout=%s\nstderr=%s", args, code, stdout.String(), stderr.String())
		}
		var rep struct {
			Count        int `json:"count"`
			Dependencies []struct {
				Name string `json:"name"`
			} `json:"dependencies"`
		}
		dec := json.NewDecoder(stdout)
		if err := dec.Decode(&rep); err != nil {
			t.Fatalf("%v: stdout is not JSON: %v\n%s", args, err, stdout.String())
		}
		var extra any
		if err := dec.Decode(&extra); err == nil {
			t.Fatalf("%v: stdout has more than one JSON document\n%s", args, stdout.String())
		}
		if rep.Count != 1 || len(rep.Dependencies) != 1 || rep.Dependencies[0].Name != "local:kernel" {
			t.Errorf("%v: report count=%d deps=%#v; want one local:kernel", args, rep.Count, rep.Dependencies)
		}
	}
}
