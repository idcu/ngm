package supplychain

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// TestV212CheckAnchorNoDowngrade —— 锚点强度阶梯的可单测判据（ADR-053）。
//
// commit / dirDigest（local-dir） > tag > branch；只有"从强退到弱"才报错。
func TestV212CheckAnchorNoDowngrade(t *testing.T) {
	emptyPolicy := &Policy{}
	on := emptyPolicy.WithTrustPolicy("no-downgrade")
	off := &Policy{}

	downgrades := []struct{ old, new string }{
		{"commit", "branch"},
		{"local-dir", "tag"},
		{"tag", "branch"},
	}
	for _, tc := range downgrades {
		err := on.CheckAnchorNoDowngrade(tc.old, tc.new)
		if err == nil {
			t.Errorf("%s→%s 应报降级", tc.old, tc.new)
			continue
		}
		var ne *errs.NgmError
		if !errors.As(err, &ne) {
			t.Fatalf("%s→%s 必须返回 *errs.NgmError; got %T", tc.old, tc.new, err)
		}
		if ne.Code.ExitCode() != 3 {
			t.Errorf("%s→%s exit code = %d, want 3", tc.old, tc.new, ne.Code.ExitCode())
		}
		if !strings.Contains(ne.Message, tc.old) || !strings.Contains(ne.Message, tc.new) {
			t.Errorf("%s→%s 错误正文要说清从什么退到什么: %q", tc.old, tc.new, ne.Message)
		}
		if !strings.Contains(ne.Hint, "trustPolicy") {
			t.Errorf("hint 应给出处置方式: %q", ne.Hint)
		}
	}

	upgradesAndEqual := []struct{ old, new string }{
		{"branch", "commit"},
		{"branch", "tag"},
		{"tag", "commit"},
		{"tag", "local-dir"},
		{"commit", "commit"},
		{"branch", "branch"},
		{"", "branch"},        // 旧条目缺失/未知 ⇒ 放行
		{"local-git", "tag"},  // 类型本身判不出强度 ⇒ 放行
		{"branch", "unknown"}, // 新类型未知 ⇒ 放行
	}
	for _, tc := range upgradesAndEqual {
		if err := on.CheckAnchorNoDowngrade(tc.old, tc.new); err != nil {
			t.Errorf("%s→%s 应放行, got %v", tc.old, tc.new, err)
		}
	}

	// 策略关闭时即使明显降级也放行；nil 策略同理。
	if err := off.CheckAnchorNoDowngrade("commit", "branch"); err != nil {
		t.Errorf("策略关闭时应放行: %v", err)
	}
	if err := (*Policy)(nil).CheckAnchorNoDowngrade("commit", "branch"); err != nil {
		t.Errorf("nil 策略时应放行: %v", err)
	}

	// --allow-fresh 副本不得顺手关掉信任门禁。
	if !on.WithoutMinimumAge().NoDowngrade() {
		t.Error("WithoutMinimumAge 不应影响 trustPolicy")
	}
	if got := on.WithTrustPolicy(" NO-DOWNGRADE ").TrustPolicy(); got != "no-downgrade" {
		t.Errorf("TrustPolicy 归一化 = %q, want no-downgrade", got)
	}
}
