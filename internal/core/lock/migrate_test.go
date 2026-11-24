package lock

import (
	"encoding/json"
	"strings"
	"testing"
)

// V215 的 lock 层判据：可安全映射的字段逐一保留，无法证明的条目进 Skipped。
func TestV215LockV2ToV3Mapping(t *testing.T) {
	commit := strings.Repeat("a", 40)
	dig := "sha256:" + strings.Repeat("b", 64)
	modDig := "sha256:" + strings.Repeat("c", 64)
	subtree := "sha1:" + strings.Repeat("d", 40)
	lockV2 := func(deps string) []byte {
		return []byte(`{"version":1,"lockfileVersion":"2.0.0","dependencies":[` + deps + `]}`)
	}
	rootEntry := `{"name":"github:org/utils","ref":"v1.2.3","refType":"tag",` +
		`"commit":"` + commit + `","archiveDigest":"` + dig + `",` +
		`"resolvedAt":"2026-09-29T10:00:00Z","vendorPath":"github.com/org/utils",` +
		`"requiredBy":["(root)"]}`
	moduleEntry := `{"name":"github:org/logger","ref":"v2.0.0","refType":"tag",` +
		`"commit":"` + strings.Repeat("e", 40) + `","moduleDigest":"` + modDig + `",` +
		`"subtreeOid":"` + subtree + `","version":"2.0.0",` +
		`"resolvedAt":"2026-09-29T10:00:00Z","vendorPath":"github.com/org/logger",` +
		`"subPath":"packages/logger","requiredBy":["github:org/utils"]}`

	t.Run("root and module entries keep every proven field", func(t *testing.T) {
		out, err := MigrateV2(lockV2(rootEntry+","+moduleEntry), "ngm.lock")
		if err != nil {
			t.Fatal(err)
		}
		if out.AlreadyCurrent || len(out.Skipped) != 0 || len(out.File.Dependencies) != 2 {
			t.Fatalf("unexpected outcome: %+v", out)
		}
		if out.File.LockfileVersion != FileVersion {
			t.Errorf("lockfileVersion=%q", out.File.LockfileVersion)
		}
		var root, mod Dependency
		for _, d := range out.File.Dependencies {
			if d.SubPath == "" {
				root = d
			} else {
				mod = d
			}
		}
		if root.Commit != commit || root.ArchiveDigest != dig || root.VendorPath != "github.com/org/utils" ||
			root.Ref != "v1.2.3" || root.RefType != "tag" || root.ResolvedAt != "2026-09-29T10:00:00Z" ||
			len(root.RequiredBy) != 1 || root.RequiredBy[0] != RootRequirer {
			t.Errorf("root entry lost a field: %+v", root)
		}
		if mod.ModuleDigest != modDig || mod.SubtreeOid != subtree || mod.Version != "2.0.0" ||
			mod.SubPath != "packages/logger" || len(mod.RequiredBy) != 1 ||
			mod.RequiredBy[0] != "github:org/utils" {
			t.Errorf("module entry lost a field: %+v", mod)
		}
		if err := out.File.Validate(); err != nil {
			t.Errorf("migrated lock must validate: %v", err)
		}
	})

	t.Run("missing digest is skipped with a fix", func(t *testing.T) {
		broken := `{"name":"github:org/bad","ref":"v1","refType":"tag",` +
			`"commit":"` + strings.Repeat("f", 40) + `","vendorPath":"github.com/org/bad","requiredBy":["(root)"]}`
		out, err := MigrateV2(lockV2(rootEntry+","+broken), "ngm.lock")
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Skipped) != 1 || len(out.File.Dependencies) != 1 {
			t.Fatalf("want 1 skipped + 1 kept, got %+v", out)
		}
		if !strings.Contains(out.Skipped[0].Reason, "archiveDigest") || out.Skipped[0].Fix == "" {
			t.Errorf("skip must name the missing digest and give a fix: %+v", out.Skipped[0])
		}
	})

	t.Run("module transition shape with archiveDigest is skipped", func(t *testing.T) {
		transition := strings.Replace(moduleEntry, `"moduleDigest":"`+modDig+`",`,
			`"archiveDigest":"`+dig+`","moduleDigest":"`+modDig+`",`, 1)
		out, err := MigrateV2(lockV2(transition), "ngm.lock")
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Skipped) != 1 || !strings.Contains(out.Skipped[0].Reason, "ambiguous") {
			t.Errorf("ambiguous v2 transition shape must be skipped: %+v", out)
		}
	})

	t.Run("v3 input is reported as current", func(t *testing.T) {
		out, err := MigrateV2([]byte(`{"version":1,"lockfileVersion":"3.0.0","dependencies":[]}`), "ngm.lock")
		if err != nil || !out.AlreadyCurrent || out.File != nil {
			t.Fatalf("want AlreadyCurrent, got %+v err=%v", out, err)
		}
	})

	t.Run("unknown top-level field is rejected", func(t *testing.T) {
		_, err := MigrateV2([]byte(`{"version":1,"lockfileVersion":"2.0.0","mystery":1,"dependencies":[]}`), "ngm.lock")
		if err == nil || !strings.Contains(err.Error(), "mystery") {
			t.Errorf("unknown field must be rejected: %v", err)
		}
	})

	t.Run("marshaled result keeps canonical field order", func(t *testing.T) {
		out, err := MigrateV2(lockV2(rootEntry), "ngm.lock")
		if err != nil {
			t.Fatal(err)
		}
		data, err := out.File.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m["lockfileVersion"] != FileVersion {
			t.Errorf("marshaled lockfileVersion=%v", m["lockfileVersion"])
		}
	})
}
