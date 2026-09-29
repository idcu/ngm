package main

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
	"github.com/idcu/ngm/internal/resolve"
	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// isolateUserEnv 把 NGM_HOME / HOME / USERPROFILE 指向临时目录，
// 使被测命令与真实用户环境完全隔离（mirror、全局配置都不外泄）。
//
// 实现委托给 testutils.IsolateUserEnv，保证全项目隔离口径一致。
func isolateUserEnv(t *testing.T) string {
	t.Helper()
	return testutils.IsolateUserEnv(t)
}

// seedMirror 在 <NGM_HOME>/mirror 下预置一个仓库的本地镜像。
func seedMirror(t *testing.T, slug, srcDir string) {
	t.Helper()
	layout, err := vendor.DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	m := vendor.NewMirror(layout.MirrorRoot(), git.Options{})
	repo := resolve.MustNormalize(slug)
	if _, err := m.EnsureLocal(context.Background(), repo, srcDir); err != nil {
		t.Fatalf("seed mirror: %v", err)
	}
}

// TestUpdate_OfflineResolvesFromMirror 是 M1 的端到端闭环：
// 无网络、无远端仓库，仅凭本地 mirror 完成 refType → commit 解析。
func TestUpdate_OfflineResolvesFromMirror(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	// 上游仓库
	src := testutils.NewGitRepo(t)
	head := src.WriteFile("src/index.ts", "export const x = 1\n").Commit("feat: index")
	src.Tag("v1.0.0", true) // annotated

	seedMirror(t, "github:my-org/utils", src.Dir)

	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@v1.0.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatalf("add: %v", err)
	}

	code, out := runCaptureCode(t, "update", "--all", "--dir="+dir, "--offline")
	if code != 0 {
		t.Fatalf("update exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, head) {
		t.Errorf("output should contain resolved commit %s:\n%s", head, out)
	}
	if !strings.Contains(out, "github:my-org/utils@v1.0.0 (tag)") {
		t.Errorf("output should describe the dependency:\n%s", out)
	}
}

func TestUpdate_OfflineWithoutMirrorFailsExit4(t *testing.T) {
	isolateUserEnv(t)
	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@v1.0.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatal(err)
	}
	code, out := runCaptureCode(t, "update", "--all", "--dir="+dir, "--offline")
	if code != 4 {
		t.Fatalf("exit=%d (want 4) out=%s", code, out)
	}
	if !strings.Contains(out, "no local mirror") {
		t.Errorf("out=%s", out)
	}
}

func TestUpdate_SpecificDependency(t *testing.T) {
	isolateUserEnv(t)
	src := testutils.NewGitRepo(t)
	head := src.WriteFile("a.ts", "1\n").Commit("feat: a")
	seedMirror(t, "github:my-org/utils", src.Dir)

	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@main", "--ref-type=branch", "--dir="+dir); err != nil {
		t.Fatal(err)
	}

	// 用等价但不同写法的 slug 也应命中
	code, out := runCaptureCode(t, "update", "github.com:my-org/utils", "--dir="+dir, "--offline")
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, head) {
		t.Errorf("out=%s", out)
	}
}

func TestUpdate_UndeclaredDependency(t *testing.T) {
	isolateUserEnv(t)
	dir := newProject(t)
	code, out := runCaptureCode(t, "update", "github:my-org/nope", "--dir="+dir, "--offline")
	if code != 3 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "not declared") {
		t.Errorf("out=%s", out)
	}
}

func TestUpdate_AllPlusExplicitConflicts(t *testing.T) {
	isolateUserEnv(t)
	dir := newProject(t)
	code, out := runCaptureCode(t, "update", "--all", "github:o/r", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "cannot be combined") {
		t.Errorf("out=%s", out)
	}
}

func TestUpdate_NoArgsShowsUsage(t *testing.T) {
	code, out := runCaptureCode(t, "update")
	if code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "USAGE:") {
		t.Errorf("out=%s", out)
	}
}

func TestUpdate_TagMovedOfflineStillResolvesLocally(t *testing.T) {
	isolateUserEnv(t)
	src := testutils.NewGitRepo(t)
	src.WriteFile("a.ts", "1\n").Commit("feat: a")
	src.Tag("v1.0.0", true)
	seedMirror(t, "github:my-org/utils", src.Dir)

	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@v1.0.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatal(err)
	}
	code, out1 := runCaptureCode(t, "update", "--all", "--dir="+dir, "--offline")
	if code != 0 {
		t.Fatalf("first update: %s", out1)
	}

	// 确定性：同一 mirror 二次解析得到同一 commit
	code, out2 := runCaptureCode(t, "update", "--all", "--dir="+dir, "--offline")
	if code != 0 {
		t.Fatalf("second update: %s", out2)
	}
	c1 := firstCommitLine(out1)
	c2 := firstCommitLine(out2)
	if c1 == "" || c1 != c2 {
		t.Errorf("resolutions differ: %q vs %q", c1, c2)
	}
}

// firstCommitLine 从 update 输出里取第一行的 commit（"→ <sha>" 之后的部分）。
func firstCommitLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "→ "); idx >= 0 {
			return strings.TrimSpace(line[idx+len("→ "):])
		}
	}
	return ""
}
