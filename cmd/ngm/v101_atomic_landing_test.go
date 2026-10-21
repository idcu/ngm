package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// landingDirFor 在隔离 home 的 mirror 根下建一个**没落地的克隆**（v0.101 的形状）。
//
// 它的名字是 ngm 自己会生成的那一种（`<repo>.git.clone-<hex>`），因此它是
// "ngm 只删自己建的东西"里**能删**的那一类——与 `<repo>.git`（通用名字，ADR-030
// 决定只报不删）成对出现。返回目录的绝对路径与相对 mirror 根的斜杠路径。
func landingDirFor(t *testing.T, slug, suffix string, bytes int, age time.Duration) (string, string) {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	repo := resolve.MustNormalize(slug)
	dir := filepath.Join(layout.MirrorRoot(), filepath.FromSlash(repo.MirrorRelPath())) +
		".git" + vendor.CloneLandingInfix + suffix
	if merr := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); merr != nil {
		t.Fatal(merr)
	}
	if werr := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	if werr := os.WriteFile(filepath.Join(dir, "objects", "half-fetched"), make([]byte, bytes), 0o644); werr != nil {
		t.Fatal(werr)
	}
	when := time.Now().Add(-age)
	if cerr := os.Chtimes(dir, when, when); cerr != nil {
		t.Fatal(cerr)
	}
	rel, rerr := filepath.Rel(layout.MirrorRoot(), dir)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return dir, filepath.ToSlash(rel)
}

// TestV101ACloneThatNeverLandedIsSeenAndReclaimed 是 v0.101 的端到端：
//
//	一个**没落地的克隆**从"谁也看不见"变成"看得见、说得清、收得掉"，
//	而且收的时候**只碰我们自己建的东西**。
//
// 它接在 v0.100 那条实测的后面：从前克隆直接落在最终路径上，一次中断就在那里
// 留下一个"长得像能用的 mirror"（本机量到 66.82 MiB）。现在克隆落在一个自己的
// 临时目录里，中断之后留下的就是这个形状。
func TestV101ACloneThatNeverLandedIsSeenAndReclaimed(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// 一个**真的** mirror：它必须活到最后（它是可证明性的本地根基）。
	src := testutils.NewGitRepo(t)
	head := src.WriteFile("a.ts", "export const a = 1\n").Commit("feat: a")
	seedMirror(t, "github:v101/real", src.Dir)

	// 两个没落地的克隆：一个够旧（可回收），一个刚建（不可回收）。
	oldDir, oldRel := landingDirFor(t, "github:v101/never-landed", "aaaa1111", 4096, 2*vendor.MirrorResidueGrace)
	youngDir, youngRel := landingDirFor(t, "github:v101/real", "bbbb2222", 512, 0)

	t.Run("usage sees them and does not count them as repos", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "usage")
		if code != 0 {
			t.Fatalf("store usage exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "1 repo(s)") {
			t.Errorf("a landing dir is NOT a repo — the repo count must not include it:\n%s", out)
		}
		if !strings.Contains(out, "unfinished clone(s): 2") {
			t.Errorf("both unfinished clones must be counted:\n%s", out)
		}
		if !strings.Contains(out, oldRel) || !strings.Contains(out, youngRel) {
			t.Errorf("each unfinished clone must be named:\n%s", out)
		}
		// 这里必须说清"是什么留下的"以及"什么时候会被收掉"，否则用户看到
		// 一个几 MB 的陌生目录只能自己猜。
		if !strings.Contains(out, "renames it into place") || !strings.Contains(out, "after one hour") {
			t.Errorf("the report must explain what these are and when they go:\n%s", out)
		}
	})

	t.Run("prune reclaims the old one and keeps the young one", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "prune")
		if code != 0 {
			t.Fatalf("store prune exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "removed 1 unfinished clone(s)") {
			t.Errorf("exactly one unfinished clone should go:\n%s", out)
		}
		if !strings.Contains(out, oldRel) {
			t.Errorf("the removed directory must be named:\n%s", out)
		}
		if _, serr := os.Stat(oldDir); !os.IsNotExist(serr) {
			t.Errorf("the old landing dir should be gone: %v", serr)
		}
		if _, serr := os.Stat(youngDir); serr != nil {
			t.Errorf("the young landing dir must survive (a clone may be writing it right now): %v", serr)
		}
		// 认领的边界：这一个**不是**我们的名字，它必须一个字节都没动。
		if !strings.Contains(out, "kept 1 mirror(s) untouched") {
			t.Errorf("the real mirror must be reported as untouched:\n%s", out)
		}
	})

	t.Run("gc never claims a directory that is not a repo", func(t *testing.T) {
		code, out := runCaptureCode(t, "store", "gc", "--dry-run")
		if code != 0 {
			t.Fatalf("store gc --dry-run exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "would collect 1 mirror(s)") {
			t.Errorf("only the real mirror is a candidate:\n%s", out)
		}
		if strings.Contains(out, "unfinished clone") {
			t.Errorf("gc must not report landing dirs as something it would collect:\n%s", out)
		}
	})

	t.Run("the real mirror is still provable", func(t *testing.T) {
		layout, err := vendor.DefaultLayout()
		if err != nil {
			t.Fatal(err)
		}
		real := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).
			PathFor(resolve.MustNormalize("github:v101/real"))
		got := strings.TrimSpace(testutils.GitOutput(t, real, "rev-parse", "main"))
		if got != head {
			t.Errorf("after all of this the mirror resolves main=%q want %q", got, head)
		}
	})
}
