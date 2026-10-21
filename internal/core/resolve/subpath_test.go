package resolve

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.82：monorepo 子路径的**唯一**规则（此前两份实现且不一致，见 ValidateSubPath）。

func TestV82SubPathBoundary(t *testing.T) {
	cases := []struct {
		path string
		want bool // true = 合法
		why  string
	}{
		{"", true, "空 = 没有子路径"},
		{"packages/core", true, "普通相对路径"},
		{"a/b/c", true, "多级"},
		{"pkg", true, "单段"},

		{"../escape", false, "越过仓库根"},
		{"a/../../b", false, "中段逃逸"},
		{".", false, "当前目录段"},
		{"..", false, "上一级"},
		{"a/./b", false, "含 `.` 段"},

		{"/abs", false, "POSIX 绝对（前导分隔符 ⇒ 空段）"},
		{`\abs`, false, "反斜杠绝对"},
		{`..\..\x`, false, "反斜杠相对逃逸——addspec 那份此前只按 / 切分，会放过它"},
		{`a\b`, true, "反斜杠是分隔符时等价于 a/b：两个普通段，仍在包内，合法"},

		{"a//b", false, "重复分隔符（空段）"},
		{"a/", false, "尾部分隔符（空段）"},
		{"//a", false, "前导重复"},
	}
	for _, tc := range cases {
		err := ValidateSubPath(tc.path)
		got := err == nil
		if got != tc.want {
			t.Errorf("ValidateSubPath(%q) 合法=%v, want %v（%s）", tc.path, got, tc.want, tc.why)
		}
		if err != nil {
			var ne *errs.NgmError
			if e, ok := err.(*errs.NgmError); ok {
				ne = e
			}
			if ne == nil {
				t.Errorf("ValidateSubPath(%q) 应带 ngm 错误码（建议要靠它传下去），got %T", tc.path, err)
			} else if ne.Hint == "" {
				t.Errorf("ValidateSubPath(%q) 必须给出下一步建议", tc.path)
			}
		}
	}
}

// TestV82SubPathRuleIsOneDefinition 规则必须只有一份：
// config 那份（ngm.json 的 `path`）与 addspec 那份（`#path=`）都转调这里。
//
// 牙齿：在任一处重写一份判定，两份迟早在某个边界上分叉——
// v0.82 现成的证据就是 `\`：config 认两种分隔符，addspec 只认 `/`。
func TestV82SubPathRuleIsOneDefinition(t *testing.T) {
	// 同一条输入，三处入口必须同处置。
	for _, p := range []string{`..\..\x`, "../../x", "a/../b", "/abs"} {
		if (ValidateSubPath(p) == nil) != (validateDepSubPath(p) == nil) {
			t.Errorf("%q: ValidateSubPath 与 validateDepSubPath 处置不一致", p)
		}
	}
}

// TestV82AddSpecRejectsBackslashPath 端到端：`ngm add <url>#path=..\..\x` 必须被拒。
//
// 这是 v0.82 的实质修复：此前 addspec 那份只按 `/` 切分，反斜杠形态会被当成
// 一个普通段名（`..\..\x` 既不是空段、也不是 `.`/`..`）⇒ 放行。
func TestV82AddSpecRejectsBackslashPath(t *testing.T) {
	for _, spec := range []string{
		"github:org/repo#path=..\\..\\x",
		"github:org/repo#path=../x",
		"github:org/repo#path=a/../../b",
	} {
		s, err := ParseAddSpec(spec)
		if err == nil {
			t.Errorf("spec %q 必须被拒绝（got path=%q）", spec, s.Path)
		}
	}
}

// TestV82LeadingSlashIsTrimmedNotRejected 钉住两个入口**已知的不对称**。
//
// `#path=/pkg/core/` → 去首尾斜杠 → `pkg/core`（`addspec_test` 里是合法用例：
// 用户写 `/pkg/core` 通常只是想说"从仓库根起"）。
// `--path=/etc` → 原样交给 config.Validate → **拒绝**（`error_surface_test`
// 里是失败用例：那个 flag 不做去斜杠）。
//
// 两条都是既有决定，不是缺陷——但"同一件事两个入口两种结果"一旦被人
// 顺手统一（无论统一成哪边），都必须是一个**有意识的决定**。这条判据
// 让那种改动变红，而不是悄悄发生。
func TestV82LeadingSlashIsTrimmedNotRejected(t *testing.T) {
	// 片段形式：去斜杠后合法 ⇒ 接受
	s, err := ParseAddSpec("github:org/mono#path=/pkg/core/")
	if err != nil {
		t.Fatalf("#path=/pkg/core/ 应被接受（去首尾斜杠），got: %v", err)
	}
	if s.Path != "pkg/core" {
		t.Errorf("Path = %q, want pkg/core", s.Path)
	}
	// 片段形式的 `/etc` 于是也被解释为 `etc`（仓库内的相对路径）——记录现状
	s2, err := ParseAddSpec("github:org/repo#path=/etc")
	if err != nil {
		t.Fatalf("#path=/etc 现状是被解释为相对路径 etc（与 --path=/etc 不同），got: %v", err)
	}
	if s2.Path != "etc" {
		t.Errorf("Path = %q, want etc", s2.Path)
	}
	// 而 ValidateSubPath 本身对前导分隔符是**拒绝**的（不去斜杠的形态）
	if err := ValidateSubPath("/etc"); err == nil {
		t.Error("ValidateSubPath 必须拒绝前导分隔符：去斜杠是 ParseAddSpec 的选择，不是规则的放宽")
	}
}

// TestV82AddSpecAcceptsNormalPath 正常子路径仍被接受（不误伤）。
func TestV82AddSpecAcceptsNormalPath(t *testing.T) {
	s, err := ParseAddSpec("github:org/repo#path=packages/core")
	if err != nil {
		t.Fatalf("正常子路径不得被拒绝: %v", err)
	}
	if s.Path != "packages/core" {
		t.Errorf("Path = %q", s.Path)
	}
	if !strings.Contains(s.Repo.String(), "github.com") {
		t.Errorf("Repo = %q", s.Repo.String())
	}
}
