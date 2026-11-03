package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/verify"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.102：内容级 verify 的三条 E2E 与成本读数。
//
// 判据刻意用 **copy** 落地而不是 hardlink：hardlink 下 vendor 与 content
// 共享 inode，篡改会同时改两侧，于是“文件级定位”在数据上不可能；
// copy 模式才让“改 vendor 一个字节”成为一个可定位到文件的事件。
// （hardlink 下聚合 digest 仍能检出，但那是另一条路径，已有判据覆盖。）

func contentProject(t *testing.T) (proj string, dep lock.Dependency) {
	t.Helper()
	// 先有上游，再 add：`add` 会立刻解析并取对象，顺序反了会拿一个不存在的
	// 远端去 fetch（实测错误里那条路径就是这么来的）。
	m5Upstream(t, "github:m5/lib", "export const x = 1;\n")
	proj = m5Project(t, "github:m5/lib v1 tag")
	setVendorConfig(t, proj, `{"mode":"local","linkMode":"copy"}`)
	dep = m5Install(t, proj, "github:m5/lib")
	return proj, dep
}

func runVerifyJSON(t *testing.T, args ...string) (int, verify.Report, string) {
	t.Helper()
	code, out := runCaptureCode(t, append([]string{"verify"}, args...)...)
	var rep verify.Report
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatalf("verify --json is not JSON: %v\n%s", err, out)
		}
	}
	return code, rep, out
}

func TestV102VerifyContentCompleteIsZero(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	proj, _ := contentProject(t)
	code, rep, out := runVerifyJSON(t, "--content", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("complete tree should verify at content level, got exit %d:\n%s", code, out)
	}
	if rep.Level != "content" || !rep.Deep {
		t.Errorf("report should declare level=content/deep=true, got level=%q deep=%v", rep.Level, rep.Deep)
	}
	if len(rep.Dependencies) != 1 || rep.Dependencies[0].Content == nil {
		t.Fatalf("content proof missing from JSON:\n%s", out)
	}
	proof := rep.Dependencies[0].Content
	if proof.ExpectedDigest == "" || proof.ComputedDigest != proof.ExpectedDigest {
		t.Errorf("content digest should replay identically: expected=%q computed=%q",
			proof.ExpectedDigest, proof.ComputedDigest)
	}
	if proof.Files == 0 || len(proof.Mismatches) != 0 {
		t.Errorf("complete tree should have files>0 and no mismatches: %+v", proof)
	}
}

func TestV102VerifyContentTamperedByteIsTwo(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	proj, _ := contentProject(t)
	// 结构级仍应通过：字节长度相同，浅校验只看 size。
	code, _, out := runVerifyJSON(t, "--dir="+proj)
	if code != 0 {
		t.Fatalf("structure-only should not see an equal-length edit, got exit %d:\n%s", code, out)
	}

	path := m5VendorFile(proj, "github.com/m5/lib", "", "index.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), raw...)
	if len(tampered) == 0 {
		t.Fatalf("fixture file is empty")
	}
	tampered[0] = 'E' // 同长度改一个字节
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	code, rep, out := runVerifyJSON(t, "--content", "--json", "--dir="+proj)
	if code != 2 {
		t.Fatalf("tampered vendor file must exit 2 at content level, got %d:\n%s", code, out)
	}
	if rep.Summary.Critical != 1 {
		t.Errorf("summary should count one critical dependency: %+v", rep.Summary)
	}
	proof := rep.Dependencies[0].Content
	if proof == nil || len(proof.Mismatches) == 0 {
		t.Fatalf("mismatch detail missing:\n%s", out)
	}
	m := proof.Mismatches[0]
	if m.Path != "index.ts" || m.Kind != "content" {
		t.Errorf("mismatch should name index.ts as a content difference, got %+v", m)
	}
	if len(m.Expected) != 64 || len(m.Actual) != 64 {
		t.Errorf("expected/actual should be full sha256 in JSON, got %q/%q", m.Expected, m.Actual)
	}
	// 人类输出走另一条渲染路径（--json 不渲染文本）：单独跑一次非 JSON 版本，
	// 钉住"能定位到文件"这件事，以及哈希在终端里被截断。
	hcode, hout := runCaptureCode(t, "verify", "--content", "--dir="+proj)
	if hcode != 2 {
		t.Fatalf("human --content should exit 2, got %d:\n%s", hcode, hout)
	}
	if !strings.Contains(hout, "content mismatch: index.ts (content)") {
		t.Errorf("human output should locate index.ts:\n%s", hout)
	}
	if !strings.Contains(hout, "checked") || strings.Contains(hout, m.Expected) {
		t.Errorf("human output should report coverage and truncate hashes:\n%s", hout)
	}
}

func TestV102VerifyContentMissingBlobIsFour(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	proj, dep := contentProject(t)
	// 删掉 content store 里该文件对应的 blob：这是“对象缺失”，不是篡改。
	blob := m5ContentFile(t, dep.ContentDigest(), "index.ts")
	if err := os.Remove(blob); err != nil {
		t.Fatal(err)
	}
	code, rep, out := runVerifyJSON(t, "--content", "--json", "--dir="+proj)
	if code != 4 {
		t.Fatalf("missing blob must be operational (4), not tamper (2), got %d:\n%s", code, out)
	}
	if rep.Summary.Critical != 0 || rep.Summary.Operational != 1 {
		t.Errorf("summary should be operational=1 critical=0: %+v", rep.Summary)
	}
	if !strings.Contains(out, "content store has no usable entry") {
		t.Errorf("missing-resource detail should say what is missing:\n%s", out)
	}
}

func TestV102VerifyContentSelectiveKeepsTrimNote(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	proj, _ := contentProject(t)
	code, rep, out := runVerifyJSON(t, "github:m5/lib", "--content", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("selective content verify should pass, got %d:\n%s", code, out)
	}
	if len(rep.Dependencies) != 1 || rep.Dependencies[0].Content == nil {
		t.Fatalf("selective content proof missing:\n%s", out)
	}
	// requiredBy 的裁剪语义由 v0.102 既有判据覆盖；这里要钉住的是
	// “选择性 + content”仍会产出完整的内容证明，而不是退回结构级。
	if !rep.Deep || rep.Level != "content" {
		t.Errorf("selective content verify must stay at content level: level=%q deep=%v", rep.Level, rep.Deep)
	}
}

// TestV102VerifyContentCost 量一次内容级的真实成本（依赖数 / 文件数 / 耗时）。
//
// 它不是门禁：只把读数写进测试日志，供文档与报告引用。
func TestV102VerifyContentCost(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)
	const files = 200
	r := testutils.NewGitRepo(t)
	for i := 0; i < files; i++ {
		r.WriteFile(fmt.Sprintf("pkg/file-%03d.ts", i), fmt.Sprintf("export const v%03d = %d;\n", i, i))
	}
	r.Commit("fixture: many files")
	r.Tag("v1", false)
	seedMirror(t, "github:m5/lib", r.Dir)

	proj := m5Project(t, "github:m5/lib v1 tag")
	setVendorConfig(t, proj, `{"mode":"local","linkMode":"copy"}`)
	m5Install(t, proj, "github:m5/lib")

	start := time.Now()
	code, _, out := runVerifyJSON(t, "--dir="+proj)
	structure := time.Since(start)
	if code != 0 {
		t.Fatalf("structure verify failed: %s", out)
	}
	start = time.Now()
	code, _, out = runVerifyJSON(t, "--content", "--dir="+proj)
	content := time.Since(start)
	if code != 0 {
		t.Fatalf("content verify failed: %s", out)
	}
	t.Logf("content-level cost: dependencies=1 files=%d structure=%s content=%s",
		files, structure.Round(time.Millisecond), content.Round(time.Millisecond))
}
