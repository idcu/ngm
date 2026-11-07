package supplychain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const osvOKBody = `{"vulns":[{"id":"GHSA-1","summary":"x","database_specific":{"severity":"HIGH"}}]}`

// TestV129OfflineNoCacheDoesNotReadStale 固定 `--offline --no-cache` 的语义。
//
// 既不允许联网、又不允许用缓存时，唯一诚实的结论是失败；此前它会读 stale，
// 于是用 24 小时前的数据报"干净"（P2-5）。
func TestV129OfflineNoCacheDoesNotReadStale(t *testing.T) {
	dir := t.TempDir()
	if err := writeCache(dir, sampleCommit, cacheEnvelope{
		FetchedAt: time.Now().Add(-48 * time.Hour),
		Vulns:     []Vuln{{ID: "GHSA-1", Severity: "HIGH"}},
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	if _, err := QueryOSV(context.Background(), OSVConfig{
		CacheDir: dir, Offline: true, NoCache: true,
	}, sampleCommit); err == nil {
		t.Fatal("offline+no-cache must fail on a cache-only hit")
	}
}

// TestV129CacheWriteFailureIsAWarning 固定"缓存是加速而非结论"。
func TestV129CacheWriteFailureIsAWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, osvOKBody)
	}))
	defer srv.Close()

	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	got, err := QueryOSV(context.Background(), OSVConfig{
		CacheDir: blocker, BaseURL: srv.URL,
		Warn: func(s string) { warnings = append(warnings, s) },
	}, sampleCommit)
	if err != nil {
		t.Fatalf("a cache write failure must not fail the query: %v", err)
	}
	if len(got) != 1 || got[0].ID != "GHSA-1" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not written") {
		t.Fatalf("want one cache warning, got %v", warnings)
	}
}

// TestV129RedirectsAreSameOrigin 固定重定向策略：同源可以，跨源拒绝。
//
// `net:<host>` 判定针对端点主机；跨源跟随等于把请求交给从未授权的主机。
func TestV129RedirectsAreSameOrigin(t *testing.T) {
	var got int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got++
		if r.URL.Path == "/v1/query" {
			http.Redirect(w, r, "/v1/after", http.StatusFound)
			return
		}
		io.WriteString(w, osvOKBody)
	}))
	defer srv.Close()

	if _, err := QueryOSV(context.Background(), OSVConfig{BaseURL: srv.URL + "/v1/query"}, sampleCommit); err != nil {
		t.Fatalf("a same-origin redirect should be followed: %v", err)
	}
	if got != 2 {
		t.Fatalf("requests = %d, want 2 (original + same-origin redirect)", got)
	}

	var otherHit int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHit++
		io.WriteString(w, osvOKBody)
	}))
	defer other.Close()

	var originHit int
	cross := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHit++
		http.Redirect(w, r, other.URL+"/v1/query", http.StatusFound)
	}))
	defer cross.Close()

	if _, err := QueryOSV(context.Background(), OSVConfig{BaseURL: cross.URL}, sampleCommit); err == nil {
		t.Fatal("a cross-origin redirect must be refused")
	}
	if otherHit != 0 {
		t.Fatalf("the second host was contacted %d times despite never being granted", otherHit)
	}
	if originHit != 1 {
		t.Fatalf("origin requests = %d, want 1", originHit)
	}
}

// TestV129OversizedResponseFailsCleanly 固定响应体上限。
func TestV129OversizedResponseFailsCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"vulns":[],"padding":"`+strings.Repeat("x", maxOSVResponseBytes+1024)+`"}`)
	}))
	defer srv.Close()

	if _, err := QueryOSV(context.Background(), OSVConfig{BaseURL: srv.URL}, sampleCommit); err == nil {
		t.Fatal("an oversized response must fail instead of being decoded whole")
	}
}
