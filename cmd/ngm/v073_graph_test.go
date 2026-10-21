package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/testutils"
)

// v0.73：依赖图形进 lock 的端到端判据（ADR-028）。
//
// 这个版本真正交付的东西只有一句：**`ngm why` 不再需要解析**（0 次 git 子进程、
// 离线可用），而 SBOM 终于有依赖关系段。其余都是这句话的推论。

func TestV73InstallWritesTheGraphIntoTheLock(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	lf, err := lock.Read(filepath.Join(p, "ngm.lock"))
	if err != nil || lf == nil {
		t.Fatalf("读不到 lock: %v", err)
	}
	if !lf.HasGraph() {
		t.Fatal("install 写出的 lock 必须带依赖图（ADR-028）——否则 why 永远走重新解析")
	}
	d := lf.Dependencies[0]
	if len(d.RequiredBy) != 1 || d.RequiredBy[0] != lock.RootRequirer {
		t.Errorf("直接依赖的 requiredBy 应为 [%s]，got %v", lock.RootRequirer, d.RequiredBy)
	}
}

// TestV73WhyReadsTheGraphFromTheLock 是本版的头条判据：
// lock 里带着图 ⇒ `why` **一次 git 子进程都不启动**。
func TestV73WhyReadsTheGraphFromTheLock(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	git.ResetSpawnCount()
	code, out, errOut := runSplit(t, "why", "github:x/dep", "--dir="+p)
	if code != 0 {
		t.Fatalf("why exit=%d\n%s\n%s", code, out, errOut)
	}
	if n := git.SpawnCount(); n != 0 {
		t.Errorf("lock 里有图，why 不该启动 git 子进程（实测 %d 次）——"+
			"ADR-028 的全部价值就在这一条；回到非零说明它又去解析了", n)
	}
	// 来源必须写在报告里：两条路径的代价差一个量级，不许静默回退。
	if !strings.Contains(out, "ngm.lock（未重新解析") {
		t.Errorf("报告要说清图来自 lock:\n%s", out)
	}
}

func TestV73WhyJSONReportsTheSource(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	code, out, errOut := runSplit(t, "why", "github:x/dep", "--json", "--dir="+p)
	if code != 0 {
		t.Fatalf("why --json exit=%d\n%s\n%s", code, out, errOut)
	}
	var rep struct {
		Source string `json:"source"`
		Paths  [][]string
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("--json 不是单个 JSON 文档: %v\n%s", err, out)
	}
	if rep.Source != "lock" {
		t.Errorf("source = %q，应为 lock（图来自 lock）", rep.Source)
	}
	if len(rep.Paths) != 1 {
		t.Errorf("paths = %v（直接依赖应只有一条 ngm.json → x/dep）", rep.Paths)
	}
}

// TestV73WhyFallsBackAndSaysSo 旧 lock（没有图）必须仍然能用——
// 且它必须**说出**自己退回了重新解析（静默回退是这个项目最忌讳的失败形态）。
func TestV73WhyFallsBackAndSaysSo(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	// 剥成旧版形态：清掉所有 requiredBy（与"老 ngm 写的 lock"同形）。
	lf, err := lock.Read(filepath.Join(p, "ngm.lock"))
	if err != nil || lf == nil {
		t.Fatal(err)
	}
	for i := range lf.Dependencies {
		lf.Dependencies[i].RequiredBy = nil
	}
	if lf.HasGraph() {
		t.Fatal("清理后应当没有图（判据本身先自证一次）")
	}
	if err := lock.Write(filepath.Join(p, "ngm.lock"), lf); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := runSplit(t, "why", "github:x/dep", "--dir="+p)
	if code != 0 {
		t.Fatalf("旧 lock 上的 why 必须仍然可用: exit=%d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "重新解析上游清单") {
		t.Errorf("必须说出自己退回了重新解析:\n%s", out)
	}
}

func TestV73SbomExportsTheDependencyGraph(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	// CycloneDX：dependencies 段（根 → 直接依赖）。
	code, out, errOut := runSplit(t, "sbom", "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom exit=%d\n%s\n%s", code, out, errOut)
	}
	var cdx struct {
		Dependencies []struct {
			Ref       string   `json:"ref"`
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
		Components []struct {
			Purl string `json:"purl"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &cdx); err != nil {
		t.Fatalf("sbom 不是合法 JSON: %v", err)
	}
	if len(cdx.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v（应只有根那一条）", cdx.Dependencies)
	}
	if cdx.Dependencies[0].Ref != "ngm:root" {
		t.Errorf("根依赖边的 ref = %q", cdx.Dependencies[0].Ref)
	}
	if len(cdx.Dependencies[0].DependsOn) != 1 ||
		!strings.HasPrefix(cdx.Dependencies[0].DependsOn[0], "pkg:git/github.com/x/dep@") {
		t.Errorf("根应当直接依赖那个依赖: %v", cdx.Dependencies[0].DependsOn)
	}

	// SPDX：DEPENDS_ON（与 CycloneDX 同一份边）。
	code, out, errOut = runSplit(t, "sbom", "--format=spdx", "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom spdx exit=%d\n%s\n%s", code, out, errOut)
	}
	var spdx struct {
		Relationships []struct {
			SPDXElementID      string `json:"spdxElementId"`
			RelatedSPDXElement string `json:"relatedSpdxElement"`
			RelationshipType   string `json:"relationshipType"`
		} `json:"relationships"`
	}
	if err := json.Unmarshal([]byte(out), &spdx); err != nil {
		t.Fatalf("spdx 不是合法 JSON: %v", err)
	}
	dependsOn := 0
	for _, r := range spdx.Relationships {
		if r.RelationshipType == "DEPENDS_ON" {
			dependsOn++
			if r.SPDXElementID != "SPDXRef-DOCUMENT" {
				t.Errorf("根边应从 DOCUMENT 出发: %+v", r)
			}
		}
	}
	if dependsOn != 1 {
		t.Errorf("DEPENDS_ON 关系 = %d（应为 1：根 → 那个依赖）", dependsOn)
	}
}

// 旧 lock ⇒ 依赖段**整段省略**，而不是编一个"一切依赖一切"的假图。
func TestV73SbomOmitsTheGraphWhenTheLockHasNone(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	lf, err := lock.Read(filepath.Join(p, "ngm.lock"))
	if err != nil || lf == nil {
		t.Fatal(err)
	}
	for i := range lf.Dependencies {
		lf.Dependencies[i].RequiredBy = nil
	}
	if err := lock.Write(filepath.Join(p, "ngm.lock"), lf); err != nil {
		t.Fatal(err)
	}

	code, out, _ := runSplit(t, "sbom", "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom exit=%d", code)
	}
	if strings.Contains(out, `"dependencies"`) {
		t.Errorf("lock 没有图时不该编出 dependencies 段:\n%s", out)
	}

	code, out, _ = runSplit(t, "sbom", "--format=spdx", "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom spdx exit=%d", code)
	}
	if strings.Contains(out, "DEPENDS_ON") {
		t.Errorf("lock 没有图时不该编出 DEPENDS_ON:\n%s", out)
	}
}
