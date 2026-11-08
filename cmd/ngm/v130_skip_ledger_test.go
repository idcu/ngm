package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// skipSitePattern 匹配一次 t.Skip / t.Skipf 的调用形态（带左括号）。
//
// 只认**调用形态**：注释里提到 `t.Skip`（例如 v105 那句"禁止 t.Skip"）不算。
var skipSitePattern = regexp.MustCompile(`t\.Skipf?\(`)

// skipLedger 是 2026-11-07 那次审计的基线：每个测试文件里被登记的 skip 调用点数。
//
// 为什么需要这份账（P2-8）：仓库里有 59 处 `t.Skip`，其中绝大多数是**边界**
// （没有 deno/tsc/esbuild/node/symlink/exec-bit，或需要 opt-in 环境变量），
// 但"没有工具的机器上全 skip ⇒ CI 绿的是空集"这种失效形态只能靠门禁挡住。
// 逐个改造成断言不现实（外部工具确实可能缺失），所以采用**预算**：
//
//   - 新增一处 skip → 本测试红，要求作者**显式**决定它属于哪一类边界，
//     并在 docs/internals/test-skip-audit.md 里登记；
//   - 删除一处 skip → 也红（防止账本漂移成"只增不减"的摆设）；
//   - 审计时发现"不是边界、而是跳过掩盖漂移"的，改成真断言并从账本里减掉。
//
// 本轮已经从账本里拿掉 3 处这类 skip：resolve 的 `TestResolveRef_NoGitInPath`
// （函数体只有一句 t.Skip）、cmd 的 JSON 信封扫面（"某条错误面现在退 0"被静默跳过），
// 以及 `internal/core/git/waitdelay_test.go` 的 `sh` 依赖——那条墙钟用例已被
// "构造点字段断言"取代，不再需要外部 shell。因此基线是 56，不是 59。
var skipLedger = map[string]int{
	"cmd/ngm/acceptance_m3_test.go":                 2,
	"cmd/ngm/acceptance_m4_test.go":                 1,
	"cmd/ngm/acceptance_m5_test.go":                 1,
	"cmd/ngm/acceptance_m6_test.go":                 1,
	"cmd/ngm/acceptance_m7_test.go":                 2,
	"cmd/ngm/adapter_real_engines_test.go":          2,
	"cmd/ngm/bench_test.go":                         1,
	"cmd/ngm/engine_floor_test.go":                  3,
	"cmd/ngm/fixtures_gen_test.go":                  1,
	"cmd/ngm/fixtures_remote_test.go":               2,
	"cmd/ngm/integrations_acceptance_test.go":       3,
	"cmd/ngm/library_standard_checker_test.go":      2,
	"cmd/ngm/p1_regressions_test.go":                1,
	"cmd/ngm/real_upstream_test.go":                 2,
	"cmd/ngm/report_sweep_test.go":                  1,
	"cmd/ngm/sandbox_acceptance_test.go":            1,
	"cmd/ngm/transform_real_engine_test.go":         1,
	"cmd/ngm/typedecl_real_tsc_test.go":             1,
	"cmd/ngm/v088_powershell_scripts_test.go":       1,
	"cmd/ngm/v090_ps1_wrappers_run_test.go":         1,
	"cmd/ngm/v091_gitee_offline_test.go":            2,
	"cmd/ngm/v095_https_fetch_e2e_test.go":          1,
	"cmd/ngm/v102_diagnostic_test.go":               1,
	"cmd/ngm/verify_signatures_test.go":             1,
	"internal/adopt/adopt_test.go":                  1,
	"internal/core/atomicfile/atomicfile_test.go":   1,
	"internal/core/git/ancestry_test.go":            1,
	"internal/core/git/auth_test.go":                2,
	"internal/core/git/signature_test.go":           2,
	"internal/core/lock/v066_write_test.go":         1,
	"internal/core/vendor/content_legacy_test.go":   2,
	"internal/core/vendor/integrity_test.go":        2,
	"internal/core/vendor/links_test.go":            1,
	"internal/core/vendor/store_incomplete_test.go": 1,
	"internal/core/vendor/store_scale_test.go":      1,
	"internal/core/vendor/v105_test.go":             1,
	"internal/security/sandbox_test.go":             2,
	"internal/testutils/gitrepo_test.go":            3,
}

// TestV130EverySkipSiteIsAccountedFor 让每一处 `t.Skip` 都必须被审计过。
//
// 判据是"每个测试文件的 skip 调用点数 == 账本里的数"。不按行号记账：行号会
// 因为任何无关编辑漂移，那样门禁会在与 skip 无关的改动上变红——会误报的门禁
// 会被忽略（v0.8 复盘 §5.8）。个数能抓住"多了/少了一处 skip"，这正是需要
// 人解释的动作。
func TestV130EverySkipSiteIsAccountedFor(t *testing.T) {
	root := repoRoot(t)
	got := map[string]int{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "ngm.vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		got[rel] = len(skipSitePattern.FindAll(data, -1))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	var diffs []string
	for file, want := range skipLedger {
		if have := got[file]; have != want {
			diffs = append(diffs, fmt.Sprintf("%s: ledger %d, file has %d", file, want, have))
		}
	}
	for file, have := range got {
		if have == 0 {
			continue
		}
		if _, ok := skipLedger[file]; !ok {
			diffs = append(diffs, fmt.Sprintf("%s: not in the ledger, file has %d skip(s)", file, have))
		}
	}
	if len(diffs) > 0 {
		sort.Strings(diffs)
		t.Fatalf("the t.Skip inventory drifted from docs/internals/test-skip-audit.md:\n  %s\n"+
			"Decide whether each change is a real boundary (then register it in the audit doc "+
			"and raise this ledger) or a skip that hides a gap (then assert instead).",
			strings.Join(diffs, "\n  "))
	}

	total := 0
	for _, n := range skipLedger {
		total += n
	}
	t.Logf("skip ledger: %d sites across %d files", total, len(skipLedger))
}
