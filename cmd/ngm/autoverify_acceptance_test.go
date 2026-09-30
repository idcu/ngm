package main

import (
	"strings"
	"testing"
)

// TestV02VerifyOnLockAcceptance 是 v0.2 A 组最后一项（`verifyOnLock`）的可执行验收。
//
// 这条策略存在的理由只有一个场景：**lock 已经提交，而上游的 ref 变了**。
// 此时 install 会老老实实按 lock 装（这是对的），但它自己说不出"ref 已经不再指向
// 你锁定的那个 commit"。自动复查就是补上这句。
func TestV02VerifyOnLockAcceptance(t *testing.T) {
	t.Run("no re-check happens unless the policy asks for it", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:vol/off", "export const off = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:vol/off@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install: %s", out)
		}
		// 无默认值：没配就不跑（verify 要重新解析每个 ref，不该替所有人付这个代价）
		if strings.Contains(out, "verifyOnLock") {
			t.Errorf("verifyOnLock must be opt-in:\n%s", out)
		}
	})

	t.Run("verifyOnLock re-checks the lock after install", func(t *testing.T) {
		isolateUserEnv(t)
		scUpstream(t, "github:vol/on", "export const on = 1\n", "")

		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:vol/on@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		writeSupplyChain(t, proj, `{"verifyOnLock": true}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("install: %s", out)
		}
		if !strings.Contains(out, "verifyOnLock: re-checked") {
			t.Errorf("the automatic check should say that it ran:\n%s", out)
		}
	})

	// 核心场景：install 成功，而 ref 已经漂移 —— 必须由自动复查报出来，且退出码非 0
	t.Run("a drifted ref is reported instead of being swallowed by a successful install", func(t *testing.T) {
		isolateUserEnv(t)

		r := scUpstream(t, "github:vol/drift", "export const d = 1\n", "")
		proj := newProject(t)
		if code, out := runCaptureCode(t, "add", "github:vol/drift@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add: %s", out)
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		writeSupplyChain(t, proj, `{"verifyOnLock": true}`)

		// 上游把 v1 挪到新 commit
		r.WriteFile("index.ts", "export const d = 2\n")
		r.Commit("feat: move on")
		r.Exec("tag", "-d", "v1")
		r.Tag("v1", false)
		removeMirror(t, "github:vol/drift")
		seedMirror(t, "github:vol/drift", r.Dir)

		code, out := runCaptureCode(t, "install", "--frozen-lockfile", "--dir="+proj)
		if code == 0 {
			t.Fatalf("the automatic check must not report success for a drifted ref:\n%s", out)
		}
		if !strings.Contains(out, "github:vol/drift") {
			t.Errorf("the report should name the dependency:\n%s", out)
		}
		if !strings.Contains(out, "drift") {
			t.Errorf("the report should say it is drift:\n%s", out)
		}
	})
}
