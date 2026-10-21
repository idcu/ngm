package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.104：install-from-lock 对模块条目优先使用 partial clone module cache。
//
// 关键点：mirror 仍在（提供 origin URL），但 content store 被清空——
// 这时 install 必须**只**为锁定 commit 建一个 blob:none 缓存，而不是重新克隆整仓 mirror。
func TestV104InstallUsesModuleCache(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("packages/a/package.json", `{"name":"pkg-a","version":"1.0.0","type":"module","license":"MIT","exports":{".":"./src/index.js"}}`+"\n")
	up.WriteFile("packages/a/src/index.js", "export const a = 1;\n")
	up.WriteFile("packages/b/src/index.js", "export const b = 1;\n")
	up.Commit("monorepo: a + b")
	up.Tag("v1.0.0", true)

	const slug = "127.0.0.1/acme/lib"
	// file:// 是 partial clone filter 生效的关键；普通本地路径会被 git 忽略 filter。
	seedMirror(t, slug, "file://"+filepath.ToSlash(up.Dir))

	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "v104.test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init failed (exit %d):\n%s", code, out)
	}
	spec := "git://" + slug + ".git#path=packages/a@v1.0.0"
	if code, out := runCaptureCode(t, "add", spec, "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add failed (exit %d):\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "update", "--all", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("update failed (exit %d):\n%s", code, out)
	}

	layout := defaultLayoutForTest(t)
	if err := os.RemoveAll(layout.ContentRoot()); err != nil {
		t.Fatal(err)
	}
	moduleCachePath := filepath.Join(layout.ModuleCacheRoot(), filepath.FromSlash("127.0.0.1/acme/lib.git"))
	if git.IsPartialClone(moduleCachePath) {
		t.Fatalf("precondition: module cache should not exist yet: %s", moduleCachePath)
	}

	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install failed (exit %d):\n%s", code, out)
	}
	if !git.IsPartialClone(moduleCachePath) {
		t.Errorf("install should create a partial clone module cache at %s", moduleCachePath)
	}
	code, usageOut := runCaptureCode(t, "store", "usage")
	if code != 0 {
		t.Fatalf("store usage exit=%d:\n%s", code, usageOut)
	}
	if !strings.Contains(usageOut, "module cache (layer 1.5)") ||
		!strings.Contains(usageOut, "127.0.0.1/acme/lib.git") {
		t.Errorf("store usage should report the module cache:\n%s", usageOut)
	}

	lf, err := lock.Read(lock.Find(proj))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	d, ok := lf.Find("127.0.0.1:acme/lib", "packages/a")
	if !ok || d.ModuleDigest == "" {
		t.Fatalf("lock entry for packages/a missing moduleDigest: %+v", d)
	}
	store := vendor.NewContentStore(layout.ContentRoot())
	entries, err := store.Entries(d.ModuleDigest)
	if err != nil {
		t.Fatalf("read module store: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("module content store has no entries")
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Path, "packages/") {
			t.Errorf("module store entries must be relative to the module root: %q", e.Path)
		}
	}
	if got := vendoredRelPaths(t, proj); len(got) == 0 {
		t.Error("install produced no vendor tree")
	}
}
