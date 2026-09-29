package supplychain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/lock"
)

const (
	vulnCommit  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cleanCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// auditServer 按请求里的 commit 返回不同结果：vulnCommit 有一条 HIGH，其余为空。
func auditServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		if body["commit"] == vulnCommit {
			_, _ = io.WriteString(w,
				`{"vulns":[{"id":"GHSA-test-0001","summary":"test issue","severity":"HIGH"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func lockFile(commits ...string) *lock.File {
	lf := &lock.File{Version: 1, LockfileVersion: "1.0.0"}
	for i, c := range commits {
		lf.Dependencies = append(lf.Dependencies, lock.Dependency{
			Name:    "github:o/dep" + string(rune('a'+i)),
			Ref:     "v1",
			RefType: "tag",
			Commit:  c,
		})
	}
	return lf
}

func TestAudit_ReportsFindingsAndExitCode(t *testing.T) {
	srv := auditServer(t)
	rep, err := Audit(context.Background(), lockFile(vulnCommit, cleanCommit),
		AuditOptions{OSV: OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL}})
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if rep.Dependencies != 2 {
		t.Errorf("Dependencies = %d, want 2", rep.Dependencies)
	}
	if rep.Vulnerabilities != 1 {
		t.Errorf("Vulnerabilities = %d, want 1", rep.Vulnerabilities)
	}
	if rep.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", rep.ExitCode)
	}
	if rep.BySeverity["HIGH"] != 1 {
		t.Errorf("BySeverity = %v, want HIGH:1", rep.BySeverity)
	}
}

// TestAudit_IgnoredIsStillCounted 固定 osvIgnoreSeverities 的语义：
// 被忽略的不算漏洞，但**条数必须报出来**——否则"忽略 LOW"看起来像"没有 LOW"。
func TestAudit_IgnoredIsStillCounted(t *testing.T) {
	srv := auditServer(t)
	rep, err := Audit(context.Background(), lockFile(vulnCommit),
		AuditOptions{
			OSV:              OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL},
			IgnoreSeverities: []string{"high"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Vulnerabilities != 0 {
		t.Errorf("Vulnerabilities = %d, want 0 (the only finding is ignored)", rep.Vulnerabilities)
	}
	if rep.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", rep.ExitCode)
	}
	if rep.Ignored != 1 {
		t.Errorf("Ignored = %d, want 1 (silently dropping it would understate reality)", rep.Ignored)
	}
}

// TestAudit_FailureIsNotACleanResult 是最重要的两条底线之一：
// 查询失败绝不能被当成"没有漏洞"。
func TestAudit_FailureIsNotACleanResult(t *testing.T) {
	srv := auditServer(t)
	_, err := Audit(context.Background(), lockFile(vulnCommit),
		AuditOptions{OSV: OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL, Offline: true}})
	if err == nil {
		t.Fatal("an offline miss must fail, never report a clean audit")
	}
	if code := errs.ExitCode(err); code != 4 {
		t.Errorf("exit = %d, want 4", code)
	}
}

// TestAudit_IncompleteLockFails 覆盖另两条底线：lock 缺 commit 时不能静默跳过。
func TestAudit_IncompleteLockFails(t *testing.T) {
	lf := lockFile("")
	_, err := Audit(context.Background(), lf, AuditOptions{})
	if err == nil {
		t.Fatal("a lock entry without a commit must fail")
	}
	if code := errs.ExitCode(err); code != 3 {
		t.Errorf("exit = %d, want 3 (lock data is incomplete)", code)
	}
}

// TestAudit_RenderIncludesCoverageAndCleanEntries 固定报告的两条取舍：
// 通过的依赖也要列出（否则看不出覆盖面），且覆盖局限必须渲染出来。
func TestAudit_RenderIncludesCoverageAndCleanEntries(t *testing.T) {
	srv := auditServer(t)
	rep, err := Audit(context.Background(), lockFile(vulnCommit, cleanCommit),
		AuditOptions{OSV: OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	rep.Render(&sb)
	out := sb.String()

	for _, want := range []string{
		"GHSA-test-0001",           // 有问题的一项
		"No known vulnerabilities", // 干净的一项也要出现
		"1 HIGH",                   // 严重度分布
		"zero-day",                 // 覆盖局限
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report should contain %q; got:\n%s", want, out)
		}
	}
}

func TestAudit_JSONRoundTrip(t *testing.T) {
	srv := auditServer(t)
	rep, err := Audit(context.Background(), lockFile(vulnCommit),
		AuditOptions{OSV: OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := rep.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		ExitCode        int    `json:"exitCode"`
		Vulnerabilities int    `json:"vulnerabilities"`
		CoverageNote    string `json:"coverageNote"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("--json must be machine readable: %v", err)
	}
	if back.ExitCode != 1 || back.Vulnerabilities != 1 || back.CoverageNote == "" {
		t.Errorf("unexpected json payload: %+v", back)
	}
}
