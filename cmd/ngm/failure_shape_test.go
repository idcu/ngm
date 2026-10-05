package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.34：建议要**承认失败的形状**。
//
// v0.33 的判据是"行动行必须点名一个真能执行的东西"——但一行**与失败毫不相干**的
// `ngm install` 也能过。这一版把**失败的性质**写进判据：
//
//	因**漂移**失败 ⇒ 建议要指向 `ngm update`
//	因**字节被改**失败 ⇒ 建议要指向 `ngm install`
//	因**漏洞**失败 ⇒ 建议要指向 `ngm audit`
//
// 依据不是我的口味：`driftKind: expected` ⇒ `run ngm update` 是
// `observability.md` 的输出示例里**产品自己写下的**对应关系；
// 每一类失败的解法都由 `reportCases[].remedies` 显式登记。
//
// 判据形状：**这一条报告里的每一条行动行，都必须命中该 case 登记的解法之一。**
func TestV34AdviceMatchesTheFailureShape(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	shapes := map[string]int{} // 解法 → 命中次数
	lines := 0

	for _, rc := range reportCases {
		t.Run(rc.name, func(t *testing.T) {
			if len(rc.remedies) == 0 {
				t.Fatalf("this case registers no remedy — the judgement would pass vacuously for it")
			}
			home := isolateUserEnv(t)
			dir := rc.setup(t, home)
			args := append(append([]string{}, rc.args...), "--dir="+dir)

			var out, errb bytes.Buffer
			if code := dispatch(args, &out, &errb); code == 0 {
				t.Fatalf("this case is meant to fail and report, but exited 0:\n%s", out.String())
			}
			text := out.String() + errb.String()

			for _, line := range reActionLine.FindAllString(text, -1) {
				lines++
				hit := ""
				for _, r := range rc.remedies {
					if strings.Contains(line, r) {
						hit = r
						break
					}
				}
				if hit == "" {
					t.Errorf("this next-step line does not match **this** failure's shape "+
						"(expected one of %v):\n  %s", rc.remedies, strings.TrimSpace(line))
					continue
				}
				shapes[hit]++
			}
		})
	}

	// 可达性守卫：**形状不能被压缩成一种**。
	// 如果所有建议都变成同一句话，这张网会退化成 v0.33 那一张——它挡不住"文不对题"。
	if len(shapes) < 3 {
		t.Fatalf("only %d distinct remedy(ies) were reached (%v) — "+
			"advice that is the same line for every failure cannot be evidence of anything",
			len(shapes), shapes)
	}
	if lines < len(reportCases) {
		t.Fatalf("only %d next-step line(s) were inspected across %d cases — "+
			"every failing case must carry at least one", lines, len(reportCases))
	}
	t.Logf("failure shapes: %d cases, %d next-step lines, remedies hit: %v", len(reportCases), lines, shapes)
}
