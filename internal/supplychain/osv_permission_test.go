package supplychain

import (
	"context"
	"errors"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// TestQueryOSV_AsksBeforeItReachesOut 固定 v0.5 C 组补上的那道门禁的两条性质：
//
//  1. 判定发生在**任何请求之前**（替身服务器的命中数为 0）
//  2. 交给判定的是**实际要访问的那个主机**——默认端点是 `api.osv.dev`
//
// 第 2 条是这道门禁敢放在这一层（而不是调用方）的理由：
// 只有这里知道最终用哪个地址（`BaseURL` 可被 `NGM_OSV_URL` 覆盖，
// 测试与自建镜像都可能指向别的主机）。放在调用方就会出现
// "判定的主机与实际访问的主机不是同一个"。
func TestQueryOSV_AsksBeforeItReachesOut(t *testing.T) {
	var calls int
	var body map[string]string
	srv := osvServer(t, &calls, &body, sampleResp)

	t.Run("a denied host is refused before the request", func(t *testing.T) {
		calls = 0
		var asked string
		denied := errors.New("permission denied: net:127.0.0.1")
		_, err := QueryOSV(context.Background(), OSVConfig{
			CacheDir: t.TempDir(),
			BaseURL:  srv.URL,
			CheckNet: func(host string) error { asked = host; return denied },
		}, sampleCommit)

		if err == nil {
			t.Fatal("a denied host must fail")
		}
		if asked != "127.0.0.1" {
			t.Errorf("asked about %q, want the host it would contact (127.0.0.1)", asked)
		}
		if calls != 0 {
			t.Errorf("the request must not be sent when the host is denied (stand-in saw %d)", calls)
		}
	})

	t.Run("the default endpoint asks for api.osv.dev", func(t *testing.T) {
		calls = 0
		var asked string
		// BaseURL 为空 → 默认端点；判定器直接拒绝，因此不会真的连出去。
		_, err := QueryOSV(context.Background(), OSVConfig{
			CheckNet: func(host string) error { asked = host; return errors.New("stop here") },
		}, sampleCommit)

		if err == nil {
			t.Fatal("expected the check to stop the call")
		}
		if asked != "api.osv.dev" {
			t.Errorf("asked about %q, want api.osv.dev (产品端点的真实主机名)", asked)
		}
		if calls != 0 {
			t.Error("no request may be made")
		}
	})

	t.Run("the policy's own error is passed through unchanged", func(t *testing.T) {
		// 原样透传而不是改写：拒绝的退出码（3）与提示文案都是策略侧定的，
		// 这里包一层就会把它换成另一个码，用户看到的建议也跟着变。
		sentinel := errs.New(errs.CodeConfigInvalid,
			"permission denied: net:127.0.0.1",
			"add \"net:127.0.0.1\" to `permissions.allow` in ~/.ngm/config.json")
		_, err := QueryOSV(context.Background(), OSVConfig{
			CacheDir: t.TempDir(), BaseURL: srv.URL,
			CheckNet: func(string) error { return sentinel },
		}, sampleCommit)

		if err != sentinel {
			t.Errorf("the caller must see the policy's error itself; got %v", err)
		}
		if errs.ExitCode(err) != 3 {
			t.Errorf("exit = %d, want 3", errs.ExitCode(err))
		}
	})

	t.Run("nil CheckNet keeps the previous behaviour", func(t *testing.T) {
		calls = 0
		if _, err := QueryOSV(context.Background(), OSVConfig{
			CacheDir: t.TempDir(), BaseURL: srv.URL,
		}, sampleCommit); err != nil {
			t.Fatalf("without a policy the old behaviour must hold: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1 (no policy means no gate, as documented)", calls)
		}
	})
}
