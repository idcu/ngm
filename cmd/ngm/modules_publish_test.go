package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestModulesIndexPerModulePublishOverridesGlobal(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)
	repo.WriteFile("packages/a/ngm.json", `{"name":"a","version":"1.0.0","publish":false}`)
	repo.WriteFile("packages/b/ngm.json", `{"name":"b","version":"1.0.0","publish":true}`)
	repo.WriteFile("packages/c/ngm.json", `{"name":"c","version":"1.0.0"}`)
	repo.Commit("collection")
	for _, tag := range []string{"packages/a/v1.0.0", "packages/b/v1.0.0", "packages/c/v1.0.0"} {
		repo.Tag(tag, false)
	}

	readReleases := func(t *testing.T) map[string]map[string]any {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repo.Dir, "ngm.collection.json"))
		if err != nil {
			t.Fatal(err)
		}
		var idx struct {
			Releases map[string]map[string]any `json:"releases"`
		}
		if err := json.Unmarshal(data, &idx); err != nil {
			t.Fatalf("parse index: %v\n%s", err, data)
		}
		return idx.Releases
	}

	// 全局 false：显式 publish=true 的 b 仍然发布；a=false 不发布；c 跟随全局 false。
	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--releases=false"); code != 0 {
		t.Fatalf("index --releases=false exit=%d:\n%s", code, out)
	}
	rel := readReleases(t)
	if _, ok := rel["packages/b"]; !ok {
		t.Errorf("publish=true must override global false; releases=%v", rel)
	}
	if _, ok := rel["packages/a"]; ok {
		t.Errorf("publish=false must override global false; releases=%v", rel)
	}
	if _, ok := rel["packages/c"]; ok {
		t.Errorf("nil publish must follow global false; releases=%v", rel)
	}

	// 全局 true：a=false 仍然不发布；b=true 与 c=nil 发布。
	if code, out := runCaptureCode(t, "modules", "index", "--dir="+repo.Dir, "--releases=true"); code != 0 {
		t.Fatalf("index --releases=true exit=%d:\n%s", code, out)
	}
	rel = readReleases(t)
	if _, ok := rel["packages/a"]; ok {
		t.Errorf("publish=false must override global true; releases=%v", rel)
	}
	for _, want := range []string{"packages/b", "packages/c"} {
		if _, ok := rel[want]; !ok {
			t.Errorf("missing %s; releases=%v", want, rel)
		}
	}
}
