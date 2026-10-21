package resolve_test

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/resolve"
	"github.com/idcu/ngm/internal/testutils"
)

func TestInferRefTypeFromRemote(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("a.txt", "a\n")
	repo.Commit("feat: a")
	repo.Tag("v1.0.0", false)
	repo.Exec("branch", "feature")

	repoCanon := resolve.MustNormalize("127.0.0.1/acme/repo")
	opts := resolve.ResolveOptions{GitURL: repo.Dir}

	t.Run("tag", func(t *testing.T) {
		got, err := resolve.InferRefTypeFromRemote(context.Background(), repoCanon, "v1.0.0", opts)
		if err != nil {
			t.Fatalf("infer tag: %v", err)
		}
		if got != resolve.RefTypeTag {
			t.Errorf("got %q, want tag", got)
		}
	})

	t.Run("branch", func(t *testing.T) {
		got, err := resolve.InferRefTypeFromRemote(context.Background(), repoCanon, "feature", opts)
		if err != nil {
			t.Fatalf("infer branch: %v", err)
		}
		if got != resolve.RefTypeBranch {
			t.Errorf("got %q, want branch", got)
		}
	})

	t.Run("both is ambiguous", func(t *testing.T) {
		repo.Exec("branch", "v1.0.0")
		_, err := resolve.InferRefTypeFromRemote(context.Background(), repoCanon, "v1.0.0", opts)
		if err == nil || !strings.Contains(err.Error(), "both a tag and a branch") {
			t.Fatalf("expected ambiguity error, got %v", err)
		}
	})

	t.Run("missing is refused", func(t *testing.T) {
		_, err := resolve.InferRefTypeFromRemote(context.Background(), repoCanon, "nope", opts)
		if err == nil || !strings.Contains(err.Error(), "not a tag or branch") {
			t.Fatalf("expected not-found error, got %v", err)
		}
	})
}
