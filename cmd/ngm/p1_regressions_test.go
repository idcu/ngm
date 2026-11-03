package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// TestP1ShortCommitVerifyResolvesThePinnedCommit 是短 commit 的回归：
// `ngm add github:org/repo@abc1234 --ref-type=commit` 写的是短 ref、完整 commit；
// verify 必须先把短 hash 在本地 mirror 里补齐，再与 lock 比较。
// 修复前它会把 7 位字符串与 40 位 commit 比较，报成 unexpected drift。
func TestP1ShortCommitVerifyResolvesThePinnedCommit(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("index.ts", "export const x = 1\n")
	up.Commit("feat: short commit")
	seedMirror(t, "github:p1/short", up.Dir)

	short := up.Head()[:7]
	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:p1/short-app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	if code, out := runCaptureCode(t, "add", "github:p1/short@"+short,
		"--ref-type=commit", "--dir="+proj); code != 0 {
		t.Fatalf("add short commit: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	lf, err := lock.Read(lock.Find(proj))
	if err != nil || lf == nil {
		t.Fatalf("read lock: %v", err)
	}
	d, ok := lf.Find("github:p1/short", "")
	if !ok {
		t.Fatalf("short dependency missing from lock:\n%s", readLockFile(t, proj))
	}
	if d.Ref != short {
		t.Errorf("lock ref=%q, want the declared short hash %q", d.Ref, short)
	}
	if d.Commit != up.Head() {
		t.Errorf("lock commit=%q, want full %q", d.Commit, up.Head())
	}

	code, out := runCaptureCode(t, "verify", "--offline", "--dir="+proj)
	if code != 0 {
		t.Fatalf("verify must not report a short commit as drift; exit=%d\n%s", code, out)
	}
	if strings.Contains(out, "unexpected drift") {
		t.Fatalf("short commit must not self-drift:\n%s", out)
	}
}

// TestP1ProjectGlobalPathStaysInNgmGlobalVendor 是 globalPath 的边界回归：
// 项目内的 ngm.json 不能把 vendor 指到 ~/.ngm/global-vendor 之外。
func TestP1ProjectGlobalPathStaysInNgmGlobalVendor(t *testing.T) {
	layout := vendor.Layout{Home: t.TempDir()}

	outside := filepath.Join(t.TempDir(), "outside")
	pf := &config.ProjectFile{Vendor: &config.VendorConfig{
		Mode:       "global",
		GlobalPath: outside,
	}}
	if err := validateVendorLocation(layout, pf); err == nil {
		t.Fatalf("globalPath outside %s must be rejected", layout.GlobalVendorRoot())
	}

	inside := filepath.Join(layout.GlobalVendorRoot(), "custom")
	pf = &config.ProjectFile{Vendor: &config.VendorConfig{
		Mode:       "global",
		GlobalPath: inside,
	}}
	if err := validateVendorLocation(layout, pf); err != nil {
		t.Fatalf("globalPath inside the ngm global vendor root must be accepted: %v", err)
	}
}
