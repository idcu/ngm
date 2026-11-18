package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
)

// TestV135ModuleExtraStoreEntryIsCritical 钉住「模块条目的反向集合」这件事的**证据路径**。
//
// 根条目与模块条目的权威形状来源不同：
//
//   - 根条目：形状来自 `git ls-tree <commit>`，重放只遍历权威 tree，所以
//     "store 多一个文件"必须靠 extraStoreEntries 的反向集合才看得见（v0.132）；
//   - 模块条目：`moduleDigest` 由 **content store 清单本身**按 module 记录集重放
//     （`replayModuleDigestFromEntries`），多一个条目就会改变重放结果，于是它被
//     digest 比对抓住——kind 是 `module-digest`，不是 `extra-store-entry`。
//
// 这条测试给上面第二句话提供证据。若哪天模块改成不重放 store 清单（例如改用
// subtreeOid 正向取条目），这里会变红：那时必须把 extraStoreEntries 扩展到模块分支，
// 否则"多一个条目"会重新变成不可见。
func TestV135ModuleExtraStoreEntryIsCritical(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	up := testutils.NewGitRepo(t)
	up.WriteFile("package.json", `{"name":"up-monorepo","private":true,"workspaces":["packages/*"]}`+"\n")
	up.WriteFile("packages/a/package.json", `{
  "name": "pkg-a",
  "version": "1.0.0",
  "type": "module",
  "license": "MIT",
  "engines": { "node": ">=18" },
  "exports": { ".": "./src/index.js" }
}`+"\n")
	up.WriteFile("packages/a/src/index.js", "export const a = 1;\n")
	up.Commit("monorepo: a")
	up.Tag("v1.0.0", true)

	const slug = "127.0.0.1/acme/mod-extra"
	seedMirror(t, slug, up.Dir)
	spec := fmt.Sprintf("git://%s.git#path=packages/a@v1.0.0", slug)

	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "v135.test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	if code, out := runCaptureCode(t, "add", spec, "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "update", "--all", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("update: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	lf, err := lock.Read(lock.Find(proj))
	if err != nil || lf == nil {
		t.Fatalf("read lock: %v", err)
	}
	var found *lock.Dependency
	for i := range lf.Dependencies {
		if lf.Dependencies[i].IsModule() {
			found = &lf.Dependencies[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("fixture must produce a module entry (subPath set); lock has none")
	}
	dep := *found
	if !dep.IsModule() {
		t.Fatalf("fixture must be a module entry (subPath set), got %+v", dep)
	}

	// 基线：注入前必须是干净的，否则下面的 exit 2 可能来自别的理由。
	if code, out := runCaptureCode(t, "verify", "--content", "--dir="+proj); code != 0 {
		t.Fatalf("baseline content verify should pass, got %d:\n%s", code, out)
	}

	layout := defaultLayoutForTest(t)
	store := vendor.NewContentStore(layout.ContentRoot())
	manifestPath := store.ManifestPath(dep.ContentDigest())
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read module content manifest: %v", err)
	}
	var mf vendor.Manifest
	if err := json.Unmarshal(raw, &mf); err != nil {
		t.Fatalf("parse module content manifest: %v", err)
	}
	if len(mf.Entries) == 0 {
		t.Fatal("fixture has no entries to clone into an injected one")
	}
	// 复用合法 blob：注入的条目字节是真的，只是路径不在 subtreeOid 覆盖的任何位置。
	injected := mf.Entries[0]
	injected.Path = "injected/evil.js"
	mf.Entries = append(mf.Entries, injected)
	mutated, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, mutated, 0o644); err != nil {
		t.Fatal(err)
	}

	code, rep, out := runVerifyJSON(t, "--content", "--json", "--dir="+proj)
	if code != 2 {
		t.Fatalf("an extra module store entry must be critical (2), got %d:\n%s", code, out)
	}
	if rep.Summary.Critical != 1 {
		t.Errorf("summary should count one critical dependency: %+v", rep.Summary)
	}
	if len(rep.Dependencies) != 1 {
		t.Fatalf("expected exactly one dependency in the report, got %d:\n%s", len(rep.Dependencies), out)
	}
	proof := rep.Dependencies[0].Content
	if proof == nil {
		t.Fatalf("content proof missing:\n%s", out)
	}
	var kinds []string
	hasModuleDigest := false
	for _, m := range proof.Mismatches {
		kinds = append(kinds, m.Kind)
		if m.Kind == "module-digest" {
			hasModuleDigest = true
		}
	}
	if !hasModuleDigest {
		t.Fatalf("module extras are caught by the digest replay; expected kind=module-digest, got %v\n%s",
			kinds, out)
	}
}
