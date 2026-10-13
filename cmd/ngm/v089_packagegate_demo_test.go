package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.89：`packagegate-demo.sh` 要在判据里**真的跑一次**。
//
// 它是 docs/COMPETITIVE-ANALYSIS.md 里那套对照的**证据生成器**：demo 的断言
// （什么算通过、什么算被拦下）只写在它里面。此前它只被文档提到、被 `.ps1` 入口提到——
// 没有任何判据跑过它，也就是"只被读过、从没被跑过"的那一类（v0.88 的教训）。
//
// 它不碰网络（全部在本地合成目录里做），因此适合进全量测试。
func TestV89PackageGateDemoRuns(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "packagegate-demo.sh")

	cmd := exec.Command(bash, filepath.ToSlash(script))
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("demo 跑不动（%v）：\n%s", err, text)
	}

	// demo 自己会打印它的结论行（"N passed, M failed"）——判据要求它**真的**报通过，
	// 而不是"跑完了没报错"（脚本自己 exit 0 但断言没跑，正是要防的那种"看起来在工作"）。
	if !strings.Contains(text, "passed") {
		t.Errorf("demo 没打印结论行（断言可能没跑）：\n%s", text)
	}
	if !strings.Contains(text, "0 failed") {
		t.Errorf("demo 报了失败：\n%s", text)
	}
}
