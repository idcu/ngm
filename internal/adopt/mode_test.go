package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.110：摘要必须**跨系统**可比，所以不看可执行位。
//
// 缺陷形态（上一版）：按 `perm&0o111` 决定 100644 / 100755。看起来更忠实，
// 但 Windows 上 Go 报告的文件 mode 恒为 0666——可执行位根本观测不到。
// 同一棵 npm 树（`node_modules/rimraf/bin.js` 在 POSIX 上是 100755）因此在
// Windows 与 Linux 上给出**两个不同的摘要**，而 `--check` 的典型用法正是
// "本机写基线、CI 复核"：结果是对每台机器都报一次假漂移，原因还完全看不出来。
//
// 两条判据：行为层（改权限不改摘要）+ 结构层（那一行不许回来）。
// 行为层在 Windows 上是**弱**的（chmod 基本无效，见下方说明），所以结构层是主力。
func TestV110DigestIgnoresPermissionBits(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "node_modules", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(pkgDir, "bin.js")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env node\nconsole.log(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}

	// 加可执行位。在 POSIX 上这会真的改变 info.Mode().Perm()；
	// 在 Windows 上它是 no-op——那正是这条判据在 Windows 上恒绿的原因，
	// 也正是我们必须靠结构层兜住它的原因（见下一个测试）。
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	after, err := Run(Options{Dir: root, ToolVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if before.Packages[0].Digest != after.Packages[0].Digest {
		t.Errorf("the digest changed after chmod +x:\n  before %s\n  after  %s\n"+
			"permission bits are not portable (Windows cannot observe them), "+
			"so including them makes the same tree hash differently on two machines",
			before.Packages[0].Digest, after.Packages[0].Digest)
	}
}

// 结构层：生产代码里不许再出现"按可执行位选 mode"的那一行。
//
// 为什么必须用源码判据：这条决定在 Windows 上**无法用行为分辨**（chmod 是 no-op），
// 于是改回去之后行为测试仍然全绿，而 Linux 用户会开始收到假漂移。
// 这类"只在一半平台上可观测"的决定，只能靠结构判据守住。
func TestV110AdoptNeverEmitsAnExecutableMode(t *testing.T) {
	root := repoRootForAdopt(t)
	body, err := os.ReadFile(filepath.Join(root, "internal", "adopt", "adopt.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, forbidden := range []string{"0o111", "ModeExecutable"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("internal/adopt/adopt.go mentions %q: the digest must not depend on "+
				"permission bits — Windows cannot observe them, so the same tree would hash "+
				"differently there", forbidden)
		}
	}
	// 反向：符号链接仍要以 120000 记录（那不是权限，是内容形态）。
	if !strings.Contains(src, "ModeSymlink") {
		t.Error("symlinks must still be recorded as 120000; only the executable BIT was dropped")
	}
}

// repoRootForAdopt 从测试文件所在位置向上找 go.mod。
func repoRootForAdopt(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find go.mod above the test file")
	return ""
}
