package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.79：把"扫自己的 vendored 产物"固化成**一条命令**。
//
// 这是 v0.76 复盘的候选 4，两版落空——落空的理由每次都是"它不属于产品代码"，
// 而它真正属于的是**标准**：标准说了"每个依赖都要符合"，那就得有一条命令能
// 对"我 vendor 了什么"整体回答"合不合格"，而不是靠人记得逐个跑。
//
// 判据里有三条是"这条命令自己不能出错"的：
//
//	① 事实源是 `ngm.mappings.json`（我 vendor 了什么），不是遍历 `ngm.vendor/`——
//	   遍历会把 prune 残留、手工塞进去的东西也算成依赖，同一份产物结论能飘；
//	② 结果必须**归属**回依赖名（id 前缀 = `from`），否则"谁不合格"要人去数；
//	③ 包装器**不许少跑判据**：某个依赖在 vendored 报告里的判据条数，
//	   必须等于直接对它跑标准自检器的条数。

type vendoredReport struct {
	Dir          string     `json:"dir"`
	OK           bool       `json:"ok"`
	Dependencies int        `json:"dependencies"`
	Checks       []stdCheck `json:"checks"`
}

func runVendoredScan(t *testing.T, bash, script, dir string, extra ...string) (string, int) {
	t.Helper()
	args := append([]string{filepath.ToSlash(script), filepath.ToSlash(dir)}, extra...)
	cmd := exec.Command(bash, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		return string(out), exitErr.ExitCode()
	}
	t.Fatalf("running %s: %v\n%s", script, err, out)
	return string(out), -1
}

func TestV79VendoredScanRunsTheStandardOnEveryDependency(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-vendored-standard.sh")
	std := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")

	dir := t.TempDir()
	writeFileAt(t, dir, "ngm.mappings.json", `{
  "version": 1,
  "mappings": [
    { "from": "gitee:idcu/good", "to": "./ngm.vendor/gitee.com/idcu/good", "main": "./src/index.js", "types": "./src/index.d.ts" },
    { "from": "gitee:idcu/bad", "to": "./ngm.vendor/gitee.com/idcu/bad", "main": "./index.js" }
  ]
}
`)
	writeVendoredGood(t, dir, "ngm.vendor/gitee.com/idcu/good")
	writeVendoredBad(t, dir, "ngm.vendor/gitee.com/idcu/bad")

	// ---- ① 文本模式：点名不合格的那个，退出码 1 ----
	out, code := runVendoredScan(t, bash, script, dir)
	if code != 1 {
		t.Fatalf("有不合格依赖时必须 exit 1，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "gitee:idcu/bad") {
		t.Errorf("不合格的依赖必须被点名（否则\"谁不合格\"要人去数）：\n%s", out)
	}
	if !strings.Contains(out, "no-install-hooks") {
		t.Errorf("点名的应该是**具体的判据**，不只依赖名：\n%s", out)
	}

	// ---- ② --json：归属（id 前缀 = 依赖名）+ 计数对账 ----
	out, code = runVendoredScan(t, bash, script, dir, "--json")
	if code != 1 {
		t.Fatalf("--json 下同样 exit 1，got %d:\n%s", code, out)
	}
	var rep vendoredReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("vendored 报告不是单个 JSON 文档：%v\n%s", err, out)
	}
	if rep.Dependencies != 2 {
		t.Errorf("dependencies = %d, want 2", rep.Dependencies)
	}
	if rep.OK {
		t.Error("有依赖不合格时 ok 必须是 false")
	}
	goodN, badN := 0, 0
	for _, c := range rep.Checks {
		switch {
		case strings.HasPrefix(c.ID, "gitee:idcu/good/"):
			goodN++
		case strings.HasPrefix(c.ID, "gitee:idcu/bad/"):
			badN++
		}
	}
	if goodN == 0 || badN == 0 {
		t.Fatalf("两个依赖的判据都必须带前缀归属（good=%d bad=%d）：\n%s", goodN, badN, out)
	}

	// ③ 包装器不许少跑判据：与直接跑标准自检器的条数对账。
	direct, _ := runChecker(t, bash, std, filepath.Join(dir, "ngm.vendor/gitee.com/idcu/good"))
	directRep := parseStdReport(t, direct)
	if goodN != len(directRep.Checks) {
		t.Errorf("vendored 报告里 good 有 %d 条判据，而直接自检是 %d 条——包装器漏跑了", goodN, len(directRep.Checks))
	}

	// ---- ④ 牙齿：mappings 指向不存在的目录 ⇒ 显式必做失败 ----
	writeFileAt(t, dir, "ngm.mappings.json", `{
  "version": 1,
  "mappings": [ { "from": "gitee:idcu/ghost", "to": "./ngm.vendor/gitee.com/idcu/ghost" } ]
}
`)
	out, code = runVendoredScan(t, bash, script, dir)
	if code != 1 {
		t.Fatalf("映射指向不存在的目录必须 exit 1，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "vendored-dir-missing") {
		t.Errorf("应有一条 vendored-dir-missing 的失败：\n%s", out)
	}

	// ---- ⑤ 牙齿：没有 mappings ⇒ exit 2（**没有查过 ≠ 查过且合格**） ----
	empty := t.TempDir()
	out, code = runVendoredScan(t, bash, script, empty)
	if code != 2 {
		t.Fatalf("没有 mappings 必须 exit 2（与\"全部合格\"分开），got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ngm.mappings.json") {
		t.Errorf("exit 2 的说明必须指出缺的是哪一份事实源：\n%s", out)
	}
}

// writeVendoredGood 造一个合格的 vendored 依赖（无 .git —— 与真实 vendored 副本一致）。
func writeVendoredGood(t *testing.T, dir, rel string) {
	t.Helper()
	writeFileAt(t, dir, rel+"/package.json", `{
  "name": "@idcu/good",
  "version": "1.0.0",
  "type": "module",
  "license": "MIT",
  "main": "./src/index.js",
  "types": "./src/index.d.ts",
  "files": ["src"],
  "engines": { "node": ">=20" }
}
`)
	writeFileAt(t, dir, rel+"/ngm.json", `{
  "schemaVersion": 1,
  "name": "good",
  "version": "1.0.0",
  "runtime": "node",
  "main": "./src/index.js",
  "types": "./src/index.d.ts"
}
`)
	writeFileAt(t, dir, rel+"/src/index.js", "export const good = 1;\n")
	writeFileAt(t, dir, rel+"/src/index.d.ts", "export declare const good: number;\n")
	writeFileAt(t, dir, rel+"/README.md", "# good\n")
	writeFileAt(t, dir, rel+"/LICENSE", "MIT\n")
	writeFileAt(t, dir, rel+"/.gitattributes", "* text=auto eol=lf\n")
}

// writeVendoredBad 造一个**不合格**的 vendored 依赖（带安装期钩子 —— 必做项）。
func writeVendoredBad(t *testing.T, dir, rel string) {
	t.Helper()
	writeFileAt(t, dir, rel+"/package.json", `{
  "name": "@idcu/bad",
  "version": "1.0.0",
  "license": "MIT",
  "main": "./index.js",
  "scripts": { "postinstall": "echo hi" }
}
`)
	writeFileAt(t, dir, rel+"/index.js", "module.exports = { bad: 1 };\n")
	writeFileAt(t, dir, rel+"/README.md", "# bad\n")
}
