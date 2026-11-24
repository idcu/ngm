package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
)

// V215 判据（评估报告 §6 P1 / ADR-055）：
// 最小 v2 lock + v2 清单能迁移；缺 digest 条目被 skipped 且不写坏文件；
// dry-run 不改文件；已 v3 幂等。
//
// 这些测试直接调 runMigrate —— migrate 尚未在 root.go 注册（接线留给集成方）。
func TestV215Migrate(t *testing.T) {
	commit := strings.Repeat("a", 40)
	digest1 := "sha256:" + strings.Repeat("a", 64)
	manifestV2 := `{
  "schemaVersion": 2,
  "name": "github.com:my-org/app",
  "version": "0.1.0",
  "runtime": "node",
  "dependencies": [{"name":"github:org/utils","ref":"v1.2.3","refType":"tag"}]
}`
	lockV2 := func(deps string) string {
		return `{
  "version": 1,
  "lockfileVersion": "2.0.0",
  "dependencies": [` + deps + `]
}`
	}
	goodEntry := func(name, commit, dig string) string {
		return `{"name":"` + name + `","ref":"v1.2.3","refType":"tag",` +
			`"commit":"` + commit + `","archiveDigest":"` + dig + `",` +
			`"resolvedAt":"2026-09-29T10:00:00Z","vendorPath":"github.com/org/utils",` +
			`"requiredBy":["(root)"]}`
	}
	badEntry := `{"name":"github:org/broken","ref":"v1.0.0","refType":"tag",` +
		`"commit":"` + strings.Repeat("b", 40) + `",` +
		`"resolvedAt":"2026-09-29T10:00:00Z","vendorPath":"github.com/org/broken",` +
		`"requiredBy":["(root)"]}`

	setup := func(t *testing.T, mf, lf string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(mf), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ngm.lock"), []byte(lf), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	call := func(t *testing.T, args ...string) (int, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runMigrate(context.Background(), args, &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	field := func(t *testing.T, path, key string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m[key].(string)
	}

	t.Run("minimal v2 lock and manifest migrate with backups", func(t *testing.T) {
		dir := setup(t, manifestV2, lockV2(goodEntry("github:org/utils", commit, digest1)))
		code, out := call(t, "--dir="+dir)
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		if got := intField(t, filepath.Join(dir, "ngm.json"), "schemaVersion"); got != 3 {
			t.Errorf("schemaVersion=%d, want 3", got)
		}
		if got := field(t, filepath.Join(dir, "ngm.lock"), "lockfileVersion"); got != "3.0.0" {
			t.Errorf("lockfileVersion=%q, want 3.0.0", got)
		}
		// 迁移后的 lock 必须能被 v3 读取器完整校验
		lf, err := lock.Read(filepath.Join(dir, "ngm.lock"))
		if err != nil || lf == nil || len(lf.Dependencies) != 1 {
			t.Fatalf("migrated lock unreadable: %v", err)
		}
		for _, name := range []string{"ngm.json.bak", "ngm.lock.bak"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("missing backup %s: %v", name, err)
			}
			if !strings.Contains(out, name) {
				t.Errorf("report should print backup path %s:\n%s", name, out)
			}
		}
	})

	t.Run("missing digest is skipped and nothing is written", func(t *testing.T) {
		dir := setup(t, manifestV2, lockV2(goodEntry("github:org/utils", commit, digest1)+","+badEntry))
		code, out := call(t, "--dir="+dir)
		if code == 0 {
			t.Fatalf("skipped 条目必须非 0 退出：%s", out)
		}
		for _, want := range []string{"skipped", "archiveDigest", "需手工处理", "未写入任何文件"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if got := intField(t, filepath.Join(dir, "ngm.json"), "schemaVersion"); got != 2 {
			t.Errorf("manifest schemaVersion=%d, want unchanged 2", got)
		}
		if got := field(t, filepath.Join(dir, "ngm.lock"), "lockfileVersion"); got != "2.0.0" {
			t.Errorf("lockfileVersion=%q, want unchanged 2.0.0", got)
		}
		for _, name := range []string{"ngm.json.bak", "ngm.lock.bak"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("skipped 时不应创建 %s", name)
			}
		}
	})

	t.Run("dry-run changes nothing", func(t *testing.T) {
		dir := setup(t, manifestV2, lockV2(goodEntry("github:org/utils", commit, digest1)))
		beforeMF, _ := os.ReadFile(filepath.Join(dir, "ngm.json"))
		beforeLF, _ := os.ReadFile(filepath.Join(dir, "ngm.lock"))
		code, out := call(t, "--dir="+dir, "--dry-run")
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		afterMF, _ := os.ReadFile(filepath.Join(dir, "ngm.json"))
		afterLF, _ := os.ReadFile(filepath.Join(dir, "ngm.lock"))
		if !bytes.Equal(beforeMF, afterMF) || !bytes.Equal(beforeLF, afterLF) {
			t.Fatal("dry-run 改写了文件")
		}
		for _, want := range []string{"dry-run", "ngm.json.bak", "ngm.lock.bak"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		for _, name := range []string{"ngm.json.bak", "ngm.lock.bak"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("dry-run 不应创建 %s", name)
			}
		}
	})

	t.Run("already v3 is idempotent", func(t *testing.T) {
		dir := setup(t,
			strings.Replace(manifestV2, `"schemaVersion": 2`, `"schemaVersion": 3`, 1),
			strings.Replace(lockV2(goodEntry("github:org/utils", commit, digest1)),
				`"lockfileVersion": "2.0.0"`, `"lockfileVersion": "3.0.0"`, 1))
		code, out := call(t, "--dir="+dir)
		if code != 0 {
			t.Fatalf("exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "无需迁移") {
			t.Errorf("output missing 无需迁移:\n%s", out)
		}
		for _, name := range []string{"ngm.json.bak", "ngm.lock.bak"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Errorf("已 v3 不应创建 %s", name)
			}
		}
	})

	t.Run("json report marks skipped and exits non-zero", func(t *testing.T) {
		dir := setup(t, manifestV2, lockV2(goodEntry("github:org/utils", commit, digest1)+","+badEntry))
		code, out := call(t, "--dir="+dir, "--json")
		if code == 0 {
			t.Fatalf("--json skipped 也应非 0：%s", out)
		}
		var rep struct {
			NeedsManual bool `json:"needsManual"`
			Skipped     []struct {
				Reason string `json:"reason"`
			} `json:"skipped"`
		}
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatalf("not valid JSON: %v\n%s", err, out)
		}
		if !rep.NeedsManual || len(rep.Skipped) == 0 {
			t.Errorf("report should flag needsManual + skipped: %s", out)
		}
	})
}

func mValue(t *testing.T, path, key string) any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m[key]
}

func intField(t *testing.T, path, key string) int {
	t.Helper()
	v, ok := mValue(t, path, key).(float64)
	if !ok {
		t.Fatalf("%s in %s is not a number", key, path)
	}
	return int(v)
}
