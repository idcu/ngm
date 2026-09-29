package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validProjectJSON = `{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "dependencies": [
    {"name": "github:my-org/utils", "ref": "v1.2.3", "refType": "tag"},
    {"name": "github:my-org/logger", "ref": "main", "refType": "branch"}
  ],
  "engines": {"transform": "esbuild"},
  "vendor": {"mode": "local", "linkMode": "auto"},
  "supplyChain": {"postInstallPolicy": "deny"}
}`

func TestDecodeProject_OK(t *testing.T) {
	p, err := decodeProject(strings.NewReader(validProjectJSON), "ngm.json")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Runtime != RuntimeNode {
		t.Errorf("runtime=%q", p.Runtime)
	}
	if len(p.Dependencies) != 2 {
		t.Fatalf("deps=%d", len(p.Dependencies))
	}
	if p.Dependencies[0].RefType != RefTypeTag {
		t.Errorf("refType=%q", p.Dependencies[0].RefType)
	}
}

func TestDecodeProject_UnknownField(t *testing.T) {
	bad := `{"name":"x","version":"0","runtime":"node","unknown":true}`
	_, err := decodeProject(strings.NewReader(bad), "ngm.json")
	if err == nil {
		t.Fatalf("expected unknown-field failure")
	}
}

func TestDecodeProject_InvalidRefType(t *testing.T) {
	bad := `{
		"name": "x",
		"version": "0",
		"runtime": "node",
		"dependencies": [{"name":"github.com:a/b","ref":"v1","refType":"banana"}]
	}`
	_, err := decodeProject(strings.NewReader(bad), "ngm.json")
	if err == nil {
		t.Fatalf("expected invalid-refType failure")
	}
}

func TestDecodeProject_InvalidRuntime(t *testing.T) {
	bad := `{"name":"x","version":"0","runtime":"bun"}`
	_, err := decodeProject(strings.NewReader(bad), "ngm.json")
	if err == nil {
		t.Fatalf("expected invalid-runtime failure")
	}
}

func TestLoad_MergesBuiltinDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(validProjectJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Effective.Vendor.LinkMode != "auto" {
		t.Errorf("linkMode=%q", r.Effective.Vendor.LinkMode)
	}
	if r.Effective.Vendor.Mode != "local" {
		t.Errorf("mode=%q", r.Effective.Vendor.Mode)
	}
}

// TestDependencyValidate_NameDelegatesToResolve 确认 name 格式校验委托给 resolve.ParseSlug。
//
// 规则细节（各协议形式、host shorthand 等）的测试位于 internal/resolve。
// 这里只验证 config 层的委托没有破坏，以及 path 字段的校验被触发。
func TestDependencyValidate_NameDelegatesToResolve(t *testing.T) {
	good := []Dependency{
		{Name: "github:my-org/utils", Ref: "v1.2.3", RefType: RefTypeTag},
		// 宽容接受显式 host + 冒号（归一化后等价）
		{Name: "github.com:my-org/utils", Ref: "v1.2.3", RefType: RefTypeTag},
		// 私有 GitLab 多级路径
		{Name: "gitlab:group/sub/proj", Ref: "v1", RefType: RefTypeTag},
		// 自建 host
		{Name: "gitlab.example.com:group/repo", Ref: "v1", RefType: RefTypeTag},
		// monorepo 子路径
		{Name: "github:org/mono", Ref: "v1", RefType: RefTypeTag, Path: "packages/core"},
	}
	for _, d := range good {
		if err := d.Validate(); err != nil {
			t.Errorf("expected ok for %+v: %v", d, err)
		}
	}

	bad := []Dependency{
		{Name: "", Ref: "v1", RefType: RefTypeTag},
		{Name: "my-org/utils", Ref: "v1", RefType: RefTypeTag},                   // 缺 host
		{Name: "github:x", Ref: "v1", RefType: RefTypeTag},                       // 只有一段
		{Name: "https://github.com/o/r", Ref: "v1", RefType: RefTypeTag},         // name 不得是 URL
		{Name: "github:org/repo@v1", Ref: "v1", RefType: RefTypeTag},             // 不得含 @
		{Name: "github:org/repo#path=x", Ref: "v1", RefType: RefTypeTag},         // 不得含 #
		{Name: "github:org/repo", Ref: "", RefType: RefTypeTag},                  // ref 必填
		{Name: "github:org/repo", Ref: "v1", RefType: ""},                        // refType 必填
		{Name: "github:org/repo", Ref: "v1", RefType: RefTypeTag, Path: "/abs"},  // path 绝对路径
		{Name: "github:org/repo", Ref: "v1", RefType: RefTypeTag, Path: "../up"}, // path 含 ..
	}
	for _, d := range bad {
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for %+v", d)
		}
	}
}

func TestLoad_NoProject(t *testing.T) {
	r, err := Load("", t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.Effective == nil {
		t.Fatalf("Effective should be non-nil")
	}
	if r.Effective.Runtime != RuntimeNode {
		t.Errorf("default runtime=%q", r.Effective.Runtime)
	}
	if r.Effective.Vendor == nil || r.Effective.Vendor.LinkMode != "auto" {
		t.Errorf("vendor defaults not applied: %+v", r.Effective.Vendor)
	}
}

func TestLoad_PicksUpGlobal(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	ngmHome := filepath.Join(home, ".ngm")
	if err := os.MkdirAll(ngmHome, 0o755); err != nil {
		t.Fatal(err)
	}
	global := []byte(`{"git":{"defaultProtocol":"https"}}`)
	if err := os.WriteFile(filepath.Join(ngmHome, "config.json"), global, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(validProjectJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(dir, home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if r.GlobalEffective.Git.DefaultProtocol != "https" {
		t.Errorf("global protocol=%q", r.GlobalEffective.Git.DefaultProtocol)
	}
}

func TestVendorValidate(t *testing.T) {
	cases := []struct {
		v       VendorConfig
		wantErr bool
	}{
		{VendorConfig{Mode: "local", LinkMode: "auto"}, false},
		{VendorConfig{Mode: "global", GlobalPath: "/tmp/x"}, false},
		{VendorConfig{Mode: "global"}, true}, // 缺 globalPath
		{VendorConfig{Mode: "weird"}, true},  // 非法 mode
		{VendorConfig{LinkMode: "banana"}, true},
	}
	for _, c := range cases {
		err := c.v.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%+v wantErr=%v got=%v", c.v, c.wantErr, err)
		}
	}
}
