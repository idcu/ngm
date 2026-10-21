package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// writeMirrorConfig 写一个裸仓库的 config（不跑 git，保留我们想要的形状）。
func writeMirrorConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestIsMirrorDistinguishesAWreckFromAnEmptyUpstream 是 v0.101 那个判据的**两个方向**，
// 而这两个方向恰好是两种不同的错法（因此它同时也是牙齿）：
//
//	把 IsMirror 实现成 IsBareMirror（只看形状） ⇒ "废墟"那一格红；
//	把 IsMirror 实现成"有没有 ref"             ⇒ "空上游"那一格红。
//
// 依据是实测（2026-10-15）：`git clone --mirror` 是**成功之后**才把镜像 refspec
// 写进 config 的，而**从空上游成功克隆**出来的 mirror 一个 ref 都没有。
func TestIsMirrorDistinguishesAWreckFromAnEmptyUpstream(t *testing.T) {
	testutils.MustHaveGit(t)
	ctx := context.Background()

	t.Run("a mirror ngm cloned itself", func(t *testing.T) {
		src := testutils.NewGitRepo(t)
		src.WriteFile("a.txt", "a\n").Commit("feat: a")
		dst := filepath.Join(t.TempDir(), "m.git")
		if err := CloneMirror(ctx, Options{}, src.Dir, dst); err != nil {
			t.Fatalf("CloneMirror: %v", err)
		}
		if !IsMirror(dst) {
			t.Error("a clone ngm just made must be a mirror")
		}
	})

	t.Run("an empty upstream: no refs, and still a mirror", func(t *testing.T) {
		upstream := filepath.Join(t.TempDir(), "empty.git")
		testutils.GitOutput(t, filepath.Dir(upstream), "init", "--bare", upstream)
		dst := filepath.Join(t.TempDir(), "m.git")
		if err := CloneMirror(ctx, Options{}, upstream, dst); err != nil {
			t.Fatalf("CloneMirror of an empty upstream: %v", err)
		}
		if !IsMirror(dst) {
			t.Error("cloning an empty upstream succeeds and yields a good mirror — " +
				"calling it wrecked would make ngm refuse a working dependency")
		}
	})

	t.Run("a clone that never finished (the measured shape)", func(t *testing.T) {
		// 复刻实测：形状完好、**有** origin url、**没有** refspec。
		dir := filepath.Join(t.TempDir(), "wreck.git")
		writeMirrorConfig(t, dir,
			"[core]\n\tbare = true\n[remote \"origin\"]\n\turl = https://example.invalid/o/r.git\n")

		if !IsBareMirror(dir) {
			t.Fatal("the fixture must pass the *shape* check — that is the whole point of this case")
		}
		if IsMirror(dir) {
			t.Error("a clone that never finished must not be reported as a mirror")
		}
	})

	t.Run("a bare repo with no origin section at all", func(t *testing.T) {
		// 连 origin 段都没有：对 url 那条读法是"问不出答案"，对 refspec 是"确定没有"。
		dir := filepath.Join(t.TempDir(), "plain.git")
		writeMirrorConfig(t, dir, "[core]\n\tbare = true\n")
		if IsMirror(dir) {
			t.Error("a bare repo that is not a mirror must not be usable as one")
		}
	})

	t.Run("not a bare repo", func(t *testing.T) {
		dir := t.TempDir()
		if IsMirror(dir) {
			t.Error("an empty directory is not a mirror")
		}
	})

	t.Run("a config we do not dare to read refuses the refusal", func(t *testing.T) {
		// 保守方向是**不拒绝**：读不准时必须照旧按 mirror 用。
		// 判错的代价不对称——把好 mirror 判死会当场弄坏一个能用的安装，
		// 把废墟当 mirror 用最多复现那个"误导人的下游报错"。
		dir := filepath.Join(t.TempDir(), "included.git")
		writeMirrorConfig(t, dir,
			"[core]\n\tbare = true\n[include]\n\tpath = /elsewhere\n")
		if !IsMirror(dir) {
			t.Error("an unreadable config must leave the path usable, not condemned")
		}
	})
}

// TestMirrorRefspecLocalSeparatesNoFromDontKnow 固定那个二分：
// "确定没有 refspec"与"这份 config 我不敢解读"是两件事，调用方对它们的处置相反。
func TestMirrorRefspecLocalSeparatesNoFromDontKnow(t *testing.T) {
	testutils.MustHaveGit(t)

	t.Run("a real mirror", func(t *testing.T) {
		src := testutils.NewGitRepo(t)
		src.WriteFile("a.txt", "a\n").Commit("feat: a")
		dst := filepath.Join(t.TempDir(), "m.git")
		if err := CloneMirror(context.Background(), Options{}, src.Dir, dst); err != nil {
			t.Fatalf("CloneMirror: %v", err)
		}
		got, ok := MirrorRefspecLocal(dst)
		if !ok {
			t.Fatal("ngm's own mirror must be readable without git")
		}
		if got != MirrorRefspec {
			t.Errorf("refspec=%q want %q", got, MirrorRefspec)
		}
	})

	t.Run("no origin section is a determined no, not a dont-know", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "plain.git")
		writeMirrorConfig(t, dir, "[core]\n\tbare = true\n")
		got, ok := MirrorRefspecLocal(dir)
		if !ok {
			t.Error("a config with no [remote \"origin\"] determines the answer: there is no refspec")
		}
		if got != "" {
			t.Errorf("refspec=%q want empty", got)
		}
	})

	t.Run("dont-know shapes", func(t *testing.T) {
		cases := []struct{ name, body string }{
			{"an include section", "[core]\n\tbare = true\n[include]\n\tpath = /elsewhere\n"},
			{"a legacy dotted section", "[remote.origin]\n\tfetch = " + MirrorRefspec + "\n"},
			{"two fetch values", "[remote \"origin\"]\n\tfetch = " + MirrorRefspec +
				"\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"},
			{"a quoted value", "[remote \"origin\"]\n\tfetch = \"" + MirrorRefspec + "\"\n"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "x.git")
				writeMirrorConfig(t, dir, tc.body)
				if _, ok := MirrorRefspecLocal(dir); ok {
					t.Error("this shape must be refused, not guessed")
				}
			})
		}
	})
}

// TestRefLockFailureGetsItsOwnHint 固定 v0.35 那条纪律（"建议要承认成因"）在
// git 失败这条路上的一格：ref 被别的进程锁住时，用户读到的必须是**这个成因**，
// 而不是"跑一下那条命令看看"。
//
// 夹具里的 stderr 是**本机逐字量到的**（2026-10-15）。反例方向同样钉住：
// 网络/凭证失败**不许**拿走这条建议（否则用户会去等一个不存在的进程）。
func TestRefLockFailureGetsItsOwnHint(t *testing.T) {
	measured := []byte("error: cannot lock ref 'refs/heads/main': Unable to create " +
		"'C:/Users/me/.ngm/mirror/github.com/o/r.git/./refs/heads/main.lock': File exists.\n" +
		"Another git process seems to be running in this repository, or the lock file may be stale\n" +
		"From C:/src\n ! f2f551a..fdee09d  main       -> main  (unable to update local ref)\n")
	hint := hintForGitFailure(measured)
	if !strings.Contains(hint, "another process") || !strings.Contains(hint, "re-run") {
		t.Errorf("the hint must name the cause and the next step, got %q", hint)
	}

	for _, tc := range []struct{ name, stderr string }{
		{"network", "fatal: unable to access 'https://x/y.git/': Could not resolve host: x"},
		{"credentials", "fatal: Authentication failed for 'https://x/y.git/'"},
		{"missing repo", "fatal: repository 'https://x/y.git/' not found"},
	} {
		t.Run("must not steal "+tc.name, func(t *testing.T) {
			got := hintForGitFailure([]byte(tc.stderr))
			if strings.Contains(got, "another process") {
				t.Errorf("%s: the lock hint must not be given for this failure: %q", tc.name, got)
			}
		})
	}
}
