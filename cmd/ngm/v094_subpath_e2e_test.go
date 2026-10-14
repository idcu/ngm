package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.94：**`subPath` 端到端**——一个真实 monorepo 的**子包**被消费的完整闭环。
//
// 这件事在 README 与几版复盘里一直挂着"需要推一个 monorepo 远端库"（从 v0.79 起）。
// 这一版把它做掉，同时纠正了那条前提里**两处**想当然：
//
//  1. "起个本机 `git daemon` 就有远端了" —— **不成立**。`git://` 只是**地址写法**，
//     抓取协议只有 ssh / https（`~/.ngm/config.json` 的 `git.defaultProtocol`，
//     见 `internal/resolve/protocol.go`：`Protocol` 只有 `ProtocolSSH` / `ProtocolHTTPS`）。
//     v0.83 的 ADR-029 放开回环主机确实是为了本机 daemon，但那一半之后**还有一半**：
//     装了 daemon 也走不到 `git://` 抓取（实测：install 把 canonical 重建成
//     `https://127.0.0.1/acme/lib.git` 去连 443）。真正的 fetch 端到端仍然需要
//     一个 https/ssh 远端。
//  2. 但 **subPath 的契约不在抓取那一步**：过滤子目录、写 `subPath`、按子包自己的
//     `exports` 推入口——全都发生在 vendor / mappings 层。用仓库既有的**本地镜像**
//     （`seedMirror`：镜像里是**整仓**，正是 monorepo 的真实形状）+ `--offline`
//     就能把这条契约走完整。
//
// 验收（前两条是 subPath 契约的本体）：
//  1. **只有子包被 vendor**：兄弟包 `packages/b` 一个文件都不许进来；
//  2. 入口来自**子包自己的 `exports“（不是根包的）；
//  3. 依赖声明里记着 `subPath: packages/a`；
//  4. 反面：不存在的子路径 ⇒ 明确失败，且**不留半个 vendor**。
func TestV94SubPathEndToEnd(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	// ---- 1. 上游 monorepo：根 + packages/a（要它）+ packages/b（不许进来）----
	up := testutils.NewGitRepo(t)
	up.WriteFile("package.json", `{"name":"up-monorepo","private":true,"workspaces":["packages/*"]}`+"\n")
	up.WriteFile("packages/a/package.json", `{
  "name": "pkg-a",
  "version": "1.0.0",
  "type": "module",
  "license": "MIT",
  "engines": { "node": ">=18" },
  "files": ["src"],
  "exports": { ".": { "types": "./src/index.d.ts", "import": "./src/index.js" } }
}`+"\n")
	up.WriteFile("packages/a/src/index.js", "export const a = 1;\n")
	up.WriteFile("packages/a/src/index.d.ts", "export declare const a: number;\n")
	up.WriteFile("packages/a/README.md", "# pkg-a\n")
	up.WriteFile("packages/b/package.json", `{"name":"pkg-b","version":"1.0.0","type":"module","license":"MIT","exports":"./src/index.js"}`+"\n")
	up.WriteFile("packages/b/src/index.js", "export const b = 2;\n")
	up.Commit("monorepo: a + b")
	up.Tag("v1.0.0", true)

	// ---- 2. 镜像里放**整仓**（monorepo 的真实形状），然后走 --offline ----
	const slug = "127.0.0.1/acme/lib"
	seedMirror(t, slug, up.Dir)

	spec := fmt.Sprintf("git://%s.git#path=packages/a@v1.0.0", slug)

	proj := t.TempDir()
	if code, out := runCaptureCode(t, "init", "v94.test/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init 失败（exit %d）：\n%s", code, out)
	}
	// 注意：`add` **没有** --offline（它只写声明，不抓取）；`install` 才有。
	if code, out := runCaptureCode(t, "add", spec, "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add 失败（exit %d）：\n%s", code, out)
	}
	// 顺序与 m1 验收同源：add 只写声明 → **update** 解析 ref 并写 ngm.lock →
	// install 才能从 lock 落盘（`--offline` 下没有 lock 会明确拒绝：
	// "an offline install can only materialise what the lock already pins"）。
	if code, out := runCaptureCode(t, "update", "--all", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("update 失败（exit %d）：\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "install", "--offline", "--dir="+proj); code != 0 {
		t.Fatalf("install 失败（exit %d）：\n%s", code, out)
	}
	if code, out := runCaptureCode(t, "verify", "--dir="+proj); code != 0 {
		t.Fatalf("verify 失败（exit %d）：\n%s", code, out)
	}
	code, mappingsOut := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
	if code != 0 {
		t.Fatalf("mappings validate 失败（exit %d）：\n%s", code, mappingsOut)
	}

	// ---- 3. 契约验收 ----
	vendored := vendoredRelPaths(t, proj)
	if len(vendored) == 0 {
		t.Fatal("vendor 目录是空的 —— install 没有落任何东西")
	}

	var hasA, hasB bool
	for _, rel := range vendored {
		slashed := filepath.ToSlash(rel)
		if strings.Contains(slashed, "packages/a/") {
			hasA = true
		}
		if strings.Contains(slashed, "packages/b") {
			hasB = true
		}
	}
	if !hasA {
		t.Errorf("子包 packages/a 的文件没进 vendor：\n  %s", strings.Join(vendored, "\n  "))
	}
	if hasB {
		t.Errorf("**subPath 的契约被破坏**：兄弟包 packages/b 也被 vendor 了：\n  %s", strings.Join(vendored, "\n  "))
	}
	// 根包的文件（monorepo 根只有 workspaces，不是包）不该进来
	for _, rel := range vendored {
		if strings.HasSuffix(filepath.ToSlash(rel), "/package.json") && !strings.Contains(filepath.ToSlash(rel), "packages/a/") {
			t.Errorf("vendor 里出现了子包之外的 package.json：%s", rel)
		}
	}

	deps := readDeps(t, proj)
	if len(deps) != 1 {
		t.Fatalf("依赖数 = %d, want 1（%v）", len(deps), deps)
	}
	// 声明里的字段名是 `path`（对用户可见的那一个）；`subPath` 是 lock/内部的名字。
	if got := fmt.Sprint(deps[0]["path"]); got != "packages/a" {
		t.Errorf("依赖声明里的 path = %q, want packages/a（deps=%v）", got, deps)
	}
	if got := fmt.Sprint(deps[0]["refType"]); got != "tag" {
		t.Errorf("refType = %q, want tag", got)
	}

	// mappings 的入口来自**子包自己的 exports**：读生成出来的那份文件
	// （`mappings validate` 只回报 OK 与条数，不回显条目——断言要落在事实上）。
	mapText := readFileString(t, filepath.Join(proj, "ngm.mappings.json"))
	if !strings.Contains(mapText, "packages/a") {
		t.Errorf("mappings 没有指向子目录（to 字段应当含 packages/a）：\n%s", mapText)
	}
	if !strings.Contains(mapText, "src/index.js") || !strings.Contains(mapText, "src/index.d.ts") {
		t.Errorf("mappings 的入口/类型不是子包 exports 里声明的那两个：\n%s", mapText)
	}
	if strings.Contains(mapText, "packages/b") {
		t.Errorf("mappings 里出现了兄弟包：\n%s", mapText)
	}
	_ = mappingsOut

	// ---- 4. 反面：不存在的子路径 ⇒ 明确失败，且不留半个 vendor ----
	proj2 := t.TempDir()
	if code, out := runCaptureCode(t, "init", "v94.test/bad", "--runtime=node", "--dir="+proj2); code != 0 {
		t.Fatalf("init 失败（exit %d）：\n%s", code, out)
	}
	badSpec := fmt.Sprintf("git://%s.git#path=packages/nope@v1.0.0", slug)
	// `add` 只写声明 ⇒ **发现不了**子路径不存在（它还没看过仓库内容）。
	// 这是合理的：能不能写声明与远端里有没有那个目录是两件事。
	if code, out := runCaptureCode(t, "add", badSpec, "--ref-type=tag", "--dir="+proj2); code != 0 {
		t.Fatalf("add 只写声明，不该因为子路径不存在而失败（exit %d）：\n%s", code, out)
	}
	// 真正该失败的是解析/落盘那一步（update 是那个"重新解析的写者"）。
	if code, out := runCaptureCode(t, "update", "--all", "--offline", "--dir="+proj2); code == 0 {
		t.Errorf("不存在的子路径不该被成功落盘：\n%s", out)
	}
	if paths := vendoredRelPaths(t, proj2); len(paths) != 0 {
		t.Errorf("失败的 update 留下了文件：%v", paths)
	}
}

// readFileString 读一个文件（读不出就判据失败——它下一步必然要看）。
func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败：%v", path, err)
	}
	return string(b)
}

// vendoredRelPaths 列出项目里 `ngm.vendor/` 下的所有文件（相对项目根）。
func vendoredRelPaths(t *testing.T, proj string) []string {
	t.Helper()
	root := filepath.Join(proj, "ngm.vendor")
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 没有 vendor 目录也算正常（返回空表）
		}
		if d.IsDir() {
			return nil
		}
		if rel, rerr := filepath.Rel(proj, path); rerr == nil {
			out = append(out, rel)
		}
		return nil
	})
	return out
}
