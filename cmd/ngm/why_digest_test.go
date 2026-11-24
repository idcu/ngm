package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/lock"
)

// V216 判据：--digest 反查的四条路径——精确匹配、前缀唯一、前缀歧义、无匹配。
// 另测 --json 契约与非法输入的 errs+hint。
func TestV216WhyDigestAudit(t *testing.T) {
	isolateUserEnv(t)
	dir := t.TempDir()
	a64 := strings.Repeat("a", 64)
	digA := "sha256:" + a64
	digB := "sha256:" + strings.Repeat("a", 63) + "b" // 与 digA 共享 63 位前缀
	digC := "sha256:" + strings.Repeat("c", 64)

	manifest := `{"schemaVersion":3,"name":"github.com:my-org/app","version":"0.1.0","runtime":"node","dependencies":[]}`
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{
		{
			Name: "github:org/a", Ref: "v1", RefType: "tag",
			Commit: strings.Repeat("1", 40), ArchiveDigest: digA,
			VendorPath: "github.com/org/a", RequiredBy: []string{lock.RootRequirer},
		},
		{
			Name: "github:org/b", Ref: "v2", RefType: "tag",
			Commit: strings.Repeat("2", 40), ArchiveDigest: digB,
			VendorPath: "github.com/org/b", RequiredBy: []string{"github:org/a"},
		},
		{
			Name: "github:org/c", Ref: "v3", RefType: "tag",
			Commit: strings.Repeat("3", 40), ModuleDigest: digC,
			SubtreeOid: "sha1:" + strings.Repeat("4", 40), SubPath: "pkg",
			VendorPath: "github.com/org/c", RequiredBy: []string{lock.RootRequirer},
		},
	}
	if err := lock.Write(filepath.Join(dir, lock.FileName), lf); err != nil {
		t.Fatal(err)
	}

	call := func(t *testing.T, extra ...string) (int, string) {
		t.Helper()
		args := append([]string{"--dir=" + dir}, extra...)
		var stdout, stderr bytes.Buffer
		code := runWhy(context.Background(), args, &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	report := func(t *testing.T, out string) digestAuditReport {
		t.Helper()
		var r digestAuditReport
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("not a JSON report: %v\n%s", err, out)
		}
		return r
	}

	t.Run("exact match", func(t *testing.T) {
		code, out := call(t, "--digest="+digA)
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		for _, want := range []string{"匹配: 1 条", "前缀歧义: 不适用", "github:org/a", "(root)", "archiveDigest"} {
			if !strings.Contains(out, want) {
				t.Errorf("human output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("unique prefix", func(t *testing.T) {
		code, out := call(t, "--digest=sha256:"+strings.Repeat("c", 12), "--json")
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		r := report(t, out)
		if r.MatchCount != 1 || r.DistinctDigests != 1 || !r.Prefix || r.Ambiguous {
			t.Errorf("unexpected report: %+v", r)
		}
		if len(r.Matches) != 1 || r.Matches[0].SubPath != "pkg" ||
			r.Matches[0].DigestField != "moduleDigest" ||
			len(r.Matches[0].RequiredBy) != 1 || r.Matches[0].RequiredBy[0] != lock.RootRequirer {
			t.Errorf("match must carry subPath/digestField/requiredBy: %+v", r.Matches)
		}
		if r.Source != "ngm.lock" {
			t.Errorf("source=%q", r.Source)
		}
	})

	t.Run("ambiguous prefix", func(t *testing.T) {
		code, out := call(t, "--digest="+strings.Repeat("a", 12)) // 裸 hex
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		for _, want := range []string{"匹配: 2 条", "2 个不同摘要", "前缀歧义: 是", "sha256:" + strings.Repeat("a", 12)} {
			if !strings.Contains(out, want) {
				t.Errorf("human output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("no match", func(t *testing.T) {
		code, out := call(t, "--digest=sha256:"+strings.Repeat("d", 12), "--json")
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		r := report(t, out)
		if r.MatchCount != 0 || len(r.Matches) != 0 || r.Ambiguous {
			t.Errorf("unexpected report: %+v", r)
		}
		// 人读那侧要说清"无匹配"
		_, human := call(t, "--digest=sha256:"+strings.Repeat("d", 12))
		if !strings.Contains(human, "无匹配") {
			t.Errorf("human output missing 无匹配:\n%s", human)
		}
	})

	t.Run("invalid query uses errs contract with hint", func(t *testing.T) {
		code, out := call(t, "--digest=abc")
		if code != 3 {
			t.Fatalf("exit=%d (want 3) out=%s", code, out)
		}
		for _, want := range []string{"ConfigInvalid", "hint:", "12"} {
			if !strings.Contains(out, want) {
				t.Errorf("error output missing %q:\n%s", want, out)
			}
		}
	})
}
