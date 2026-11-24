package main

import (
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/lock"
)

// TestV219PatchWiring 固定 2026-11-24 竞品改进 P1：
// patches 不能只停在配置校验层——必须真的换掉解析锚点，并在 lock 留痕。
func TestV219PatchWiring(t *testing.T) {
	pf := &config.ProjectFile{
		Dependencies: []config.Dependency{{
			Name: "gitee:org/repo", Ref: "v1.0.0", RefType: config.RefTypeTag,
		}},
		Patches: []config.Patch{{
			Name: "gitee:org/repo", From: "v1.0.0", To: "v1.0.1", Why: "upstream tag withdrawn",
		}},
	}
	got := patchedDeps(pf)
	if len(got) != 1 || got[0].Ref != "v1.0.1" {
		t.Fatalf("patch was not applied to the resolved declaration: %#v", got)
	}
	// 原件不能被静默改写。
	if pf.Dependencies[0].Ref != "v1.0.0" {
		t.Fatal("patchedDeps mutated the original declaration")
	}

	lf := &lock.File{Dependencies: []lock.Dependency{{Name: "gitee:org/repo", Ref: "v1.0.1"}}}
	stampPatch(pf, lf, "gitee:org/repo", "v1.0.1")
	d := lf.Dependencies[0]
	if d.PatchFrom != "v1.0.0" || d.PatchWhy != "upstream tag withdrawn" {
		t.Fatalf("lock did not record patch provenance: %#v", d)
	}
	// 不匹配的条目不能被误标成补丁。
	stampPatch(pf, lf, "gitee:other/repo", "v1.0.1")
	if lf.Dependencies[0].PatchFrom != "v1.0.0" {
		t.Fatal("stampPatch marked an unrelated entry")
	}
}
