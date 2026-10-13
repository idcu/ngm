package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.84：把"开工 / 收口先看工作树"变成**读数**并加以判据。
//
// 起因是一次真实事故（v0.83 复盘 §1）：开工时没看工作树，覆盖了并行工作线的两份
// **未跟踪**文件（plan / retrospective）——未提交 ⇒ 没有 Git 副本、没有 IDE 历史 ⇒ 只能重建。
//
// 这个读数不试图"阻止"下一次（一个恒红的门禁等于没有门禁：开发中途工作树本来就不干净），
// 它做的是让"这里还有没提交的版本工件"在任何一次收口**打印出来**。
//
// 判据在一个**合成仓库**上跑（git init + 一份复盘 + 一个附注 tag），因此不依赖本仓库状态：
//
//	① 干净 ⇒ 只计数，不点名；
//	② 出现未提交的版本工件 ⇒ **点名它**并写出"没有版本保护"那句；
//	③ 把它提交 ⇒ 点名消失（牙齿：证明点名认的是"未提交"，不是"文件存在"）；
//	④ 就地改一份**已跟踪**的快照 ⇒ 出现"snapshot docs edited in place"（快照不可改）。

func TestV84WorktreeStateNamesUncommittedVersionArtifacts(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-release-status.sh")
	dir := t.TempDir()
	initSyntheticReleaseRepo(t, dir)

	run := func() string {
		cmd := exec.Command(bash, filepath.ToSlash(script))
		cmd.Dir = dir
		out, _ := cmd.CombinedOutput() // 退出码不重要：这一节是读数，不是门禁
		return string(out)
	}

	// ① 干净：有这一节，但没有点名
	out := run()
	if !strings.Contains(out, "worktree state") {
		t.Fatalf("应当打印工作树状态一节：\n%s", out)
	}
	if strings.Contains(out, "version artifacts NOT committed") {
		t.Errorf("干净仓库不该点名任何版本工件：\n%s", out)
	}

	// ② 未提交的版本工件 ⇒ 点名 + 那句教训
	plan := filepath.Join(dir, "docs", "development", "v0.2-plan.md")
	if err := os.WriteFile(plan, []byte("# 临时计划\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = run()
	if !strings.Contains(out, "version artifacts NOT committed") || !strings.Contains(out, "v0.2-plan.md") {
		t.Errorf("未提交的版本工件必须被点名：\n%s", out)
	}
	if !strings.Contains(out, "没有版本保护") {
		t.Errorf("点名时要写出那句理由（被覆盖就永久丢失）：\n%s", out)
	}

	// ③ 提交它 ⇒ 点名消失（点名认的是"未提交"，不是"文件存在"）
	gitIn(t, dir, "add", "docs/development/v0.2-plan.md")
	gitIn(t, dir, "commit", "-q", "-m", "add plan")
	out = run()
	if strings.Contains(out, "version artifacts NOT committed") {
		t.Errorf("提交之后不该再点名：\n%s", out)
	}

	// ④ 就地改一份已跟踪的快照 ⇒ 单独一句话（快照不可改）
	retro := filepath.Join(dir, "docs", "development", "v0.1-retrospective.md")
	if err := os.WriteFile(retro, []byte("# 被就地改过的复盘\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = run()
	if !strings.Contains(out, "snapshot docs edited in place") || !strings.Contains(out, "v0.1-retrospective.md") {
		t.Errorf("就地修改快照必须被指出来：\n%s", out)
	}
}

// initSyntheticReleaseRepo 造一个满足离线三条规则的最小仓库
// （一份复盘 + 一个附注 tag，两者对得上），因此脚本不会因为别的原因吵闹。
func initSyntheticReleaseRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "test")
	if err := os.MkdirAll(filepath.Join(dir, "docs", "development"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileAt(t, dir, "docs/development/v0.1-retrospective.md", "# v0.1 复盘\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "v0.1")
	gitIn(t, dir, "tag", "-a", "v0.1.0", "-m", "v0.1.0")
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
