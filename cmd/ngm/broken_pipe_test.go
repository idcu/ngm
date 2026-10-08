package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.61：**管道被打断是用户的意图，不是失败**。
//
// v0.60 把人读路径的写失败折算成码 6（"结论有了却送不出去"）——那是对的，
// 但它把 `ngm verify | head` 也变成了失败 ✗：用户想看的就是前几行。
//
// 判据的前提是**能可靠识别"对端关闭"**，而那个错误的形态是**平台相关**的。
// 本机实测（v0.61 探针）：
//
//	写一个读端已关闭的 os.Pipe →
//	  err = write |1: The pipe is being closed.   type = *fs.PathError
//	  errors.Is(err, syscall.EPIPE) = **false**     ← Unix 的判法在 Windows 上不管用
//	  errno = 232                                   ← ERROR_NO_DATA（syscall 里没有这个常量）
//
// 判据三半：
//
//	① 人读路径 + 对端关闭 ⇒ **安静退出**（命令本来退 0，那就还是 0）；
//	② 对照：人读路径 + **一般写失败** ⇒ 仍是 **6**（v0.60 的规则不许被这条例外吃掉）；
//	③ **有意的不对称**：JSON 路径 + 对端关闭 ⇒ **6**——要 JSON 的人多半在解析，
//	   半份 JSON 必须报失败（v0.16 规则①"不会出现半份"说的就是这件事）。
//	   它写成断言是为了让这个不对称**可查**，而不是悄悄不同。
func TestV61BrokenPipeIsIntentNotFailure(t *testing.T) {
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

	t.Run("人读/对端关闭", func(t *testing.T) {
		w := brokenPipeWriter(t)
		defer w.Close()
		var errb bytes.Buffer
		code := dispatch([]string{"verify", "--dir=" + p}, w, &errb)
		if code != 0 {
			t.Errorf("`ngm verify | head` 这种用法退了 %d，想要 **0**（安静退出）——"+
				"管道被打断是用户的意图:\n%s", code, errb.String())
		}
	})

	t.Run("人读/一般写失败（对照）", func(t *testing.T) {
		var errb bytes.Buffer
		code := dispatch([]string{"verify", "--dir=" + p}, failWriter{}, &errb)
		if code != 6 {
			t.Errorf("一般的写失败退了 %d，想要 **6**——例外不许把这条规则吃掉", code)
		}
	})

	t.Run("JSON/对端关闭（有意的不对称）", func(t *testing.T) {
		w := brokenPipeWriter(t)
		defer w.Close()
		var errb bytes.Buffer
		code := dispatch([]string{"verify", "--json", "--dir=" + p}, w, &errb)
		if code != 6 {
			t.Errorf("`ngm verify --json | …`（对端关闭）退了 %d，想要 **6**——"+
				"要 JSON 的人多半在解析，半份 JSON 必须报失败（v0.16 规则①）", code)
		}
	})

	t.Run("判定函数本身", func(t *testing.T) {
		if isBrokenPipe(nil) {
			t.Errorf("isBrokenPipe(nil) 说是对端关闭")
		}
		_, err := (failWriter{}).Write(nil)
		if err == nil {
			t.Fatalf("failWriter 竟然写成功了——这条对照没有对象")
		}
		if isBrokenPipe(err) {
			t.Errorf("isBrokenPipe(一般错误) 说是对端关闭——那会把真失败也吞掉")
		}
	})
}

// brokenPipeWriter 返回一个"对端已关闭"的写端：读端先关，再写就会得到对端关闭的错误。
func brokenPipeWriter(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return w
}
