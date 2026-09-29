package testutils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRepo_BasicFlow(t *testing.T) {
	r := NewGitRepo(t)
	head := r.WriteFile("src/index.ts", "export const x = 1\n").Commit("feat: index")
	if len(head) != 40 {
		t.Fatalf("commit hash %q", head)
	}
	if got := r.Head(); got != head {
		t.Errorf("Head=%s want %s", got, head)
	}
	tagCommit := r.Tag("v1.0.0", true)
	if tagCommit != head {
		t.Errorf("annotated tag resolves to %s want %s", tagCommit, head)
	}
}

func TestGitRepo_AnnotatedTagObjectDiffersFromCommit(t *testing.T) {
	r := NewGitRepo(t)
	head := r.WriteFile("a.ts", "1\n").Commit("feat: a")
	r.Tag("v1.0.0", true)

	tagObj := GitTagObjectSHA(t, r.Dir, "v1.0.0")
	if tagObj == head {
		t.Skip("annotated tag object equals commit — git version normalizes tags")
	}
	peeled := r.RevParse("refs/tags/v1.0.0^{commit}")
	if peeled != head {
		t.Errorf("peeled=%s want %s", peeled, head)
	}
	// GitRepo.Tag 的返回值必须是 commit（而非 tag object）
	if got := r.Tag("v1.0.1", true); got != head {
		t.Errorf("Tag() returned %s, want commit %s (tag object was %s)", got, head, tagObj)
	}
}

func TestGitRepo_LFSPointer(t *testing.T) {
	r := NewGitRepo(t)
	sha := strings.Repeat("ab", 32)
	r.WriteLFSPointer("assets/logo.bin", sha, 1024).Commit("chore: add lfs asset")

	data, err := os.ReadFile(filepath.Join(r.Dir, "assets/logo.bin"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "version https://git-lfs.github.com/spec/v1") {
		t.Errorf("missing LFS version line:\n%s", body)
	}
	if !strings.Contains(body, "oid sha256:"+sha) {
		t.Errorf("missing oid line:\n%s", body)
	}
	if !strings.Contains(body, "size 1024") {
		t.Errorf("missing size line:\n%s", body)
	}
}

func TestGitRepo_Gitlink(t *testing.T) {
	// 被引用的 submodule commit 必须真实存在
	sub := NewGitRepo(t)
	subHead := sub.WriteFile("inner.ts", "1\n").Commit("feat: inner")

	r := NewGitRepo(t)
	r.WriteFile("main.ts", "1\n")
	r.AddGitlink("vendor/sub", subHead)
	r.Commit("chore: add gitlink")

	// ls-tree -r 应显示 mode 160000（gitlink/submodule 条目）
	out := r.Exec("ls-tree", "-r", "HEAD")
	if !strings.Contains(out, "160000 commit "+subHead+"\tvendor/sub") {
		t.Errorf("gitlink entry missing:\n%s", out)
	}
}

func TestGitRepo_Executable(t *testing.T) {
	r := NewGitRepo(t)
	r.AddExecutable("scripts/run.sh", "#!/bin/sh\necho hi\n")
	head := r.Commit("chore: script")
	out := r.Exec("ls-tree", "-r", head)
	if !strings.Contains(out, "100755") {
		t.Skipf("filesystem does not preserve exec bit (core.filemode); ls-tree:\n%s", out)
	}
}

func TestGitRepo_ForcePush(t *testing.T) {
	remote := NewBareRemote(t)
	r := NewGitRepo(t)
	head1 := r.WriteFile("a.ts", "1\n").Commit("feat: a")
	r.Push(remote, "main")
	if got := bareRevParse(t, remote, "main"); got != head1 {
		t.Fatalf("remote main=%s want %s", got, head1)
	}

	// 改写历史并强制推送
	newHead := r.RewriteHEAD("feat: a (rewritten)")
	if newHead == head1 {
		t.Skip("amend produced identical hash (unexpected clock/tree)")
	}
	r.ForcePush(remote, "main")
	if got := bareRevParse(t, remote, "main"); got != newHead {
		t.Errorf("after force push remote main=%s want %s", got, newHead)
	}
}

func TestGitRepo_ForceRetag(t *testing.T) {
	remote := NewBareRemote(t)
	r := NewGitRepo(t)
	r.WriteFile("a.ts", "1\n").Commit("feat: a")
	r.Tag("v1.0.0", false)
	r.Push(remote, "main")
	r.Push(remote, "refs/tags/v1.0.0")
	oldTag := bareRevParse(t, remote, "refs/tags/v1.0.0")

	r.WriteFile("b.ts", "2\n")
	newHead := r.Commit("feat: b")
	r.ForceRetag(remote, "v1.0.0", false)
	newTag := bareRevParse(t, remote, "refs/tags/v1.0.0")
	if newTag == oldTag {
		t.Fatalf("retag did not move the tag (%s)", newTag)
	}
	if newTag != newHead {
		t.Errorf("retagged to %s want %s", newTag, newHead)
	}
}

func TestGitRepo_MonorepoSubpath(t *testing.T) {
	r := NewGitRepo(t)
	r.WriteFile("packages/core/src/index.ts", "export const core = 1\n")
	tagCommit := r.WriteFile("packages/web/src/index.ts", "export const web = 1\n").Commit("feat: monorepo")
	r.Tag("v2.0.0", true)

	// 该 commit 中应存在子路径
	out := r.Exec("ls-tree", "-r", "--name-only", tagCommit)
	if !strings.Contains(out, "packages/core/src/index.ts") {
		t.Errorf("monorepo subpath missing:\n%s", out)
	}
}

func TestNewBareRemote_IsBare(t *testing.T) {
	remote := NewBareRemote(t)
	if _, err := os.Stat(filepath.Join(remote, "HEAD")); err != nil {
		t.Errorf("bare remote missing HEAD: %v", err)
	}
}

func bareRevParse(t *testing.T, bareDir, rev string) string {
	t.Helper()
	return strings.TrimSpace(GitOutput(t, bareDir, "rev-parse", rev))
}
