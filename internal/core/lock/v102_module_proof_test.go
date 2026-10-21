package lock

import (
	"strings"
	"testing"
)

// v0.102：模块证明字段是有条件可选的——一旦出现必须成对、形态正确，
// 且只允许出现在 subPath 条目上。
func TestV102ModuleProofFields(t *testing.T) {
	base := func() Dependency {
		return Dependency{
			Name:         "github:acme/modules",
			Ref:          "packages/a/v1",
			RefType:      "tag",
			Commit:       strings.Repeat("a", 40),
			SubPath:      "packages/a",
			VendorPath:   "github.com/acme/modules",
			ModuleDigest: "sha256:" + strings.Repeat("c", 64),
			SubtreeOid:   "sha1:" + strings.Repeat("d", 40),
		}
	}

	t.Run("valid module proof", func(t *testing.T) {
		d := base()
		if err := d.validate(); err != nil {
			t.Fatalf("valid module proof rejected: %v", err)
		}
	})

	t.Run("module fields must be paired", func(t *testing.T) {
		d := base()
		d.SubtreeOid = ""
		if err := d.validate(); err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("expected missing-proof error, got %v", err)
		}
	})

	t.Run("root entry must not carry module proof", func(t *testing.T) {
		d := base()
		d.SubPath = ""
		d.ArchiveDigest = "sha256:" + strings.Repeat("b", 64)
		if err := d.validate(); err == nil || !strings.Contains(err.Error(), "only valid for sub-path") {
			t.Fatalf("expected root-entry error, got %v", err)
		}
	})

	t.Run("subtreeOid shape", func(t *testing.T) {
		d := base()
		d.SubtreeOid = "sha1:" + strings.Repeat("d", 39)
		if err := d.validate(); err == nil || !strings.Contains(err.Error(), "subtreeOid") {
			t.Fatalf("expected subtreeOid error, got %v", err)
		}
	})
}

func TestV102ModuleProofRoundTrip(t *testing.T) {
	f := NewFile()
	f.Dependencies = []Dependency{{
		Name:         "github:acme/modules",
		Ref:          "packages/a/v1",
		RefType:      "tag",
		Commit:       strings.Repeat("a", 40),
		ModuleDigest: "sha256:" + strings.Repeat("c", 64),
		SubtreeOid:   "sha1:" + strings.Repeat("d", 40),
		Version:      "1.0.0",
		ResolvedAt:   "2026-10-20T00:00:00Z",
		VendorPath:   "github.com/acme/modules",
		SubPath:      "packages/a",
		RequiredBy:   []string{RootRequirer},
	}}
	data, err := f.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(data)
	for _, key := range []string{`"moduleDigest"`, `"subtreeOid"`, `"version"`} {
		if !strings.Contains(text, key) {
			t.Errorf("marshaled lock missing %s:\n%s", key, text)
		}
	}
}
