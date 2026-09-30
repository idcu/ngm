package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// m7Upstream 建一个可被依赖的上游仓库并预置 mirror。
func m7Upstream(t *testing.T, slug, body string) {
	t.Helper()
	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", body)
	r.Commit("feat: lib")
	r.Tag("v1.0.0", false)
	seedMirror(t, slug, r.Dir)
}

// TestM7Quickstart 是 guides/quickstart.md 那条 5 分钟流程的**可执行版**：
//
//	init → add → install → verify → build
//
// 其中 build 用受控替身（把清单里的 `esbuild` 指向假引擎），因此整条流程
// 在没有真 esbuild 的机器上也能复现。命令本身与文档逐字一致——换掉的只是
// "esbuild 这个程序"，不是 ngm 的行为。
//
// 真正的"用户拿到的东西能跑"由 TestM7Quickstart_RealToolchain 证明。
func TestM7Quickstart(t *testing.T) {
	testutils.MustHaveGit(t)
	testutils.AllowEngines(t, "esbuild", "fake-engine")
	isolateUserEnv(t)

	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "esbuild")
	m7Upstream(t, "github:demo/lib", "export const lib = \"ok\";\n")

	proj := filepath.Join(t.TempDir(), "my-app")

	// ---- 1) init -------------------------------------------------------
	code, out := runCaptureCode(t, "init", "github.com:my-org/my-app", "--runtime=node", "--dir="+proj)
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	// 文档承诺的产物必须真的存在（quickstart §1 的目录树）
	for _, rel := range []string{"ngm.json", "src/index.ts"} {
		if _, err := os.Stat(filepath.Join(proj, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("init should have created %s: %v", rel, err)
		}
	}
	// 重复 init 不得覆盖用户代码
	testutils.WriteFile(t, proj, "src/index.ts", "// mine\n")
	if code, out := runCaptureCode(t, "init", "github.com:my-org/my-app", "--dir="+proj, "--force"); code != 0 {
		t.Fatalf("re-init: %s", out)
	}
	if body, err := os.ReadFile(filepath.Join(proj, "src", "index.ts")); err != nil || string(body) != "// mine\n" {
		t.Errorf("init must never overwrite an existing entry file (got %q, err=%v)", body, err)
	}
	// 恢复成 init 生成的 stub（后续 build 要一个能打包的入口）
	testutils.WriteFile(t, proj, "src/index.ts", initEntryStub)

	// ---- 2) add --------------------------------------------------------
	if code, out := runCaptureCode(t, "add", "github:demo/lib@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add exit=%d out=%s", code, out)
	}

	// ---- 3) install ----------------------------------------------------
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install exit=%d out=%s", code, out)
	}
	for _, rel := range []string{
		"ngm.lock",
		"ngm.mappings.json",
		"ngm.vendor/github.com/demo/lib/index.ts",
	} {
		if _, err := os.Stat(filepath.Join(proj, filepath.FromSlash(rel))); err != nil {
			t.Errorf("install should have produced %s: %v", rel, err)
		}
	}

	// ---- 4) verify -----------------------------------------------------
	if code, out := runCaptureCode(t, "verify", "--dir="+proj); code != 0 {
		t.Fatalf("verify exit=%d out=%s", code, out)
	}
	// 深度校验也要通过（quickstart 之外，但这是"可证明"的实质）
	if code, out := runCaptureCode(t, "verify", "--deep", "--dir="+proj); code != 0 {
		t.Fatalf("verify --deep exit=%d out=%s", code, out)
	}

	// ---- 5) build ------------------------------------------------------
	// 把清单里的 esbuild 指向受控替身：命令仍是文档里的 `ngm build --engine=esbuild`
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "esbuild", Kind: "bundle", Adapter: "subprocess",
		Command: `"` + fake + `"`,
	})
	read := m6Dump(t)

	if code, out := runCaptureCode(t, "build", "--engine=esbuild", "--outfile=dist/app.js", "--dir="+proj); code != 0 {
		t.Fatalf("build exit=%d out=%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(proj, "dist", "app.js")); err != nil {
		t.Errorf("build should have written the bundle: %v", err)
	}

	argv := strings.Join(read(), " ")
	// 入口来自 ngm.json 的 `main`（init 写入），因此无需任何参数
	if !strings.Contains(argv, "src/index.ts") {
		t.Errorf("build should take the entry from `main`: %q", argv)
	}
	// 整条链的终点：mappings 变成别名，且指向**文件**（不是目录）
	if !strings.Contains(argv, "--alias:github:demo/lib=./ngm.vendor/github.com/demo/lib/index.ts") {
		t.Errorf("mappings should become an alias pointing at the entry file: %q", argv)
	}
	if !strings.Contains(argv, "--bundle") {
		t.Errorf("esbuild should be invoked in bundle mode: %q", argv)
	}
}

// TestM7InstallDoesNotNeedGitWhenTheStoreIsWarm 固定 locking.md 的承诺：
//
//	"CI 可复现：配合 content store 或 vendor，环境无网络也能安装与构建"
//
// 证明方式：装好一次后**把 mirror 整个删掉**，再装一次。成功即说明它既没联网、
// 也没碰 mirror，只靠 content store 落地。这比"测耗时"更贴近承诺本身——
// 耗时只能暗示，删掉数据源才能证明。
func TestM7InstallDoesNotNeedGitWhenTheStoreIsWarm(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	m7Upstream(t, "github:demo/lib", "export const lib = 1;\n")
	proj := filepath.Join(t.TempDir(), "my-app")

	if code, out := runCaptureCode(t, "init", "github.com:my-org/my-app", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
	if code, out := runCaptureCode(t, "add", "github:demo/lib@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("first install: %s", out)
	}

	// 断掉一切 git 来源
	layout := defaultLayoutForTest(t)
	if err := os.RemoveAll(layout.MirrorRoot()); err != nil {
		t.Fatal(err)
	}

	code, out := runCaptureCode(t, "install", "--dir="+proj)
	if code != 0 {
		t.Fatalf("a warm install must not need the mirror: exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "no git access") {
		t.Errorf("install should report that everything came from the content store:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proj, "ngm.vendor", "github.com", "demo", "lib", "index.ts")); err != nil {
		t.Errorf("the vendor tree should still be materialized: %v", err)
	}
}

// TestM7Quickstart_RealToolchain 用**真实的 esbuild 与 node** 跑同一条流程，
// 并真的执行产出的 bundle。
//
// 与上一个测试的分工：那个证明 ngm 侧的行为（协议、argv、文件落地），
// 这个证明"用户拿到的东西能跑"——裸导入 `github:demo/lib` 在真实引擎下
// 解析进 vendor，产物被 node 执行并打印出依赖里的值。
//
// 缺任一工具时跳过：CI 的 engine-adapter job 两者都装。
func TestM7Quickstart_RealToolchain(t *testing.T) {
	if _, err := exec.LookPath("esbuild"); err != nil {
		t.Skip("esbuild is not installed; TestM7Quickstart covers the same flow hermetically")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	testutils.AllowEngines(t, "esbuild")
	isolateUserEnv(t)

	m7Upstream(t, "github:demo/lib", "export const lib = \"ok\";\n")
	proj := filepath.Join(t.TempDir(), "my-app")

	if code, out := runCaptureCode(t, "init", "github.com:my-org/my-app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	if code, out := runCaptureCode(t, "add", "github:demo/lib@v1.0.0", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add exit=%d out=%s", code, out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install exit=%d out=%s", code, out)
	}
	if code, out := runCaptureCode(t, "verify", "--dir="+proj); code != 0 {
		t.Fatalf("verify exit=%d out=%s", code, out)
	}

	// 用裸导入消费依赖：这正是 mappings → --alias 存在的理由
	testutils.WriteFile(t, proj, "src/index.ts",
		"import { lib } from \"github:demo/lib\";\nconsole.log(\"lib=\" + lib);\n")

	if code, out := runCaptureCode(t, "build", "--engine=esbuild", "--outfile=dist/app.js", "--dir="+proj); code != 0 {
		t.Fatalf("build exit=%d out=%s", code, out)
	}

	bundle := filepath.Join(proj, "dist", "app.js")
	if _, err := os.Stat(bundle); err != nil {
		t.Fatalf("bundle missing: %v", err)
	}

	cmd := exec.Command("node", bundle)
	cmd.Dir = proj
	nodeOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the produced bundle is not runnable: %v\n%s", err, nodeOut)
	}
	if !strings.Contains(string(nodeOut), "lib=ok") {
		t.Errorf("the bundle should print the vendored value, got %q", nodeOut)
	}
}
