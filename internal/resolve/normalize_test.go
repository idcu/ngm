package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// TestNormalize_SpecTable 锁定 architecture/dependency-resolution.md §1 的 4 种协议表格。
//
// 每条用例的 "doc" 字段引用文档条目，便于规格即测试（development/README.md 策略 #4）。
func TestNormalize_SpecTable(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // canonical String()
		slug string // Canonical.Slug()
		doc  string // 文档条目引用
	}{
		{
			name: "https with .git",
			in:   "https://github.com/org/repo.git",
			want: "github.com/org/repo",
			slug: "github:org/repo",
			doc:  "dependency-resolution §1 输入形式 1",
		},
		{
			name: "scp-like git@",
			in:   "git@github.com:org/repo.git",
			want: "github.com/org/repo",
			slug: "github:org/repo",
			doc:  "dependency-resolution §1 输入形式 2",
		},
		{
			name: "git protocol",
			in:   "git://github.com/org/repo.git",
			want: "github.com/org/repo",
			slug: "github:org/repo",
			doc:  "dependency-resolution §1 输入形式 3",
		},
		{
			name: "shorthand github",
			in:   "github:org/repo",
			want: "github.com/org/repo",
			slug: "github:org/repo",
			doc:  "dependency-resolution §1 输入形式 4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in)
			if err != nil {
				t.Fatalf("%s: %v", tc.doc, err)
			}
			if got.String() != tc.want {
				t.Errorf("%s: canonical=%q want %q", tc.doc, got.String(), tc.want)
			}
			if got.Slug() != tc.slug {
				t.Errorf("%s: slug=%q want %q", tc.doc, got.Slug(), tc.slug)
			}
		})
	}
}

func TestNormalize_AdditionalForms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https without .git", "https://github.com/org/repo", "github.com/org/repo"},
		{"http", "http://github.com/org/repo.git", "github.com/org/repo"},
		{"ssh url-like", "ssh://git@github.com/org/repo.git", "github.com/org/repo"},
		{"ssh with port", "ssh://git@github.com:22/org/repo.git", "github.com/org/repo"},
		{"ssh with user:pass", "ssh://git:tok@github.com/org/repo.git", "github.com/org/repo"},
		{"trailing slash", "https://github.com/org/repo/", "github.com/org/repo"},
		{"uppercase host", "https://GitHub.COM/org/repo.git", "github.com/org/repo"},
		{"gitee shorthand", "gitee:org/repo", "gitee.com/org/repo"},
		{"gitlab shorthand", "gitlab:org/repo", "gitlab.com/org/repo"},
		{"gitlab multi-level path", "gitlab:group/subgroup/repo", "gitlab.com/group/subgroup/repo"},
		{"self-hosted gitlab", "gitlab.example.com:group/repo", "gitlab.example.com/group/repo"},
		{"explicit host with colon", "github.com:org/repo", "github.com/org/repo"},
		{"canonical idempotent", "github.com/org/repo", "github.com/org/repo"},
		{"canonical idempotent with .git", "github.com/org/repo.git", "github.com/org/repo"},
		{"backslash separators", `github.com\org\repo`, "github.com/org/repo"},
		{"surrounding spaces", "  github:org/repo  ", "github.com/org/repo"},
		{"deep path", "https://gitlab.com/a/b/c/d/repo.git", "gitlab.com/a/b/c/d/repo"},
		{"dots and dashes", "github:my-org.name/my-repo.js", "github.com/my-org.name/my-repo.js"},
		// 重复斜杠被压缩（宽容处理，与 Git 自身的规范化一致）
		{"collapsed double slash", "github:org//repo", "github.com/org/repo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.in)
			if err != nil {
				t.Fatalf("Normalize(%q): %v", tc.in, err)
			}
			if got.String() != tc.want {
				t.Errorf("Normalize(%q)=%q want %q", tc.in, got.String(), tc.want)
			}
		})
	}
}

// TestNormalize_Idempotent 保证 Normalize(Normalize(x)) == Normalize(x)。
//
// 幂等性是关键纪律：canonical 形式会写进 ngm.json 与 lock，
// 若再次读入会得到不同的结果就破坏了可复现性。
func TestNormalize_Idempotent(t *testing.T) {
	inputs := []string{
		"https://github.com/org/repo.git",
		"git@github.com:org/repo.git",
		"git://github.com/org/repo.git",
		"github:org/repo",
		"gitee:org/repo",
		"gitlab:group/sub/repo",
		"gitlab.example.com:group/repo",
		"github.com:org/repo",
		"github.com/org/repo",
	}
	for _, in := range inputs {
		first, err := Normalize(in)
		if err != nil {
			t.Fatalf("first pass %q: %v", in, err)
		}
		second, err := Normalize(first.String())
		if err != nil {
			t.Fatalf("second pass %q: %v", in, err)
		}
		if !first.Equal(second) {
			t.Errorf("not idempotent for %q: %q vs %q", in, first.String(), second.String())
		}
		// slug 也应幂等（slug 被写入 ngm.json）
		third, err := Normalize(first.Slug())
		if err != nil {
			t.Fatalf("slug pass %q: %v", in, err)
		}
		if !first.Equal(third) {
			t.Errorf("slug not idempotent for %q: %q vs %q", in, first.String(), third.String())
		}
	}
}

func TestNormalize_Errors(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantHint string // 期望错误提示片段
	}{
		{"empty", "", "provide a git address"},
		{"only spaces", "   ", "provide a git address"},
		{"unknown scheme", "ftp://github.com/org/repo", "unsupported scheme"},
		{"no path (url)", "https://github.com", "missing repository path"},
		{"no path (url trailing slash only)", "https://github.com/", "missing repository path"},
		{"single segment", "github:repo", "at least 2 segments"},
		{"empty path after colon", "github:", "repository path is empty"},
		{"shorthand empty host", ":org/repo", "unsupported git address form"},
		{"parent traversal", "github:org/../repo", "must not contain"},
		{"current dir segment", "github:org/./repo", "must not contain"},
		{"unknown short host", "bitbucket:org/repo", "unknown host"},
		{"control char", "github:org/re\x00po", "control characters"},
		{"scp-like without colon", "git@github.com", "scp-like"},
		{"space in path", "github:org/re po", "whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(tc.in)
			if err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
			var ne *errs.NgmError
			if !errors.As(err, &ne) {
				t.Fatalf("error is not *errs.NgmError: %T", err)
			}
			if ne.Code != errs.CodeConfigInvalid {
				t.Errorf("exit code = %d, want %d (CodeConfigInvalid)", ne.Code.ExitCode(), errs.CodeConfigInvalid.ExitCode())
			}
			if !strings.Contains(ne.Hint, tc.wantHint) {
				t.Errorf("hint %q does not contain %q", ne.Hint, tc.wantHint)
			}
		})
	}
}

func TestCanonical_MirrorRelPath(t *testing.T) {
	c := MustNormalize("github:my-org/utils")
	if got := c.MirrorRelPath(); got != "github.com/my-org/utils" {
		t.Errorf("MirrorRelPath=%q", got)
	}
	// 多级路径保持层级
	c2 := MustNormalize("gitlab:group/sub/repo")
	if got := c2.MirrorRelPath(); got != "gitlab.com/group/sub/repo" {
		t.Errorf("MirrorRelPath=%q", got)
	}
	// 一律使用 `/`（跨平台一致，由调用方转 filepath）
	if strings.Contains(c.MirrorRelPath(), "\\") {
		t.Errorf("MirrorRelPath must use forward slashes")
	}
}

func TestCanonical_Equal(t *testing.T) {
	a := MustNormalize("github:org/repo")
	b := MustNormalize("https://github.com/org/repo.git")
	c := MustNormalize("github:org/repo2")
	if !a.Equal(b) {
		t.Errorf("expected equal: %v vs %v", a, b)
	}
	if a.Equal(c) {
		t.Errorf("expected unequal")
	}
	if !(Canonical{}).IsZero() {
		t.Errorf("zero value should report IsZero")
	}
	if a.IsZero() {
		t.Errorf("non-zero value reported IsZero")
	}
}

func TestMustNormalize_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on invalid input")
		}
	}()
	MustNormalize("not a url")
}
