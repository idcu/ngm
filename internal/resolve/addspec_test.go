package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// TestParseAddSpec 覆盖 `ngm add` 位置参数的完整语法：
// `<git-url>[#path=<sub>][@<ref>]`（见 dependency-resolution §1 的 monorepo 示例）。
func TestParseAddSpec(t *testing.T) {
	cases := []struct {
		name string
		in   string
		repo string // canonical
		path string
		ref  string
	}{
		{
			name: "plain shorthand with tag",
			in:   "github:org/repo@v1.2.3",
			repo: "github.com/org/repo",
			ref:  "v1.2.3",
		},
		{
			name: "https url with .git and ref",
			in:   "https://github.com/org/repo.git@main",
			repo: "github.com/org/repo",
			ref:  "main",
		},
		{
			name: "scp-like without ref keeps userinfo",
			in:   "git@github.com:org/repo.git",
			repo: "github.com/org/repo",
		},
		{
			name: "scp-like with ref",
			in:   "git@github.com:org/repo.git@v2.0.0",
			repo: "github.com/org/repo",
			ref:  "v2.0.0",
		},
		{
			name: "monorepo path fragment with ref",
			in:   "github:org/monorepo#path=packages/utils@v1.0.0",
			repo: "github.com/org/monorepo",
			path: "packages/utils",
			ref:  "v1.0.0",
		},
		{
			name: "bare fragment (no `path=`)",
			in:   "github:org/monorepo#packages/core@v1.0.0",
			repo: "github.com/org/monorepo",
			path: "packages/core",
			ref:  "v1.0.0",
		},
		{
			name: "path fragment without ref",
			in:   "github:org/monorepo#path=pkg/core",
			repo: "github.com/org/monorepo",
			path: "pkg/core",
		},
		{
			name: "commit hash as ref",
			in:   "github:org/repo@abc1234",
			repo: "github.com/org/repo",
			ref:  "abc1234",
		},
		{
			name: "no ref at all",
			in:   "github:org/repo",
			repo: "github.com/org/repo",
		},
		{
			name: "git protocol url",
			in:   "git://github.com/org/repo.git@v1",
			repo: "github.com/org/repo",
			ref:  "v1",
		},
		{
			name: "ssh url-like with port and ref",
			in:   "ssh://git@github.com:22/org/repo.git@dev",
			repo: "github.com/org/repo",
			ref:  "dev",
		},
		{
			name: "leading and trailing slashes in path",
			in:   "github:org/mono#path=/pkg/core/@v1",
			repo: "github.com/org/mono",
			path: "pkg/core",
			ref:  "v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAddSpec(tc.in)
			if err != nil {
				t.Fatalf("ParseAddSpec(%q): %v", tc.in, err)
			}
			if got.Repo.String() != tc.repo {
				t.Errorf("repo=%q want %q", got.Repo.String(), tc.repo)
			}
			if got.Path != tc.path {
				t.Errorf("path=%q want %q", got.Path, tc.path)
			}
			if got.Ref != tc.ref {
				t.Errorf("ref=%q want %q", got.Ref, tc.ref)
			}
		})
	}
}

func TestParseAddSpec_Errors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // hint 片段
	}{
		{"empty", "", "usage:"},
		{"empty fragment", "github:org/repo#", "empty fragment"},
		{"path traversal", "github:org/repo#path=../etc", "must not contain"},
		{"single segment repo", "github:repo@v1", "at least 2 segments"},
		{"path with empty segment", "github:org/repo#path=a//b", "empty segment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAddSpec(tc.in)
			if err == nil {
				t.Fatalf("expected error for %q", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				// hint 在 NgmError.Hint 中
				hint := hintOf(err)
				if !strings.Contains(hint, tc.want) && !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error %q / hint %q does not contain %q", err.Error(), hint, tc.want)
				}
			}
		})
	}
}

// TestInferRefType 锁定 guides/configuration.md 的推断规则表。
//
// 推断仅供 Hint 使用；写入 ngm.json 必须显式提供 refType。
func TestInferRefType(t *testing.T) {
	cases := []struct {
		ref  string
		want RefType
	}{
		{"v1.2.3", RefTypeTag},
		{"1.2.3", RefTypeTag},
		{"v1.2.3-rc.1", RefTypeTag},
		{"v1.2.3+build.5", RefTypeTag},
		{"abc1234", RefTypeCommit},
		{"deadbeef", RefTypeCommit},
		{"0123456789abcdef0123456789abcdef01234567", RefTypeCommit},
		{"main", RefTypeBranch},
		{"develop", RefTypeBranch},
		{"v1", RefTypeBranch}, // 不是完整 semver
		{"release/1.x", RefTypeBranch},
		{"abc123", RefTypeBranch}, // 6 位 hex 不足 7 位
	}
	for _, tc := range cases {
		got, ok := InferRefType(tc.ref)
		if !ok {
			t.Errorf("InferRefType(%q) returned ok=false", tc.ref)
			continue
		}
		if got != tc.want {
			t.Errorf("InferRefType(%q)=%s want %s", tc.ref, got, tc.want)
		}
	}
	if _, ok := InferRefType("  "); ok {
		t.Errorf("empty ref should not be inferable")
	}
}

// hintOf 取出 NgmError 的 Hint（非 NgmError 时返回空串）。
func hintOf(err error) string {
	var ne *errs.NgmError
	if errors.As(err, &ne) {
		return ne.Hint
	}
	return ""
}
