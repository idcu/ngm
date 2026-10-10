package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.76：**自检器自己的判据**。
//
// 为什么需要它——两次缺陷都是"跑出来才发现"，而它们的共同点是
// **脚本的结论本身没有任何东西守着**：
//
//  1. v0.74：脚本里的 `$'\r'` 被改写成 `$'\\r'`，判据于是按错的模式搜索——
//     它报了红，看起来在认真工作（假阳性比假阴性更难发现）；
//  2. v0.76：拿它扫 ngm 自己的 vendored 副本（没有 `.git`）时，
//     把"没有 semver tag"当成缺陷（假阴性之外的第三种错：判错了场景）。
//
// 本判据把**三种结论各钉一个场景**：pass（合规夹具）· fail（注入缺陷要点名）·
// skip（不是 git 仓库 ⇒ "不适用"，显式打印且不计入）。
//
// 它需要 bash + node；缺任一时**大声跳过**（写明原因），而不是静默通过——
// 这与脚本自身的行为一致：缺 node 时它 exit 2，绝不假装检查过。

func TestV76LibraryStandardCheckerHoldsItsVerdicts(t *testing.T) {
	bash := findBash(t)
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("this net needs `node` on PATH (the checker reads package.json with it); " +
			"the checker itself refuses with exit 2 rather than skipping silently")
	}
	_ = node

	script := filepath.Join(repoRoot(t), "scripts", "check-library-standard.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the checker has no implementation to test: %v", err)
	}

	dir := t.TempDir()
	writeConformingFixture(t, dir)

	// ---- ① 合规夹具：ok:true，唯一"不适用"是 semver-tag（夹具不是 git 仓库）
	out, code := runChecker(t, bash, script, dir)
	if code != 0 {
		t.Fatalf("a conforming fixture must exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("合规夹具必须 ok:true:\n%s", out)
	}
	if !strings.Contains(out, `"skipped":1`) {
		t.Errorf("不是 git 仓库时应恰好一条「不适用」（semver-tag）:\n%s", out)
	}
	if strings.Contains(out, `"status":"fail"`) {
		t.Errorf("合规夹具不该有任何 fail:\n%s", out)
	}

	// ---- ② 注入 postinstall：必须点名 no-install-hooks 并 exit 1
	editJSON(t, filepath.Join(dir, "package.json"), func(o map[string]any) {
		o["scripts"] = map[string]any{"postinstall": "node build.js"}
	})
	out, code = runChecker(t, bash, script, dir)
	if code != 1 {
		t.Fatalf("注入 postinstall 后必须 exit 1，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no-install-hooks") {
		t.Errorf("失败要点名具体判据（no-install-hooks）:\n%s", out)
	}

	// 撤销注入（后面的场景要干净起点）
	editJSON(t, filepath.Join(dir, "package.json"), func(o map[string]any) { delete(o, "scripts") })

	// ---- ③ 删掉 ngm.json 的 runtime：v0.75 新增的那条必须生效
	editJSON(t, filepath.Join(dir, "ngm.json"), func(o map[string]any) { delete(o, "runtime") })
	out, code = runChecker(t, bash, script, dir)
	if code != 1 {
		t.Fatalf("ngm.json 缺 runtime 必须 exit 1，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ngm-json-valid") || !strings.Contains(out, "runtime") {
		t.Errorf("失败要点名 ngm-json-valid 与 runtime:\n%s", out)
	}
}

// runChecker 跑一次自检器（--json），返回输出与退出码。
func runChecker(t *testing.T, bash, script, dir string) (string, int) {
	t.Helper()
	// 传正斜杠路径：Git bash 认它（反斜杠在它那里是转义字符）
	cmd := exec.Command(bash, filepath.ToSlash(script), filepath.ToSlash(dir), "--json")
	raw, err := cmd.CombinedOutput()
	out := string(raw)
	if err == nil {
		return out, 0
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		return out, exitErr.ExitCode()
	}
	t.Fatalf("running the checker failed: %v\n%s", err, out)
	return out, -1
}

// asExitError 是 errors.As 的薄包装（避免在测试里到处 import errors）。
func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

// findBash 找 bash：**先 Git for Windows，再退到 PATH**——
// 与 scripts/check-library-standard.ps1 的查找顺序一致，理由也在那里写着：
// 装了 WSL 的机器上 PATH 里的 `bash` 是 `C:\Windows\System32\bash.exe`，
// 它与 Windows 路径不互通（拿 Windows 路径喂它只会得到一段"安装 WSL"的提示，
// 而退出码是 1——看起来像"库不合规"，其实是跑错了解释器）。
func findBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		for _, c := range []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin", "bash.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git", "bin", "bash.exe"),
		} {
			if c == "" {
				continue
			}
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	t.Skip("this net needs `bash` (Git for Windows ships it); " +
		"without it the checker cannot run — and skipping is loud on purpose")
	return ""
}

// writeConformingFixture 造一个最小但**完全合规**的库（每个必做项都满足）。
func writeConformingFixture(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, content string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{
  "name": "@x/lib",
  "version": "1.0.0",
  "type": "module",
  "license": "MIT",
  "exports": { ".": { "types": "./src/index.d.ts", "default": "./src/index.js" } },
  "files": ["src"],
  "engines": { "node": ">=20" }
}
`)
	write("ngm.json", `{
  "schemaVersion": 1,
  "name": "lib",
  "version": "1.0.0",
  "runtime": "node",
  "main": "./src/index.js",
  "types": "./src/index.d.ts"
}
`)
	write("src/index.js", "export const x = 1;\n")
	write("src/index.d.ts", "export declare const x: number;\n")
	write("README.md", "# lib\n")
	write("LICENSE", "MIT\n")
	write(".gitattributes", "* text=auto eol=lf\n")
}

// editJSON 就地改一个 JSON 文件（判据 ②/③ 用它注入缺陷）。
func editJSON(t *testing.T, path string, mutate func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	mutate(obj)
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
