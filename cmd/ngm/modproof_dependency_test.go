package main

import (
	"strings"
	"testing"
)

// TestModproofDependencyIsPinned 防止本地 replace 或被放宽的版本误提交。
// 外部模块是抽离后的公共契约；ngm 应精确依赖已发布的 tag。
func TestModproofDependencyIsPinned(t *testing.T) {
	mod := readDoc(t, "../../go.mod")
	if strings.Contains(mod, "replace gitee.com/idcu-go/modproof") {
		t.Errorf("go.mod must not contain a local replace for modproof:\n%s", mod)
	}
	if !strings.Contains(mod, "gitee.com/idcu-go/modproof v0.2.0") {
		t.Errorf("go.mod must pin gitee.com/idcu-go/modproof v0.2.0:\n%s", mod)
	}
}
