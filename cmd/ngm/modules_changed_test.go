package main

import (
	"encoding/json"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestModulesChangedListsTouchedModules(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0"}`)
	repo.WriteFile("packages/a/index.ts", "a1\n")
	repo.WriteFile("packages/b/ngm.json", `{"name":"b","version":"2.0.0"}`)
	repo.WriteFile("packages/b/index.ts", "b1\n")
	repo.Commit("initial")

	repo.WriteFile("packages/a/index.ts", "a2\n")
	repo.Commit("change a")

	code, out := runCaptureCode(t, "modules", "changed", "--dir="+repo.Dir, "--since=HEAD~1", "--json")
	if code != 0 {
		t.Fatalf("modules changed exit=%d:\n%s", code, out)
	}
	var rep struct {
		Since   string `json:"since"`
		Modules []struct {
			Path string `json:"path"`
		} `json:"modules"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("parse changed report: %v\n%s", err, out)
	}
	if rep.Since != "HEAD~1" {
		t.Errorf("since = %q", rep.Since)
	}
	if len(rep.Modules) != 1 || rep.Modules[0].Path != "packages/a" {
		t.Fatalf("changed modules = %+v, want only packages/a", rep.Modules)
	}

	// 没有改动时为空，不是"所有模块"。
	code, out = runCaptureCode(t, "modules", "changed", "--dir="+repo.Dir, "--since=HEAD", "--json")
	if code != 0 {
		t.Fatalf("modules changed HEAD exit=%d:\n%s", code, out)
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Modules) != 0 {
		t.Errorf("HEAD diff should be empty, got %+v", rep.Modules)
	}
}
