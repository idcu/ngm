package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.80：**嵌套 monorepo** —— 子包自己也是 monorepo。
//
// 此前它掉进"它是个包"的判据里，于是红的是 `entry-resolvable`（"三层都没找到入口"）。
// **用错理由红，等于没红**：看报告的人会去给出入口，而那个仓库缺的不是入口——
// 它是个 monorepo 根，缺的东西在它自己的子包里。
//
// 这一版把它分成三件事，每件都有一条断言：
//
//	① 说得对：一条 `nested-monorepo` 记录，点名它声明的子包模式与"约 N 个子包没被体检"；
//	② 不假装查过：它的**包专属**判据全部记"不适用"（而不是必做失败），
//	   孙辈**一条判据都不出现**；
//	③ 不借机豁免：**根能承担的判据照旧生效**（例如 README 缺失仍要红）——
//	   "嵌套形态"不是免检牌。
//
// 外加一条口径断言：结论里 `complete:false`——"已查的通过"与"符合标准"必须分得开。

func TestV80NestedMonorepoIsReportedAsIncomplete(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")

	dir := t.TempDir()
	writeNestedMonorepoFixture(t, dir)

	out, code := runChecker(t, bash, script, dir, "--strict")
	if code != 0 {
		t.Fatalf("嵌套形态不该被判不合格（它只是没被查全），got exit %d:\n%s", code, out)
	}
	rep := parseStdReport(t, out)

	// ① 说得对
	status, ok := findStatus(rep, "packages/inner/nested-monorepo")
	if !ok || status != "skip" {
		t.Fatalf("应有 packages/inner/nested-monorepo 且状态为 skip，got %q（存在=%v）:\n%s", status, ok, out)
	}
	var nestedWhy string
	for _, c := range rep.Checks {
		if c.ID == "packages/inner/nested-monorepo" {
			nestedWhy = c.Why
		}
	}
	for _, want := range []string{"units/*", "1 个子包", "没有被体检"} {
		if !strings.Contains(nestedWhy, want) {
			t.Errorf("nested-monorepo 的说明必须写清 %q（声明与「没查到什么」）：%s", want, nestedWhy)
		}
	}

	// ② 不假装查过：包专属判据是不适用（不是失败），孙辈不出现
	if status, ok := findStatus(rep, "packages/inner/entry-resolvable"); !ok || status != "skip" {
		t.Errorf("嵌套根的入口判据应为 skip（理由必须是「它是 monorepo 根」，不是「找不到入口」），got %q（存在=%v）", status, ok)
	}
	for _, c := range rep.Checks {
		if strings.Contains(c.ID, "g1") {
			t.Errorf("孙辈不该出现在结论里（本工具不递归，且绝不假装查过）：%s", c.ID)
		}
	}

	// ③ 口径：`complete` 与 `ok` **互不顶替**——这一组是"已查的都过、但没查全"。
	out, code = runChecker(t, bash, script, dir, "--json")
	rep = parseStdReport(t, out)
	if code != 0 || !rep.OK {
		t.Fatalf("这一组已查的判据都过 ⇒ ok 应为 true 且 exit 0，got exit %d ok=%v:\n%s", code, rep.OK, out)
	}
	if rep.Complete {
		t.Error("含有嵌套 monorepo ⇒ complete 必须是 false（「已查的通过」与「符合标准」不能混为一谈）")
	}

	// ④ 不借机豁免：删掉嵌套根自己的 README ⇒ 根能承担的判据照旧红。
	//
	// 带 `--strict`：`readme` 是**推荐项**，而推荐项在不加 --strict 时不影响退出码
	// （那条纪律有别的判据守着）。这里要证的是"嵌套形态没有把根上的判据关掉"。
	removeFileAt(t, filepath.Join(dir, "packages/inner", "README.md"))
	out, code = runChecker(t, bash, script, dir, "--strict")
	if code != 1 {
		t.Fatalf("嵌套根缺 README（--strict）必须 exit 1（嵌套不是免检牌），got %d:\n%s", code, out)
	}
	if status, _ := findStatus(parseStdReport(t, out), "packages/inner/readme"); status != "fail" {
		t.Errorf("packages/inner/readme 应为 fail，got %q", status)
	}
	// 两者可以同时为 false（有失败 + 没查全），此时仍要各说各的。
	out, code = runChecker(t, bash, script, dir, "--json", "--strict")
	rep = parseStdReport(t, out)
	if code != 1 || rep.OK || rep.Complete {
		t.Errorf("有失败且没查全时 exit=1 ok=false complete=false，got exit=%d ok=%v complete=%v", code, rep.OK, rep.Complete)
	}
}

// writeNestedMonorepoFixture 造 外层 monorepo → 子包 inner（自己是 monorepo）→ units/g1。
func writeNestedMonorepoFixture(t *testing.T, dir string) {
	t.Helper()
	writeFileAt(t, dir, "package.json", `{
  "name": "@idcu/outer",
  "version": "1.0.0",
  "private": true,
  "license": "MIT",
  "workspaces": ["packages/*"]
}
`)
	writeFileAt(t, dir, "README.md", "# outer\n")
	writeFileAt(t, dir, "LICENSE", "MIT\n")
	writeFileAt(t, dir, ".gitattributes", "* text=auto eol=lf\n")

	writeExportsOnlyChild(t, dir, "packages/simple", "@idcu/simple", "module")

	writeFileAt(t, dir, "packages/inner/package.json", `{
  "name": "@idcu/inner",
  "version": "1.0.0",
  "private": true,
  "license": "MIT",
  "workspaces": ["units/*"]
}
`)
	writeFileAt(t, dir, "packages/inner/README.md", "# inner（嵌套的 monorepo 根）\n")
	writeExportsOnlyChild(t, dir, "packages/inner/units/g1", "@idcu/g1", "module")
}

func removeFileAt(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
