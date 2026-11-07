package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// decodeSingleJSONDocument 断言 r 里恰好有一个顶层 JSON 值。
//
// 这是 v0.51 立下的契约（cli.md："--json 时 stdout 恰好一份 JSON 文档"），
// 而"两个对象拼在一起"在 json.Unmarshal 下**不会报错**——它会静默只读第一个，
// 于是判据必须读到 EOF 才算数。
func decodeSingleJSONDocument(t *testing.T, r io.Reader, target any) {
	t.Helper()
	dec := json.NewDecoder(r)
	if err := dec.Decode(target); err != nil {
		t.Fatalf("stdout is not a JSON document: %v", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout carries more than one JSON document (second value: %v)", extra)
	}
}

// 判据（S1-1）：`audit --json --hook` 失败时，stdout 里仍然只能有
// **一份**报告 JSON；hook 的错误只走 stderr。
func TestV110AuditJSONHookIsSingleDocument(t *testing.T) {
	proj := v3AuditProject(t, `{}`)

	var out, errb bytes.Buffer
	code := dispatch([]string{"audit", "--json", "--hook=does-not-exist.js", "--dir=" + proj}, &out, &errb)
	if code != 3 {
		t.Fatalf("a missing hook must exit 3, got %d\nstderr:\n%s", code, errb.String())
	}
	var rep map[string]any
	decodeSingleJSONDocument(t, &out, &rep)
	if _, ok := rep["dependencies"]; !ok {
		t.Fatalf("the single document is not the audit report: %v", rep)
	}
	if !strings.Contains(errb.String(), "audit hook not found") {
		t.Errorf("the hook failure must be explained on stderr:\n%s", errb.String())
	}
}

// 判据（S1-2）：基础层已经判定 2（被篡改）时，附加层缺 Deno 的 5
// **不得覆盖**它；进程码与 `summary.exitCode` 都必须是 2。
func TestV110VerifyCriticalBeatsMissingDeno(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	proj, _ := contentProject(t)

	// 让依赖提供一个自检脚本：这样 --sandbox 才会真的去找 Deno。
	vendorDir := filepath.Dir(m5VendorFile(proj, "github.com/m5/lib", "", "index.ts"))
	if err := os.WriteFile(filepath.Join(vendorDir, "verify.js"), []byte("Deno.exit(0);\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 同长度篡改一个字节：基础层必判 critical（2）。
	path := m5VendorFile(proj, "github.com/m5/lib", "", "index.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatalf("fixture file is empty")
	}
	raw[0] = 'E'
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir()) // 让 FindDeno 失败（exit 5）

	var out, errb bytes.Buffer
	code := dispatch([]string{"verify", "--content", "--sandbox", "--json", "--dir=" + proj}, &out, &errb)
	if code != 2 {
		t.Fatalf("critical must beat missing-deno: exit = %d (want 2)\nstderr:\n%s", code, errb.String())
	}
	var rep struct {
		Summary struct {
			Critical int `json:"critical"`
			ExitCode int `json:"exitCode"`
		} `json:"summary"`
	}
	decodeSingleJSONDocument(t, &out, &rep)
	if rep.Summary.ExitCode != 2 {
		t.Errorf("summary.exitCode = %d, want 2 (the synthetic code must match the process code)", rep.Summary.ExitCode)
	}
	if rep.Summary.Critical != 1 {
		t.Errorf("summary.critical = %d, want 1", rep.Summary.Critical)
	}
	if !strings.Contains(errb.String(), "deno") {
		t.Errorf("the missing sandbox must still be explained on stderr:\n%s", errb.String())
	}
}

// 判据（S1-3 的 JSON 半边）：配置/lock 读不出来时，`gc --json` 也要给一份
// 完整的错误信封，而不是"stdout 空 + 退出码 3"。
func TestV110GCJSONConfigFailureEnvelope(t *testing.T) {
	var out, errb bytes.Buffer
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	code := dispatch([]string{"gc", "--json", "--dir=" + missing}, &out, &errb)
	if code != 3 {
		t.Fatalf("gc --json --dir=<missing> exit = %d, want 3\nstderr:\n%s", code, errb.String())
	}
	var env struct {
		Version int `json:"version"`
		Error   struct {
			Code     string `json:"code"`
			ExitCode int    `json:"exitCode"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	decodeSingleJSONDocument(t, &out, &env)
	if env.Version != 1 || env.Error.ExitCode != 3 || env.Error.Message == "" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}
