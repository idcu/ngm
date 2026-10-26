package observability

import (
	"bytes"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
)

func TestWhyModuleLockedInfoIncludesProofFields(t *testing.T) {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/mono", Ref: "packages/a/v1.0.0", RefType: "tag",
		Commit: "abc123", SubPath: "packages/a",
		ModuleDigest: "sha256:aaa", SubtreeOid: "sha1:bbb", Version: "1.0.0",
	}}

	info := LockedFrom(lf, "github:acme/mono", "packages/a")
	if info == nil {
		t.Fatal("LockedFrom returned nil")
	}
	if info.SubtreeOID != "sha1:bbb" || info.Version != "1.0.0" {
		t.Fatalf("locked info = %+v", info)
	}

	var buf bytes.Buffer
	rep := WhyReport{
		Name: "github:acme/mono", Ref: "packages/a/v1.0.0", RefType: "tag",
		Commit: "abc123", SubPath: "packages/a", Locked: info, Source: WhySourceLock,
	}
	rep.Render(&buf)
	out := buf.String()
	for _, want := range []string{"moduleDigest: sha256:aaa", "subtreeOid: sha1:bbb", "version: 1.0.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("why output missing %q:\n%s", want, out)
		}
	}
}
