package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.77：库标准自检器对**其他包形态**的判据。
//
// 起因是一次真实的"误伤"实测：把标准套在两种常见形态上——
//
//	① CJS 优先 + 有构建步骤（产物已提交）：它**显式**写了 `"type": "commonjs"`，
//	   判据却在要求 `type=module`——而它自己的措辞正是"CJS 需显式说明"。
//	   一个无法被满足的推荐项不是严格，是错的。
//	② monorepo（workspaces）：根**不是包**，于是入口 / exports / 类型 / ESM / 发布范围
//	   五条全红——那是五个"这里没有这个问题"，不是五个缺陷。
//
// 本判据把两种形态各钉一个场景，并守住"归属"这件事：子包的结论必须带上路径前缀，
// 否则合并后的报告说不清是哪个体检没通过。

type stdCheck struct {
	ID     string `json:"id"`
	Level  string `json:"level"`
	Status string `json:"status"`
	Why    string `json:"why"`
}

type stdReport struct {
	Dir        string     `json:"dir"`
	OK         bool       `json:"ok"`
	Complete   bool       `json:"complete"`
	MustFail   int        `json:"mustFail"`
	ShouldFail int        `json:"shouldFail"`
	Skipped    int        `json:"skipped"`
	Checks     []stdCheck `json:"checks"`
}

func parseStdReport(t *testing.T, out string) stdReport {
	t.Helper()
	var rep stdReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("checker output is not a single JSON document: %v\n%s", err, out)
	}
	return rep
}

// findStatus 找某个 id 的状态（id 可带子包前缀）。
func findStatus(rep stdReport, id string) (string, bool) {
	for _, c := range rep.Checks {
		if c.ID == id {
			return c.Status, true
		}
	}
	return "", false
}

func TestV77LibraryStandardHandlesOtherPackageShapes(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")

	// ---- ① CJS 优先 + 构建产物：显式 commonjs 必须被当成"写明的选择" ----------
	cjs := t.TempDir()
	writeCJSFixture(t, cjs)
	out, code := runChecker(t, bash, script, cjs, "--strict")
	if code != 0 {
		t.Fatalf("显式 type=commonjs 的包必须通过（含 --strict），got exit %d:\n%s", code, out)
	}
	rep := parseStdReport(t, out)
	if status, ok := findStatus(rep, "esm-first"); !ok || status != "pass" {
		t.Errorf("esm-first 对显式 commonjs 应判 pass（判据自己的措辞就是「CJS 需显式说明」），got %q", status)
	}
	if status, ok := findStatus(rep, "dist-committed"); !ok || status != "pass" {
		t.Errorf("dist-committed 应 pass（产物已提交且未被忽略），got %q", status)
	}

	// 牙齿：把 dist 从"已提交"变成"被忽略" ⇒ 必做项红。
	writeFileAt(t, cjs, ".gitignore", "node_modules\ndist\n")
	out, code = runChecker(t, bash, script, cjs)
	if code != 1 {
		t.Fatalf("dist 被 .gitignore 排除后必须 exit 1，got %d:\n%s", code, out)
	}
	if status, _ := findStatus(parseStdReport(t, out), "dist-committed"); status != "fail" {
		t.Errorf("dist-committed 应为 fail，got %q", status)
	}

	// ---- ② monorepo：根不是包（包专属判据不适用），子包逐个按同一条判据体检 ----
	mono := t.TempDir()
	writeMonorepoFixture(t, mono)
	out, code = runChecker(t, bash, script, mono, "--strict")
	if code != 0 {
		t.Fatalf("合规的 monorepo 必须通过，got exit %d:\n%s", code, out)
	}
	rep = parseStdReport(t, out)

	// 根：包专属判据全部是"不适用"，且**没有** fail。
	rootOnly := 0
	for _, c := range rep.Checks {
		if strings.Contains(c.ID, "/") {
			continue
		}
		rootOnly++
		if c.Status == "fail" {
			t.Errorf("monorepo 根不该有 fail：%s (%s)", c.ID, c.Why)
		}
	}
	for _, id := range []string{"entry-resolvable", "dist-committed", "esm-first",
		"exports-conditions", "types-provided", "publish-scope"} {
		if status, ok := findStatus(rep, id); !ok || status != "skip" {
			t.Errorf("根上的 %s 应为 skip（不是包），got %q（存在=%v）", id, status, ok)
		}
	}

	// 子包：结论必须带路径前缀（归属），且各自的判据真的跑了。
	childChecks := map[string]int{}
	for _, c := range rep.Checks {
		if strings.HasPrefix(c.ID, "packages/a/") {
			childChecks[c.ID]++
		}
	}
	if len(childChecks) == 0 {
		t.Fatalf("子包的判据没有出现在合并结果里（归属丢失）：\n%s", out)
	}
	if status, ok := findStatus(rep, "packages/a/entry-resolvable"); !ok || status != "pass" {
		t.Errorf("子包的 entry-resolvable 应 pass 且带前缀，got %q（存在=%v）", status, ok)
	}
	// 根：6 条包专属（入口/产物/ESM/exports/类型/发布范围）+ engines + semver-tag；
	// 子包各 1 条 semver-tag（v0.78 起 engines 也归"根的包专属"）。
	if rep.Skipped <= 6 {
		t.Errorf("skipped = %d：根应有 7 条包专属 + semver-tag，子包各 1 条 semver-tag", rep.Skipped)
	}

	// 牙齿：workspaces 匹配到的**目录里没有 package.json** ⇒ 显式失败。
	//
	// 注意语义：`packages/*` 只匹配**存在**的目录，所以这里要造一个"目录在、
	// 但没有清单"的子包——而不是删掉一个（删掉之后它自然不在清单里，那不是缺陷）。
	writeFileAt(t, mono, "packages/c/README.md", "# 忘了放 package.json 的子包\n")
	out, code = runChecker(t, bash, script, mono)
	if code != 1 {
		t.Fatalf("workspaces 里的目录缺 package.json 必须 exit 1，got %d:\n%s", code, out)
	}
	rep = parseStdReport(t, out)
	if status, ok := findStatus(rep, "workspace-package-json"); !ok || status != "fail" {
		t.Errorf("应有一条 workspace-package-json 的 fail，got %q（存在=%v）", status, ok)
	}
}

// ---- 夹具 -----------------------------------------------------------------

func writeCJSFixture(t *testing.T, dir string) {
	t.Helper()
	writeFileAt(t, dir, "package.json", `{
  "name": "@idcu/legacy-cjs",
  "version": "0.3.1",
  "type": "commonjs",
  "license": "MIT",
  "main": "./dist/index.cjs",
  "types": "./dist/index.d.ts",
  "exports": { ".": { "types": "./dist/index.d.ts", "require": "./dist/index.cjs" } },
  "files": ["dist"],
  "engines": { "node": ">=18" },
  "scripts": { "build": "tsc -p ." }
}
`)
	writeFileAt(t, dir, "ngm.json", `{
  "schemaVersion": 1,
  "name": "legacy-cjs",
  "version": "0.3.1",
  "runtime": "node",
  "main": "./dist/index.cjs",
  "types": "./dist/index.d.ts"
}
`)
	writeFileAt(t, dir, "dist/index.cjs", "module.exports = { x: 1 };\n")
	writeFileAt(t, dir, "dist/index.d.ts", "export declare const x: number;\n")
	writeFileAt(t, dir, "README.md", "# legacy-cjs\n")
	writeFileAt(t, dir, "LICENSE", "MIT\n")
	writeFileAt(t, dir, ".gitattributes", "* text=auto eol=lf\n")
	writeFileAt(t, dir, ".gitignore", "node_modules\n")
}

func writeMonorepoFixture(t *testing.T, dir string) {
	t.Helper()
	writeFileAt(t, dir, "package.json", `{
  "name": "@idcu/mono",
  "version": "1.0.0",
  "private": true,
  "workspaces": ["packages/*"],
  "license": "MIT",
  "engines": { "node": ">=20" }
}
`)
	writeFileAt(t, dir, "README.md", "# mono\n")
	writeFileAt(t, dir, "LICENSE", "MIT\n")
	writeFileAt(t, dir, ".gitattributes", "* text=auto eol=lf\n")
	writeFileAt(t, dir, ".gitignore", "node_modules\n")

	writeFileAt(t, dir, "packages/a/package.json", `{
  "name": "@idcu/mono-a",
  "version": "1.0.0",
  "type": "module",
  "license": "MIT",
  "exports": { ".": { "types": "./src/index.d.ts", "default": "./src/index.js" } },
  "files": ["src"],
  "engines": { "node": ">=20" }
}
`)
	writeFileAt(t, dir, "packages/a/ngm.json", `{
  "schemaVersion": 1,
  "name": "mono-a",
  "version": "1.0.0",
  "runtime": "node",
  "main": "./src/index.js",
  "types": "./src/index.d.ts"
}
`)
	writeFileAt(t, dir, "packages/a/src/index.js", "export const a = 1;\n")
	writeFileAt(t, dir, "packages/a/src/index.d.ts", "export declare const a: number;\n")
	writeFileAt(t, dir, "packages/a/README.md", "# mono-a\n")
}

func writeFileAt(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rmDir(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}
