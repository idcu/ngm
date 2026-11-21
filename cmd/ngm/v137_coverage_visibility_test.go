package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I1（2026-11-20 评估）：跨包覆盖率此前只守 verify 一个包，其余产品包的覆盖率
// 不可见、下滑无人察觉。修复是在**同一次 go test**里把 coverpkg 扩到 ./internal/...，
// 门槛读数从 profile 过滤 verify 语句块算出（数值与旧口径一致），其余包发 ::notice
// 只报告不设闸——不给"判据网过剩"再加一道闸。
//
// 本判据是 workflow 静态检查：CI 的 annotation 要登录才看得全，而这四条结构不变量
// 必须在源码层就守住，否则下次改 workflow 会静默退化成"只测 verify"或"悄悄加闸"。
func TestV137CoverageVisibilityKeepsGateAndAddsNotices(t *testing.T) {
	ci := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml"))
	if ci == "" {
		t.Fatal("ci.yml is empty — this guard would silently pass")
	}

	mustContain := []struct {
		frag string
		why  string
	}{
		{"-coverpkg=./internal/...", "coverpkg must instrument every internal package (single test run)"},
		{"pkgcov 'internal/core/verify'", "the floor must still be computed from verify lines only"},
		{"floor=78", "the calibrated verify floor must remain (calibrated on Linux 2026-11-19)"},
		{"::notice title=coverage ", "per-package visibility notices must be emitted"},
		{"::error::internal/core/verify coverage", "the hard floor gate must remain"},
		{"/internal/core/verify/", "profile must be checked to contain verify statements"},
		// 关键口径守卫：读数必须交给 go tool cover 合并重复块。用 awk 直接求和会把
		// ./... 下每个测试二进制重发的语句块重复计入，实测从 76.5% 塌到 3.8% —— 门槛误红。
		{"go tool cover -func=\"$filtered\"", "coverage must be computed by go tool cover, which merges duplicate blocks"},
		{"grep -F \"/${prefix}/\" \"$cov\"", "the per-package filter must match package paths exactly"},
	}
	for _, m := range mustContain {
		if !strings.Contains(ci, m.frag) {
			t.Errorf("ci.yml missing %q — %s", m.frag, m.why)
		}
	}

	// 不能出现"每包一个 floor"——那正是停止清单禁止的新契约网。
	// 审计 T-06：旧写的 `Contains("::error::") && Count(verify)==0` 在 mustContain 已强制
	// verify gate 的前提下恒假，新增逐包 floor 照样绿。现在枚举所有 error 行的路径。
	floors := map[string]bool{}
	for _, line := range strings.Split(ci, "\n") {
		at := strings.Index(line, "::error::")
		if at < 0 {
			continue
		}
		rest := line[at+len("::error::"):]
		if end := strings.Index(rest, " coverage"); end >= 0 && strings.Contains(rest[:end], "/") {
			// 只认 `::error::<path> coverage ... is below` 这种 floor 行；
			// "::error::no coverage profile" 不是路径门槛（审计 T-06 实测）。
			floors[strings.TrimSpace(rest[:end])] = true
		}
	}
	if len(floors) != 1 || !floors["internal/core/verify"] {
		t.Errorf("the only coverage gate must be internal/core/verify, got %v — "+
			"per-package notices must not set floors", floors)
	}
	// 退化守卫要匹配 broaden 后的值，否则它永远红/永远绿都看不见。
	if !strings.Contains(ci, `of statements in \./internal/\.\.\.`) {
		t.Errorf("coverage degradation guard still greps the old verify-only string")
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
