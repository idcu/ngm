package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/integrations"
	"github.com/idcu/ngm/internal/testutils"
)

// v3UIRepo 建一个"两个子包、入口都不是 index"的 monorepo 上游。
//
// 入口刻意用 `./src/main.ts` 而不是 index.ts：目录形式的别名只在恰好含
// index.* 时才解析得到，用 index 会让"指向文件"这条规则**看起来**也对，
// 从而测不出它到底有没有生效。
func v3UIRepo(t *testing.T, slug string) {
	t.Helper()
	r := testutils.NewGitRepo(t)
	for _, pkg := range []struct{ name, value string }{{"core", "42"}, {"web", "7"}} {
		dir := "packages/" + pkg.name
		r.WriteFile(dir+"/src/main.ts", "export const "+pkg.name+" = "+pkg.value+"\n")
		r.WriteFile(dir+"/ngm.json", `{"name":"`+pkg.name+`","version":"1.0.0","runtime":"node",`+
			`"main":"./src/main.ts"}`)
	}
	r.Commit("feat: two packages")
	r.Tag("v1", false)
	seedMirror(t, slug, r.Dir)
}

// v3UIProject 建一个装了该 monorepo 两个子路径的项目，并写一份引用子路径的源码。
func v3UIProject(t *testing.T) string {
	t.Helper()
	isolateUserEnv(t)
	v3UIRepo(t, "github:v3/ui")

	proj := newProject(t)
	for _, sub := range []string{"packages/core", "packages/web"} {
		if code, out := runCaptureCode(t, "add", "github:v3/ui@v1",
			"--ref-type=tag", "--path="+sub, "--dir="+proj); code != 0 {
			t.Fatalf("add %s: %s", sub, out)
		}
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
	testutils.WriteFile(t, proj, "src/index.ts",
		"import { core } from \"github:v3/ui/packages/core\"\nconsole.log(core)\n")
	return proj
}

// TestV03IntegrationsAcceptance 是 v0.3 B 组的 hermetic 验收：
// 生成什么、冲突怎么办、幂等性、以及 `ngm build` 是否为每个子路径给出各自的别名。
func TestV03IntegrationsAcceptance(t *testing.T) {
	t.Run("scaffolding writes the tool config and the tsconfig paths", func(t *testing.T) {
		proj := v3UIProject(t)

		code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj)
		if code != 0 {
			t.Fatalf("integrations add vite exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "created") {
			t.Errorf("the report should say what was created:\n%s", out)
		}

		vite := readProjectFile(t, proj, integrations.ViteConfigPath)
		if !strings.Contains(vite, "ngm.mappings.json") {
			t.Errorf("the generated Vite config must read mappings:\n%s", vite)
		}

		sidecar := readProjectFile(t, proj, integrations.TsconfigSidecarPath)
		// 子路径的标识符与它指向的**文件**（不是目录）
		want := `"github:v3/ui/packages/core": [`
		if !strings.Contains(sidecar, want) {
			t.Errorf("the tsconfig sidecar must map the subpath specifier:\n%s", sidecar)
		}
		if !strings.Contains(sidecar, "packages/core/src/main.ts") {
			t.Errorf("the tsconfig entry must point at the entry file:\n%s", sidecar)
		}
		if strings.Contains(sidecar, "baseUrl") {
			t.Errorf("TypeScript 7 removed baseUrl; writing it makes tsc fail:\n%s", sidecar)
		}
	})

	t.Run("re-running reports up to date and changes nothing", func(t *testing.T) {
		proj := v3UIProject(t)
		if code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj); code != 0 {
			t.Fatalf("first run: %s", out)
		}
		before := readProjectFile(t, proj, integrations.ViteConfigPath)

		code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj)
		if code != 0 {
			t.Fatalf("second run exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "up to date") {
			t.Errorf("a second run should be a no-op:\n%s", out)
		}
		if after := readProjectFile(t, proj, integrations.ViteConfigPath); after != before {
			t.Error("the file changed on a no-op run")
		}
	})

	t.Run("an existing config is reported, not overwritten", func(t *testing.T) {
		proj := v3UIProject(t)
		mine := "export default { resolve: { alias: { mine: true } } }\n"
		testutils.WriteFile(t, proj, integrations.ViteConfigPath, mine)

		code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a file ngm will not overwrite must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "did not touch") {
			t.Errorf("the report should say the file was left alone:\n%s", out)
		}
		if got := readProjectFile(t, proj, integrations.ViteConfigPath); got != mine {
			t.Errorf("the user's config was modified:\n%s", got)
		}
		// 全有或全无：其余产物一个都不能写
		if _, err := os.Stat(filepath.Join(proj, integrations.TsconfigSidecarPath)); err == nil {
			t.Errorf("%s must not be written when another artifact conflicts", integrations.TsconfigSidecarPath)
		}
	})

	// 已存在的 tsconfig.json 不是冲突：它没有任何错，只是 ngm 无权改它。
	t.Run("an existing tsconfig is skipped with a one-line hint", func(t *testing.T) {
		proj := v3UIProject(t)
		mine := "{\n  \"compilerOptions\": { \"strict\": true }\n}\n"
		testutils.WriteFile(t, proj, integrations.TsconfigPath, mine)

		code, out := runCaptureCode(t, "integrations", "add", "esbuild", "--dir="+proj)
		if code != 0 {
			t.Fatalf("an existing tsconfig must not be an error, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "skipped") || !strings.Contains(out, integrations.TsconfigSidecarPath) {
			t.Errorf("the report should skip it and say what to add:\n%s", out)
		}
		if got := readProjectFile(t, proj, integrations.TsconfigPath); got != mine {
			t.Errorf("the user's tsconfig was modified:\n%s", got)
		}
	})

	t.Run("json output is machine readable", func(t *testing.T) {
		proj := v3UIProject(t)
		code, out := runCaptureCode(t, "integrations", "add", "deno", "--json", "--dir="+proj)
		if code != 0 {
			t.Fatalf("integrations add deno --json exit=%d:\n%s", code, out)
		}
		var parsed struct {
			Tool      string `json:"tool"`
			ExitCode  int    `json:"exitCode"`
			Artifacts []struct {
				Path   string `json:"path"`
				Status string `json:"status"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal([]byte(out), &parsed); err != nil {
			t.Fatalf("--json must be parseable: %v\n%s", err, out)
		}
		if parsed.Tool != "deno" || parsed.ExitCode != 0 || len(parsed.Artifacts) == 0 {
			t.Errorf("unexpected payload: %+v", parsed)
		}
		// 静态生成的 import map 必须带上两个子路径，否则 Deno 什么都解析不到
		importMap := readProjectFile(t, proj, integrations.DenoImportMapPath)
		for _, sub := range []string{"packages/core", "packages/web"} {
			if !strings.Contains(importMap, "github:v3/ui/"+sub) {
				t.Errorf("the import map is missing %s:\n%s", sub, importMap)
			}
		}
	})

	// 这是 B 组最容易漏掉、也最该固定的一条：**同一个仓库的两个子路径必须
	// 各自拿到别名**。用 `from` 当键时它们会互相覆盖，而覆盖后构建仍可能成功，
	// 只是把两个子路径解析到了同一份代码。
	t.Run("ngm build gives every subpath its own alias", func(t *testing.T) {
		proj := v3UIProject(t)

		code, out := runCaptureCode(t, "build", "src/index.ts", "--dry-run", "--dir="+proj)
		if code != 0 {
			t.Fatalf("build --dry-run exit=%d:\n%s", code, out)
		}
		core := "--alias:github:v3/ui/packages/core="
		web := "--alias:github:v3/ui/packages/web="
		if !strings.Contains(out, core) || !strings.Contains(out, web) {
			t.Fatalf("both subpaths need their own alias:\n%s", out)
		}
		// 别名指向文件而不是目录（目录只在恰好含 index.* 时才解析得到）
		if !strings.Contains(out, "packages/core/src/main.ts") {
			t.Errorf("the alias should point at the entry file:\n%s", out)
		}
	})
}

// TestV03IntegrationsRealTools 用**真工具**验证生成的配置能直接工作。
//
// 为什么必须分开：hermetic 那组只能证明"生成的内容看起来对"。E 组的教训正是
// 这一点——当时我凭记忆断言 esbuild 的别名是精确匹配，实测才发现是前缀替换，
// 于是文档、生成器、测试一起错。这里让真 esbuild 打包一次、真 tsc 检查一次。
//
// 缺工具时跳过；CI 的 engine-integration job 会装上它们（已加入该 job 的 -run 列表）。
func TestV03IntegrationsRealTools(t *testing.T) {
	node, nodeErr := exec.LookPath("node")
	esbuildNodePath := esbuildNodePath(t)

	t.Run("the generated esbuild script bundles a monorepo subpath", func(t *testing.T) {
		if nodeErr != nil {
			t.Skip("node is not installed")
		}
		if esbuildNodePath == "" {
			t.Skip("esbuild is not installed; the hermetic acceptance covers the generated content")
		}
		proj := v3UIProject(t)
		if code, out := runCaptureCode(t, "integrations", "add", "esbuild", "--dir="+proj); code != 0 {
			t.Fatalf("integrations add esbuild: %s", out)
		}

		code, out := runExternal(t, proj, append(os.Environ(), "NODE_PATH="+esbuildNodePath),
			node, integrations.EsbuildScriptPath)
		if code != 0 {
			t.Fatalf("node %s exit=%d:\n%s", integrations.EsbuildScriptPath, code, out)
		}
		bundle := readProjectFile(t, proj, "dist/index.js")
		// core 子包的导出值：它出现在产物里，说明解析到的是**那个**子路径的代码
		if !strings.Contains(bundle, "42") {
			t.Errorf("the bundle should contain the value from the core subpath:\n%s", bundle)
		}
	})

	t.Run("the generated tsconfig resolves the github: prefix", func(t *testing.T) {
		tsc, err := exec.LookPath("tsc")
		if err != nil {
			t.Skip("tsc is not installed; the hermetic acceptance covers the generated content")
		}
		proj := v3UIProject(t)
		if code, out := runCaptureCode(t, "integrations", "add", "vite", "--dir="+proj); code != 0 {
			t.Fatalf("integrations add vite: %s", out)
		}

		code, out := runExternal(t, proj, os.Environ(), tsc, "--noEmit")
		if code != 0 {
			t.Fatalf("tsc --noEmit exit=%d:\n%s", code, out)
		}
		if strings.Contains(out, "Cannot find module") {
			t.Errorf("the generated paths did not resolve the specifier:\n%s", out)
		}
	})
}

// readProjectFile 读取项目内的文件（失败即致命：验收依赖它存在）。
func readProjectFile(t *testing.T, proj, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// runExternal 在指定目录运行外部工具，返回退出码与合并输出。
func runExternal(t *testing.T, dir string, env []string, name string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run %s: %v\n%s", name, err, out)
	return 1, ""
}

// esbuildNodePath 返回能让 `import 'esbuild'` 解析成功的 NODE_PATH。
//
// 两种安装方式都要支持：CI 用 `npm install --global`（要从 npm 的全局根找），
// 本地可能是项目内安装（从可执行文件的位置往上找）。找不到就跳过而不是失败——
// 但"跳过"在 CI 里不会发生，因为那个 job 明确装了 esbuild。
func esbuildNodePath(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("esbuild")
	if err != nil {
		return ""
	}

	// 1) 项目内/node_modules/.bin/esbuild(.cmd) → 上一级的 node_modules
	dir := filepath.Dir(bin)
	if filepath.Base(dir) == ".bin" {
		cand := filepath.Dir(dir)
		if hasEsbuildPackage(cand) {
			return cand
		}
	}

	// 2) npm 全局根
	out, err := exec.Command("npm", "root", "-g").Output()
	if err != nil {
		return ""
	}
	if root := strings.TrimSpace(string(out)); root != "" && hasEsbuildPackage(root) {
		return root
	}
	return ""
}

func hasEsbuildPackage(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "esbuild", "package.json"))
	return err == nil
}
