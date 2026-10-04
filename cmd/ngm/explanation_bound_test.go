package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV17ExplanationIsBoundedEndToEnd 是 ADR-024 的端到端验收：
// `why` 的路径枚举与 `tree` 的展开在**真实仓库、真实 mirror** 上都有上界，
// 而且达到上界时**说出来**。
//
// 单元级的那两张网（internal/observability/limits_test.go）证明的是"界本身正确"，
// 这一张证明的是**接线正确**：命令真的把界传下去了、真的把标记渲染出来了、
// 真的没有把截断当成错误（退出码不变）。
//
// 夹具是 depth 层的格：每层的两个仓库都依赖下一层的两个仓库，
// 于是从根到叶的路径数 = 2^depth。depth=7 → 128 条，超过 why 的 64 上限。
func TestV17ExplanationIsBoundedEndToEnd(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	const depth = 7
	rootA, rootB, leaf := latticeUpstreams(t, depth)

	proj := newProject(t)
	for _, a := range [][]string{
		{"add", rootA + "@v1", "--ref-type=tag", "--dir=" + proj},
		{"add", rootB + "@v1", "--ref-type=tag", "--dir=" + proj},
		{"install", "--dir=" + proj},
	} {
		if code, out := runCaptureCode(t, a...); code != 0 {
			t.Fatalf("%v: %s", a, out)
		}
	}

	type whyJSON struct {
		Paths          [][]string `json:"paths"`
		PathsTruncated bool       `json:"pathsTruncated"`
		PathsLimit     int        `json:"pathsLimit"`
	}
	readWhy := func(args ...string) (int, whyJSON, string) {
		code, out := runCaptureCode(t, args...)
		var rep whyJSON
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatalf("%v: --json must be one document: %v\n%s", args, err, out)
		}
		return code, rep, out
	}

	// ① 默认：最多 64 条，且**说出来**。
	code, rep, out := readWhy("why", leaf, "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("why --json exit=%d:\n%s", code, out)
	}
	if len(rep.Paths) != 64 {
		t.Errorf("paths=%d want 64 (MaxWhyPaths)", len(rep.Paths))
	}
	if !rep.PathsTruncated || rep.PathsLimit != 64 {
		t.Errorf("the report must say it stopped: truncated=%v limit=%d", rep.PathsTruncated, rep.PathsLimit)
	}

	// 文字版同样要写出来——CI 读 JSON，人读文字，两边都不许静默。
	code, text := runCaptureCode(t, "why", leaf, "--dir="+proj)
	if code != 0 {
		t.Fatalf("why exit=%d:\n%s", code, text)
	}
	if !strings.Contains(text, "达到上限") || !strings.Contains(text, "未列出") {
		t.Errorf("the text report must say the list is incomplete:\n%s", text)
	}
	if !strings.Contains(text, "--all") {
		t.Errorf("it must say how to see the rest:\n%s", text)
	}

	// ② `--all` 解除上界：128 条路径全列出来，且不再标截断。
	code, rep, out = readWhy("why", leaf, "--all", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("why --all exit=%d:\n%s", code, out)
	}
	if len(rep.Paths) != 1<<depth {
		t.Errorf("--all: paths=%d want %d", len(rep.Paths), 1<<depth)
	}
	if rep.PathsTruncated || rep.PathsLimit != 0 {
		t.Errorf("--all must not claim truncation: truncated=%v limit=%d", rep.PathsTruncated, rep.PathsLimit)
	}

	// ③ tree 在这种图形上**不该**被截断（预算 4096 条目，这里是 3·2^7-2 = 382），
	//    但一旦被截断也必须说出来——这一条同时是"正常图形不受影响"的对照。
	type treeJSON struct {
		Entries          []any `json:"entries"`
		EntriesTruncated bool  `json:"entriesTruncated"`
		EntriesLimit     int   `json:"entriesLimit"`
	}
	code, out = runCaptureCode(t, "tree", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("tree --json exit=%d:\n%s", code, out)
	}
	var tr treeJSON
	if err := json.Unmarshal([]byte(out), &tr); err != nil {
		t.Fatalf("tree --json must be one document: %v\n%s", err, out)
	}
	if tr.EntriesTruncated {
		t.Errorf("this graph is well inside the budget (382 entries) and must not be marked truncated")
	}
	if tr.EntriesLimit != 4096 {
		t.Errorf("entriesLimit=%d want 4096 (the default budget must be visible even when unused)", tr.EntriesLimit)
	}
}

// latticeUpstreams 造一个 depth 层的格：每层的两个仓库都依赖下一层的两个仓库。
// 返回根声明的两个 slug 与叶 slug；路径数 = 2^depth。
func latticeUpstreams(t *testing.T, depth int) (rootA, rootB, leaf string) {
	t.Helper()

	leaf = fmt.Sprintf("github:lat/e2e-leaf-%d", depth)
	scUpstream(t, leaf, "export const l = 1\n", "")

	manifest := func(name string, children []string) string {
		var deps []string
		for _, c := range children {
			deps = append(deps, fmt.Sprintf(`{"name":%q,"ref":"v1","refType":"tag"}`, c))
		}
		return fmt.Sprintf(`{"name":%q,"version":"1.0.0","runtime":"node","dependencies":[%s]}`,
			name, strings.Join(deps, ","))
	}

	children := []string{leaf}
	for level := 1; level <= depth; level++ {
		a := fmt.Sprintf("github:lat/e2e-a%d-%d", depth, level)
		b := fmt.Sprintf("github:lat/e2e-b%d-%d", depth, level)
		scUpstream(t, a, "export const a = 1\n", manifest(fmt.Sprintf("lat-a%d-%d", depth, level), children))
		scUpstream(t, b, "export const b = 1\n", manifest(fmt.Sprintf("lat-b%d-%d", depth, level), children))
		children = []string{a, b}
	}
	return children[0], children[1], leaf
}
