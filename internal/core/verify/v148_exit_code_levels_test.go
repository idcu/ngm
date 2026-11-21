package verify

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/vendor"
)

// TestV148RenderLevelsFollowExitSemantics 固定审计 K-01：
// SARIF/GitHub 级别必须随 --allow-drift / --strict 的阻断语义变化，
// 不能只按 DriftKind 给出与进程退出码相反的结论。
func TestV148RenderLevelsFollowExitSemantics(t *testing.T) {
	t.Run("allow-drift downgrades unexpected", func(t *testing.T) {
		rep := sarifFixture()
		rep.AllowDrift = true
		results := decodeSARIF(t, mustRender(t, rep)).Runs[0].Results
		var found bool
		for _, r := range results {
			if r.RuleID == RuleRefUnexpected {
				found = true
				if r.Level != "warning" {
					t.Errorf("allow-drift unexpected level = %q, want warning", r.Level)
				}
			}
		}
		if !found {
			t.Fatal("the fixture's unexpected drift produced no SARIF result")
		}
		if out := RenderGitHub(rep); strings.Contains(out, "::error title=ngm: github:acme/moved ref") {
			t.Errorf("GitHub annotation still blocks an allowed drift:\n%s", out)
		}
	})

	t.Run("strict upgrades expected", func(t *testing.T) {
		rep := sarifFixture()
		rep.Dependencies[1].DriftKind = DriftExpected
		rep.Dependencies[1].Checks[0].Drift = DriftExpected
		rep.Strict = true
		results := decodeSARIF(t, mustRender(t, rep)).Runs[0].Results
		var found bool
		for _, r := range results {
			if r.RuleID == RuleRefExpected {
				found = true
				if r.Level != "error" {
					t.Errorf("strict expected level = %q, want error", r.Level)
				}
			}
		}
		if !found {
			t.Fatal("the expected drift produced no SARIF result")
		}
	})
}

// TestV149ForgedManifestIsTypedAsIntegrityFailure 固定审计 K-02：
// 不可信清单的 ValidateRecord 失败必须带类型，才能与"文件读不出来"分开归类。
func TestV149ForgedManifestIsTypedAsIntegrityFailure(t *testing.T) {
	entries := []vendor.ContentEntry{{
		Path: "bad\x00path", Symlink: true, LinkTarget: "x", Mode: digest.ModeSymlink,
	}}
	_, err := replayModuleDigestFromEntries(entries)
	if err == nil {
		t.Fatal("a NUL-bearing path must be rejected")
	}
	var bad *invalidManifestError
	if !errors.As(err, &bad) {
		t.Fatalf("error must be typed as a manifest integrity failure, got %T: %v", err, err)
	}
}
