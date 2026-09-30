package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v3PostInstallProject 建一个依赖了 `github:v3/hook` 的项目，并把 ngm.json 的
// supplyChain 段设为给定策略。
//
// files 会写进上游仓库（`postinstall.js`、`package.json` 等）——
// 它们由依赖作者写、随依赖进入 vendor，因此正是"不可信代码"的入口。
func v3PostInstallProject(t *testing.T, files map[string]string, supplyChain string) string {
	t.Helper()
	isolateUserEnv(t)

	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const h = 1\n")
	for name, body := range files {
		r.WriteFile(name, body)
	}
	r.Commit("feat: postinstall fixture")
	r.Tag("v1", false)
	seedMirror(t, "github:v3/hook", r.Dir)

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:v3/hook@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	// 先按默认策略（deny）装一次：这一步只负责把 vendor 铺好，
	// 与"钩子会不会执行"无关——策略在它之后才写，由测试自己再跑一次 install。
	// 顺序反了的话，一个注定失败的钩子会在铺 vendor 时就失败，测试还没开始就红了。
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
	if supplyChain != "" {
		writeSupplyChain(t, proj, supplyChain)
	}
	return proj
}

// TestV03PostInstallAcceptance 是 postinstall 执行入口的 hermetic 验收。
//
// 核心纪律（ADR-009 决策 5，v0.3 修订）：**配了不等于会跑**，不跑的时候必须明说。
func TestV03PostInstallAcceptance(t *testing.T) {
	// deny 是默认档位。钩子存在时不执行，并在输出里**明说**——
	// 用户配过 postInstallPolicy 却没看到任何动静，很容易以为它已经跑过了。
	//
	// 怎么断言"真的没跑"：这个钩子 exit 1，它一旦执行，install 就会以 exit 2 结束。
	t.Run("the default policy does not run hooks and says so", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": "Deno.exit(1);\n",
		}, "")

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("deny must not run the hook (a failing hook would fail the install), got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "nothing was executed") {
			t.Errorf("the report must say the hook was not executed:\n%s", out)
		}
		if !strings.Contains(out, "github:v3/hook") {
			t.Errorf("the report must name the dependency that declares a hook:\n%s", out)
		}
		if !strings.Contains(out, "postInstallPolicy") {
			t.Errorf("the report must name the knob that controls this:\n%s", out)
		}
	})

	// `prompt` 在非交互工具里没有真实形态：它的语义是"不要自动执行"，
	// 因此提示必须给出**可操作**的下一步，而不是让人以为需要敲回车。
	t.Run("prompt means do not run, and says how to enable", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": "Deno.exit(1);\n",
		}, `{"postInstallPolicy": "prompt"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("prompt must not run the hook, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "never asks interactively") {
			t.Errorf("the report should explain why prompt does not run:\n%s", out)
		}
		if !strings.Contains(out, `"allow"`) {
			t.Errorf("the report should say how to enable execution:\n%s", out)
		}
	})

	// npm 风格的 shell 钩子**永不执行**：派生的 shell 不受沙箱约束（ADR-012 实测）。
	// 检测到就要说清，而不是默默跳过——"我配了 allow 却什么都没发生"最难查。
	t.Run("npm-style shell hooks are reported but never run", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"package.json": `{"name":"hook","version":"1.0.0","scripts":{"postinstall":"node build.js"}}` + "\n",
		}, `{"postInstallPolicy": "allow"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a shell-only hook leaves nothing for the sandbox to run, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "does not run npm-style shell hooks") {
			t.Errorf("the report must explain why the shell hook was skipped:\n%s", out)
		}
		if !strings.Contains(out, "node build.js") {
			t.Errorf("the warning should quote the hook so the user can find it:\n%s", out)
		}
	})

	// 有 JS 钩子要执行、但没有 Deno → exit 5，**不降级**为非沙箱执行。
	t.Run("allow without deno exits 5 instead of running unsandboxed", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": "Deno.exit(0);\n",
		}, `{"postInstallPolicy": "allow"}`)

		// 把 PATH 清空，让 LookPath 找不到 deno（进程内有效）
		t.Setenv("PATH", t.TempDir())

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a missing sandbox must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "deno is required") {
			t.Errorf("the error should say what is missing:\n%s", out)
		}
		if !strings.Contains(out, "will not run a script outside a sandbox") {
			t.Errorf("the hint must state that there is no fallback:\n%s", out)
		}
	})
}

// TestV03PostInstallRealDeno 用**真实 Deno**验收执行路径。
func TestV03PostInstallRealDeno(t *testing.T) {
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno is not installed; TestV03PostInstallAcceptance covers the policy wiring")
	}

	t.Run("a passing hook runs and is reported", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": `console.log("hook ran"); Deno.exit(0);` + "\n",
		}, `{"postInstallPolicy": "allow"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a passing hook must keep exit 0, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "executed in a Deno sandbox") || !strings.Contains(out, "hook ran") {
			t.Errorf("the report should show that the hook ran:\n%s", out)
		}
	})

	// 钩子失败 = 依赖自己的代码没完成它该完成的事 → exit 2（完整性问题），
	// 与 digest 不匹配同级：CI 应当在这里停下来。
	t.Run("a failing hook fails the install with exit 2", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": `console.error("cannot build"); Deno.exit(1);` + "\n",
		}, `{"postInstallPolicy": "allow"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 2 {
			t.Fatalf("a failing hook must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:v3/hook") {
			t.Errorf("the error should name the dependency:\n%s", out)
		}
	})

	// vendor 是**可证明的**：钩子能改写它就会让 digest 失效，
	// 因此沙箱里写权限从不授予。
	t.Run("a hook cannot write into the vendor tree", func(t *testing.T) {
		proj := v3PostInstallProject(t, map[string]string{
			"postinstall.js": `let blocked = false;
try {
  Deno.writeTextFileSync("tampered.txt", "x");
} catch (e) {
  blocked = e.name === "PermissionDenied";
}
console.log(blocked ? "write: blocked" : "write: ALLOWED");
Deno.exit(blocked ? 0 : 1);
` + "\n",
		}, `{"postInstallPolicy": "allow"}`)

		code, out := runCaptureCode(t, "install", "--dir="+proj)
		if code != 0 {
			t.Fatalf("the sandbox did not hold (exit %d):\n%s", code, out)
		}
		if !strings.Contains(out, "write: blocked") {
			t.Errorf("a hook wrote into the vendor tree:\n%s", out)
		}
	})
}
