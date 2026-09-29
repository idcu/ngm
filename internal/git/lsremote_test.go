package git

import "testing"

// lsRemoteSample 模拟 `git ls-remote <repo>` 的真实输出。
//
// 关键点：annotated tag 会多出一行 `<sha>\t<ref>^{}`——这是 M1 最容易出错的点，
// 若不解引用，lock 记录的是 tag object 而不是 commit。
const lsRemoteSample = `1111111111111111111111111111111111111111	HEAD
1111111111111111111111111111111111111111	refs/heads/main
2222222222222222222222222222222222222222	refs/heads/dev
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa	refs/tags/v1.0.0
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb	refs/tags/v1.1.0
cccccccccccccccccccccccccccccccccccccccc	refs/tags/v1.1.0^{}
dddddddddddddddddddddddddddddddddddddddd	refs/tags/legacy
`

func TestParseLSRemoteOutput(t *testing.T) {
	refs := ParseLSRemoteOutput([]byte(lsRemoteSample))
	if len(refs) != 7 {
		t.Fatalf("parsed %d refs, want 7: %+v", len(refs), refs)
	}
	// 找 peeled 行
	var peeledCount int
	for _, r := range refs {
		if r.Peeled {
			peeledCount++
			if r.Name != "refs/tags/v1.1.0" {
				t.Errorf("peeled row name=%q", r.Name)
			}
			if r.SHA != "cccccccccccccccccccccccccccccccccccccccc" {
				t.Errorf("peeled row sha=%q", r.SHA)
			}
		}
	}
	if peeledCount != 1 {
		t.Errorf("peeled rows = %d, want 1", peeledCount)
	}
}

func TestParseLSRemoteOutput_SkipsMalformed(t *testing.T) {
	in := `not-a-ref-line
zzzz	refs/heads/bad
1111111111111111111111111111111111111111	refs/heads/good
shortsha	refs/heads/nope
`
	refs := ParseLSRemoteOutput([]byte(in))
	if len(refs) != 1 {
		t.Fatalf("parsed %d refs, want 1: %+v", len(refs), refs)
	}
	if refs[0].Name != "refs/heads/good" {
		t.Errorf("name=%q", refs[0].Name)
	}
}

func TestParseLSRemoteOutput_CRLF(t *testing.T) {
	in := "1111111111111111111111111111111111111111\trefs/heads/main\r\n"
	refs := ParseLSRemoteOutput([]byte(in))
	if len(refs) != 1 || refs[0].Name != "refs/heads/main" {
		t.Fatalf("CRLF handling failed: %+v", refs)
	}
}

// TestRefIndex_AnnotatedTagPeeling 锁定 M1 的核心纪律：
// annotated tag 必须通过 `^{}` 解引用为 commit。
func TestRefIndex_AnnotatedTagPeeling(t *testing.T) {
	idx := NewRefIndex(ParseLSRemoteOutput([]byte(lsRemoteSample)))

	// annotated tag：peeled 应给出 commit（cccc...），Lookup 给出 tag object（bbbb...）
	peeled, ok := idx.Peeled("refs/tags/v1.1.0")
	if !ok || peeled != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Errorf("Peeled(v1.1.0)=(%q,%v)", peeled, ok)
	}
	raw, ok := idx.Lookup("refs/tags/v1.1.0")
	if !ok || raw.SHA != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("Lookup(v1.1.0)=(%+v,%v)", raw, ok)
	}

	// lightweight tag：无 peeled 行
	if _, ok := idx.Peeled("refs/tags/v1.0.0"); ok {
		t.Errorf("lightweight tag should have no peeled row")
	}
	if r, ok := idx.Lookup("refs/tags/v1.0.0"); !ok || r.SHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("Lookup(v1.0.0)=(%+v,%v)", r, ok)
	}

	// branch
	if r, ok := idx.Lookup("refs/heads/main"); !ok || r.SHA != "1111111111111111111111111111111111111111" {
		t.Errorf("Lookup(main)=(%+v,%v)", r, ok)
	}

	// 不存在的 ref
	if _, ok := idx.Lookup("refs/heads/nonexistent"); ok {
		t.Errorf("unexpected lookup hit")
	}
	if idx.Len() != 6 {
		t.Errorf("Len=%d want 6 (7 rows minus 1 peeled)", idx.Len())
	}
}

func TestIsHex(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"abcdef0123456789", true},
		{"ABCDEF0123456789", true},
		{"abcxyz", false},
		{"", true}, // 空串在 isHex 视角为真；长度检查由调用方负责
	}
	for _, c := range cases {
		if got := isHex(c.in); got != c.want {
			t.Errorf("isHex(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
