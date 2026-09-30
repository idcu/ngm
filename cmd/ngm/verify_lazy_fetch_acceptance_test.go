package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/vendor"
)

// 本文件固定 ADR-010 的核心行为：**ref 问远端、对象按需取**。
//
// 探针选用 `FETCH_HEAD`——`git fetch` 会写它，而 `git ls-remote` 不会。
// 用 git 自己的可观察副作用而不是我们内部的调用计数：这样断言的是
// **对外可观察的行为**，换实现也不会失效。

func mirrorDirForTest(t *testing.T, slug string) string {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	return vendor.NewMirror(layout.MirrorRoot(), git.Options{}).PathFor(resolve.MustNormalize(slug))
}

func fetchHeadCleared(t *testing.T, mirror string) string {
	t.Helper()
	fetchHead := filepath.Join(mirror, "FETCH_HEAD")
	if err := os.Remove(fetchHead); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(fetchHead); !os.IsNotExist(err) {
		t.Fatalf("cannot clear the probe file %s", fetchHead)
	}
	return fetchHead
}

// TestV03VerifyDoesNotFetchWhenNothingChanged 是 ADR-010 的核心断言：
// ref 未变、对象已在本地时，**一次 fetch 都不该发生**。
//
// 这条直接对应 100 依赖规模下约 1.4s 的成本，也正是"在线 verify 达标"的来源。
func TestV03VerifyDoesNotFetchWhenNothingChanged(t *testing.T) {
	isolateUserEnv(t)
	scUpstream(t, "github:lazy/quiet", "export const q = 1\n", "")

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:lazy/quiet@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	// install 会 fetch（那是它的职责），因此先清掉它留下的痕迹再测 verify
	fetchHead := fetchHeadCleared(t, mirrorDirForTest(t, "github:lazy/quiet"))

	code, out := runCaptureCode(t, "verify", "--dir="+proj)
	if code != 0 {
		t.Fatalf("verify exit=%d:\n%s", code, out)
	}
	if _, err := os.Stat(fetchHead); err == nil {
		t.Errorf("nothing changed, yet verify fetched (FETCH_HEAD reappeared).\n"+
			"ADR-010: deciding whether a ref still points at the pinned commit needs only a ref\n"+
			"advertisement - the objects are already here. Output was:\n%s", out)
	}
}

// 反向固定：ref **变了**就必须取对象——判"分支快进还是历史被改写"需要祖先关系，
// 而祖先关系需要对象。惰性化不能变成"永不 fetch"。
func TestV03VerifyFetchesWhenTheRefMoved(t *testing.T) {
	isolateUserEnv(t)

	r := scUpstream(t, "github:lazy/moved", "export const m = 1\n", "")
	branch := strings.TrimSpace(r.Exec("rev-parse", "--abbrev-ref", "HEAD"))

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:lazy/moved@"+branch, "--ref-type=branch", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	// 上游前进（mirror 保持落后：这正是"远端比本地新"的真实情形）
	r.WriteFile("index.ts", "export const m = 2\n")
	r.Commit("feat: advance")

	fetchHead := fetchHeadCleared(t, mirrorDirForTest(t, "github:lazy/moved"))

	code, out := runCaptureCode(t, "verify", "--dir="+proj)
	// 分支快进属"预期更新"：默认不失败，但要被认出来
	if code != 0 {
		t.Fatalf("a fast-forward should not fail by default; exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, "expected update") {
		t.Errorf("the branch advance should be classified as expected:\n%s", out)
	}
	if _, err := os.Stat(fetchHead); err != nil {
		t.Errorf("classifying the drift needs the ancestor relation, so verify must have fetched: %v", err)
	}
}
