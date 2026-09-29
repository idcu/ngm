package supplychain

import "testing"

// TestMatchRepoPattern 固定 ADR-009 定下的通配语义。
//
// 负例比正例更重要：白名单一旦"多放行一层"，用户以为限定了一个组织，
// 实际却放行了所有组织——这类错误不会有人主动报 bug，只会安静地留在配置里。
func TestMatchRepoPattern(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		want    bool
	}{
		// * 不跨层
		{"org glob matches direct repo", "github.com/my-org/*", "github.com/my-org/utils", true},
		{"org glob rejects other org", "github.com/my-org/*", "github.com/other/utils", false},
		{"org glob does NOT match deeper path", "github.com/my-org/*", "github.com/my-org/a/b", false},
		{"host glob does NOT match org/repo", "github.com/*", "github.com/org/repo", false},

		// ** 跨层
		{"double star matches deep path", "gitlab.example.com/**", "gitlab.example.com/a/b/c", true},
		{"double star matches zero segments", "gitlab.example.com/**", "gitlab.example.com", true},
		{"leading double star", "**/utils", "github.com/my-org/utils", true},
		{"middle double star", "github.com/**/utils", "github.com/a/b/utils", true},

		// 段内通配
		{"prefix glob within a segment", "github.com/pre-*/utils", "github.com/pre-fix/utils", true},
		{"prefix glob does not leak to another segment", "github.com/pre-*", "github.com/pre-fix/utils", false},
		{"bare star segment matches empty-ish rest", "github.com/o/*", "github.com/o/", true},

		// 无通配（精确）
		{"exact pattern", "github.com/my-org/utils", "github.com/my-org/utils", true},
		{"exact pattern differs", "github.com/my-org/utils", "github.com/my-org/other", false},

		// 大小写不敏感（Git host 与托管平台的 org/repo 名称本身大小写不敏感）
		{"case-insensitive pattern", "GitHub.COM/My-Org/*", "github.com/my-org/utils", true},
		{"case-insensitive target", "github.com/my-org/*", "GitHub.com/My-Org/Utils", true},

		// 边界
		{"empty pattern", "", "github.com/o/r", false},
		{"star alone does not match multi-segment", "*", "github.com/o/r", false},
		{"star alone matches single segment", "*", "github.com", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchRepoPattern(c.pattern, c.target); got != c.want {
				t.Errorf("MatchRepoPattern(%q, %q) = %v, want %v", c.pattern, c.target, got, c.want)
			}
		})
	}
}

// TestMatchRepoPattern_StarDoesNotCrossSlash 把最容易被误解的一条单独立出来。
//
// 如果哪天有人"优化"成用 filepath.Match 或正则 `.*`，这条会立刻失败——
// 那正是我们需要它失败的时刻。
func TestMatchRepoPattern_StarDoesNotCrossSlash(t *testing.T) {
	for _, target := range []string{
		"github.com/org/repo",
		"github.com/org/repo/sub",
	} {
		if MatchRepoPattern("github.com/*", target) {
			t.Errorf("`*` must not cross '/': pattern github.com/* matched %q", target)
		}
	}
	if !MatchRepoPattern("github.com/**", "github.com/org/repo/sub") {
		t.Errorf("`**` must cross '/': pattern github.com/** did not match a deeper path")
	}
}
