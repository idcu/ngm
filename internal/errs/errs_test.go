package errs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		code Code
		want int
	}{
		{CodeRefDrift, 1},
		{CodeDigestMismatch, 2},
		{CodeConfigInvalid, 3},
		{CodeGitFetch, 4},
		{CodeEngineNotFound, 5},
	}
	for _, tc := range cases {
		if got := tc.code.ExitCode(); got != tc.want {
			t.Errorf("%s.ExitCode()=%d, want %d", tc.code, got, tc.want)
		}
	}
}

func TestNewAndWrap(t *testing.T) {
	base := errors.New("boom")
	wrapped := Wrap(CodeConfigInvalid, "bad ngm.json", "run `ngm config validate`", base)
	if wrapped.Code != CodeConfigInvalid {
		t.Fatalf("code=%v", wrapped.Code)
	}
	if !errors.Is(wrapped, base) {
		t.Fatalf("errors.Is should reach cause")
	}
	if ExitCode(wrapped) != 3 {
		t.Fatalf("ExitCode=%d", ExitCode(wrapped))
	}
	if ExitCode(nil) != 0 {
		t.Fatalf("ExitCode(nil)=%d", ExitCode(nil))
	}
	if ExitCode(errors.New("plain")) != 1 {
		t.Fatalf("plain err should map to 1")
	}
}

func TestFormatHuman(t *testing.T) {
	base := errors.New("io: closed")
	e := Wrap(CodeGitFetch, "fetch failed", "check `git credential`", base)
	out := FormatHuman(e)
	for _, want := range []string{"GitFetch", "fetch failed", "io: closed", "hint:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := FormatHuman(nil); got != "" {
		t.Errorf("nil err should give empty, got %q", got)
	}
	plain := FormatHuman(fmt.Errorf("x"))
	if plain != "error: x" {
		t.Errorf("plain format=%q", plain)
	}
}

// TestLabeledOnlyChangesTheDisplayName 固定 Labeled 的边界：
// **换的只有给人看的那个词**——码、退出码、可解包性一字不动。
//
// 为什么需要它：退出码 1 至少有四种来源（verify 漂移 / audit 超阈值 /
// audit 钩子否决 / 引擎运行失败），而一个数字只能有一个默认名。
// 默认名落在哪个来源上，其余三个就会读到**与自己无关的词**。
func TestLabeledOnlyChangesTheDisplayName(t *testing.T) {
	base := errors.New("engine exit 1")
	e := Wrap(CodeRefDrift, "typeCheck: fake: boom", "fix it", base).Labeled("EngineFailed")

	if e.Code != CodeRefDrift || ExitCode(e) != 1 {
		t.Fatalf("Labeled must not touch the numeric contract: code=%v exit=%d", e.Code, ExitCode(e))
	}
	if !errors.Is(e, base) {
		t.Fatal("Labeled must keep errors.Is reachable (it returns a shallow copy, not a new error)")
	}
	var ne *NgmError
	if !errors.As(e, &ne) {
		t.Fatal("Labeled must stay an *NgmError for errors.As")
	}
	if got := FormatHuman(e); !strings.HasPrefix(got, "EngineFailed: ") {
		t.Errorf("the human line must start with the label, got:\n%s", got)
	}
	if got := e.Error(); !strings.HasPrefix(got, "EngineFailed: ") {
		t.Errorf("Error() must use the label too, got:\n%s", got)
	}
	if strings.Contains(e.Error(), "RefDrift") {
		t.Errorf("the default name must not leak once a label is set:\n%s", e.Error())
	}

	// 原错误不受影响（Labeled 返回副本）。
	orig := Wrap(CodeRefDrift, "verify: tag moved", "", nil)
	if got := FormatHuman(orig); !strings.HasPrefix(got, "RefDrift: ") {
		t.Errorf("the unlabeled error must keep the code's default name, got:\n%s", got)
	}

	// 空标签等价于不设标签（不是"名字变成空"）。
	if got := FormatHuman(Wrap(CodeGitFetch, "x", "", nil).Labeled("")); !strings.HasPrefix(got, "GitFetch: ") {
		t.Errorf("an empty label must fall back to the code name, got:\n%s", got)
	}
}
