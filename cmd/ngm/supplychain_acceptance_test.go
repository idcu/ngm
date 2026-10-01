package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// keysOf 仅为让失败信息可读：列出 JSON 报告里实际有哪些顶层键。
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// 组 A（v0.2）的端到端验收：供应链策略从 ngm.json 读入，在解析阶段生效。
//
// 单元测试分别覆盖了 resolve 侧的门禁与 supplychain 侧的策略匹配，但**接线**
// （ngm.json → config → policy → GraphOptions.CheckRepo）只有走 CLI 才能证明。
// 本文件补的正是这一段。

// scUpstream 建一个上游 fixture，可同时声明 ngm.json（用于构造传递依赖）。
func scUpstream(t *testing.T, slug, body, manifest string) *testutils.GitRepo {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", body)
	if manifest != "" {
		r.WriteFile("ngm.json", manifest)
	}
	r.Commit("feat: " + slug)
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
	return r
}

// writeSupplyChain 把 supplyChain 段写进项目的 ngm.json（保留其余字段）。
func writeSupplyChain(t *testing.T, proj, sc string) {
	t.Helper()
	path := filepath.Join(proj, "ngm.json")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var section map[string]any
	if err := json.Unmarshal([]byte(sc), &section); err != nil {
		t.Fatal(err)
	}
	doc["supplyChain"] = section

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestV02SupplyChainAcceptance(t *testing.T) {
	t.Run("transitive dependency outside the allowlist blocks install with a provenance chain", func(t *testing.T) {
		isolateUserEnv(t)

		// evil 是不会被允许的仓库；mid 合法，但它引入了 evil
		scUpstream(t, "github:sc/evil", "export const evil = 1\n", "")
		scUpstream(t, "github:sc/mid", "export const mid = 1\n",
			`{"dependencies":[{"name":"github:sc/evil","ref":"v1","refType":"tag"}]}`)

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/mid@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		// 白名单只放行 mid：evil 只能通过传递依赖进入，正是要拦的情况
		writeSupplyChain(t, proj, `{"allowlistRepos":["github.com/sc/mid"]}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a policy rejection must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "allowlistRepos") {
			t.Errorf("the error should name the violated field; got:\n%s", out)
		}
		// 来源链：必须能看出是谁把它引进来的
		const wantChain = "github:sc/mid → github:sc/evil"
		if !strings.Contains(out, wantChain) {
			t.Errorf("the error should carry the provenance chain %q; got:\n%s", wantChain, out)
		}
		// 且拒绝理由指向是**传递依赖**，而不是用户的直接声明
		if !strings.Contains(out, "not in supplyChain.allowlistRepos") {
			t.Errorf("the error should state the actual rule; got:\n%s", out)
		}
	})

	t.Run("an invalid policy value fails with exit 3 and explains the rule", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/ok", "export const ok = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/ok@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		// P1M（一个月）被刻意拒绝：长度不固定，会让同一阈值在不同时刻判出不同结果
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P1M"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an invalid policy must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "minimumReleaseAge") {
			t.Errorf("the error should name the offending field; got:\n%s", out)
		}
		if !strings.Contains(out, "28-31") {
			t.Errorf("the error should explain why months are rejected, not just say invalid; got:\n%s", out)
		}
	})

	t.Run("a dependency younger than the minimum release age is blocked", func(t *testing.T) {
		isolateUserEnv(t)

		// fixture 是刚刚提交的，必然比任何晾晒期都新
		scUpstream(t, "github:sc/fresh", "export const fresh = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/fresh@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P30D"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a too-young dependency must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "younger than") {
			t.Errorf("the error should say why it was blocked; got:\n%s", out)
		}
		// 报错必须给出最早可用的时间，否则用户无从决定"等还是走例外"
		if !strings.Contains(out, "wait until") {
			t.Errorf("the error should state when it becomes usable; got:\n%s", out)
		}
	})

	t.Run("an allowlisted repository bypasses the release age gate", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/urgent", "export const urgent = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/urgent@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		// 同一个"刚刚提交"的依赖，但被显式放行 → 紧急通道生效
		writeSupplyChain(t, proj, `{"minimumReleaseAge":"P30D","allowlistRepos":["github.com/sc/urgent"]}`)

		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("an allowlisted repository must not be blocked by the age gate, got %d:\n%s", code, out)
		}
	})

	t.Run("audit reports a known vulnerability with exit 1", func(t *testing.T) {
		home := isolateUserEnv(t)

		scUpstream(t, "github:sc/vuln", "export const v = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/vuln@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// OSV 替身：对任何 commit 都返回一条 HIGH
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w,
				`{"vulns":[{"id":"GHSA-accept-1","summary":"acceptance finding","severity":"HIGH"}]}`)
		}))
		t.Cleanup(srv.Close)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)

		code, out := runCaptureCode(t, "audit", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a known vulnerability must exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "GHSA-accept-1") {
			t.Errorf("the report should name the advisory; got:\n%s", out)
		}
		// 覆盖局限必须跟着报告一起出现——否则"通过"会被读成"安全"
		if !strings.Contains(out, "zero-day") {
			t.Errorf("the report must carry the coverage note; got:\n%s", out)
		}
	})

	t.Run("audit --json is machine readable", func(t *testing.T) {
		home := isolateUserEnv(t)

		scUpstream(t, "github:sc/clean", "export const c = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/clean@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)

		code, out := runCaptureCode(t, "audit", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a clean audit must exit 0, got %d:\n%s", code, out)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("--json output must be parseable: %v\n%s", err, out)
		}
		if _, ok := parsed["exitCode"]; !ok {
			t.Errorf("the json report should carry exitCode; got keys: %v", keysOf(parsed))
		}
		if _, ok := parsed["coverageNote"]; !ok {
			t.Errorf("the json report should carry coverageNote; got keys: %v", keysOf(parsed))
		}
	})

	t.Run("audit fails instead of reporting clean when the query cannot run", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/offline", "export const o = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/offline@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		// 离线且无缓存：不能报"干净"
		code, out := runCaptureCode(t, "audit", "--offline", "--dir="+proj)
		if code != 4 {
			t.Fatalf("an offline query failure must exit 4, got %d:\n%s", code, out)
		}
	})

	t.Run("a policy that allows everything declared is not in the way", func(t *testing.T) {
		isolateUserEnv(t)

		scUpstream(t, "github:sc/plain", "export const plain = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:sc/plain@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		// 这一条防的是"把没配策略当成全部拒绝"的镜像错误：配了策略也不该误伤
		writeSupplyChain(t, proj, `{"allowedGitHosts":["github.com"],"allowlistRepos":["github.com/sc/*"]}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a satisfied policy must not block install, got %d:\n%s", code, out)
		}
	})
}
