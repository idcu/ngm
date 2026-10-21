package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestModulesIndexWritesReleaseShards(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0"}`)
	repo.Commit("collection")
	commit := repo.Tag("packages/a/v1.0.0", false)

	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--shards"); code != 0 {
		t.Fatalf("modules index --shards exit=%d:\n%s", code, out)
	}
	root, err := os.ReadFile(filepath.Join(repo.Dir, "ngm.collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx struct {
		ReleaseDir string `json:"releaseDir"`
		Releases   any    `json:"releases"`
	}
	if err := json.Unmarshal(root, &idx); err != nil {
		t.Fatalf("parse root index: %v\n%s", err, root)
	}
	if idx.ReleaseDir != "releases" {
		t.Errorf("releaseDir = %q, want releases", idx.ReleaseDir)
	}
	if idx.Releases != nil {
		t.Errorf("sharded root index must not carry inline releases:\n%s", root)
	}

	shardPath := filepath.Join(repo.Dir, "releases", "packages", "a.json")
	shard, err := os.ReadFile(shardPath)
	if err != nil {
		t.Fatalf("read shard: %v", err)
	}
	var s struct {
		SchemaVersion int    `json:"schemaVersion"`
		Module        string `json:"module"`
		Releases      map[string]struct {
			Tag    string `json:"tag"`
			Commit string `json:"commit"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(shard, &s); err != nil {
		t.Fatalf("parse shard: %v\n%s", err, shard)
	}
	if s.SchemaVersion != 1 || s.Module != "packages/a" {
		t.Errorf("shard header = %+v", s)
	}
	rel, ok := s.Releases["1.0.0"]
	if !ok || rel.Commit != commit || rel.Tag != "packages/a/v1.0.0" {
		t.Errorf("shard release = %+v, want %s", rel, commit)
	}

	// 分片分支：根索引与 releases/<module>.json 必须在同一棵树里。
	branchRoot := repo.Exec("show", "ngm-index:ngm.collection.json")
	if !strings.Contains(branchRoot, `"packages/a"`) {
		t.Errorf("branch root index missing module:\n%s", branchRoot)
	}
	branchShard := repo.Exec("show", "ngm-index:releases/packages/a.json")
	if !strings.Contains(branchShard, "packages/a/v1.0.0") {
		t.Errorf("branch shard missing release:\n%s", branchShard)
	}
	tree := repo.Exec("ls-tree", "-r", "--name-only", "ngm-index")
	for _, want := range []string{"ngm.collection.json", "releases/packages/a.json"} {
		if !strings.Contains(tree, want) {
			t.Errorf("branch tree missing %s:\n%s", want, tree)
		}
	}

	// 显式再次要求分支也必须成功（同一套批量 tree 写入）。
	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--shards", "--branch=ngm-index"); code != 0 {
		t.Fatalf("explicit sharded branch should succeed, got %d:\n%s", code, out)
	}
}
