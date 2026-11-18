package version

import "testing"

// TestV133SharedParsersKeepTheirDistinctSemantics 钉住"收在一处但不强行同语义"的决策。
//
// 两份解析器曾各写一个正则，行为分叉（v2.4 在 semver 侧不匹配、在宽松侧可比）。
// 收进本包是为了让差异**可见且被测**，不是把 outdated 的严格 tag 规则改成宽松——
// 那会让 tag-date 回退与 semver 两条规则互相抢。
func TestV133SharedParsersKeepTheirDistinctSemantics(t *testing.T) {
	strict := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"v1.2.3", [3]int{1, 2, 3}, true},
		{"2.4.1-rc1", [3]int{2, 4, 1}, true},
		{"2.4.1+meta", [3]int{2, 4, 1}, true},
		{"v2.4", [3]int{}, false}, // 严格：缺 patch 不是 semver tag
		{"deno 2.4.3", [3]int{}, false},
	}
	for _, c := range strict {
		got, ok := SemverParts(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("SemverParts(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}

	loose := []struct {
		in   string
		want string
	}{
		{"deno 2.4.3", "2.4.3"},
		{"v2.4", "2.4"},
		{"not a version", ""},
	}
	for _, c := range loose {
		if got := LooseNumber(c.in); got != c.want {
			t.Errorf("LooseNumber(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := DottedParts("2.4.1"); len(got) != 3 || got[0] != 2 || got[1] != 4 || got[2] != 1 {
		t.Errorf("DottedParts = %v, want [2 4 1]", got)
	}
}
