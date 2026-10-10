package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// v0.78：monorepo 的**另外两个信号**（pnpm / lerna）与"根可以没有清单"。
//
// v0.77 只认得 npm 的 `workspaces` 数组。实测另外两种常见形态时，
// 三件事同时暴露：
//
//	① pnpm 的 monorepo 根**可以没有** package.json（子包清单在 pnpm-workspace.yaml），
//	   而"没有根清单"此前是必做红；
//	② lerna 的子包清单只写在 lerna.json 里（根有清单但没有 workspaces）⇒ 整个仓库被当成单包；
//	③ 子包**只写 `exports`**（正是标准推荐的第 1 条）时，自检器找不到入口——
//	   因为自检器的候选列表没有读 `exports`，而产品侧 `internal/mappings.InferEntry`
//	   一直是读的（字符串 / `"."` 字符串 / 条件映射三种写法）。**自检器比产品严 = 误判**。
//
// 本判据把三件事各钉住一条，并守住"读不出来的写法不许猜"。

func TestV78LibraryStandardReadsOtherMonorepoSignals(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")

	// ---- ① pnpm：根没有 package.json，子包清单在 pnpm-workspace.yaml ----
	pnpmDir := t.TempDir()
	writePnpmFixture(t, pnpmDir)
	out, code := runChecker(t, bash, script, pnpmDir, "--strict")
	if code != 0 {
		t.Fatalf("合规的 pnpm monorepo（根无清单）必须通过，got exit %d:\n%s", code, out)
	}
	rep := parseStdReport(t, out)

	for _, id := range []string{"pkg-json-exists", "pkg-name", "pkg-version", "license", "engines"} {
		if status, ok := findStatus(rep, id); !ok || status != "skip" {
			t.Errorf("pnpm 根（没有根清单）上的 %s 应为 skip，got %q（存在=%v）", id, status, ok)
		}
	}
	// 子包只写 exports —— 入口与类型都必须从 exports 里读出来（与 InferEntry 对齐）。
	if status, ok := findStatus(rep, "packages/alpha/entry-resolvable"); !ok || status != "pass" {
		t.Errorf("只写 exports 的子包必须解析出入口（与 InferEntry 对齐），got %q（存在=%v）\n%s", status, ok, out)
	}
	if status, ok := findStatus(rep, "packages/alpha/types-provided"); !ok || status != "pass" {
		t.Errorf("`exports.types` 必须算类型入口，got %q（存在=%v）", status, ok)
	}

	// ---- ② lerna：根有清单、没有 npm workspaces，子包在 lerna.json ----
	lernaDir := t.TempDir()
	writeLernaFixture(t, lernaDir)
	out, code = runChecker(t, bash, script, lernaDir, "--strict")
	if code != 0 {
		t.Fatalf("合规的 lerna monorepo 必须通过，got exit %d:\n%s", code, out)
	}
	rep = parseStdReport(t, out)
	if status, ok := findStatus(rep, "packages/beta/entry-resolvable"); !ok || status != "pass" {
		t.Errorf("lerna 的子包必须被体检到（信号来自 lerna.json），got %q（存在=%v）", status, ok)
	}
	if status, ok := findStatus(rep, "engines"); !ok || status != "skip" {
		t.Errorf("monorepo 根的 engines 应为 skip（根不发布），got %q（存在=%v）", status, ok)
	}

	// ---- ③ 牙齿：读不出来的 packages 写法 ⇒ 显式失败，不许猜 ----
	writeFileAt(t, pnpmDir, "pnpm-workspace.yaml", "packages:\n  - { glob: packages/*, dirs: [a] }\n")
	out, code = runChecker(t, bash, script, pnpmDir)
	if code != 1 {
		t.Fatalf("读不出的 packages 写法必须 exit 1，got %d:\n%s", code, out)
	}
	if status, ok := findStatus(parseStdReport(t, out), "workspace-manifest-unreadable"); !ok || status != "fail" {
		t.Errorf("应有 workspace-manifest-unreadable 的 fail，got %q（存在=%v）", status, ok)
	}

	// ---- ④ 花括号 glob：`{packages,apps}/*` 必须展开成两条模式（逗号不是分隔符）----
	writeFileAt(t, pnpmDir, "pnpm-workspace.yaml", "packages:\n  - '{packages,apps}/*'\n")
	out, code = runChecker(t, bash, script, pnpmDir, "--strict")
	if code != 0 {
		t.Fatalf("花括号 glob 必须展开（两个目录都存在），got exit %d:\n%s", code, out)
	}
	rep = parseStdReport(t, out)
	for _, id := range []string{"packages/alpha/entry-resolvable", "apps/web/entry-resolvable"} {
		if status, ok := findStatus(rep, id); !ok || status != "pass" {
			t.Errorf("花括号 glob 的展开结果应当覆盖 %s，got %q（存在=%v）", id, status, ok)
		}
	}
	for _, c := range rep.Checks {
		if c.ID == "workspace-package-json" && strings.Contains(c.Why, "{") {
			t.Errorf("花括号 glob 被当成了字面路径：%s", c.Why)
		}
	}
}

// writePnpmFixture 造一个 pnpm 形态的 monorepo：**根没有 package.json**。
func writePnpmFixture(t *testing.T, dir string) {
	t.Helper()
	writeFileAt(t, dir, "pnpm-workspace.yaml", "# pnpm 的 workspace 清单\npackages:\n  # 子包\n  - 'packages/*'\n  - \"apps/web\"\n")
	writeFileAt(t, dir, "README.md", "# pnpm 根（没有 package.json）\n")
	writeFileAt(t, dir, ".gitattributes", "* text=auto eol=lf\n")
	writeFileAt(t, dir, ".gitignore", "node_modules\n")

	// 子包只写 exports（不给 main/types 字段）——正是标准推荐的第 1 条。
	writeExportsOnlyChild(t, dir, "packages/alpha", "@idcu/alpha", "module")
	writeExportsOnlyChild(t, dir, "apps/web", "@idcu/web", "module")
}

func writeLernaFixture(t *testing.T, dir string) {
	t.Helper()
	writeFileAt(t, dir, "package.json", `{
  "name": "@idcu/lerna-root",
  "version": "1.0.0",
  "private": true,
  "license": "MIT"
}
`)
	writeFileAt(t, dir, "lerna.json", `{
  "version": "independent",
  "packages": ["packages/*"]
}
`)
	writeFileAt(t, dir, "README.md", "# lerna 根\n")
	writeFileAt(t, dir, "LICENSE", "MIT\n")
	writeFileAt(t, dir, ".gitattributes", "* text=auto eol=lf\n")
	writeFileAt(t, dir, ".gitignore", "node_modules\n")
	writeExportsOnlyChild(t, dir, "packages/beta", "@idcu/beta", "commonjs")
}

// writeExportsOnlyChild 造一个**只靠 exports 声明入口与类型**的子包。
func writeExportsOnlyChild(t *testing.T, dir, rel, name, moduleType string) {
	t.Helper()
	writeFileAt(t, dir, rel+"/package.json", `{
  "name": "`+name+`",
  "version": "1.0.0",
  "type": "`+moduleType+`",
  "license": "MIT",
  "exports": { ".": { "types": "./src/index.d.ts", "default": "./src/index.js" } },
  "files": ["src"],
  "engines": { "node": ">=20" }
}
`)
	writeFileAt(t, dir, rel+"/src/index.js", "export const x = 1;\n")
	writeFileAt(t, dir, rel+"/src/index.d.ts", "export declare const x: number;\n")
	writeFileAt(t, dir, rel+"/README.md", "# "+name+"\n")
}
