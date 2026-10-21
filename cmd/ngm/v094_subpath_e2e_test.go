package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/vendor"
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
	// v0.101：subPath 的 --deep 必须把 `ls-tree` 的整仓路径与 content store 的
	// “相对模块根”路径对齐；否则每个文件都会被报成 missing from the content store。
	if code, out := runCaptureCode(t, "verify", "--deep", "--dir="+proj); code != 0 {
		t.Fatalf("verify --deep 失败（exit %d）：\n%s", code, out)
	}
	code, mappingsOut := runCaptureCode(t, "mappings", "validate", "--dir="+proj)
	if code != 0 {
		t.Fatalf("mappings validate 失败（exit %d）：\n%s", code, mappingsOut)
	}

	// v0.102：子路径 lock 条目带模块证明；篡改它必须被 verify 判成 critical。
	lockPath := filepath.Join(proj, "ngm.lock")
	lockText := readFileString(t, lockPath)
	for _, key := range []string{`"moduleDigest"`, `"subtreeOid"`} {
		if !strings.Contains(lockText, key) {
			t.Errorf("ngm.lock 缺少模块证明字段 %s：\n%s", key, lockText)
		}
	}
	lf, err := lock.Read(lockPath)
	if err != nil {
		t.Fatalf("read ngm.lock: %v", err)
	}
	d, ok := lf.Find("127.0.0.1:acme/lib", "packages/a")
	if !ok || d.ModuleDigest == "" {
		t.Fatalf("lock entry for packages/a has no moduleDigest: %+v", d)
	}
	store := vendor.NewContentStore(defaultLayoutForTest(t).ContentRoot())
	moduleEntries, err := store.Entries(d.ModuleDigest)
	if err != nil {
		t.Fatalf("read module-scoped content store: %v", err)
	}
	if len(moduleEntries) == 0 {
		t.Fatal("module-scoped content store has no entries")
	}
	for _, e := range moduleEntries {
		if strings.HasPrefix(e.Path, "packages/") {
			t.Errorf("module store entries must be relative to the module root: %q", e.Path)
		}
	}

	// 篡改模块 store 的字节，--deep 必须用 moduleDigest 发现（模块 landing 路径）。
	var tamperPath string
	for _, e := range moduleEntries {
		if !e.Symlink && e.Full != "" {
			tamperPath = e.Full
			break
		}
	}
	if tamperPath == "" {
		t.Fatal("no regular module blob to tamper")
	}
	originalBytes, err := os.ReadFile(tamperPath)
	if err != nil {
		t.Fatalf("read module blob: %v", err)
	}
	tamperedBytes := append([]byte(nil), originalBytes...)
	tamperedBytes[0] ^= 0x01
	if err := os.WriteFile(tamperPath, tamperedBytes, 0o644); err != nil {
		t.Fatalf("tamper module blob: %v", err)
	}
	if code, out := runCaptureCode(t, "verify", "--deep", "--dir="+proj); code != 2 {
		t.Errorf("篡改模块 store 后 verify --deep 应退 2，实际 %d：\n%s", code, out)
	}
	if err := os.WriteFile(tamperPath, originalBytes, 0o644); err != nil {
		t.Fatalf("restore module blob: %v", err)
	}
	var lockDoc struct {
		Version         int              `json:"version"`
		LockfileVersion string           `json:"lockfileVersion"`
		Dependencies    []map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(lockText), &lockDoc); err != nil {
		t.Fatalf("parse ngm.lock: %v", err)
	}
	if len(lockDoc.Dependencies) != 1 {
		t.Fatalf("lock deps=%d want 1", len(lockDoc.Dependencies))
	}
	lockDoc.Dependencies[0]["moduleDigest"] = "sha256:" + strings.Repeat("0", 64)
	tampered, err := json.MarshalIndent(lockDoc, "", "  ")
	if err != nil {
		t.Fatalf("marshal tampered lock: %v", err)
	}
	if err := os.WriteFile(lockPath, append(tampered, '\n'), 0o644); err != nil {
		t.Fatalf("write tampered lock: %v", err)
	}
	if code, out := runCaptureCode(t, "verify", "--dir="+proj); code != 2 {
		t.Errorf("篡改 moduleDigest 后 verify 应退 2，实际 %d：\n%s", code, out)
	}
	if err := os.WriteFile(lockPath, []byte(lockText), 0o644); err != nil {
		t.Fatalf("restore lock: %v", err)
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
