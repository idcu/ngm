package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV227EngineBindingCodes 固定 E8：版本不符 5、摘要不符 2、都符合 0、都不填保持旧行为。
//
// 变异检验：把 adapter.ExitCode 里 IssueDigestBinding 的分支从 2 改成 5 →
// "digest only" 与 "both mismatch" 两条子用例立刻红；恢复 → 绿。
func TestV227EngineBindingCodes(t *testing.T) {
	testutils.AllowEngines(t, "fake-engine")
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	raw, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	goodDigest := "sha256:" + hex.EncodeToString(sum[:])

	// 每项都重新写清单并重新 LoadCatalog，确保读到的就是刚写的那一份（字段接线）。
	check := func(t *testing.T, expectedVersion, expectedDigest string, wantCode int, wantKind adapter.IssueKind) {
		t.Helper()
		isolateUserEnv(t)
		proj := newProject(t)
		entry := map[string]any{
			"name": "bound", "kind": "css", "adapter": "subprocess", "command": fake,
			"version": "1.0.0",
		}
		if expectedVersion != "" {
			entry["expectedVersion"] = expectedVersion
		}
		if expectedDigest != "" {
			entry["expectedDigest"] = expectedDigest
		}
		body, err := json.Marshal(map[string]any{"version": 1, "engines": []any{entry}})
		if err != nil {
			t.Fatal(err)
		}
		testutils.WriteFile(t, proj, "ngm.engines.json", string(body))

		cat, err := adapter.LoadCatalog(proj, "")
		if err != nil {
			t.Fatal(err)
		}
		e, ok := cat.Find(adapter.KindCSS, "bound")
		if !ok {
			t.Fatal("the declared entry was not loaded")
		}
		if e.ExpectedVersion != expectedVersion || e.ExpectedDigest != expectedDigest {
			t.Fatalf("binding fields were not wired into adapter.Entry: %+v", e)
		}
		// 只保留被测条目：内置清单里装没装 esbuild 与本命题无关。
		cat.Engines = []adapter.Entry{e}
		issues := cat.Validate()
		if got := adapter.ExitCode(issues); got != wantCode {
			t.Errorf("adapter.ExitCode=%d want %d; issues=%+v", got, wantCode, issues)
		}
		var saw bool
		for _, is := range issues {
			if is.Kind == wantKind {
				saw = true
			}
		}
		if wantKind != "" && !saw {
			t.Errorf("issues lack kind %q: %+v", wantKind, issues)
		}
		if wantKind == "" {
			for _, is := range issues {
				if is.Kind == adapter.IssueVersionBinding || is.Kind == adapter.IssueDigestBinding {
					t.Errorf("unexpected binding issue for a clean/passthrough entry: %+v", is)
				}
			}
		}

		var stdout, stderr bytes.Buffer
		code := runEnginesValidate(&engineContext{catalog: cat}, false, &stdout, &stderr)
		if code != wantCode {
			t.Fatalf("ngm engines validate exit=%d want %d:\n%s%s", code, wantCode, stdout.String(), stderr.String())
		}
	}

	t.Run("version mismatch is exit 5", func(t *testing.T) {
		check(t, "9.9.9", "", 5, adapter.IssueVersionBinding)
	})
	t.Run("digest mismatch is exit 2", func(t *testing.T) {
		check(t, "", "sha256:"+strings.Repeat("0", 64), 2, adapter.IssueDigestBinding)
	})
	t.Run("both match is exit 0", func(t *testing.T) {
		check(t, "1.0.0", goodDigest, 0, "")
	})
	t.Run("neither field keeps the old behavior", func(t *testing.T) {
		// 声明版本 1.0.0、实装 1.0.0-fake：旧语义是"信息"，不是失败；
		// 而且这里连信息都不产生（前缀一致），所以没有任何绑定类 issue。
		check(t, "", "", 0, "")
	})
	t.Run("digest mismatch outranks version mismatch", func(t *testing.T) {
		check(t, "9.9.9", "sha256:"+strings.Repeat("0", 64), 2, adapter.IssueDigestBinding)
	})
}
