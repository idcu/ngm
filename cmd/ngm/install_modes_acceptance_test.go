package main

import (
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

// TestV02InstallModesAcceptance 是 v0.2 F 组（install 的 CI 模式）的可执行验收。
//
// 三条契约，各自一条测试：
//
//	--frozen-lockfile     不重新解析、不改写 lock；缺 lock 或不一致 → exit 3
//	--offline             不碰网络；本地资源缺失 → exit 4
//	两者组合              完全离线可复现安装（CI 首选）
func TestV02InstallModesAcceptance(t *testing.T) {
	t.Run("frozen-lockfile installs from a matching lock without rewriting it", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/ok", "export const ok = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/ok@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		before := readLockFile(t, proj)

		code, out := runCaptureCode(t, "install", "--frozen-lockfile", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a matching lock must install under --frozen-lockfile, got %d:\n%s", code, out)
		}
		if after := readLockFile(t, proj); after != before {
			t.Errorf("--frozen-lockfile must not rewrite the lock:\n--- before ---\n%s\n--- after ---\n%s", before, after)
		}
	})

	t.Run("frozen-lockfile fails when there is no lock", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/nolock", "export const n = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/nolock@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		code, out := runCaptureCode(t, "install", "--frozen-lockfile", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a missing lock must exit 3 under --frozen-lockfile, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "no ngm.lock") {
			t.Errorf("the error should name the cause:\n%s", out)
		}
	})

	// 这条是 frozen 的立身之本：声明变了却没更新 lock，CI 必须红，
	// 而不是悄悄解析出新 commit（那样"lock 已提交"就失去意义）
	t.Run("frozen-lockfile fails when the lock is out of sync with ngm.json", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/drift", "export const d = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/drift@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		// 改声明（add 只写 ngm.json，不解析，因此不需要 v2 真的存在）
		if code, out := runCaptureCode(t, "add", "github:f/drift@v2", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add v2: %s", out)
		}

		code, out := runCaptureCode(t, "install", "--frozen-lockfile", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an out-of-sync lock must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "does not match ngm.json") {
			t.Errorf("the error should explain the mismatch:\n%s", out)
		}
	})

	// 删掉 mirror 后再装：能成功就证明它真的没访问任何远端
	t.Run("offline installs from the content store with no mirror present", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/store", "export const s = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/store@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		removeMirror(t, "github:f/store")

		code, out := runCaptureCode(t, "install", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a warm content store must satisfy --offline, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "no git access") {
			t.Errorf("the report should say this install touched no git remote:\n%s", out)
		}
	})

	t.Run("offline fails when the required content is not available locally", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/cold", "export const c = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/cold@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		// 把 content store 与 mirror 一并清空：离线时无从落地
		layout, err := vendor.DefaultLayout()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(layout.ContentRoot()); err != nil {
			t.Fatal(err)
		}
		removeMirror(t, "github:f/cold")

		code, out := runCaptureCode(t, "install", "--offline", "--dir="+proj)
		if code != 4 {
			t.Fatalf("a cold store under --offline must exit 4, got %d:\n%s", code, out)
		}
	})

	// CI 首选组合：完全离线、完全由提交的 lock 决定
	t.Run("frozen-lockfile plus offline is the reproducible CI install", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/ci", "export const ci = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/ci@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		removeMirror(t, "github:f/ci")
		before := readLockFile(t, proj)

		code, out := runCaptureCode(t, "install", "--frozen-lockfile", "--offline", "--dir="+proj)
		if code != 0 {
			t.Fatalf("the CI combination must succeed, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "no git access") {
			t.Errorf("the CI install must not touch git:\n%s", out)
		}
		if after := readLockFile(t, proj); after != before {
			t.Errorf("the lock must be untouched by the CI install")
		}
	})

	t.Run("offline without a lock cannot resolve anything", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:f/bare", "export const b = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:f/bare@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}

		code, out := runCaptureCode(t, "install", "--offline", "--dir="+proj)
		if code != 4 {
			t.Fatalf("offline without a lock must exit 4, got %d:\n%s", code, out)
		}
	})
}

// removeMirror 删掉某个依赖的本地镜像（用于证明离线路径不依赖它）。
func removeMirror(t *testing.T, slug string) {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	path := vendor.NewMirror(layout.MirrorRoot(), git.Options{}).PathFor(resolve.MustNormalize(slug))
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("mirror still present at %s", path)
	}
}
