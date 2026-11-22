package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/localfs"
	"github.com/idcu/ngm/internal/core/lock"
)

// TestLocalDirVerifyDetectsDrift 钉住 ADR-052 的核心安全判据：
// local-dir 来源安装后，源目录内容被改动，verify 必须给出 Critical 漂移（exit 2 级别），
// 而不是"无结论"或"通过"。这是"可证明同一性"不变量在第二种来源上的落点。
func TestLocalDirVerifyDetectsDrift(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "kernel")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports=1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	digest, err := localfs.BuildDirDigest(src)
	if err != nil {
		t.Fatal(err)
	}

	d := &lock.Dependency{
		Name:       "local:kernel",
		Ref:        digest,
		RefType:    "local-dir",
		SourcePath: "kernel",
		DirDigest:  digest,
		VendorPath: "local/kernel",
	}
	opts := Options{
		ProjectRoot:     root,
		ResolveLocalDir: localfs.BuildDirDigest,
	}
	res := DepResult{Name: d.Name, Ref: d.Ref, RefType: d.RefType}
	got, err := verifyLocalDir(d, opts, res)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Checks) != 1 || got.Checks[0].Status != StatusOK {
		t.Fatalf("clean local dir must pass: %#v", got.Checks)
	}

	// 改一个字节 → Critical 漂移
	if err := os.WriteFile(filepath.Join(src, "index.js"), []byte("module.exports=2;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err = verifyLocalDir(d, opts, res)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Checks) != 1 || got.Checks[0].Status != StatusFail || got.Checks[0].Drift != DriftCritical {
		t.Fatalf("changed local dir must be DriftCritical: %#v", got.Checks)
	}
	if got.Err == "" || !strings.Contains(got.Err, "changed") {
		t.Fatalf("drift must carry a diagnostic: %q", got.Err)
	}
}

func TestLocalDirVerifyOfflineAndMissingResolver(t *testing.T) {
	d := &lock.Dependency{
		Name:       "local:x",
		RefType:    "local-dir",
		SourcePath: "kernel",
		DirDigest:  "sha256:" + strings.Repeat("a", 64),
	}
	res := DepResult{Name: d.Name}

	// 未注入 resolver → operational fail（不是结论）
	got, _ := verifyLocalDir(d, Options{ProjectRoot: "/x"}, res)
	if len(got.Checks) != 1 || !got.Checks[0].Operational {
		t.Fatalf("missing resolver must be operational: %#v", got.Checks)
	}

	// 注入 resolver 后，--offline 不影响结论（本地来源天然离线）
	root2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root2, "kernel"), 0755); err != nil {
		t.Fatal(err)
	}
	got, _ = verifyLocalDir(d, Options{
		ProjectRoot:     root2,
		Offline:         true,
		ResolveLocalDir: localfs.BuildDirDigest,
	}, res)
	if len(got.Checks) != 1 || got.Checks[0].Operational {
		t.Fatalf("offline local verify must still reach a conclusion: %#v", got.Checks)
	}
	_ = context.Background() // 显式说明本测试不需要上下文（verifyLocalDir 同步）
}
