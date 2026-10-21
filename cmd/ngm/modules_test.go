package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func writeModuleFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModulesListAndValidate(t *testing.T) {
	root := t.TempDir()
	// 根 ngm.json 是项目本身，不应出现在 modules list 里。
	if err := os.WriteFile(filepath.Join(root, "ngm.json"), []byte(`{"schemaVersion":1,"name":"app","version":"0.1.0","runtime":"node"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeModuleFixture(t, root, "packages/a", `{"name":"a","version":"1.0.0"}`)
	writeModuleFixture(t, root, "packages/b", `{"name":"b"}`)
	writeModuleFixture(t, root, "packages/c", `{not json`)
	writeModuleFixture(t, root, "packages/d", `{"name":"a","version":"2.0.0"}`)

	code, out := runCaptureCode(t, "modules", "list", "--dir="+root, "--json")
	if code != 0 {
		t.Fatalf("modules list exit=%d:\n%s", code, out)
	}
	var list struct {
		Version int `json:"version"`
		Modules []struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Version string `json:"version"`
			Err     string `json:"error"`
		} `json:"modules"`
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("parse modules list JSON: %v\n%s", err, out)
	}
	if list.Version != 1 || list.Limit != 1000 {
		t.Errorf("report header = %+v", list)
	}
	if len(list.Modules) != 4 {
		t.Fatalf("modules=%d want 4:\n%s", len(list.Modules), out)
	}
	for i, want := range []string{"packages/a", "packages/b", "packages/c", "packages/d"} {
		if list.Modules[i].Path != want {
			t.Errorf("module[%d].path = %q, want %q", i, list.Modules[i].Path, want)
		}
	}
	if list.Modules[2].Err == "" {
		t.Error("packages/c should carry a parse error")
	}

	code, out = runCaptureCode(t, "modules", "list", "--dir="+root, "--json", "--limit=1")
	if code != 0 {
		t.Fatalf("modules list --limit exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, `"truncated": true`) {
		t.Errorf("limit must mark truncation:\n%s", out)
	}

	code, out = runCaptureCode(t, "modules", "validate", "--dir="+root)
	if code != 3 {
		t.Fatalf("validate with bad JSON/duplicate names should exit 3, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "invalid JSON") || !strings.Contains(out, "duplicate module name") {
		t.Errorf("validate should report both problems:\n%s", out)
	}

	code, out = runCaptureCode(t, "modules", "validate", "--dir="+root, "--strict")
	if code != 3 {
		t.Fatalf("strict validate should exit 3, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "missing `version`") {
		t.Errorf("strict validate should require version:\n%s", out)
	}
}

func TestModulesIndexWritesDeterministicCollection(t *testing.T) {
	root := t.TempDir()
	writeModuleFixture(t, root, "packages/b", `{"name":"b","version":"2.0.0"}`)
	writeModuleFixture(t, root, "packages/a", `{"name":"a","version":"1.0.0"}`)

	code, out := runCaptureCode(t, "modules", "index", "--dir="+root, "--json")
	if code != 0 {
		t.Fatalf("modules index exit=%d:\n%s", code, out)
	}
	path := filepath.Join(root, "ngm.collection.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(first), `"packages/a"`) || !strings.Contains(string(first), `"packages/b"`) {
		t.Errorf("index missing modules:\n%s", first)
	}
	if strings.Index(string(first), "packages/a") > strings.Index(string(first), "packages/b") {
		t.Errorf("index must be sorted by path:\n%s", first)
	}

	if code, out := runCaptureCode(t, "modules", "index", "--dir="+root); code != 0 {
		t.Fatalf("second index exit=%d:\n%s", code, out)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("index is not deterministic:\n%s\n---\n%s", first, second)
	}

	code, out = runCaptureCode(t, "modules", "index", "--dir="+root, "--dry-run")
	if code != 0 || !strings.Contains(out, `"modules"`) {
		t.Fatalf("dry-run should print the index, got %d:\n%s", code, out)
	}
}

func TestModulesIndexCollectsReleaseTags(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0"}`)
	repo.WriteFile("packages/b/ngm.json", `{"name":"b","version":"2.0.0"}`)
	repo.Commit("collection")
	aCommit := repo.Tag("packages/a/v1.0.0", false)
	repo.Tag("packages/b/v2.0.0", false)
	// 兄弟模块的 tag 不能进入 a 的 releases。
	repo.Tag("packages/ab/v9.9.9", false)

	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir); code != 0 {
		t.Fatalf("modules index exit=%d:\n%s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(repo.Dir, "ngm.collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		Releases map[string]map[string]struct {
			Tag    string `json:"tag"`
			Commit string `json:"commit"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parse index: %v\n%s", err, data)
	}
	rel, ok := idx.Releases["packages/a"]["1.0.0"]
	if !ok {
		t.Fatalf("packages/a release missing:\n%s", data)
	}
	if rel.Commit != aCommit || rel.Tag != "packages/a/v1.0.0" {
		t.Errorf("release = %+v, want commit %s / tag packages/a/v1.0.0", rel, aCommit)
	}
	if _, leaked := idx.Releases["packages/a"]["9.9.9"]; leaked {
		t.Errorf("sibling tag leaked into packages/a releases:\n%s", data)
	}
}

func TestModulesIndexUpdatesLocalBranch(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0"}`)
	head := repo.Commit("collection")

	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir); code != 0 {
		t.Fatalf("modules index exit=%d:\n%s", code, out)
	}
	branchCommit := repo.RevParse("ngm-index")
	if branchCommit == "" {
		t.Fatal("ngm-index branch was not created")
	}
	indexText := repo.Exec("show", "ngm-index:ngm.collection.json")
	if !strings.Contains(indexText, "packages/a") {
		t.Errorf("branch index missing module:\n%s", indexText)
	}
	var idx struct {
		Source struct {
			Commit string `json:"commit"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(indexText), &idx); err != nil {
		t.Fatalf("parse branch index: %v\n%s", err, indexText)
	}
	if idx.Source.Commit != head {
		t.Errorf("index source commit = %q, want %q", idx.Source.Commit, head)
	}

	// 非 Git 目录里显式要求分支必须失败，而不是静默跳过。
	plain := t.TempDir()
	if code, out := runCaptureCode(t, "modules", "index", "--dir="+plain, "--branch=ngm-index"); code != 3 {
		t.Fatalf("explicit --branch outside a Git repo should exit 3, got %d:\n%s", code, out)
	}
}

func TestModulesValidateIndexConsistency(t *testing.T) {
	root := t.TempDir()
	writeModuleFixture(t, root, "packages/a", `{"name":"a","version":"1.0.0"}`)
	writeModuleFixture(t, root, "packages/b", `{"name":"b","version":"2.0.0"}`)
	if code, out := runCaptureCode(t, "modules", "index", "--dir="+root); code != 0 {
		t.Fatalf("modules index exit=%d:\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--index=ngm.collection.json"); code != 0 {
		t.Fatalf("validate --index should pass, got %d:\n%s", code, out)
	}

	// 目录新增模块而索引没跟上 → 必须红，而不是让索引漂移。
	writeModuleFixture(t, root, "packages/c", `{"name":"c","version":"3.0.0"}`)
	code, out := runCaptureCode(t, "modules", "validate", "--dir="+root, "--index=ngm.collection.json")
	if code != 3 || !strings.Contains(out, "packages/c") {
		t.Fatalf("missing index module should fail, got %d:\n%s", code, out)
	}
}

func TestModulesListRejectsBadLimit(t *testing.T) {
	code, out := runCaptureCode(t, "modules", "list", "--limit=0")
	if code != 3 || !strings.Contains(out, "--limit") {
		t.Fatalf("limit=0 should be a usage error, got %d:\n%s", code, out)
	}
}
