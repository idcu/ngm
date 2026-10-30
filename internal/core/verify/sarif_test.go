package verify

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- v0.105：SARIF / GitHub 两个渲染出口 ---
//
// 它们的共同前提是一条纪律：**渲染不引入新判定**。因此这里的判据全部是
// "从同一份 Report 映射出来是否忠实、是否确定性、是否不编造信息"，
// 而没有一条在测"该不该失败"——那由 Report.ExitCode 的网看着。

func sarifFixture() *Report {
	return &Report{
		Version: ReportVersion,
		Dependencies: []DepResult{
			{
				Name: "github:acme/ok", Ref: "v1.0.0", RefType: "tag",
				Commit: "1111111111111111111111111111111111111111",
				Checks: []CheckResult{{Check: CheckRef, Status: StatusOK}, {Check: CheckDigest, Status: StatusOK}},
			},
			{
				Name: "github:acme/moved", Ref: "v2.0.0", RefType: "tag",
				Commit:         "2222222222222222222222222222222222222222",
				ResolvedCommit: "3333333333333333333333333333333333333333",
				DriftKind:      DriftUnexpected,
				Checks: []CheckResult{{
					Check: CheckRef, Status: StatusFail, Drift: DriftUnexpected,
					Detail: "tag moved from 2222222 to 3333333",
				}},
				Remediation: "run `ngm update github:acme/moved` if this is intended",
			},
			{
				Name: "github:acme/tampered", Ref: "main", RefType: "branch",
				Commit:     "4444444444444444444444444444444444444444",
				VendorPath: "ngm.vendor/github.com/acme/tampered",
				DriftKind:  DriftCritical,
				Checks: []CheckResult{{
					Check: CheckLanding, Status: StatusFail, Drift: DriftCritical,
					Detail: "index.js: size differs (30 vs 20 bytes)\nsecond line",
				}},
			},
			{
				Name: "github:acme/incomplete", Ref: "main", RefType: "branch",
				Commit: "5555555555555555555555555555555555555555",
				Err:    "no local mirror",
				Checks: []CheckResult{{
					Check: CheckRef, Status: StatusFail, Operational: true,
					Detail: "the local mirror is cold",
				}},
			},
		},
		Summary: Summary{Total: 4, OK: 1, Unexpected: 1, Critical: 1, Operational: 1, ExitCode: 2},
	}
}

func decodeSARIF(t *testing.T, data []byte) sarifLog {
	t.Helper()
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("the output must be valid JSON: %v\n%s", err, data)
	}
	return log
}

func TestV105SARIFIsValidAndDeterministic(t *testing.T) {
	rep := sarifFixture()

	a, err := RenderSARIF(rep, "0.105.0")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderSARIF(rep, "0.105.0")
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Error("two renders of the same report must be byte-identical — " +
			"otherwise every CI run shows a diff")
	}

	log := decodeSARIF(t, a)
	if log.Version != "2.1.0" {
		t.Errorf("version = %q", log.Version)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("runs = %d", len(log.Runs))
	}
	if log.Runs[0].Tool.Driver.Name != "ngm" || log.Runs[0].Tool.Driver.Version != "0.105.0" {
		t.Errorf("tool = %+v", log.Runs[0].Tool.Driver)
	}
	// 规则表固定存在：消费者的抑制配置要有稳定的 id 可写。
	if len(log.Runs[0].Tool.Driver.Rules) != 6 {
		t.Errorf("rules = %d, want the fixed set of 6", len(log.Runs[0].Tool.Driver.Rules))
	}
}

func TestV105SARIFMapsFailuresToOneResultEach(t *testing.T) {
	log := decodeSARIF(t, mustRender(t, sarifFixture()))
	results := log.Runs[0].Results

	// 通过的依赖不产 result（3 条失败 → 3 条 result）。
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3 (the passing dependency must not produce one): %+v", len(results), results)
	}
	want := []struct {
		rule, level string
	}{
		{RuleRefUnexpected, "error"},
		{RuleLandingIntegrity, "error"},
		{RuleCheckIncomplete, "warning"}, // 未能完成不是"有问题"，但也不是通过
	}
	for i, w := range want {
		if results[i].RuleID != w.rule || results[i].Level != w.level {
			t.Errorf("result[%d] = %s/%s, want %s/%s",
				i, results[i].RuleID, results[i].Level, w.rule, w.level)
		}
	}
}

// 不许编造位置：没有 region 就没有，写一个 `line: 1` 会让 GitHub 把每条注解
// 钉在文件第一行——那是假信息，而且看起来"很确定"。
func TestV105SARIFEmitsNoInventedRegion(t *testing.T) {
	data := mustRender(t, sarifFixture())
	if strings.Contains(string(data), `"region"`) {
		t.Error("ngm does not know line numbers; emitting a region would pin every annotation to a made-up line")
	}
	if strings.Contains(string(data), `"startLine"`) {
		t.Error("same as above: no startLine may be invented")
	}
	// 指向的必须是**仓库内相对路径**：绝对路径会泄漏构建机目录，且匹配不上。
	if strings.Contains(string(data), `":\\`) || strings.Contains(string(data), `"/"`) &&
		strings.Contains(string(data), `"uri": "/`) {
		t.Errorf("artifact URIs must be repository-relative:\n%s", data)
	}
}

// message 必须是单行：Detail 里带 vendor 树的多行差异时，
// 换行会把一条 result 的 message 在查看器里截断。
func TestV105SARIFMessageIsSingleLine(t *testing.T) {
	log := decodeSARIF(t, mustRender(t, sarifFixture()))
	for _, r := range log.Runs[0].Results {
		if strings.Contains(r.Message.Text, "\n") {
			t.Errorf("message contains a newline: %q", r.Message.Text)
		}
		if r.Message.Text == "" {
			t.Error("a result with an empty message is not actionable")
		}
	}
	// 建议必须被带进来（"有下一步"这条契约跨通道成立，v0.50）。
	if !strings.Contains(log.Runs[0].Results[0].Message.Text, "ngm update") {
		t.Errorf("the remediation must reach the SARIF message: %q", log.Runs[0].Results[0].Message.Text)
	}
}

func TestV105GitHubAnnotationsAreEscaped(t *testing.T) {
	rep := sarifFixture()
	rep.Dependencies = append(rep.Dependencies, DepResult{
		Name:      "github:acme/pct%0Ainject",
		Ref:       "main",
		RefType:   "branch",
		Commit:    "6666666666666666666666666666666666666666",
		DriftKind: DriftCritical,
		Checks: []CheckResult{{
			Check: CheckLanding, Status: StatusFail, Drift: DriftCritical,
			Detail: "malicious\nnewline%25 injection",
		}},
	})

	out := RenderGitHub(rep)
	if out == "" {
		t.Fatal("failing dependencies must produce annotations")
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(line, "::error ") && !strings.HasPrefix(line, "::warning ") &&
			!strings.HasPrefix(line, "::notice ") {
			t.Errorf("every line must be a workflow command, got: %q", line)
		}
	}
	if !strings.Contains(out, "%0A") {
		t.Error("a newline inside a message must be escaped to %0A, " +
			"otherwise the annotation is truncated and the rest leaks as a new command")
	}
	if !strings.Contains(out, "%2525") {
		t.Error("a literal % must become %25 first (so `%25` in the source becomes `%2525`)")
	}
}

func mustRender(t *testing.T, rep *Report) []byte {
	t.Helper()
	data, err := RenderSARIF(rep, "test")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
