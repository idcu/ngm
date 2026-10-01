package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
)

// grantNetFor 把某个替身服务器的主机加进 `permissions.allow`。
//
// ## 为什么这几条验收突然需要它（v0.5 C 组）
//
// `net:` 的默认档位是"需配置"，而 OSV 查询在 C 组之前**没有过门禁**——
// 那几条 audit 验收因此从来不需要这条权限，也就一直没有人发现缺口。
// 现在它们需要，而且这行授权本身就是"门禁真的生效"的证据：
// 少了它，`ngm audit` 会以 exit 3 结束，而不是静默连上 api.osv.dev。
//
// 主机从 `srv.URL` 推导，不写死 127.0.0.1：万一替身换了地址，
// 授权与端点仍然一致——**判定的主机必须与实际访问的主机是同一个**，
// 这正是把判定放在"最后知道地址的那一层"的理由。
func grantNetFor(t *testing.T, home string, srv *httptest.Server) {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, merr := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": []string{"net:" + u.Hostname()}},
	})
	if merr != nil {
		t.Fatal(merr)
	}
	writeGlobalConfig(t, home, string(body))
}
