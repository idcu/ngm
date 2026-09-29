package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/testutils"
)

// TestBaseline 记录 v0.1 的性能 baseline（默认跳过）。
//
//	NGM_BENCH=1 go test -count=1 -run TestBaseline -v ./cmd/ngm
//
// 为什么把它放进仓库而不是在复盘文档里写一串散文数字：数字随机器变化，
// **方法**不会。internals/metrics.md 的目标值是设计值，本测试产出实测值，
// 两者对照记录在 development/v0.1-retrospective.md。
//
// 全部使用本地 fixture（无网络），因此换一台机器也能得到可比的口径。
func TestBaseline(t *testing.T) {
	if os.Getenv("NGM_BENCH") != "1" {
		t.Skip("set NGM_BENCH=1 to run the performance baseline")
	}
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	type row struct {
		name string
		dur  time.Duration
		note string
	}
	var rows []row

	measure := func(name, note string, run func() (int, string)) {
		start := time.Now()
		code, out := run()
		d := time.Since(start)
		if code != 0 {
			t.Fatalf("%s: exit=%d\n%s", name, code, out)
		}
		rows = append(rows, row{name: name, dur: d, note: note})
		fmt.Printf("BENCH %-44s %10s  %s\n", name, d.Round(time.Millisecond), note)
	}

	// -----------------------------------------------------------------
	// 依赖数量维度：10 与 100（对照 metrics.md 的目标值）
	// -----------------------------------------------------------------
	for _, n := range []int{10, 100} {
		proj := benchProject(t, n)

		measure(fmt.Sprintf("cold install (%d deps, local mirror warm)", n),
			"解析 + digest + content store + vendor 落地 + lock",
			func() (int, string) { return runCaptureCode(t, "install", "--dir="+proj) })

		measure(fmt.Sprintf("warm install (%d deps, no change)", n),
			"有 lock → 尊重 lock，不重新解析 ref",
			func() (int, string) { return runCaptureCode(t, "install", "--dir="+proj) })

		measure(fmt.Sprintf("verify (%d deps, online ref check)", n),
			"三级检查 + 远端 ref 对比",
			func() (int, string) { return runCaptureCode(t, "verify", "--dir="+proj) })

		measure(fmt.Sprintf("verify --offline (%d deps)", n),
			"三级检查 + 本地 mirror 快照",
			func() (int, string) { return runCaptureCode(t, "verify", "--offline", "--dir="+proj) })

		measure(fmt.Sprintf("verify --deep (%d deps)", n),
			"追加：从落地内容树重放 digest（全量哈希）",
			func() (int, string) { return runCaptureCode(t, "verify", "--deep", "--offline", "--dir="+proj) })
	}

	// -----------------------------------------------------------------
	// 体积维度：单个 ~100MB 依赖（对照 metrics.md 的 digest 目标）
	// -----------------------------------------------------------------
	bigProj, bigSize := benchBigRepo(t)
	measure(fmt.Sprintf("cold install (1 dep, %s)", bigSize),
		"含 content store 解包（全量写盘）",
		func() (int, string) { return runCaptureCode(t, "install", "--dir="+bigProj) })
	measure(fmt.Sprintf("verify --offline (1 dep, %s)", bigSize),
		"digest 重放：从 mirror 重建清单并逐 blob 计算 sha256",
		func() (int, string) { return runCaptureCode(t, "verify", "--offline", "--dir="+bigProj) })

	// 汇总表（便于直接抄进复盘文档）
	fmt.Println("\n| 操作 | 实测 | 口径 |")
	fmt.Println("|------|------|------|")
	for _, r := range rows {
		fmt.Printf("| %s | %s | %s |\n", r.name, r.dur.Round(time.Millisecond), r.note)
	}
}

// benchProject 建一个声明 n 个依赖的项目（每个依赖一个独立的上游 + mirror）。
//
// 依赖名带序号，内容各不相同（避免 mirror 与 digest 被复用，那样测不出解析成本）。
func benchProject(t *testing.T, n int) string {
	t.Helper()
	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:bench/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	for i := 0; i < n; i++ {
		slug := fmt.Sprintf("github:bench/dep%03d", i)
		r := testutils.NewGitRepo(t)
		r.WriteFile("index.ts", fmt.Sprintf("export const v = %d;\n", i))
		r.Commit("feat: dep")
		r.Tag("v1.0.0", false)
		seedMirror(t, slug, r.Dir)
		if code, out := runCaptureCode(t, "add", slug+"@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add %s: %s", slug, out)
		}
	}
	return proj
}

// benchBigRepo 建一个 ~100MB 的依赖并预置 mirror，返回项目目录与实际体积。
//
// 内容必须**不可压缩**才能测出真实成本：全零或重复文本会被 git 的 zlib 压到
// 几 MB，那样测的是压缩率而不是 digest 计算。这里用可复现的伪随机字节
// （固定种子，避免每次跑的数据不同）。
func benchBigRepo(t *testing.T) (proj string, size string) {
	t.Helper()

	r := testutils.NewGitRepo(t)
	const (
		files   = 100
		perFile = 1 << 20 // 1 MiB
	)
	rng := uint64(0x9E3779B97F4A7C15)
	next := func() byte {
		// xorshift64*：确定、快、无需引入 crypto/rand 的熵开销
		rng ^= rng >> 12
		rng ^= rng << 25
		rng ^= rng >> 27
		return byte((rng * 0x2545F4914F6CDD1D) >> 56)
	}
	buf := make([]byte, perFile)
	for i := 0; i < files; i++ {
		for j := range buf {
			buf[j] = next()
		}
		r.WriteFile(fmt.Sprintf("blob/%03d.bin", i), string(buf))
	}
	r.Commit("feat: big")
	r.Tag("v1.0.0", false)
	seedMirror(t, "github:bench/big", r.Dir)

	proj = t.TempDir()
	if code, out := runCaptureCode(t, "init", "github.com:bench/bigapp", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	if code, out := runCaptureCode(t, "add", "github:bench/big@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	return proj, fmt.Sprintf("%d MiB", files*perFile/(1<<20))
}
