package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// murlOriginURL 用 **git 自己**把 origin URL 写进一个裸仓库，返回裸仓库路径。
//
// 刻意不让测试手写 config 文本：那样测的是"我的解析器能不能读我的写法"，
// 而真实世界里这份文件是 git 写的。夹具必须由 git 生成。
func murlOriginURL(t *testing.T, dir, url string) {
	t.Helper()
	testutils.GitOutput(t, dir, "init", "--bare")
	testutils.GitOutput(t, dir, "config", "remote.origin.url", url)
}

// murlGitAnswer 是**独立于被测实现**的参考答案：直接问 git。
func murlGitAnswer(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(testutils.GitOutput(t, dir, "config", "--get", "remote.origin.url"))
}

// TestMirrorRemoteURLLocal_NeverDisagreesWithGit 是本改动**唯一真正重要的性质**：
// 快路径要么拒绝作答，要么给出的答案与 git 逐字相同。**不允许有第三种情况**
// （一个看起来正常、其实指向另一个仓库的地址）。
//
// 这条性质对"奇形怪状的 config"也必须成立，因此夹具里既有常见形态，也有刻意刁难的形态。
func TestMirrorRemoteURLLocal_NeverDisagreesWithGit(t *testing.T) {
	testutils.MustHaveGit(t)

	cases := []struct {
		name string
		url  string
	}{
		{name: "https", url: "https://github.com/org/repo.git"},
		{name: "ssh scp form", url: "git@github.com:org/repo.git"},
		{name: "posix path", url: "/tmp/ngm-fixture/repo.git"},
		{name: "path with a space", url: "/tmp/ngm fixture/repo.git"},
		{name: "url with a fragment", url: "https://example.com/org/repo.git#main"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			murlOriginURL(t, dir, tc.url)

			want := murlGitAnswer(t, dir)
			if want != tc.url {
				t.Fatalf("fixture did not take: git reports %q, wanted %q", want, tc.url)
			}

			got, ok := MirrorRemoteURLLocal(dir)
			if ok && got != want {
				t.Errorf("the fast path must never disagree with git:\n fast: %q\n  git: %q", got, want)
			}

			// 完整函数（含回退）必须给出 git 的答案，无论走了哪条路。
			full, err := MirrorRemoteURL(context.Background(), Options{}, dir)
			if err != nil {
				t.Fatalf("MirrorRemoteURL: %v", err)
			}
			if full != want {
				t.Errorf("MirrorRemoteURL = %q, want %q", full, want)
			}
		})
	}
}

// TestMirrorRemoteURLLocal_FastPathCoversTheCommonShapes 反过来钉住"优化真的生效"。
//
// 只测"永不答错"是不够的：一个永远返回 false 的实现也满足那一条，
// 而它会悄悄把这次优化变成零收益（每次仍起一个 git）。
func TestMirrorRemoteURLLocal_FastPathCoversTheCommonShapes(t *testing.T) {
	testutils.MustHaveGit(t)

	t.Run("https", func(t *testing.T) {
		dir := t.TempDir()
		murlOriginURL(t, dir, "https://github.com/org/repo.git")
		if got, ok := MirrorRemoteURLLocal(dir); !ok || got != "https://github.com/org/repo.git" {
			t.Errorf("the common shape must be answered without git; got (%q, %v)", got, ok)
		}
	})

	t.Run("ssh", func(t *testing.T) {
		dir := t.TempDir()
		murlOriginURL(t, dir, "git@gitee.com:org/repo.git")
		if got, ok := MirrorRemoteURLLocal(dir); !ok || got != "git@gitee.com:org/repo.git" {
			t.Errorf("ssh must be answered without git; got (%q, %v)", got, ok)
		}
	})

	t.Run("a real mirror clone", func(t *testing.T) {
		// 与在线 verify 遇到的是同一种仓库：ngm 自己 clone --mirror 出来的。
		// 这条在 Windows 上顺带覆盖"本地路径 URL 的转义"——git 写进 config 的是
		// `C:\\Users\\...` 而 `--get` 回来是 `C:\Users\...`。
		src := testutils.NewGitRepo(t)
		src.WriteFile("index.ts", "export const v = 1\n")
		src.Commit("feat: dep")
		src.Tag("v1.0.0", false)

		dst := filepath.Join(t.TempDir(), "mirror.git")
		if err := CloneMirror(context.Background(), Options{}, src.Dir, dst); err != nil {
			t.Fatalf("CloneMirror: %v", err)
		}
		want := murlGitAnswer(t, dst)
		got, ok := MirrorRemoteURLLocal(dst)
		if !ok {
			t.Fatal("a mirror ngm created itself must be readable without git")
		}
		if got != want {
			t.Errorf("fast path %q, git %q", got, want)
		}
	})

	t.Run("the fast path spawns nothing", func(t *testing.T) {
		dir := t.TempDir()
		murlOriginURL(t, dir, "https://github.com/org/repo.git")

		ResetSpawnCount()
		if _, ok := MirrorRemoteURLLocal(dir); !ok {
			t.Fatal("expected the fast path to answer")
		}
		if n := SpawnCount(); n != 0 {
			t.Errorf("reading the config file must not start git (spawned %d)", n)
		}
	})
}

// TestMirrorRemoteURLLocal_RefusesWhatItCannotPromise 覆盖**回退**：
// 凡是本函数不敢解读的形态都必须说 false，而且完整函数仍要给出 git 的答案。
func TestMirrorRemoteURLLocal_RefusesWhatItCannotPromise(t *testing.T) {
	testutils.MustHaveGit(t)

	write := func(t *testing.T, dir, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("an included file defines the url", func(t *testing.T) {
		// 这条最能说明"为什么必须回退"：git 会展开 include，而我们不展开。
		// 若在这里作答，就会漏掉 included 里那个真正的地址。
		dir := t.TempDir()
		testutils.GitOutput(t, dir, "init", "--bare")
		if err := os.WriteFile(filepath.Join(dir, "shared.config"),
			[]byte("[remote \"origin\"]\n\turl = https://included.example.com/o/r.git\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		testutils.GitOutput(t, dir, "config", "include.path", "shared.config")

		want := murlGitAnswer(t, dir)
		if want != "https://included.example.com/o/r.git" {
			t.Fatalf("fixture did not take: git reports %q", want)
		}
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("a config with an include must be refused (git expands it, we do not)")
		}
		got, err := MirrorRemoteURL(context.Background(), Options{}, dir)
		if err != nil || got != want {
			t.Errorf("the fallback must still return git's answer: %q (%v), want %q", got, err, want)
		}
	})

	t.Run("two url values", func(t *testing.T) {
		dir := t.TempDir()
		murlOriginURL(t, dir, "https://one.example.com/o/r.git")
		testutils.GitOutput(t, dir, "config", "--add", "remote.origin.url", "https://two.example.com/o/r.git")

		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("a multi-valued url must be refused: `--get` picks one, and that tie-break is git's")
		}
		if got, err := MirrorRemoteURL(context.Background(), Options{}, dir); err != nil || got != murlGitAnswer(t, dir) {
			t.Errorf("the fallback must match git: %q (%v)", got, err)
		}
	})

	t.Run("no remote at all", func(t *testing.T) {
		dir := t.TempDir()
		testutils.GitOutput(t, dir, "init", "--bare")
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("no origin section means no answer")
		}
	})

	t.Run("a quoted value", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "[remote \"origin\"]\n\turl = \"https://quoted.example.com/o/r.git\"\n")
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("a quoted value needs git's unescaping rules; refuse rather than guess")
		}
	})

	t.Run("a legacy dotted section", func(t *testing.T) {
		// `[remote.origin]` 与 `[remote "origin"]` 在 git 里等价；
		// 只认其中一种就可能与取值顺序不一致，因此见到就拒绝。
		dir := t.TempDir()
		write(t, dir, "[remote.origin]\n\turl = https://legacy.example.com/o/r.git\n")
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("the legacy dotted form must be refused")
		}
	})

	t.Run("an extensions section", func(t *testing.T) {
		// `extensions.worktreeConfig` 会让 git 继续读 config.worktree——
		// 那是"git 读的文件比我们多"的情形，唯一可能让答案不一致的来源。
		dir := t.TempDir()
		write(t, dir, "[extensions]\n\tworktreeConfig = true\n[remote \"origin\"]\n\turl = https://x.example.com/o/r.git\n")
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("an extensions section must be refused")
		}
	})

	t.Run("an escape we do not implement", func(t *testing.T) {
		// 值里出现 `\t` 这类没复刻的转义：必须拒绝，而不是原样返回。
		dir := t.TempDir()
		write(t, dir, "[remote \"origin\"]\n\turl = https://x.example.com/\\tfoo\n")
		if _, ok := MirrorRemoteURLLocal(dir); ok {
			t.Error("an unimplemented escape must be refused")
		}
	})
}
