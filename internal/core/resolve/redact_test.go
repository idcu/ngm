package resolve

import "testing"

// TestRedactURL 固定"展示原始 remote"的唯一脱敏规则。
//
// origin remote 可能内嵌 `user:token@`；Normalize 的结果不含凭据，但它失败的
// 那一刻手里只剩原始串。这个判据要求：任何保留下来的部分都不能包含凭据，
// 同时仍然是可辨认的地址（host/path 不丢）。
func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://user:tok3n@github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"https://github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"https://user@github.com/org/repo.git", "https://github.com/org/repo.git"},
		{"https://github.com/org/repo.git?access_token=abc", "https://github.com/org/repo.git"},
		{"ftp://user:tok3n@example.invalid/org/repo.git", "ftp://example.invalid/org/repo.git"},
		// scp-like 不含 ://：`git` 是协议约定，不是秘密。
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git"},
		{"github:org/repo", "github:org/repo"},
		{"", ""},
		// 坏地址：url.Parse 失败也要脱敏。
		{"https://user:tok3n@bad host/org/repo.git", "https://bad host/org/repo.git"},
	}
	for _, tc := range cases {
		if got := RedactURL(tc.in); got != tc.want {
			t.Errorf("RedactURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
