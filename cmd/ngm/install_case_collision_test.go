package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/resolve"
)

// TestCaseOnlyVendorCollision 固定"仅大小写不同"的路径检测。
//
// 是否真的冲突由目标文件系统决定（vendor.IsCaseInsensitiveDir），这里只锁
// 纯检测：大小写敏感的文件系统仍然允许两个真实目录共存。
func TestCaseOnlyVendorCollision(t *testing.T) {
	items := []digestNode{
		{Repo: resolve.MustNormalize("github.com/org/repo")},
		{Repo: resolve.MustNormalize("github.com/Org/Repo")},
	}
	a, b, ok := caseOnlyVendorCollision(items)
	if !ok {
		t.Fatal("case-only vendor path collision must be detected")
	}
	if !strings.EqualFold(a, b) || a == b {
		t.Fatalf("the pair must differ only by case, got %q and %q", a, b)
	}

	// 同一个 casing 重复出现（理论上不该有）不算冲突：它不是"两个目录"。
	same := []digestNode{
		{Repo: resolve.MustNormalize("github.com/org/repo")},
		{Repo: resolve.MustNormalize("github.com/org/repo")},
	}
	if _, _, ok := caseOnlyVendorCollision(same); ok {
		t.Error("identical paths must not be reported as a collision")
	}
}
