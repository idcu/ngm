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
	// v0.59 · ADR-026：非 NgmError 的兜底从 1 改成 6（内部失败）——
	// 它是"错误没走 ngm 的错误模型"，与"结论算出来了却说不出去"同一类；
	// 而 1 的语义是**策略失败**（漂移 / 引擎 / 漏洞 / 钩子）。
	if got := ExitCode(errors.New("plain")); got != 6 {
		t.Fatalf("plain err should map to 6 (internal failure), got %d", got)
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

// TestV28HintSurvivesWrapping 固定：**可行动的建议不许在包装时丢掉。**
//
// 实测的缺陷（v0.28）：`FormatHuman` 只打印最外层那条错误的 hint。
// 内层带着"check your credentials"、外层 `Wrap` 时给了空 hint——
// 用户看到的就只有 `cause:` 一行，**建议消失了**。
//
// 修法是在**渲染**处沿着 `Cause` 链取第一条非空 hint：
// 一次覆盖全部构造点（全仓库 300 个里 129 个是空 hint 字面量），
// 且不改动错误数据（`--json` 与 `errors.As` 的消费者看不到变化）。
func TestV28HintSurvivesWrapping(t *testing.T) {
	inner := New(CodeGitFetch, "fetch failed", "check your credentials")

	one := Wrap(CodeConfigInvalid, "cannot read the lock", "", inner)
	if got := FormatHuman(one); !strings.Contains(got, "check your credentials") {
		t.Errorf("the inner hint must survive one Wrap with an empty hint:\n%s", got)
	}

	two := Wrap(CodeConfigInvalid, "outermost", "", one)
	if got := FormatHuman(two); !strings.Contains(got, "check your credentials") {
		t.Errorf("the inner hint must survive two levels of wrapping:\n%s", got)
	}

	// 自己的建议优先：内层的话不该盖住外层更贴切的下一步。
	own := Wrap(CodeConfigInvalid, "cannot read the lock", "run `ngm config validate`", inner)
	got := FormatHuman(own)
	if !strings.Contains(got, "run `ngm config validate`") {
		t.Errorf("the outer hint must win when it is present:\n%s", got)
	}
	if strings.Contains(got, "check your credentials") {
		t.Errorf("only **one** hint is shown, and it is the nearest one:\n%s", got)
	}

	// 整条链都没有建议时，**不许凭空造一句**。
	bare := Wrap(CodeConfigInvalid, "no lock", "", errors.New("io: closed"))
	if got := FormatHuman(bare); strings.Contains(got, "hint:") {
		t.Errorf("a chain with no hint must not grow one:\n%s", got)
	}

	// 标签仍然来自最外层（v0.23 的契约不许被这次改动碰掉）。
	if got := FormatHuman(one); !strings.HasPrefix(got, "ConfigInvalid: ") {
		t.Errorf("the label must still come from the outermost error:\n%s", got)
	}

	// Error() 一直不含 hint——这次改动也不许让它长出来。
	if got := two.Error(); strings.Contains(got, "hint") {
		t.Errorf("Error() is not the human renderer and must stay hint-free:\n%s", got)
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
