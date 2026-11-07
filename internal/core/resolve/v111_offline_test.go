package resolve

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// 判据（S1-3）：`Offline` 下冷 mirror 必须是 4，且**不得**回退去构造远端地址。
//
// 黑盒可观测面是"没有网络调用"：这里给一个不存在的 mirror 目录与一个合法
// 的协议，若实现仍走 CloneURL，ls-remote 会失败并返回 4 —— 那与期望码相同，
// 所以本用例真正钉住的是**错误消息**（"offline and no local mirror"），
// 它只可能来自 offline 分支。
func TestV111OfflineColdMirrorIsFourAndNeverFallsBack(t *testing.T) {
	repo := MustNormalize("github:acme/widget")
	cold := t.TempDir() // 存在，但里面没有 <host>/<org>/<repo>.git

	cases := []struct {
		name string
		opts ResolveOptions
	}{
		{"mirror only", ResolveOptions{MirrorDir: cold, Protocol: ProtocolHTTPS, Offline: true}},
		{"git url that does not exist", ResolveOptions{GitURL: cold + "/nope.git", Offline: true}},
		{"no local source at all", ResolveOptions{Offline: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveRef(context.Background(), repo, "v1.0.0", RefTypeTag, tc.opts)
			if err == nil {
				t.Fatalf("offline with no local mirror must fail")
			}
			if code := errs.ExitCode(err); code != 4 {
				t.Fatalf("offline cold mirror must be exit 4, got %d: %v", code, err)
			}
			if !strings.Contains(err.Error(), "offline and no local mirror") {
				t.Fatalf("error must come from the offline branch (no remote fallback), got: %v", err)
			}
		})
	}
}
