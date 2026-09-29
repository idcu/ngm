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
