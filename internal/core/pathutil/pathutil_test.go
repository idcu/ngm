package pathutil

import "testing"

// TestSubPathNormalizationIsCrossPlatform 钉住 v0.136 的统一口径：
// 反斜杠在**所有平台**都等价于正斜杠，且已校验路径的首尾斜杠被去掉。
// 此前 config 用 filepath.ToSlash —— 只在 Windows 转换，Linux 上把
// `a/b` 与 `a\b` 当成两条依赖，绕过"重复根声明"判重。
func TestSubPathNormalizationIsCrossPlatform(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"packages/core", "packages/core"},
		{`packages\core`, "packages/core"},
		{`/packages\core/`, "packages/core"},
		{`a\b\c`, "a/b/c"},
		{"", ""},
		{"///", ""},
	}
	for _, tc := range cases {
		if got := NormalizeSubPath(tc.in); got != tc.want {
			t.Errorf("NormalizeSubPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// 校验口径：归一分隔符但**保留**空段，让 ValidateSubPath 能拒绝绝对路径
	if got := NormalizeSeparators(`/abs`); got != "/abs" {
		t.Errorf("NormalizeSeparators must not trim: got %q", got)
	}
	if got := NormalizeSeparators(`a\`); got != "a/" {
		t.Errorf("NormalizeSeparators must keep trailing separator: got %q", got)
	}
}
