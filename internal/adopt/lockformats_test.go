package adopt

import "testing"

// TestYarnNameFromKeyScopedWithRef 钉住 v0.136：`"@scope/pkg#v1.0.0"` 必须
// 得到 `@scope/pkg`——此前 scoped 分支只找第二个 `@`，把 `#v1.0.0` 留在
// 名字里，provenance 查不到 node_modules/@scope/pkg，Git 来源被误降为 unknown。
func TestYarnNameFromKeyScopedWithRef(t *testing.T) {
	cases := map[string]string{
		`"@scope/pkg#v1.0.0"`:       "@scope/pkg",
		"@scope/pkg@^1.0.0":         "@scope/pkg",
		"@scope/pkg":                "@scope/pkg",
		"github.com/foo/bar#v1.0.0": "github.com/foo/bar",
		"@scope/pkg@npm:1.0.0":      "@scope/pkg",
		"left-pad@1.3.0":            "left-pad",
	}
	for key, want := range cases {
		if got := yarnNameFromKey(key); got != want {
			t.Errorf("yarnNameFromKey(%q) = %q, want %q", key, got, want)
		}
	}
}
