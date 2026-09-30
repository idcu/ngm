package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/mappings"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV03MappingsMonorepoAcceptance 是 v0.3 E 组的端到端验收。
//
// 它固定的问题是：**同一个仓库的两个子包，怎么让构建工具分别找到它们**。
// v1 里两条映射只有 `from`（同 slug）与不同的 `to`——消费方只能从 vendor 路径
// 反推导入标识符，那是猜测。`path` 把这个事实写进协议。
func TestV03MappingsMonorepoAcceptance(t *testing.T) {
	// v3MonorepoProject 造一个含两个子包的仓库并装进项目。
	v3MonorepoProject := func(t *testing.T) string {
		t.Helper()
		isolateUserEnv(t)

		r := testutils.NewGitRepo(t)
		r.WriteFile("packages/core/index.ts", "export const core = 1\n")
		r.WriteFile("packages/web/index.ts", "export const web = 1\n")
		r.WriteFile("ngm.json", `{"name":"mono","version":"1.0.0","runtime":"node"}`)
		r.Commit("feat: monorepo")
		r.Tag("v1", false)
		seedMirror(t, "github:v3/mono", r.Dir)

		proj := newProject(t)
		for _, sub := range []string{"packages/core", "packages/web"} {
			if code, out := runCaptureCode(t, "add", "github:v3/mono@v1",
				"--ref-type=tag", "--path="+sub, "--dir="+proj); code != 0 {
				t.Fatalf("add %s: %s", sub, out)
			}
		}
		if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
			t.Fatalf("install: %s", out)
		}
		return proj
	}

	readMappings := func(t *testing.T, proj string) []mappings.Mapping {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(proj, mappings.FileName))
		if err != nil {
			t.Fatalf("read mappings: %v", err)
		}
		var f mappings.File
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("parse mappings: %v\n%s", err, raw)
		}
		return f.Mappings
	}

	t.Run("each subpath gets its own mapping with an explicit path", func(t *testing.T) {
		proj := v3MonorepoProject(t)
		got := readMappings(t, proj)

		if len(got) != 2 {
			t.Fatalf("expected 2 mappings (one per subpath), got %d: %+v", len(got), got)
		}
		want := map[string]string{
			"packages/core": "github.com/v3/mono/packages/core",
			"packages/web":  "github.com/v3/mono/packages/web",
		}
		for _, m := range got {
			if m.From != "github:v3/mono" {
				t.Errorf("unexpected `from`: %q", m.From)
			}
			rel, ok := want[m.Path]
			if !ok {
				t.Errorf("unexpected `path`: %q", m.Path)
				continue
			}
			if !strings.HasSuffix(m.To, "/"+rel) {
				t.Errorf("`to` for %s = %q, want it to end with %s", m.Path, m.To, rel)
			}
			// 两个子包必须指向**不同**的 vendor 目录，否则构建工具会互相串
			if m.Main == "" {
				t.Errorf("subpath %s should have an inferred entry point", m.Path)
			}
		}
	})

	t.Run("mappings validate accepts the generated file", func(t *testing.T) {
		proj := v3MonorepoProject(t)
		if code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj); code != 0 {
			t.Fatalf("mappings validate exit=%d:\n%s", code, out)
		}
	})

	// 手改 `path` 成"看着像、其实没锁"的值：必须被拦下，且要指出 lock 里到底是什么。
	// 这类错误的运行时症状是"模块找不到"，很难回溯到 mappings。
	t.Run("a path that is not the locked subpath is rejected", func(t *testing.T) {
		proj := v3MonorepoProject(t)

		path := filepath.Join(proj, mappings.FileName)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		tampered := strings.Replace(string(raw), `"path": "packages/core"`, `"path": "packages/typo"`, 1)
		if tampered == string(raw) {
			t.Fatalf("the tamper did not apply; file was:\n%s", raw)
		}
		if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 3 {
			t.Fatalf("an unlocked `path` must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "not a locked subpath") {
			t.Errorf("the report should name the problem:\n%s", out)
		}
		if !strings.Contains(out, "packages/core") {
			t.Errorf("the report should say what IS locked:\n%s", out)
		}
	})

	// 旧文件（v1，没有 `path`）仍可读、仍可通过校验，只给一条可操作的警告。
	t.Run("a v1 file without path still validates, with a warning", func(t *testing.T) {
		proj := v3MonorepoProject(t)

		path := filepath.Join(proj, mappings.FileName)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f mappings.File
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		// 模拟"由 v0.2 的 ngm 生成的文件"：去掉 path 字段
		for i := range f.Mappings {
			f.Mappings[i].Path = ""
		}
		old, err := f.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, old, 0o644); err != nil {
			t.Fatal(err)
		}

		code, out := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a v1 file must remain valid (warning only), got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "warning") {
			t.Errorf("the report should carry the regeneration warning:\n%s", out)
		}
	})
}
