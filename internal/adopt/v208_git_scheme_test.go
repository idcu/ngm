package adopt

import "testing"

// TestV208GitSchemeIsKeptInAdoptResolution 固定 2026-11-23 审计 P3：
// I-04 修过 adopt.go 的 `git://` 前缀，但 lockEntryFromGit 里第二层
// TrimPrefix("git:") 仍把完整 scheme 剥成 `//host/repo.git`，
// 同一协议在两个入口产生两种 provenance URL。
func TestV208GitSchemeIsKeptInAdoptResolution(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	for _, spec := range []string{
		"git://github.com/org/repo.git",
		"git+git://github.com/org/repo.git",
	} {
		entry, ok := lockEntryFromGit(spec, commit, "", "")
		if !ok {
			t.Fatalf("%s: entry should be recognised as a git source", spec)
		}
		want := "git+git://github.com/org/repo.git#" + commit
		if entry.Resolved != want {
			t.Errorf("%s: Resolved=%q, want %q", spec, entry.Resolved, want)
		}
	}
}
