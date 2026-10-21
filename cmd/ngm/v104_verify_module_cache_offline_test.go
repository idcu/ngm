package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.104：没有完整 mirror 时，verify --offline 对 commit 型模块条目复用
// partial clone module cache 完成 binding/digest/landing 检查。
//
// 关键点：mirror 被删掉后，verify 不许为了一个模块重新克隆整仓；它应该只认
// module cache 与 content store，并对 commit 型 ref 给出可判定结论。
func TestV104VerifyOfflineUsesModuleCache(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("packages/a/package.json", `{"name":"pkg-a","version":"1.0.0","type":"module","license":"MIT","exports":{".":"./src/index.js"}}`+"\n")
	up.WriteFile("packages/a/src/index.js", "export const a = 1;\n")
	up.Commit("monorepo: a")
	head := up.Head()

	const slug = "127.0.0.1/acme/lib"
	seedMirror(t, slug, "file://"+filepath.ToSlash(up.Dir))

	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "v104.test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init failed (exit %d):\n%s", code, out)
	}
	spec := "git://" + slug + ".git#path=packages/a@" + head
	if code, out := runCaptureCode(t, "add", spec, "--dir="+proj); code != 0 {
		t.Fatalf("add failed (exit %d):\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "update", "--all", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("update failed (exit %d):\n%s", code, out)
	}

	layout := defaultLayoutForTest(t)
	if err := os.RemoveAll(layout.ContentRoot()); err != nil {
		t.Fatal(err)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install failed (exit %d):\n%s", code, out)
	}

	moduleCachePath := filepath.Join(layout.ModuleCacheRoot(), filepath.FromSlash("127.0.0.1/acme/lib.git"))
	if !git.IsPartialClone(moduleCachePath) {
		t.Fatalf("precondition: module cache should exist: %s", moduleCachePath)
	}
	mirrorPath := filepath.Join(layout.MirrorRoot(), filepath.FromSlash("127.0.0.1/acme/lib.git"))
	if err := os.RemoveAll(mirrorPath); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "verify", "--offline", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("offline verify should pass from module cache, got %d:\n%s", code, out)
	}
	for _, want := range []string{`"check": "binding"`, `"check": "digest"`, `"check": "landing"`} {
		if !strings.Contains(out, want) {
			t.Errorf("verify report should include %s:\n%s", want, out)
		}
	}
	if git.IsBareMirror(mirrorPath) {
		t.Error("offline verify must not re-create the full mirror")
	}
}
