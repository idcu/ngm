package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.102：模块级 material 只收窄到 subPath；路径相对模块根；使用独立 header。
func TestBuildModuleMaterial_OnlySubtree(t *testing.T) {
	testutils.MustHaveGit(t)

	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "export const a = 1\n")
	repo.WriteFile("packages/a/src/util.ts", "export const u = 1\n")
	repo.WriteFile("packages/b/index.ts", "export const b = 1\n")
	commit := repo.Commit("monorepo: a + b")

	m, err := BuildModuleMaterial(context.Background(), Options{}, repo.Dir, commit, "packages/a")
	if err != nil {
		t.Fatalf("BuildModuleMaterial: %v", err)
	}
	if m.ManifestVersion != digest.ModuleManifestVersion {
		t.Errorf("ManifestVersion = %q, want %q", m.ManifestVersion, digest.ModuleManifestVersion)
	}
	if len(m.Records) != 2 {
		t.Fatalf("records = %d, want 2: %+v", len(m.Records), m.Records)
	}
	for _, r := range m.Records {
		if strings.HasPrefix(r.Path, "packages/") {
			t.Errorf("module record path must be relative to the module root: %q", r.Path)
		}
	}
	if !strings.HasPrefix(string(m.Manifest()), digest.ModuleManifestVersion+"\x00") {
		t.Errorf("module manifest header is wrong: %q", string(m.Manifest())[:32])
	}

	rootDigest, err := BuildArchiveDigest(context.Background(), Options{}, repo.Dir, commit)
	if err != nil {
		t.Fatalf("BuildArchiveDigest: %v", err)
	}
	if m.Digest == rootDigest {
		t.Error("module digest must be domain-separated from the root archive digest")
	}

	// 同一内容、不同 subPath 的模块，内容身份相同（位置由 subtreeOid 绑定）。
	same := testutils.NewGitRepo(t)
	same.WriteFile("packages/x/index.ts", "export const a = 1\n")
	same.WriteFile("packages/x/src/util.ts", "export const u = 1\n")
	commit2 := same.Commit("same content at another path")
	m2, err := BuildModuleMaterial(context.Background(), Options{}, same.Dir, commit2, "packages/x")
	if err != nil {
		t.Fatalf("BuildModuleMaterial(same): %v", err)
	}
	if m2.Digest != m.Digest {
		t.Errorf("location-independent module digest mismatch:\n%s\n%s", m.Digest, m2.Digest)
	}
}

func TestBuildModuleMaterials_Batch(t *testing.T) {
	testutils.MustHaveGit(t)

	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "a\n")
	repo.WriteFile("packages/a/src/util.ts", "u\n")
	repo.WriteFile("packages/b/index.ts", "b\n")
	repo.WriteFile("packages/c/index.ts", "c\n")
	commit := repo.Commit("monorepo: a + b + c")

	got, err := BuildModuleMaterials(context.Background(), Options{}, repo.Dir, commit, []string{"packages/a", "packages/b"})
	if err != nil {
		t.Fatalf("BuildModuleMaterials: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("materials=%d want 2", len(got))
	}
	for _, sub := range []string{"packages/a", "packages/b"} {
		m, ok := got[sub]
		if !ok {
			t.Fatalf("missing material for %s", sub)
		}
		one, err := BuildModuleMaterial(context.Background(), Options{}, repo.Dir, commit, sub)
		if err != nil {
			t.Fatalf("BuildModuleMaterial(%s): %v", sub, err)
		}
		if m.Digest != one.Digest {
			t.Errorf("%s: batch digest %s != single %s", sub, m.Digest, one.Digest)
		}
		oid, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, sub)
		if err != nil {
			t.Fatalf("TreeOIDAtPath(%s): %v", sub, err)
		}
		if m.SubtreeOid != oid {
			t.Errorf("%s: SubtreeOid %s != %s", sub, m.SubtreeOid, oid)
		}
		for _, e := range m.Entries {
			if strings.Contains(e.Path, "packages/") {
				t.Errorf("%s: sibling/prefix leaked into entries: %q", sub, e.Path)
			}
		}
	}

	t.Run("overlapping modules are refused", func(t *testing.T) {
		_, err := BuildModuleMaterials(context.Background(), Options{}, repo.Dir, commit,
			[]string{"packages/a", "packages/a/src"})
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("expected overlap error, got %v", err)
		}
	})

	t.Run("missing module is refused", func(t *testing.T) {
		_, err := BuildModuleMaterials(context.Background(), Options{}, repo.Dir, commit, []string{"packages/nope"})
		if err == nil {
			t.Fatal("expected missing-module error")
		}
	})
}

func TestTreeOIDAtPath(t *testing.T) {
	testutils.MustHaveGit(t)

	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "a\n")
	repo.WriteFile("packages/b/index.ts", "b\n")
	commit := repo.Commit("monorepo")

	a, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "packages/a")
	if err != nil {
		t.Fatalf("TreeOIDAtPath(packages/a): %v", err)
	}
	b, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "packages/b")
	if err != nil {
		t.Fatalf("TreeOIDAtPath(packages/b): %v", err)
	}
	root, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "")
	if err != nil {
		t.Fatalf("TreeOIDAtPath(root): %v", err)
	}
	if a == b || a == root || b == root {
		t.Errorf("tree oids must differ: a=%s b=%s root=%s", a, b, root)
	}

	if _, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "../escape"); err == nil {
		t.Error("escaping sub-path must be refused")
	}
	if _, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "packages/nope"); err == nil {
		t.Error("missing sub-path must be refused")
	}
	if _, err := TreeOIDAtPath(context.Background(), Options{}, repo.Dir, commit, "packages/a/index.ts"); err == nil {
		t.Error("a file path must be refused (not a tree)")
	}
}
