package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// 报告式失败也要说话。
//
// v0.31 量出：非零退出里有**两种**形态——错误文本（走 v0.30 的契约：必须带 `hint:`）
// 与**报告**（`verify` 的逐条落地检查、`audit` 的漏洞清单）。
// 当时对报告只断言了"它存在"。这一版补上它该说的话：
//
//	**每一条失败项（`✗`）都要配一行"接下来做什么"。**
//
// 两种约定都算：`→ …`（verify 用它给 driftKind 与建议）
// 与 `Fixed in: <版本>`（audit 用它给出修复版本）。判据不要求它们长得一样，
// 只要求**每一条失败项都有**——这正是实测里缺的那一格：
// 公告**没有**记录修复版本时，audit 的报告里一句话都没有。
var reActionLine = regexp.MustCompile(`(?m)^\s*(?:→ |Fixed in: )`)

// failMark 是"这一项失败了"的记号。
//
// 判据只数**记号本身**，不要求它在行首：`verify` 与 `audit` 把 `✗` 写在行首，
// 而 `tree` 把它写在行中（`… → 2b47c7a ✗ 1 known vuln(s)`）。
// 第一版按行首匹配，于是 tree 的报告被判成"没有失败项"——
// **判据不该要求版式统一，只该要求它要说的话都在**。
const failMark = "✗"

type reportCase struct {
	name  string
	setup func(t *testing.T, home string) string
	args  []string
}

var reportCases = []reportCase{
	{"verify/依赖的树被删", func(t *testing.T, home string) string {
		p := newProject(t)
		seedDep(t, p)
		if err := os.RemoveAll(filepath.Join(p, "ngm.vendor")); err != nil {
			t.Fatal(err)
		}
		return p
	}, []string{"verify"}},

	{"verify/依赖的字节被改", func(t *testing.T, home string) string {
		p := newProject(t)
		seedDep(t, p)
		writeSurfaceFile(t, p, filepath.Join("ngm.vendor", "github.com", "x", "dep", "index.ts"),
			"export const tampered = 1\n")
		return p
	}, []string{"verify"}},

	{"verify/tag 被挪走", func(t *testing.T, home string) string {
		return buildFixture(t, home, setupDrift)
	}, []string{"verify"}},

	{"audit/公告没有修复版本", func(t *testing.T, home string) string {
		return buildFixture(t, home, setupOsvVuln)
	}, []string{"audit", "--no-cache"}},

	{"audit/公告带修复版本", func(t *testing.T, home string) string {
		p := newProject(t)
		seedDep(t, p)
		srv := osvStub(t, `{"vulns":[{"id":"GHSA-withfix","summary":"Prototype pollution",`+
			`"severity":"HIGH","affected":[{"ranges":[{"events":[{"fixed":"1.2.4"}]}]}]}]}`)
		t.Setenv("NGM_OSV_URL", srv.URL)
		grantNetFor(t, home, srv)
		return p
	}, []string{"audit", "--no-cache"}},

	{"tree/--osv 撞上漏洞", func(t *testing.T, home string) string {
		return buildFixture(t, home, setupOsvVuln)
	}, []string{"tree", "--osv"}},

	{"install/verifyOnLock 且上游漂移", func(t *testing.T, home string) string {
		return buildFixture(t, home, setupDrifted)
	}, []string{"install"}},
}

// TestV32EveryFailureReportSaysWhatToDoNext 断言"报告式失败"给出了下一步。
//
// 三条断言：
//
//	① 每条用例都真的**失败**（否则它测的不是报告）；
//	② 每条用例**至少一条** `✗` 失败项；
//	③ **行动行数 ≥ 失败项数**——每一个失败项都要有自己那行"接下来做什么"。
//
// 第 ③ 条写成"≥ 条数"而不是"逐块配对"：报告是给人看的，
// 逐块解析会把判据绑死在缩进上；而"条数对齐"已经能抓住**整块缺行动行**这种真缺陷
// （v0.32 实测的就是这一种：公告没有修复版本时，整条发现项没有行动行）。
func TestV32EveryFailureReportSaysWhatToDoNext(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	conventions := map[string]int{} // 行动行约定 → 出现次数
	items, actions := 0, 0

	for _, rc := range reportCases {
		t.Run(rc.name, func(t *testing.T) {
			home := isolateUserEnv(t)
			dir := rc.setup(t, home)
			args := append(append([]string{}, rc.args...), "--dir="+dir)

			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)
			text := out.String() + errb.String()

			if code == 0 {
				t.Fatalf("this case is meant to fail and report, but exited 0:\n%s", text)
			}
			// 报告式失败的特征：没有错误前缀，但有逐条 `✗`。
			if reErrPrefix.MatchString(errb.String()) {
				t.Fatalf("this case produced error text, not a report — the wrong net owns it:\n%s", errb.String())
			}
			nItems := strings.Count(text, failMark)
			nActions := len(reActionLine.FindAllString(text, -1))
			items += nItems
			actions += nActions

			for _, m := range reActionLine.FindAllString(text, -1) {
				conventions[strings.TrimSpace(m)]++
			}
			if nItems == 0 {
				t.Fatalf("a failing report with no failing item (`✗`) in it:\n%s", text)
			}
			if nActions < nItems {
				t.Errorf("this report has %d failing item(s) but only %d next-step line(s) — "+
					"a user reading it learns what is wrong but not what to do:\n%s", nItems, nActions, text)
			}
		})
	}

	// 可达性守卫：表不能缩水，且**两种行动行约定都要被走到**——
	// 只走一种的话，"另一种哪天整块没了"这张网看不见。
	if len(reportCases) < 7 {
		t.Fatalf("the table shrank to %d cases — cases were removed", len(reportCases))
	}
	if conventions["→"] == 0 || conventions["Fixed in:"] == 0 {
		t.Fatalf("only these action-line conventions were reached: %v — "+
			"a report net that never meets one of them cannot notice it going missing", conventions)
	}
	t.Logf("failure reports: %d failing items, %d next-step lines, conventions: %v",
		items, actions, conventions)
}
