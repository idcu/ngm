package modproof_test

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/modproof"
	"github.com/idcu/ngm/internal/testutils"
)

func TestProveAndVerifyModule(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/index.ts", "export const a = 1\n")
	repo.WriteFile("packages/a/src/util.ts", "export const u = 1\n")
	repo.WriteFile("packages/b/index.ts", "export const b = 1\n")
	commit := repo.Commit("monorepo")

	src := modproof.Repo{URL: "example", Path: repo.Dir}
	proof, err := modproof.Prove(context.Background(), src, commit, "packages/a")
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof.ModuleDigest == "" || proof.RootDigest != "" {
		t.Fatalf("module proof = %+v", proof)
	}
	if !strings.HasPrefix(proof.SubtreeOID, "sha1:") {
		t.Errorf("SubtreeOID = %q", proof.SubtreeOID)
	}

	report, err := modproof.Verify(context.Background(), src, commit, "packages/a", proof)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if report.Status != modproof.StatusOK {
		t.Fatalf("report = %+v", report)
	}

	bad := proof
	bad.ModuleDigest = "sha256:" + strings.Repeat("0", 64)
	report, err = modproof.Verify(context.Background(), src, commit, "packages/a", bad)
	if err != nil {
		t.Fatalf("Verify(bad): %v", err)
	}
	if report.Status != modproof.StatusMismatch {
		t.Fatalf("bad digest report = %+v", report)
	}
}

func TestProveAndVerifyRoot(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("index.ts", "export const root = 1\n")
	commit := repo.Commit("root")

	src := modproof.Repo{Path: repo.Dir}
	proof, err := modproof.Prove(context.Background(), src, commit, "")
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof.RootDigest == "" || proof.ModuleDigest != "" {
		t.Fatalf("root proof = %+v", proof)
	}
	report, err := modproof.Verify(context.Background(), src, commit, "", proof)
	if err != nil || report.Status != modproof.StatusOK {
		t.Fatalf("Verify = %+v, %v", report, err)
	}
}

func TestProveMissingPathIsClassified(t *testing.T) {
	_, err := modproof.Prove(context.Background(), modproof.Repo{}, "deadbeef", "packages/a")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Repo.Path") {
		t.Fatalf("error = %v", err)
	}
}
