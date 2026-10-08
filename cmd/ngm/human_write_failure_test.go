package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.60：**两条通道对"写不出去"必须判法一致**。
//
// 起点是 v0.59 的一条新发现（它的探针量到的）：`ngm verify`（人读）在 stdout
// 写失败时退 **0** ✗——报告只写出去一半，而退出码说成功。
// 这一版先量清全貌：
//
//	ngm verify / why / outdated / tree（人读，stdout 永远写失败）→ exit 0 · stderr 空
//	ngm engines list（同条件）                                    → exit 6 · write listing: …
//	ngm --help（同条件）                                          → exit 0 · stderr 空
//
// 也就是**同一条通道里判法不一致** ✗：`engines` 检查了 `tw.Flush()`，
// 其余把 `fmt.Fprintf` 的错误丢了；而 JSON 路径**一律**检查 ✓。
//
// 处置在**唯一入口**（`dispatch` 的包装）包一层记账 writer，命令返回后统一折算：
// 命令**本来成功**（码 0）而 stdout 写失败 ⇒ 码 **6**（ADR-026：结论有了却送不出去）。
//
// 判据两半：
//
//	① 六个场景（5 个命令的人读路径 + `--help`）都必须是 **6**；
//	② **对照**：同一组命令在正常 stdout 下必须仍然退 **0**
//	   （否则"什么都退 6"也能让 ① 通过）。
func TestV60TheHumanChannelTreatsWriteFailureTheSameWay(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	isolateUserEnv(t)
	p := newProject(t)
	scUpstream(t, "github:x/dep", "export const dep = 1\n", "")
	if code, out := runCaptureCode(t, "add", "github:x/dep@v1", "--ref-type=tag", "--dir="+p); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+p); code != 0 {
		t.Fatalf("install: %s", out)
	}

	// ---- ① 人读路径 + 写失败 ⇒ 6 ----
	//
	// 子测试名写成字面量：`TestV56RegisteredPointersResolve` 从**源码**核对名字，
	// 表驱动的名字它看不见（v0.59 实测过一次）。
	t.Run("verify", func(t *testing.T) {
		humanWriteFailure(t, "verify", "--dir="+p)
	})
	t.Run("why", func(t *testing.T) {
		humanWriteFailure(t, "why", "github:x/dep", "--dir="+p)
	})
	t.Run("outdated", func(t *testing.T) {
		humanWriteFailure(t, "outdated", "--dir="+p)
	})
	t.Run("tree", func(t *testing.T) {
		humanWriteFailure(t, "tree", "--dir="+p)
	})
	t.Run("engines", func(t *testing.T) {
		// engines 自己检查 `tw.Flush()` —— 它即使走自己的分支，**结果必须一样**。
		humanWriteFailure(t, "engines", "list", "--dir="+p)
	})
	t.Run("help", func(t *testing.T) {
		humanWriteFailure(t, "--help")
	})

	// ---- ② 对照：正常 stdout ⇒ 仍退 0 ----
	controls := []struct {
		label string
		args  []string
	}{
		{"verify", []string{"verify", "--dir=" + p}},
		{"why", []string{"why", "github:x/dep", "--dir=" + p}},
		{"outdated", []string{"outdated", "--dir=" + p}},
		{"tree", []string{"tree", "--dir=" + p}},
		{"engines", []string{"engines", "list", "--dir=" + p}},
		{"help", []string{"--help"}},
	}
	for _, c := range controls {
		t.Run("对照/"+c.label, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := dispatch(c.args, &out, &errb); code != 0 {
				t.Fatalf("`ngm %s`（正常 stdout）退 %d，想要 0——"+
					"写失败的判据不该把「什么都退 6」也算通过:\n%s",
					strings.Join(c.args, " "), code, errb.String())
			}
			if out.Len() == 0 {
				t.Errorf("`ngm %s`（正常 stdout）什么都没写出来", strings.Join(c.args, " "))
			}
		})
	}

	t.Logf("human channel: 6 write-failure case(s) exit 6 · 6 control(s) still exit 0")
}

// humanWriteFailure 要求"人读路径 + 写不出去"退 **6**。
func humanWriteFailure(t *testing.T, args ...string) {
	t.Helper()
	var errb bytes.Buffer
	code := dispatch(args, failWriter{}, &errb)
	if code != 6 {
		t.Fatalf("`ngm %s`（stdout 永远写失败）退 %d，想要 **6**——"+
			"报告只写出去一半却报成功，是最坏的一种（与 JSON 路径的判法必须一致）:\n%s",
			strings.Join(args, " "), code, errb.String())
	}
	if strings.TrimSpace(errb.String()) == "" {
		t.Errorf("`ngm %s` 退了 6，却**一句解释都没有**——"+
			"人读那侧要说明是「写不出去」", strings.Join(args, " "))
	}
}
