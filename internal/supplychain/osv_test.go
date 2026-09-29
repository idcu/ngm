package supplychain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

const sampleCommit = "abc123def4567890abcdef1234567890abcdef12"

// osvServer 起一个本地 OSV 替身：记录调用次数与收到的请求体，便于断言契约与缓存行为。
//
// 用 httptest 而不是真网——全项目纪律是"禁止测试依赖公网"。
func osvServer(t *testing.T, calls *int, lastBody *map[string]string, resp string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, lastBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const sampleResp = `{"vulns":[
  {"id":"GHSA-aaaa","summary":"Prototype pollution","severity":"HIGH",
   "affected":[{"ranges":[{"events":[{"fixed":"1.2.4"}]}]}]},
  {"id":"GHSA-bbbb","summary":"ReDoS in parser","severity":"MODERATE"}
]}`

func TestQueryOSV_ContractAndCache(t *testing.T) {
	var calls int
	var body map[string]string
	srv := osvServer(t, &calls, &body, sampleResp)

	cfg := OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL}

	first, err := QueryOSV(context.Background(), cfg, sampleCommit)
	if err != nil {
		t.Fatalf("QueryOSV: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("vulns = %d, want 2", len(first))
	}
	// 契约：必须按 commit 查询
	if body["commit"] != sampleCommit {
		t.Errorf("request body commit = %q, want %q", body["commit"], sampleCommit)
	}
	// MODERATE 归一化为 MEDIUM
	if first[1].Severity != "MEDIUM" {
		t.Errorf("severity = %q, want MEDIUM (OSV uses MODERATE, ngm normalises it)", first[1].Severity)
	}
	// 修复版本来自 affected[].ranges[].events[].fixed
	if first[0].FixedIn != "1.2.4" {
		t.Errorf("first.FixedIn = %q, want 1.2.4", first[0].FixedIn)
	}

	// 第二次查询应命中缓存，不再请求网络
	second, err := QueryOSV(context.Background(), cfg, sampleCommit)
	if err != nil {
		t.Fatalf("QueryOSV (cached): %v", err)
	}
	if len(second) != 2 {
		t.Errorf("cached vulns = %d, want 2", len(second))
	}
	if calls != 1 {
		t.Errorf("network calls = %d, want 1 (the second query must hit the cache)", calls)
	}

	// NoCache 强制刷新
	if _, err := QueryOSV(context.Background(), OSVConfig{CacheDir: cfg.CacheDir, BaseURL: srv.URL, NoCache: true}, sampleCommit); err != nil {
		t.Fatalf("QueryOSV (no-cache): %v", err)
	}
	if calls != 2 {
		t.Errorf("network calls = %d, want 2 after --no-cache", calls)
	}
}

// TestQueryOSV_CacheExpiresAfterTTL 固定 24 小时有效期。
func TestQueryOSV_CacheExpiresAfterTTL(t *testing.T) {
	var calls int
	var body map[string]string
	srv := osvServer(t, &calls, &body, sampleResp)

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cfg := OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL, Now: func() time.Time { return now }}

	if _, err := QueryOSV(context.Background(), cfg, sampleCommit); err != nil {
		t.Fatal(err)
	}
	// 23 小时后仍在有效期内
	later := OSVConfig{CacheDir: cfg.CacheDir, BaseURL: srv.URL, Now: func() time.Time { return now.Add(23 * time.Hour) }}
	if _, err := QueryOSV(context.Background(), later, sampleCommit); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (23h old cache is still fresh)", calls)
	}
	// 25 小时后应重新拉取
	expired := OSVConfig{CacheDir: cfg.CacheDir, BaseURL: srv.URL, Now: func() time.Time { return now.Add(25 * time.Hour) }}
	if _, err := QueryOSV(context.Background(), expired, sampleCommit); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (25h old cache must be refetched)", calls)
	}
}

// TestQueryOSV_Offline 覆盖离线语义：命中缓存即用（哪怕过期），未命中即失败。
func TestQueryOSV_Offline(t *testing.T) {
	var calls int
	var body map[string]string
	srv := osvServer(t, &calls, &body, sampleResp)
	dir := t.TempDir()

	// 先在线填充缓存
	if _, err := QueryOSV(context.Background(), OSVConfig{CacheDir: dir, BaseURL: srv.URL}, sampleCommit); err != nil {
		t.Fatal(err)
	}

	// 离线命中缓存（不触网）
	got, err := QueryOSV(context.Background(), OSVConfig{CacheDir: dir, BaseURL: srv.URL, Offline: true}, sampleCommit)
	if err != nil {
		t.Fatalf("an offline hit must use the cache: %v", err)
	}
	if len(got) != 2 || calls != 1 {
		t.Errorf("got %d vulns, calls=%d; want 2 and 1 (no network in offline mode)", len(got), calls)
	}

	// 离线且无缓存 → exit 4，而不是"当作没有漏洞"
	_, err = QueryOSV(context.Background(), OSVConfig{CacheDir: t.TempDir(), BaseURL: srv.URL, Offline: true}, strings.Repeat("0", 40))
	if err == nil {
		t.Fatal("an offline miss must fail")
	}
	if code := errs.ExitCode(err); code != 4 {
		t.Errorf("exit = %d, want 4 (network unavailable, not a clean audit)", code)
	}
}

func TestIgnoreSeverities(t *testing.T) {
	vulns := []Vuln{
		{ID: "GHSA-low", Severity: "LOW"},
		{ID: "GHSA-med", Severity: "MEDIUM"},
		{ID: "GHSA-high", Severity: "HIGH"},
	}
	kept, ignored := IgnoreSeverities(vulns, []string{"low", "MEDIUM"})
	if len(kept) != 1 || kept[0].ID != "GHSA-high" {
		t.Errorf("kept = %+v, want only the HIGH one", kept)
	}
	if ignored != 2 {
		t.Errorf("ignored = %d, want 2", ignored)
	}
}

// TestCoverageNoteIsNotOptional 固定那句覆盖局限说明的存在。
//
// 它看起来只是文案，但它是"审计通过"与"没有已知记录"之间唯一的界限。
func TestCoverageNoteIsNotOptional(t *testing.T) {
	for _, want := range []string{"zero-day", "coverage is limited"} {
		if !strings.Contains(CoverageNote, want) {
			t.Errorf("CoverageNote must mention %q; got: %s", want, CoverageNote)
		}
	}
}

// TestOSVCacheIsWrittenWhereWeSay 固定缓存位置与文件名（文档写的是 <commit>.json）。
func TestOSVCacheIsWrittenWhereWeSay(t *testing.T) {
	var calls int
	var body map[string]string
	srv := osvServer(t, &calls, &body, sampleResp)
	dir := t.TempDir()

	if _, err := QueryOSV(context.Background(), OSVConfig{CacheDir: dir, BaseURL: srv.URL}, sampleCommit); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sampleCommit+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("expected the cache at %s: %v", p, err)
	}
	if !strings.Contains(string(data), "fetchedAt") {
		t.Errorf("the cache entry should carry its own fetchedAt (not rely on mtime): %s", data)
	}
}
