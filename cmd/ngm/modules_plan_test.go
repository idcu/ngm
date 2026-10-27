package main

import (
	"encoding/json"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestModulesPlanBumpsOnlyChangedModules(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.2.3"}`)
	repo.WriteFile("packages/a/index.ts", "a1\n")
	repo.WriteFile("packages/b/ngm.json", `{"name":"b","version":"2.0.0"}`)
	repo.WriteFile("packages/b/index.ts", "b1\n")
	repo.Commit("initial")

	repo.WriteFile("packages/a/index.ts", "a2\n")
	repo.Commit("change a")

	code, out := runCaptureCode(t, "modules", "plan", "--dir="+repo.Dir, "--since=HEAD~1", "--json")
	if code != 0 {
		t.Fatalf("modules plan exit=%d:\n%s", code, out)
	}
	var rep struct {
		Bump    string `json:"bump"`
		Modules []struct {
			Path        string `json:"path"`
			Version     string `json:"version"`
			NextVersion string `json:"nextVersion"`
			Tag         string `json:"tag"`
		} `json:"modules"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("parse plan: %v\n%s", err, out)
	}
	if rep.Bump != "patch" {
		t.Errorf("bump = %q", rep.Bump)
	}
	if len(rep.Modules) != 1 {
		t.Fatalf("modules = %+v, want only packages/a", rep.Modules)
	}
	m := rep.Modules[0]
	if m.Path != "packages/a" || m.Version != "1.2.3" || m.NextVersion != "1.2.4" ||
		m.Tag != "packages/a/v1.2.4" {
		t.Errorf("plan item = %+v", m)
	}

	code, out = runCaptureCode(t, "modules", "plan", "--dir="+repo.Dir, "--since=HEAD~1", "--bump=minor", "--json")
	if code != 0 {
		t.Fatalf("minor plan exit=%d:\n%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Modules[0].NextVersion != "1.3.0" {
		t.Errorf("minor next = %q", rep.Modules[0].NextVersion)
	}
}

func TestModulesPlanRejectsMissingVersion(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a"}`)
	repo.WriteFile("packages/a/index.ts", "a1\n")
	repo.Commit("initial")
	repo.WriteFile("packages/a/index.ts", "a2\n")
	repo.Commit("change a")

	code, out := runCaptureCode(t, "modules", "plan", "--dir="+repo.Dir, "--since=HEAD~1")
	if code != 3 {
		t.Fatalf("missing version should exit 3, got %d:\n%s", code, out)
	}
}
