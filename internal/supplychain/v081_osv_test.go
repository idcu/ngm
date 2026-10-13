package supplychain

import (
	"context"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// v0.81：commit 参与缓存**路径拼接**，形态必须在入口处校验。
//
// 当前调用方传来的都是 lock.Validate 过的 40 位 hex，所以这条不是可利用的漏洞。
// 但它属于本项目反复记录过的那一族：**靠上游不变量成立**的路径拼接——
// 换一个调用方，前提就悄悄没了。
//
// 牙齿：把 QueryOSV 里的 isCommitHex 判据删掉，本测试红。

func TestV81QueryOSVRejectsNonCommit(t *testing.T) {
	bad := []string{
		"",
		"abc",
		"../../../etc/passwd",
		"..\\..\\x",
		"0123456789abcdef0123456789abcdef0123456",   // 39 位
		"0123456789abcdef0123456789abcdef012345678", // 41 位
		"0123456789ABCDEF0123456789abcdef01234567",  // 大写（合法 hex，但非 ngm 产出形态之外仍接受）
	}
	for _, c := range bad {
		if c == "0123456789ABCDEF0123456789abcdef01234567" {
			continue // 大写 hex 也是 commit，故意放行
		}
		_, err := QueryOSV(context.Background(), OSVConfig{Offline: true}, c)
		if err == nil {
			t.Fatalf("QueryOSV(%q) 必须被拒绝（它会参与缓存路径拼接）", c)
		}
		var ne *errs.NgmError
		if e, ok := err.(*errs.NgmError); ok {
			ne = e
		}
		if ne == nil {
			t.Fatalf("QueryOSV(%q) 应返回带码的 ngm 错误，got %T: %v", c, err, err)
		}
		if ne.Code != errs.CodeConfigInvalid {
			t.Errorf("QueryOSV(%q) 码 = %v, want %v", c, ne.Code, errs.CodeConfigInvalid)
		}
	}
}

// TestV81QueryOSVRejectsBeforeTouchingNetwork 校验必须发生在任何缓存/网络动作之前。
//
// 否则"非 commit 值"会先被用来拼一个路径，判据就形同虚设。
func TestV81QueryOSVRejectsBeforeTouchingNetwork(t *testing.T) {
	dir := t.TempDir()
	_, err := QueryOSV(context.Background(), OSVConfig{CacheDir: dir, Offline: true}, "../../escape")
	if err == nil {
		t.Fatal("必须在读缓存之前拒绝")
	}
	if got := err.Error(); !contains(got, "40-char commit") {
		t.Errorf("拒绝时要说清成因，got: %v", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
