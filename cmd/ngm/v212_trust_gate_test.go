package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/supplychain"
)

// TestV212TrustNoDowngradeGate —— update 写新 lock 前的信任门禁接线判据（ADR-053）。
//
// 覆盖四条分支：commit→branch 红、branch→commit 绿、策略关闭放行、旧条目缺失放行。
func TestV212TrustNoDowngradeGate(t *testing.T) {
	const key = "github:org/lib"
	base, err := supplychain.FromConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	on := base.WithTrustPolicy("no-downgrade")

	graphWith := func(rt resolve.RefType) *resolve.Graph {
		return &resolve.Graph{Nodes: []*resolve.Node{{
			Key: key, Name: key, RefType: rt,
		}}}
	}
	prevWith := func(rt string, sub string) *lock.File {
		dep := lock.Dependency{Name: key, RefType: rt, SubPath: sub}
		return &lock.File{Dependencies: []lock.Dependency{dep}}
	}

	t.Run("commit to branch is rejected", func(t *testing.T) {
		err := checkTrustNoDowngrade(on, prevWith("commit", ""), graphWith(resolve.RefTypeBranch))
		if err == nil {
			t.Fatal("commit→branch 应被拒绝")
		}
		var ne *errs.NgmError
		if !errors.As(err, &ne) {
			t.Fatalf("必须返回 *errs.NgmError; got %T", err)
		}
		if ne.Code.ExitCode() != 3 {
			t.Errorf("exit code = %d, want 3", ne.Code.ExitCode())
		}
		if !strings.Contains(ne.Message, key) || !strings.Contains(ne.Message, "commit") ||
			!strings.Contains(ne.Message, "branch") {
			t.Errorf("message 要说清哪个依赖从什么退到什么: %q", ne.Message)
		}
		if !strings.Contains(ne.Hint, "trustPolicy") {
			t.Errorf("hint 要给出固定 ref 或关闭 trustPolicy: %q", ne.Hint)
		}
	})

	t.Run("branch to commit is allowed", func(t *testing.T) {
		if err := checkTrustNoDowngrade(on, prevWith("branch", ""), graphWith(resolve.RefTypeCommit)); err != nil {
			t.Errorf("branch→commit 是升级，应放行: %v", err)
		}
	})

	t.Run("policy off allows downgrade", func(t *testing.T) {
		if err := checkTrustNoDowngrade(base, prevWith("commit", ""), graphWith(resolve.RefTypeBranch)); err != nil {
			t.Errorf("策略关闭时应放行: %v", err)
		}
		if err := checkTrustNoDowngrade(nil, prevWith("commit", ""), graphWith(resolve.RefTypeBranch)); err != nil {
			t.Errorf("nil 策略时应放行: %v", err)
		}
	})

	t.Run("missing previous entry is allowed", func(t *testing.T) {
		empty := &lock.File{}
		if err := checkTrustNoDowngrade(on, empty, graphWith(resolve.RefTypeBranch)); err != nil {
			t.Errorf("旧 lock 缺失条目应放行: %v", err)
		}
		if err := checkTrustNoDowngrade(on, nil, graphWith(resolve.RefTypeBranch)); err != nil {
			t.Errorf("无旧 lock 应放行: %v", err)
		}
	})

	t.Run("subPath keys are matched independently", func(t *testing.T) {
		old := &lock.File{Dependencies: []lock.Dependency{
			{Name: key, SubPath: "a", RefType: "commit"},
		}}
		g := &resolve.Graph{Nodes: []*resolve.Node{
			{Key: key + "#b", Name: key, SubPath: "b", RefType: resolve.RefTypeBranch},
		}}
		if err := checkTrustNoDowngrade(on, old, g); err != nil {
			t.Errorf("#a 的旧条目不应约束 #b: %v", err)
		}
	})
}
