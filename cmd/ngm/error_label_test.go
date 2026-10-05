package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV23EngineFailureNamesItself 固定一条**用户可见**的诚实性：
// 引擎跑了但失败时，错误前缀必须说"引擎失败"，而不是 verify 那套"引用漂移"。
//
// 背景（v0.22 实测）：退出码 1 是**一个数字四种含义**——verify 的引用漂移、
// audit 的超阈值漏洞、audit 钩子否决、以及引擎运行失败。而 `CodeRefDrift`
// 是它的默认名，于是 `ngm typecheck` 失败时用户读到的是：
//
//	RefDrift: typeCheck: fake: … failed
//
// 跑 typecheck 的人会去找"漂移"，而实际发生的事是引擎退出非零。
// 修法是给错误加一个**只改显示名**的覆盖（`errs.NgmError.Labeled`）：
// 数值契约（退出码、--json）一字不动，换的只有给人看的那个词。
//
// 这张网守两件事：**名字必须对上**（含 EngineFailed、不含 RefDrift），
// 且**数值不许跟着名字变**（仍然退 1）。
func TestV23EngineFailureNamesItself(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	isolateUserEnv(t)
	proj := newProject(t)
	writeEngineCatalog(t, proj, engineEntry{
		Name: "fake", Kind: "typeCheck", Adapter: "subprocess", Command: fake,
	})
	t.Setenv("FAKE_EXIT", "1")

	var out, errb bytes.Buffer
	code := dispatch([]string{"typecheck", "--engine=fake", "--dir=" + proj}, &out, &errb)

	if code != 1 {
		t.Fatalf("an engine that ran and failed must still exit 1, got %d:\n%s", code, errb.String())
	}
	msg := errb.String()
	if strings.Contains(msg, "RefDrift") {
		t.Errorf("the error calls this a ref drift, but the engine simply failed:\n%s", msg)
	}
	if !strings.Contains(msg, "EngineFailed") {
		t.Errorf("the error should name what actually happened (EngineFailed):\n%s", msg)
	}

	// 对照：新的显示名**不许泄漏到别的路径**上。
	//
	// 注意 verify 的漂移是**走报告**的（stdout 里那一行 ✗，退出码 1），
	// 不是走错误前缀——所以这里该断言的是"它没有把 EngineFailed 带上"，
	// 而不是"它显示了 RefDrift"（那个词在 verify 路径上本来就不出现）。
	isolateUserEnv(t)
	r := scUpstream(t, "github:z/label", "export const a = 1\n", "")
	drift := newProject(t)
	if c, o := runCaptureCode(t, "add", "github:z/label@v1", "--ref-type=tag", "--dir="+drift); c != 0 {
		t.Fatalf("add: %s", o)
	}
	if c, o := runCaptureCode(t, "install", "--dir="+drift); c != 0 {
		t.Fatalf("install: %s", o)
	}
	r.WriteFile("index.ts", "export const a = 2\n")
	r.Commit("fix: move the tag")
	r.Exec("tag", "-f", "v1")

	var o2, e2 bytes.Buffer
	if c := dispatch([]string{"verify", "--dir=" + drift}, &o2, &e2); c != 1 {
		t.Fatalf("a moved tag must exit 1, got %d:\n%s", c, e2.String())
	}
	if combined := o2.String() + e2.String(); strings.Contains(combined, "EngineFailed") {
		t.Errorf("the engine label must not leak into verify's report:\n%s", combined)
	}
}
