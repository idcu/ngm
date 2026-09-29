package supplychain

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/idcu/ngm/internal/git"
)

// AuditFinding 是单个依赖的审计结果。
type AuditFinding struct {
	Name    string `json:"name"`
	Ref     string `json:"ref"`
	RefType string `json:"refType"`
	Commit  string `json:"commit"`
	SubPath string `json:"subPath,omitempty"`
	Vulns   []Vuln `json:"vulnerabilities"`
}

// AuditReport 是 `ngm audit` 的完整报告。
//
// `ExitCode` 只在本包裁定 `0` / `1`（策略判定）；`3`、`4` 作为**错误**返回而不进报告——
// "发现了漏洞"与"查询没跑成"是两件事，混在同一个数字里 CI 无法区分。
type AuditReport struct {
	GeneratedAt     string         `json:"generatedAt"`
	Dependencies    int            `json:"dependencies"`
	Vulnerabilities int            `json:"vulnerabilities"`
	Ignored         int            `json:"ignoredByPolicy"`
	BySeverity      map[string]int `json:"bySeverity,omitempty"`
	CoverageNote    string         `json:"coverageNote"`
	Findings        []AuditFinding `json:"findings"`
	ExitCode        int            `json:"exitCode"`
}

// Marshal 输出机器可读报告（`--json`）。
func (r *AuditReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Render 输出人类可读报告，结构对齐 architecture/supply-chain.md §audit 的输出示例。
//
// 两处刻意的取舍：
//   - **通过的依赖也会列出**（`✓ ... No known vulnerabilities`）。审计的说服力来自"查了什么"，
//     只列问题会让人无法判断覆盖面。
//   - **覆盖局限必须渲染出来**。不写它，「审计通过」就会被读成「没有漏洞」，
//     而实际含义只是「库里没有关于这个 commit 的记录」。
func (r *AuditReport) Render(w io.Writer) {
	fmt.Fprintf(w, "ngm audit report (%s)\n", r.GeneratedAt)
	fmt.Fprintf(w, "==========================================\n\n")
	fmt.Fprintf(w, "Dependencies: %d\n", r.Dependencies)
	fmt.Fprintf(w, "Vulnerabilities: %d\n\n", r.Vulnerabilities)

	for _, f := range r.Findings {
		where := ""
		if f.SubPath != "" {
			where = "#" + f.SubPath
		}
		if len(f.Vulns) == 0 {
			fmt.Fprintf(w, "✓ %s%s@%s (%s) → %s\n", f.Name, where, f.Ref, f.RefType, git.ShortSHA(f.Commit))
			fmt.Fprintf(w, "  No known vulnerabilities\n\n")
			continue
		}
		fmt.Fprintf(w, "✗ %s%s@%s (%s) → %s\n", f.Name, where, f.Ref, f.RefType, git.ShortSHA(f.Commit))
		for _, v := range f.Vulns {
			fmt.Fprintf(w, "  %s: %s (%s)\n", v.Severity, v.Summary, v.ID)
			if v.FixedIn != "" {
				fmt.Fprintf(w, "  Fixed in: %s\n", v.FixedIn)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "---\n\n")
	fmt.Fprintf(w, "Summary:\n")
	if r.Vulnerabilities == 0 {
		fmt.Fprintf(w, "  no known vulnerabilities for the audited commits\n")
	} else {
		fmt.Fprintf(w, "  %d vulnerabilities (%s)\n", r.Vulnerabilities, severitySummary(r.BySeverity))
	}
	if r.Ignored > 0 {
		// 被忽略的条数必须报出来：否则"忽略 LOW"看起来像"没有 LOW"
		fmt.Fprintf(w, "  %d ignored by osvIgnoreSeverities\n", r.Ignored)
	}
	fmt.Fprintf(w, "\n  Note: %s\n", CoverageNote)
}

// severitySummary 按严重度从高到低渲染计数。
func severitySummary(by map[string]int) string {
	order := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"}
	parts := make([]string, 0, len(order))
	for _, s := range order {
		if n, ok := by[s]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	// 兜底：出现未知级别时也要显示，不能静默丢掉
	for s, n := range by {
		if n <= 0 || containsString(order, s) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, s))
	}
	sort.Strings(parts)
	return joinStrings(parts, ", ")
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
