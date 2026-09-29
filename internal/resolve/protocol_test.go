package resolve

import "testing"

func TestCanonical_CloneURL(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		protocol Protocol
		want     string
	}{
		{
			name:     "https",
			in:       "github:org/repo",
			protocol: ProtocolHTTPS,
			want:     "https://github.com/org/repo.git",
		},
		{
			name:     "ssh",
			in:       "github:org/repo",
			protocol: ProtocolSSH,
			want:     "git@github.com:org/repo.git",
		},
		{
			name:     "ssh multi-level (gitlab subgroup)",
			in:       "gitlab:group/sub/repo",
			protocol: ProtocolSSH,
			want:     "git@gitlab.com:group/sub/repo.git",
		},
		{
			name:     "https self-hosted",
			in:       "gitlab.example.com:group/repo",
			protocol: ProtocolHTTPS,
			want:     "https://gitlab.example.com/group/repo.git",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := MustNormalize(tc.in)
			if got := c.CloneURL(tc.protocol); got != tc.want {
				t.Errorf("CloneURL(%s)=%q want %q", tc.protocol, got, tc.want)
			}
		})
	}
}

// TestCloneURL_NeverContainsToken 锁定安全纪律：
// CloneURL 永远不包含 userinfo/token（即使输入 URL 里带了）。
func TestCloneURL_NeverContainsToken(t *testing.T) {
	c, err := Normalize("https://x-access-token:ghp_supersecret@github.com/org/repo.git")
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	for _, proto := range []Protocol{ProtocolHTTPS, ProtocolSSH} {
		u := c.CloneURL(proto)
		if containsAny(u, "supersecret", "ghp_", "x-access-token") {
			t.Errorf("CloneURL(%s)=%q leaks credentials", proto, u)
		}
	}
	// canonical 形式也不应包含
	if containsAny(c.String(), "supersecret", "ghp_") {
		t.Errorf("canonical form leaks credentials: %q", c.String())
	}
}

func TestParseProtocol(t *testing.T) {
	cases := []struct {
		in       string
		fallback Protocol
		want     Protocol
	}{
		{"ssh", ProtocolHTTPS, ProtocolSSH},
		{"HTTPS", ProtocolSSH, ProtocolHTTPS},
		{"  ssh  ", ProtocolHTTPS, ProtocolSSH},
		{"", ProtocolHTTPS, ProtocolHTTPS},
		{"", ProtocolSSH, ProtocolSSH},
		{"bogus", ProtocolSSH, ProtocolSSH},
		{"bogus", "", ProtocolSSH}, // fallback 非法 → 回退 ssh
	}
	for _, tc := range cases {
		if got := ParseProtocol(tc.in, tc.fallback); got != tc.want {
			t.Errorf("ParseProtocol(%q,%q)=%q want %q", tc.in, tc.fallback, got, tc.want)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
