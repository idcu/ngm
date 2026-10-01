package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV05AuditNeedsNetPermission 是 v0.5 C 组补上的那道门禁的**端到端**断言。
//
// ## 缺口（实测，不是读码推断）
//
// OSV 查询此前直连端点，**不过 `net:` 门禁**：`internal/supplychain` 里
// 一个 `CheckNet` 调用都没有。于是写着 `permissions.allow: ["net:github.com"]`
// 的用户，`ngm audit` 照样会连上**另一个**主机——而 `ngm install` 在同样配置下
// 会以 exit 3 结束。同一个配置文件，两条网络路径，两种行为。
//
// ## 这两条断言为什么缺一不可
//
//	不授权 → exit 3 且**一个请求都没发**（"未发起任何网络请求"是文档对 net 门禁的承诺）
//	授权   → 同一个调用通过（否则"门禁"可能只是"把功能弄坏了"）
//
// 计数用替身服务器自己的命中数，而不是看报错文案：文案对、请求照发，
// 正是门禁类改动最容易留下的形态。
func TestV05AuditNeedsNetPermission(t *testing.T) {
	t.Run("without the permission no request is made", func(t *testing.T) {
		var calls int
		home := isolateUserEnv(t)

		scUpstream(t, "github:v5/net", "export const n = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:v5/net@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)
		t.Setenv("NGM_OSV_URL", srv.URL)
		_ = home // 刻意**不**调用 grantNetFor：这条用例要的就是"没授权"

		code, out := runCaptureCode(t, "audit", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a missing net permission must be exit 3 (a configuration decision); got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "net:127.0.0.1") {
			t.Errorf("the error must name the host that would be contacted:\n%s", out)
		}
		if calls != 0 {
			t.Errorf("no request may be sent when the permission is missing (the stand-in saw %d)", calls)
		}
	})

	t.Run("granting the host lets the same call through", func(t *testing.T) {
		var calls int
		home := isolateUserEnv(t)

		scUpstream(t, "github:v5/netok", "export const n = 2\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:v5/netok@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}))
		t.Cleanup(srv.Close)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)

		code, out := runCaptureCode(t, "audit", "--dir="+proj)
		if code != 0 {
			t.Fatalf("with the permission granted a clean audit must exit 0; got %d:\n%s", code, out)
		}
		if calls == 0 {
			t.Error("the request should have been sent once the host was allowed")
		}
	})
}
