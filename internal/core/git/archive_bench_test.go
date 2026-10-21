package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// BenchmarkBuildArchive 测"从一个 commit 生成规范化清单"的成本（ADR-008）。
//
// 这是 digest 重放的核心路径：`ngm install`、`ngm verify`、`verify --deep` 都要走它。
// v0.2 复盘 §2.3 把"单个依赖内部并行哈希"挂账，本基准让"改没改得动"**可复现**——
// 而不是在复盘里写一句"更快了"。
//
// A/B 对照就在同一份代码上，只改并发度：
//
//	go test -run '^$' -bench BuildArchive -benchtime 20x ./internal/git
//	GOMAXPROCS=1 go test -run '^$' -bench BuildArchive -benchtime 20x ./internal/git
//
// 为什么用 GOMAXPROCS 而不是"临时把并行度改成 1"：前者的数字**别人也能复现**，
// 后者需要改代码才能测量。
//
// 形态刻意取"文件多、单文件小"（2000 × 8 KiB）——那看起来是并行最有利的形状。
//
// **实测结果与预期相反，因此值得记在这里**（2026-10-01，Windows 10 / i3-10100）：
//
//	GOMAXPROCS=1   278.9ms/op   58.75 MB/s
//	默认（8）      244.1ms/op   67.12 MB/s     → 1.14×
//
// 只有 1.14×，因为本函数的成本大头是 `git ls-tree` + `git cat-file --batch` 两个
// 子进程，而 16 MiB 的 sha256 只占其中一小段。把那段并行化，上限就是那段本身。
// **结论**：digest 重放的瓶颈在 git 子进程，不在哈希；下一步要动的是"少开子进程"。
// 与之对照，`--deep` 的逐文件校验是**纯文件哈希**（没有子进程），
// 同一台机器上是 2.09×——见 internal/vendor/integrity_bench_test.go。
func BenchmarkBuildArchive(b *testing.B) {
	if _, err := exec.LookPath("git"); err != nil {
		// 缺工具 ≠ 已验证：跳过而不是伪造一个通过
		b.Skip("git not found on PATH")
	}

	const files = 2000
	const perFile = 8 << 10

	dir := b.TempDir()
	gitRun := func(args ...string) string {
		b.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// 与 testutils 同一套纪律：不让本机的全局 git 配置影响结果
		// （gpgsign 会要求密钥、autocrlf 会改字节、hooks 会在夹具里乱跑）。
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=bench", "GIT_AUTHOR_EMAIL=bench@example.invalid",
			"GIT_COMMITTER_NAME=bench", "GIT_COMMITTER_EMAIL=bench@example.invalid",
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, "no-global-gitconfig"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(dir, "no-system-gitconfig"),
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	gitRun("init", "-q")
	gitRun("config", "commit.gpgsign", "false")
	gitRun("config", "core.autocrlf", "false")
	gitRun("config", "core.hooksPath", filepath.Join(dir, ".empty-hooks"))

	body := make([]byte, perFile)
	for j := range body {
		body[j] = byte('a' + j%26)
	}
	for i := 0; i < files; i++ {
		rel := filepath.Join("pkg", fmt.Sprintf("mod%03d", i%50), fmt.Sprintf("f%04d.ts", i))
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			b.Fatal(err)
		}
	}
	gitRun("add", "-A")
	gitRun("commit", "-q", "-m", "bench")
	head := gitRun("rev-parse", "HEAD")
	head = head[:len(head)-1] // 去掉换行

	ctx := context.Background()
	b.SetBytes(int64(files * perFile))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manifest, err := BuildArchive(ctx, Options{}, dir, head)
		if err != nil {
			b.Fatal(err)
		}
		if len(manifest) == 0 {
			b.Fatal("empty manifest")
		}
	}
}
