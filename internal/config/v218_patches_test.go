package config

import (
	"strings"
	"testing"
)

// V218：显式补丁登记的四条判据——合法通过、缺 why 红、同 name 重复红、
// 非法 to 红。另测补丁锚点不逃逸与 Effective 深拷贝。
func TestV218PatchValidation(t *testing.T) {
	manifest := func(patches string) []byte {
		return []byte(`{"schemaVersion":3,"name":"app","version":"0.1.0","runtime":"node","dependencies":[],"patches":` + patches + `}`)
	}

	valid := `[{"name":"gitee:org/repo","from":"v1.0.0","to":"v1.0.1","why":"upstream tag was retracted"}]`
	p, err := ParseProjectFile(manifest(valid), "ngm.json")
	if err != nil {
		t.Fatalf("legal patch must pass: %v", err)
	}
	if len(p.Patches) != 1 || p.Patches[0].Name != "gitee:org/repo" || p.Patches[0].To != "v1.0.1" {
		t.Fatalf("patch fields not parsed: %+v", p.Patches)
	}

	cases := []struct {
		name    string
		patches string
		want    string
	}{
		{"missing why", `[{"name":"gitee:org/repo","from":"v1.0.0","to":"v1.0.1"}]`, "why"},
		{"duplicate name", `[{"name":"gitee:org/repo","from":"v1","to":"v2","why":"a"},{"name":"gitee:org/repo","from":"v2","to":"v3","why":"b"}]`, "duplicate patch"},
		{"bare range to", `[{"name":"gitee:org/repo","from":"v1.0.0","to":"^1.2.3","why":"a"}]`, "not a valid ref"},
		{"x range to", `[{"name":"gitee:org/repo","from":"v1.0.0","to":"1.x","why":"a"}]`, "version range"},
		{"local escape name", `[{"name":"local:../escape","from":"v1","to":"v2","why":"a"}]`, "escapes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProjectFile(manifest(tc.patches), "ngm.json")
			if err == nil {
				t.Fatalf("%s must be rejected", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestV218PatchAnchorsAndDeepCopy(t *testing.T) {
	// commit / sha256 摘要都是合法锚点（local 形态）。
	sha := strings.Repeat("a", 40)
	dirDigest := "sha256:" + strings.Repeat("b", 64)
	p, err := ParseProjectFile([]byte(`{"schemaVersion":3,"name":"app","version":"0.1.0","runtime":"node","dependencies":[],"patches":[
		{"name":"gitee:org/repo","from":"`+sha+`","to":"v2.0.0","why":"pin to release"},
		{"name":"local:kernel","from":"v1","to":"`+dirDigest+`","why":"pin dir digest"}]}`), "ngm.json")
	if err != nil {
		t.Fatalf("commit and local-dir digest anchors must pass: %v", err)
	}

	eff := mergeProject(DefaultBuiltin(), p)
	eff.Patches[0].Why = "mutated"
	if p.Patches[0].Why == "mutated" {
		t.Error("Effective.Patches must not share the backing array with Project.Patches")
	}
}
