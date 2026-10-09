package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/vendor"
)

// v0.66：`internal/verify` 的**包内**单测（评估报告 P1#5）。
//
// 这一组只盯**判定本身**——退出码优先级、漂移聚合、建议选择、落地检查的部分
// ——全部可离线构造，不需要真实 git。需要真实 git 的路径（ref 解析、digest
// 重放、mirror 就绪）仍由 cmd 层的验收测试覆盖：这里补的是"最贵的那一层判定
// 此前只被 cmd 层间接看着"这个缺口。

// --- 退出码优先级（observability.md 的契约） ---

func TestV66ExitCodePrecedence(t *testing.T) {
	okDep := DepResult{Name: "a"}
	expectedDep := DepResult{Name: "b", DriftKind: DriftExpected}
	unexpectedDep := DepResult{Name: "c", DriftKind: DriftUnexpected}
	criticalDep := DepResult{Name: "d", DriftKind: DriftCritical}
	operationalDep := DepResult{Name: "e", Err: "network down"}

	cases := []struct {
		name string
		rep  Report
		want int
	}{
		{"全匹配", Report{Dependencies: []DepResult{okDep}}, 0},
		{"预期更新默认不阻断", Report{Dependencies: []DepResult{expectedDep}}, 0},
		{"预期更新 + --strict", Report{Dependencies: []DepResult{expectedDep}, Strict: true}, 1},
		{"非预期漂移", Report{Dependencies: []DepResult{unexpectedDep}}, 1},
		{"非预期漂移 + --allow-drift", Report{Dependencies: []DepResult{unexpectedDep}, AllowDrift: true}, 0},
		{"完整性失败", Report{Dependencies: []DepResult{criticalDep}}, 2},
		{"--allow-drift 不降级 critical", Report{Dependencies: []DepResult{criticalDep}, AllowDrift: true}, 2},
		{"操作性失败", Report{Dependencies: []DepResult{operationalDep}}, 4},
		{"4 高于 2：不完整的校验不得被读作完整性结论", Report{Dependencies: []DepResult{operationalDep, criticalDep}}, 4},
		{"2 高于 1", Report{Dependencies: []DepResult{criticalDep, unexpectedDep}}, 2},
		{"1 高于 0", Report{Dependencies: []DepResult{unexpectedDep, okDep}}, 1},
		{"--strict 与 --allow-drift 作用于不同分类，互不抵消",
			Report{Dependencies: []DepResult{expectedDep, unexpectedDep}, Strict: true, AllowDrift: true}, 1},
		{"空报告", Report{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rep.ExitCode(); got != tc.want {
				t.Errorf("ExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
}

// --- Summarize 的计数与 ExitCode 同步 ---

func TestV66SummarizeCounts(t *testing.T) {
	rep := Report{Dependencies: []DepResult{
		{Name: "ok"},
		{Name: "expected", DriftKind: DriftExpected},
		{Name: "unexpected", DriftKind: DriftUnexpected},
		{Name: "critical", DriftKind: DriftCritical},
		{Name: "operational", Err: "boom"},
	}}
	rep.Summarize()

	s := rep.Summary
	if s.Total != 5 || s.OK != 1 || s.Expected != 1 || s.Unexpected != 1 || s.Critical != 1 || s.Operational != 1 {
		t.Errorf("counts = %+v, want 1/1/1/1/1 with total 5", s)
	}
	// ExitCode 必须与 Summary 同步（它正是 Summize 写回的那一个）
	if s.ExitCode != rep.ExitCode() {
		t.Errorf("Summary.ExitCode = %d, but ExitCode() = %d", s.ExitCode, rep.ExitCode())
	}
	// 有操作性失败 → 4
	if s.ExitCode != 4 {
		t.Errorf("Summary.ExitCode = %d, want 4", s.ExitCode)
	}
}

// --- 漂移聚合 ---

func TestV66AggregateDrift(t *testing.T) {
	cases := []struct {
		name   string
		checks []CheckResult
		want   DriftKind
	}{
		{"空", nil, DriftNone},
		{"全通过", []CheckResult{{Status: StatusOK}, {Status: StatusOK}}, DriftNone},
		{"最严重者胜出", []CheckResult{
			{Drift: DriftExpected}, {Drift: DriftCritical}, {Drift: DriftUnexpected},
		}, DriftCritical},
		{"expected 不遮蔽 unexpected", []CheckResult{{Drift: DriftExpected}, {Drift: DriftUnexpected}}, DriftUnexpected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aggregateDrift(tc.checks); got != tc.want {
				t.Errorf("aggregateDrift = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestV66DriftKindRank(t *testing.T) {
	// Rank 是聚合的排序键：它错一位，最严重者就可能被遮蔽。
	if !(DriftCritical.Rank() > DriftUnexpected.Rank() &&
		DriftUnexpected.Rank() > DriftExpected.Rank() &&
		DriftExpected.Rank() > DriftNone.Rank()) {
		t.Error("Rank 的严重度顺序必须是 critical > unexpected > expected > none")
	}
}

// --- 建议的选择（v0.35 的"最贴近成因的那一层优先"） ---

func TestV66RemediationFor(t *testing.T) {
	cases := []struct {
		name    string
		res     DepResult
		wantHas string
		wantEq  string
	}{
		{
			name:   "成因层自己的建议优先",
			res:    DepResult{Err: "permission denied: net:github.com", ErrHint: "add \"net:github.com\" to permissions.allow"},
			wantEq: "add \"net:github.com\" to permissions.allow",
		},
		{
			name:    "没有成因建议时退回兜底（并点名命令）",
			res:     DepResult{Err: "boom"},
			wantHas: "`ngm install`",
		},
		{
			name:    "预期更新",
			res:     DepResult{Name: "x", DriftKind: DriftExpected},
			wantHas: "`ngm update x`",
		},
		{
			name:    "非预期漂移：先确认再接受",
			res:     DepResult{Name: "y", DriftKind: DriftUnexpected},
			wantHas: "confirm the change with the upstream owner",
		},
		{
			name:    "critical：禁止构建",
			res:     DepResult{Name: "z", DriftKind: DriftCritical},
			wantHas: "do NOT build",
		},
		{
			name:   "无漂移且无错误：没有建议",
			res:    DepResult{Name: "ok"},
			wantEq: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := remediationFor(&tc.res)
			if tc.wantEq != "" && got != tc.wantEq {
				t.Errorf("remediationFor = %q, want %q", got, tc.wantEq)
			}
			if tc.wantHas != "" && !strings.Contains(got, tc.wantHas) {
				t.Errorf("remediationFor = %q, should contain %q", got, tc.wantHas)
			}
		})
	}
}

// --- meta 比对（层 2 的身份类字段） ---

func TestV66MetaMismatches(t *testing.T) {
	dep := &lock.Dependency{Name: "github:x/dep", ArchiveDigest: "sha256:aaaa"}

	if got := metaMismatches(dep, &vendor.Meta{Digest: "sha256:aaaa", ManifestVersion: digest.ManifestVersion}); len(got) != 0 {
		t.Errorf("一致的 meta 不该有发现，got %v", got)
	}
	if got := metaMismatches(dep, &vendor.Meta{Digest: "sha256:bbbb", ManifestVersion: digest.ManifestVersion}); len(got) != 1 {
		t.Errorf("digest 不符必须报出，got %v", got)
	}
	if got := metaMismatches(dep, &vendor.Meta{Digest: "sha256:aaaa", ManifestVersion: "ngm-archive-digest/v0"}); len(got) != 1 {
		t.Errorf("清单规范版本不符必须报出，got %v", got)
	}
}

// --- 落地检查：逃逸 symlink 是 critical（v0.66 新增的检查项） ---

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeStoreEntry 手写一份 v2 清单与 meta（verify 只读它们，不需要 vendor 的私有 API）。
func writeStoreEntry(t *testing.T, store *vendor.ContentStore, dg, entryTarget string) {
	t.Helper()
	dir := store.V2Dir(dg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, store.ManifestPath(dg), map[string]any{
		"schemaVersion":   2,
		"manifestVersion": digest.ManifestVersion,
		"digest":          dg,
		"entries": []map[string]any{
			{"path": "escape.ts", "mode": digest.ModeSymlink, "size": len(entryTarget), "target": entryTarget},
		},
	})
	writeJSONFile(t, store.MetaPathV2(dg), map[string]any{
		"schemaVersion":   1,
		"repo":            "github:x/dep",
		"commit":          strings.Repeat("b", 40),
		"digest":          dg,
		"manifestVersion": digest.ManifestVersion,
	})
}

func TestV66LandingFlagsEscapingSymlinkAsCritical(t *testing.T) {
	contentRoot, vendorRoot := t.TempDir(), t.TempDir()
	store := vendor.NewContentStore(contentRoot)

	dg := "sha256:" + strings.Repeat("22", 32)
	writeStoreEntry(t, store, dg, "../../outside.ts")

	// vendor 落地路径存在（否则会先报 vendor path missing，掩盖逃逸这一条）。
	if err := os.MkdirAll(filepath.Join(vendorRoot, "github.com", "x", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}

	dep := &lock.Dependency{
		Name:          "github:x/dep",
		Ref:           "v1",
		RefType:       "tag",
		Commit:        strings.Repeat("b", 40),
		ArchiveDigest: dg,
		VendorPath:    "github.com/x/dep",
	}
	res := checkLanding(context.Background(), "", dep, store,
		Options{ContentRoot: contentRoot, VendorRoot: vendorRoot})

	if res.Status != StatusFail || res.Drift != DriftCritical {
		t.Fatalf("逃逸 symlink 必须判为 critical：status=%q drift=%q detail=%s", res.Status, res.Drift, res.Detail)
	}
	if !strings.Contains(res.Detail, "escapes the dependency tree") {
		t.Errorf("detail 必须说清是逃逸（而不是别的差异）：%s", res.Detail)
	}
}

// 对照：包内目标的 symlink 不触发逃逸判定（其余差异照旧由结构比对负责）。
// 对照组的意义在于"该响的响、不该响的不响"——只测前者会漏掉误报。
func TestV66LandingDoesNotFlagInTreeSymlink(t *testing.T) {
	contentRoot, vendorRoot := t.TempDir(), t.TempDir()
	store := vendor.NewContentStore(contentRoot)

	dg := "sha256:" + strings.Repeat("33", 32)
	writeStoreEntry(t, store, dg, "./index.ts")

	if err := os.MkdirAll(filepath.Join(vendorRoot, "github.com", "x", "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	dep := &lock.Dependency{
		Name:          "github:x/dep",
		Ref:           "v1",
		RefType:       "tag",
		Commit:        strings.Repeat("b", 40),
		ArchiveDigest: dg,
		VendorPath:    "github.com/x/dep",
	}
	res := checkLanding(context.Background(), "", dep, store,
		Options{ContentRoot: contentRoot, VendorRoot: vendorRoot})

	if strings.Contains(res.Detail, "escapes the dependency tree") {
		t.Errorf("包内目标的 symlink 不该被报成逃逸：%s", res.Detail)
	}
}
