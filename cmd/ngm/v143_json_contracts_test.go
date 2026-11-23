package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件四条判据对应逐行审计 A-01/A-06/A-03 与 spec §3.2（行为级枚举）。
// ADR-049 合规：每一条都指着审计报告里**已发生**的缺陷条目。

// TestV143InstallJSONFailureHasOneEnvelope 固定 A-01：
// install --json 的中途失败必须在 stdout 留恰好一份错误信封，不能混入 writeReport。
func TestV143InstallJSONFailureHasOneEnvelope(t *testing.T) {
	isolateUserEnv(t)
	scUpstream(t, "github:sc/evil", "export const evil = 1\n", "")
	scUpstream(t, "github:sc/mid", "export const mid = 1\n",
		`{"dependencies":[{"name":"github:sc/evil","ref":"v1","refType":"tag"}]}`)

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:sc/mid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	writeSupplyChain(t, proj, `{"allowlistRepos":["github.com/sc/mid"]}`)

	var out, errb bytes.Buffer
	code := dispatch([]string{"install", "--json", "--dir=" + proj}, &out, &errb)
	if code == 0 {
		t.Fatal("the allowlist rejection must not exit 0")
	}
	// 用 Decoder 严格检查"恰好一份 JSON 文档"——两份连写在第二次 Decode 时不是 EOF。
	var env jsonErrorEnvelope
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\n%s", err, out.String())
	}
	if dec.More() {
		t.Errorf("stdout contains more than one JSON document:\n%s", out.String())
	}
	if env.Error.ExitCode != code {
		t.Errorf("envelope exitCode=%d, process=%d", env.Error.ExitCode, code)
	}
	if bytes.Contains(out.Bytes(), []byte(`"command":"install"`)) ||
		bytes.Contains(out.Bytes(), []byte(`"dependencies"`)) {
		t.Errorf("stdout still contains a writeReport next to the error envelope:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "allowlistRepos") {
		t.Errorf("the human channel lost the reason:\n%s", errb.String())
	}
}

// TestV144JSONAwareCommandsFailWithEnvelope 固定 spec §3.2：
// 所有接受 --json 的命令，在参数级失败上都给信封——此前 P1-4 只迁了 5 个命令。
// 这是**行为级枚举**，不是源码扫描网。
func TestV144JSONAwareCommandsFailWithEnvelope(t *testing.T) {
	cases := [][]string{
		{"modules", "--json", "extra"},
		{"modules", "list", "--json", "extra"},
		{"modules", "validate", "--json", "extra"},
		{"modules", "changed", "--json", "extra"},
		{"modules", "plan", "--json", "extra"},
		{"modules", "index", "--json", "extra"},
		{"modules", "--json", "bogus"},
		{"integrations", "--json"},
	}
	checked := 0
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			env, code, _ := runForEnvelope(t, args...)
			checked++
			// runForEnvelope 已保证 code != 0；这里只比对真正生效的那一维，
			// 不留恒假的 code==0 分支（2026-11-23 判据复健）。
			if env.Error.ExitCode != code {
				t.Errorf("process=%d envelope=%d", code, env.Error.ExitCode)
			}
		})
	}
	if checked != len(cases) {
		t.Errorf("checked %d of %d cases", checked, len(cases))
	}
}

// TestV145ModulesIndexDryRunJSONKeepsReportShape 固定 A-06：
// --dry-run 与正常成功路径必须给同一种 modulesIndexReport，而不是裸 collectionIndex。
func TestV145ModulesIndexDryRunJSONKeepsReportShape(t *testing.T) {
	isolateUserEnv(t)
	proj := newProject(t)
	var out, errb bytes.Buffer
	code := dispatch([]string{"modules", "index", "--json", "--dry-run", "--dir=" + proj}, &out, &errb)
	if code != 0 {
		t.Fatalf("modules index --dry-run --json exit=%d:\n%s", code, errb.String())
	}
	var rep struct {
		Version int             `json:"version"`
		DryRun  bool            `json:"dryRun"`
		Out     string          `json:"out"`
		Index   json.RawMessage `json:"index"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &rep); err != nil {
		t.Fatalf("stdout is not a single report document: %v\n%s", err, out.String())
	}
	if rep.Version != 1 || !rep.DryRun || rep.Out == "" || len(rep.Index) == 0 {
		t.Errorf("dry-run report shape = %+v; want version=1 dryRun=true out/index present", rep)
	}
}

// TestV146ConfigValidateDoesNotPrintOKBeforePolicyCheck 固定 A-03：
// 权限策略非法时 stdout 不得出现 "ngm.json OK"。
func TestV146ConfigValidateDoesNotPrintOKBeforePolicyCheck(t *testing.T) {
	home := isolateUserEnv(t)
	ngmHome := filepath.Join(home, "ngm-home")
	if err := os.MkdirAll(ngmHome, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"schemaVersion":3,"permissions":{"allow":["bogus:foo"]}}`
	if err := os.WriteFile(filepath.Join(ngmHome, "config.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := newProject(t)
	code, out := runCaptureCode(t, "config", "validate", "--dir="+proj)
	if code == 0 {
		t.Fatal("an invalid permission namespace must fail")
	}
	if strings.Contains(out, "ngm.json OK") {
		t.Errorf("false positive: OK printed while validation failed:\n%s", out)
	}
	if !strings.Contains(out, "bogus") {
		t.Errorf("the error should name the bad permission namespace:\n%s", out)
	}
}
