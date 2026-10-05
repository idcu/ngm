package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.36：**扫描，而不是挑选。**
//
// v0.32 立报告契约时，用例是我**手工挑的 9 条**——挑的依据是"我知道这几个命令会出报告"。
// 这一版换一种做法：把**全部非零用例**（退出码表的 43 条 + 错误面表的 14 条）都跑一遍，
// 凡输出里出现 `✗` 的，都必须带下一步。
//
// 为什么值得换：**扫描比挑选更接近全称，而且不需要我猜哪里会有报告。**
// 实测立刻抓到一个我没想起来要看的：`ngm engines validate` 报 7 个问题、退 5，
// 一行下一步都没有。
//
// 与 v0.32 的关系：v0.32 是**深度**（每条失败项都要有自己那行，逐条对齐条数），
// 这一张是**广度**（每一个产生报告的非零用例都至少有一行）。两者都要：
// 广度覆盖我没想到的地方，深度保证"有"不等于"够"。
var sweepExcluded = map[string]string{
	// 这张表应当**永远为空**。留在这里是为了让"暂时排除某条"有一个被看见的位置：
	// 一旦有人往里加东西，diff 里就会多一行带理由的排除。
}

func TestV36EveryFailingReportCarriesANextStep(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	type sweepCase struct {
		label string
		run   func(t *testing.T) (int, string)
	}
	var cases []sweepCase

	for _, c := range exitCodeMeasured {
		if c.code == 0 {
			continue
		}
		c := c
		cases = append(cases, sweepCase{
			label: c.cmd + " (exit " + itoa(c.code) + ")",
			run: func(t *testing.T) (int, string) {
				home := isolateUserEnv(t)
				args := append([]string{c.cmd}, c.args...)
				if !c.noDir {
					args = append(args, "--dir="+buildFixture(t, home, c.setup))
				}
				for k, v := range c.env {
					t.Setenv(k, v)
				}
				return runCaptureCode(t, args...)
			},
		})
	}
	for _, c := range surfaceCases {
		if c.expectZero != "" {
			continue
		}
		c := c
		cases = append(cases, sweepCase{
			label: "surface/" + c.name,
			run: func(t *testing.T) (int, string) {
				isolateUserEnv(t)
				dir := c.setup(t)
				return runCaptureCode(t, append(append([]string{}, c.args...), "--dir="+dir)...)
			},
		})
	}
	if len(cases) < 55 {
		t.Fatalf("only %d non-zero cases are in scope — the sweep has shrunk to something "+
			"that can no longer notice much", len(cases))
	}

	failing, reports := 0, 0
	for _, sc := range cases {
		t.Run(sc.label, func(t *testing.T) {
			code, text := sc.run(t)
			if code == 0 {
				t.Fatalf("this case is meant to fail, but exited 0:\n%s", text)
			}
			failing++
			if why, ok := sweepExcluded[sc.label]; ok {
				t.Skipf("excluded: %s", why)
			}

			body := reportBody(text)
			if !strings.Contains(body, failMark) {
				return // 错误文本、或没有逐条失败项的输出：不套这条契约
			}
			reports++
			if len(reActionLine.FindAllString(body, -1)) == 0 {
				t.Errorf("this failing report lists items (`%s`) but says nothing about what to do next:\n%s",
					failMark, text)
			}
		})
	}

	// 可达性守卫：**扫描必须真的扫到了东西**。
	// 若哪天所有失败都变成错误文本，这张网会静默地变成"检查了 57 个用例、什么都没查"。
	if reports < 8 {
		t.Fatalf("only %d case(s) produced a failing report across %d non-zero cases — "+
			"either reports vanished or this sweep is looking at the wrong thing",
			reports, failing)
	}
	t.Logf("sweep: %d non-zero cases, %d produced a failing report, all of them carry a next step",
		failing, reports)
}

// reportBody 去掉**用法文本**再判。
//
// 为什么必须去掉：用法文本里那一段 MARKERS 就是在**解释记号本身**
// （`✗  known vulnerabilities …`）。第一版扫描没做这一步，于是把
// `tree` 的一条用法错误（"flag provided but not defined"）误报成
// "有失败项却没有下一步"——**判据自己造了一个假缺陷**。
//
// 截断点取 USAGE: / MARKERS:：两者都只出现在用法文本里。
func reportBody(text string) string {
	cut := len(text)
	for _, marker := range []string{"USAGE:", "MARKERS:"} {
		if i := strings.Index(text, marker); i >= 0 && i < cut {
			cut = i
		}
	}
	return text[:cut]
}
