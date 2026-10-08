package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// failWriter 是"永远写不出去"的 stdout——**码 6 可测量**的关键。
//
// `dispatch` 接受任意 `io.Writer`，于是不必真去弄坏磁盘或管道：
// 传一个永远返回错误的 writer，命令就会算完结论、写报告时失败。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("stdout 不收了") }

// TestV22StdoutWriteFailureIsMeasured 是**码 6 的专属测量**（ADR-026）。
//
// V22 的规矩是：**声明了的退出码必须被测量**（或别处测量、或记进缺口表）。
// `verify` / `audit` / `outdated` / `tree` / `engines` / `integrations` 的
// usage 里都写了 `6  internal failure`，于是它们必须在 `exitCodeElsewhere`
// 里指到本测试的对应子测试——而 v0.56 那条判据会核对指针**真的指到东西**。
//
// 断言两件事：① 退出码是 **6**；② stderr 上那句话说明是"写不出去"（不是别的失败）。
func TestV22StdoutWriteFailureIsMeasured(t *testing.T) {
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

	// 子测试名**写成字面量**（而不是 `t.Run(c.label, …)`）：`exitCodeElsewhere` 里的指针
	// 是 `TestV22StdoutWriteFailureIsMeasured/<名字>`，而 `TestV56RegisteredPointersResolve`
	// 从**源码**核对那个名字真的存在——表驱动的名字它看不见（这一版实测到的）。
	t.Run("verify", func(t *testing.T) {
		measureInternalFailure(t, "verify", "--json", "--dir="+p)
	})
	t.Run("why", func(t *testing.T) {
		measureInternalFailure(t, "why", "github:x/dep", "--json", "--dir="+p)
	})
	t.Run("outdated", func(t *testing.T) {
		measureInternalFailure(t, "outdated", "--json", "--dir="+p)
	})
	t.Run("tree", func(t *testing.T) {
		measureInternalFailure(t, "tree", "--json", "--dir="+p)
	})
	t.Run("engines", func(t *testing.T) {
		measureInternalFailure(t, "engines", "list", "--dir="+p)
	})
}

// measureInternalFailure 跑一次"写不出去"的命令并要求它退 **6**。
func measureInternalFailure(t *testing.T, args ...string) {
	t.Helper()
	var errb bytes.Buffer
	code := dispatch(args, failWriter{}, &errb)
	if code != 6 {
		t.Fatalf("`ngm %s`（stdout 永远写失败）退 %d，想要 **6**（内部失败）:\n%s",
			strings.Join(args, " "), code, errb.String())
	}
	if !strings.Contains(errb.String(), "不收了") {
		t.Errorf("stderr 没说清是「写不出去」这一类:\n%s", errb.String())
	}
}
