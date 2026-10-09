package supplychain

import (
	"encoding/json"
	"testing"
)

// v0.66：severity 的三种形态（评估报告 P2#12）。
//
// 此前的解析强假设 `severity` 是字符串，而 OSV 的现行 schema 是**对象数组**
// （`[{"type":"CVSS_V3","score":"…"}]`）⇒ 对真实响应整体 decode 失败报 exit 4。
// 修复后：任何形态都不得让整条公告消失——读不出等级就是 UNKNOWN。

func TestV66PickSeverity(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		db   string
		want string
	}{
		{"database_specific 优先（feed 的常见形态）", `[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]`, "HIGH", "HIGH"},
		{"字符串形态（旧 API / 既有测试替身）", `"MEDIUM"`, "", "MEDIUM"},
		{"数组 + 等级标签 score", `[{"type":"X","score":"CRITICAL"}]`, "", "CRITICAL"},
		{"数组 + 数值分数（CVSS 基础分）", `[{"type":"CVSS_V4","score":9.5}]`, "", "CRITICAL"},
		{"数组 + 低分", `[{"type":"CVSS_V4","score":2.0}]`, "", "LOW"},
		{"数组 + CVSS 向量：读不出等级，但不丢公告", `[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L"}]`, "", ""},
		{"null", `null`, "", ""},
		{"缺失（空 RawMessage）", ``, "", ""},
		{"未知形状（对象）", `{"weird":true}`, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickSeverity(json.RawMessage(tc.raw), tc.db); got != tc.want {
				t.Errorf("pickSeverity(%s, %q) = %q, want %q", tc.raw, tc.db, got, tc.want)
			}
		})
	}
}

// TestV66SeverityNeverDropsAWholeAdvisory：等级读不出来时，公告仍然要以
// UNKNOWN 的身份留下——**丢公告比丢等级严重得多**。
func TestV66SeverityNeverDropsAWholeAdvisory(t *testing.T) {
	body := []byte(`{
  "vulns": [{
    "id": "GHSA-xxxx-yyyy-zzzz",
    "summary": "example advisory",
    "severity": [{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]
  }]
}`)
	var parsed struct {
		Vulns []struct {
			ID               string          `json:"id"`
			Summary          string          `json:"summary"`
			Severity         json.RawMessage `json:"severity"`
			DatabaseSpecific struct {
				Severity string `json:"severity"`
			} `json:"database_specific"`
		} `json:"vulns"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("真实 schema 的响应必须能被解析: %v", err)
	}
	if len(parsed.Vulns) != 1 {
		t.Fatalf("公告数 = %d, want 1", len(parsed.Vulns))
	}
	sev := normalizeSeverity(pickSeverity(parsed.Vulns[0].Severity, parsed.Vulns[0].DatabaseSpecific.Severity))
	if sev != "UNKNOWN" {
		t.Errorf("读不出等级的公告应为 UNKNOWN（而不是让整条记录消失），got %q", sev)
	}
}

func TestV66ScoreToSeverity(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{10.0, "CRITICAL"}, {9.0, "CRITICAL"},
		{8.9, "HIGH"}, {7.0, "HIGH"},
		{6.9, "MEDIUM"}, {4.0, "MEDIUM"},
		{3.9, "LOW"}, {0.1, "LOW"},
		{0, ""},
	}
	for _, tc := range cases {
		if got := scoreToSeverity(tc.score); got != tc.want {
			t.Errorf("scoreToSeverity(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}
