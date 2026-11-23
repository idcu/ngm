package git

import (
	"context"
	"strings"
	"testing"
)

// TestV210LSRemoteWarningAndPatternBoundary 固定 2026-11-23 审计 P3：
// C-03 的 fail-closed 把 git 的 warning 行也当畸形 ref 行拒绝（可用性回归），
// 而 patterns 未封 `-` 前缀，可能被 git 当 flag 解析。
func TestV210LSRemoteWarningAndPatternBoundary(t *testing.T) {
	out := "warning: falling back to http\n" +
		strings.Repeat("a", 40) + "\trefs/tags/v1\n"
	refs, err := ParseLSRemoteOutput([]byte(out))
	if err != nil {
		t.Fatalf("a git warning line must be skipped: %v", err)
	}
	if len(refs) != 1 || refs[0].Name != "refs/tags/v1" {
		t.Fatalf("unexpected refs: %#v", refs)
	}
	if _, err := ParseLSRemoteOutput([]byte("this is not a ref line\n")); err == nil {
		t.Fatal("a genuinely malformed line must still fail closed")
	}
	if _, err := LSRemote(context.Background(), Options{}, "/tmp/repo", "--upload-pack=evil"); err == nil {
		t.Fatal("a pattern starting with '-' must be rejected before spawning git")
	}
}
