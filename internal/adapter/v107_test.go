package adapter

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/boundbuf"
)

// 探针：`NGM_BIG_OUTPUT=<n>` 时把 n 个字节写到 stdout 后退出。
// 复用 v105 的 TestMain（那儿已经处理了 NGM_ENV_PROBE）。
func init() {
	n, _ := strconv.Atoi(os.Getenv("NGM_BIG_OUTPUT"))
	if n <= 0 {
		return
	}
	chunk := []byte(strings.Repeat("x", 64<<10))
	written := 0
	for written < n {
		m := n - written
		if m > len(chunk) {
			m = len(chunk)
		}
		if _, err := os.Stdout.Write(chunk[:m]); err != nil {
			break
		}
		written += m
	}
	os.Exit(0)
}

// v0.107：诊断通道与产物通道的上界必须不同。
//
// 缺陷形态（本版自己差点引入）：把 stdout 一律按 1 MiB 截断。对 `tsc` 的 warning
// 那没问题；但 `ngm transform` / `ngm css` / 不带 `--outfile` 的 `ngm build` 里
// **stdout 就是产物**——按 1 MiB 截断等于静默改坏用户的构建产物。
// 所以这里断言两件事：诊断通道确实有界，产物通道确实是另一个量级。
func TestV107ArtifactChannelHasItsOwnLimit(t *testing.T) {
	if artifactLimit <= boundbuf.DefaultLimit {
		t.Fatalf("artifactLimit (%d) must be larger than the diagnostic limit (%d): "+
			"stdout is the product for transform/css/build-without-outfile",
			artifactLimit, boundbuf.DefaultLimit)
	}
}

func TestV107DiagnosticCaptureIsBoundedAndSaysSo(t *testing.T) {
	t.Setenv("NGM_BIG_OUTPUT", "65536")

	// 小上界：必须截断、必须带标记、必须仍报告"写成功"。
	res, err := runProcessEnvLimit(context.Background(), os.Args[0], nil, "", nil, nil, nil, 1024)
	if err != nil {
		t.Fatalf("runProcessEnvLimit: %v", err)
	}
	if !res.Truncated {
		t.Error("a 64 KiB output under a 1 KiB cap must report truncation")
	}
	if !strings.Contains(string(res.Stdout), "output truncated") {
		t.Error("the captured text must say it was cut — silent truncation makes " +
			"`the output is this` indistinguishable from `we kept only this`")
	}

	// 大上界（产物通道同款）：不许截断，且字节数原样。
	res2, err := runProcessEnvLimit(context.Background(), os.Args[0], nil, "", nil, nil, nil, artifactLimit)
	if err != nil {
		t.Fatalf("runProcessEnvLimit(artifactLimit): %v", err)
	}
	if res2.Truncated {
		t.Error("64 KiB must not be truncated by the artifact limit")
	}
	if len(res2.Stdout) != 65536 {
		t.Errorf("captured %d bytes, want 65536 intact", len(res2.Stdout))
	}
}

// stderr 始终用诊断上界：它是给人看的，不该跟着产物一起放大。
func TestV107StderrKeepsTheDiagnosticLimit(t *testing.T) {
	t.Setenv("NGM_BIG_OUTPUT", "65536")

	res, err := runProcessEnvLimit(context.Background(), os.Args[0], nil, "", nil, nil, nil, artifactLimit)
	if err != nil {
		t.Fatalf("runProcessEnvLimit: %v", err)
	}
	// stdout 完整（产物通道），stderr 为空（探针不写它）。
	if len(res.Stderr) != 0 {
		t.Errorf("stderr = %q, want empty", res.Stderr)
	}
}
