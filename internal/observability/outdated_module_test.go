package observability

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/testutils"
)

func TestOutdatedModuleOnlySeesItsOwnTags(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "a\n")
	repo.WriteFile("packages/b/index.ts", "b\n")
	repo.Commit("monorepo")
	repo.Tag("packages/a/v1.0.0", false)
	repo.Tag("packages/a/v1.1.0", false)
	repo.Tag("packages/b/v9.9.9", false)

	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/mono", Ref: "packages/a/v1.0.0", RefType: "tag",
		Commit: "deadbeef", SubPath: "packages/a",
	}}

	rep, err := CheckOutdated(context.Background(), lf, OutdatedOptions{
		EnsureMirror: func(context.Context, string) (string, error) { return repo.Dir, nil },
	})
	if err != nil {
		t.Fatalf("CheckOutdated: %v", err)
	}
	if len(rep.Entries) != 1 {
		t.Fatalf("entries = %d", len(rep.Entries))
	}
	e := rep.Entries[0]
	if e.SubPath != "packages/a" {
		t.Errorf("SubPath = %q", e.SubPath)
	}
	if e.Latest != "packages/a/v1.1.0" {
		t.Errorf("Latest = %q, want packages/a/v1.1.0 (must not use packages/b/v9.9.9)", e.Latest)
	}
	if !e.Update {
		t.Errorf("module v1.0.0 should see v1.1.0 as update: %+v", e)
	}
}

func TestOutdatedModuleWithoutOwnTagsIsStale(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "a\n")
	repo.WriteFile("packages/c/index.ts", "c\n")
	repo.Commit("monorepo")
	repo.Tag("packages/a/v1.0.0", false)

	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{{
		Name: "github:acme/mono", Ref: "packages/c/v1.0.0", RefType: "tag",
		Commit: "deadbeef", SubPath: "packages/c",
	}}

	rep, err := CheckOutdated(context.Background(), lf, OutdatedOptions{
		EnsureMirror: func(context.Context, string) (string, error) { return repo.Dir, nil },
	})
	if err != nil {
		t.Fatalf("CheckOutdated: %v", err)
	}
	e := rep.Entries[0]
	if !e.Stale {
		t.Errorf("module without tags must be stale, got %+v", e)
	}
	if e.Latest != "" {
		t.Errorf("must not borrow sibling tag, got Latest=%q", e.Latest)
	}
	if !strings.Contains(e.Note, "no tags for module packages/c") {
		t.Errorf("note = %q", e.Note)
	}
}
