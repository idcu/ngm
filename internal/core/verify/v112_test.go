package verify

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
)

// v0.112：ref 解析失败时，**只有"问到了、ref 不存在"才是漂移结论**。
//
// 缺陷形态（实测）：没配 `net:` 权限时
//
//	detail: tag "packages-a-v1.0.0" can no longer be resolved:
//	        ConfigInvalid: permission denied: net:gitee.com
//	driftKind: unexpected   ← exit 1
//
// 于是 CI 的读数是"上游漂了，去改 lock"，而真相是"补一条 net: 权限"。
// 这与 `TagSignature` 把执行失败说成签名结论同族：**把操作失败说成判断**。
//
// 判据钉三格：权限拒绝 → operational；ref 真没了 → unexpected；网络失败 → operational。
func TestV112RefResolveFailureSeparatesVerdictFromIgnorance(t *testing.T) {
	dep := &lock.Dependency{Name: "github:acme/lib", Ref: "v1.0.0", RefType: "tag"}

	cases := []struct {
		name        string
		err         error
		operational bool
		drift       DriftKind
		detailHas   string
	}{
		{
			name: "权限拒绝（我们没问到）",
			err: errs.New(errs.CodeConfigInvalid, "permission denied: net:gitee.com",
				`add "net:gitee.com" to permissions.allow in ~/.ngm/config.json`),
			operational: true,
			drift:       "",
			// 成因必须留在 detail 里：只说"没查成"而不说为什么，等于把人送回去猜。
			detailHas: "permission denied",
		},
		{
			name:        "ref 真的不存在（问到了，答案是没了）",
			err:         &resolve.RefNotFoundError{Err: errs.New(errs.CodeConfigInvalid, "tag \"v1.0.0\" not found in remote", "")},
			operational: false,
			drift:       DriftUnexpected,
			detailHas:   "can no longer be resolved",
		},
		{
			name:        "网络失败",
			err:         errs.New(errs.CodeGitFetch, "could not resolve host", ""),
			operational: true,
		},
		{
			// 未知失败也必须倒向"没查到"。这条是这次改动的**方向性**断言：
			// 老的实现（白名单）会把它算成漂移。
			name:        "未知失败（既不是网络也不是 not-found）",
			err:         errors.New("some new failure mode nobody predicted"),
			operational: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := refResolveFailure(Options{}, dep, tc.err)
			if res.Check != CheckRef || res.Status != StatusFail {
				t.Fatalf("unexpected shape: %+v", res)
			}
			if res.Operational != tc.operational {
				t.Errorf("operational = %v, want %v (detail: %s)", res.Operational, tc.operational, res.Detail)
			}
			if res.Drift != tc.drift {
				t.Errorf("drift = %q, want %q — an operational failure must never be reported "+
					"as a drift conclusion; that claim needs evidence we do not have",
					res.Drift, tc.drift)
			}
			if tc.detailHas != "" && !strings.Contains(res.Detail, tc.detailHas) {
				t.Errorf("detail must contain %q, got: %s", tc.detailHas, res.Detail)
			}
		})
	}
}

// 反向：`IsRefNotFound` 不能把普通的配置错误当成 not-found，
// 否则权限拒绝又会滑回"漂移"那一侧。
func TestV112RefNotFoundMarkerIsNarrow(t *testing.T) {
	plain := errs.New(errs.CodeConfigInvalid, "permission denied: net:gitee.com", "")
	if resolve.IsRefNotFound(plain) {
		t.Fatal("a plain config error must not be classified as ref-not-found")
	}
	wrapped := &resolve.RefNotFoundError{Err: plain}
	if !resolve.IsRefNotFound(wrapped) {
		t.Fatal("the marker must survive its own wrapper")
	}
	// 错误码与建议必须仍能从包装里取到（否则 exit code 会掉成 0/3 的猜）。
	if got := errs.ExitCode(wrapped); got != errs.CodeConfigInvalid.ExitCode() {
		t.Errorf("ExitCode through the wrapper = %d, want %d", got, errs.CodeConfigInvalid.ExitCode())
	}
}
