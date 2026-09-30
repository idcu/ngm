package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
	"github.com/idcu/ngm/internal/vendor"
)

// v3SandboxProject 建一个依赖了 `github:v3/sbx` 的项目。
//
// extra 里的文件会写进上游仓库（例如 `verify.js`、`packages/x/data.txt`）。
// 上游仓库的构造刻意不走 scUpstream：那个助手只会写 index.ts 与 ngm.json。
func v3SandboxProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	isolateUserEnv(t)

	r := testutils.NewGitRepo(t)
	r.WriteFile("index.ts", "export const v = 1\n")
	for name, body := range extra {
		r.WriteFile(name, body)
	}
	r.Commit("feat: sandbox fixture")
	r.Tag("v1", false)
	seedMirror(t, "github:v3/sbx", r.Dir)

	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:v3/sbx@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}
	return proj
}

// sandboxDepDir 返回该依赖在 vendor 里的目录。
func sandboxDepDir(t *testing.T, proj string) string {
	t.Helper()
	layout := defaultLayoutForTest(t)
	_ = layout
	return filepath.Join(proj, vendor.VendorDirName, "github.com", "v3", "sbx")
}

// TestV03SandboxAcceptance 是 C 组的 hermetic 验收：不需要 Deno 也能断言的部分。
func TestV03SandboxAcceptance(t *testing.T) {
	// 没有依赖提供脚本时不需要 Deno，也不该报"沙箱不可用"——
	// 那时没有任何东西要跑，那句话与事实不符。
	t.Run("no scripts means no Deno requirement", func(t *testing.T) {
		proj := v3SandboxProject(t, nil)

		code, out := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if code != 0 {
			t.Fatalf("verify --sandbox exit=%d:\n%s", code, out)
		}
		if !strings.Contains(out, "no dependency provides") {
			t.Errorf("the report should say there was nothing to execute:\n%s", out)
		}
	})

	// `--sandbox` **只追加**：常规判定的结论不因它而改变（对象相同、权限不同）。
	t.Run("--sandbox does not change the verdict", func(t *testing.T) {
		proj := v3SandboxProject(t, nil)

		plain, plainOut := runCaptureCode(t, "verify", "--dir="+proj)
		withSandbox, sandboxOut := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if plain != withSandbox {
			t.Fatalf("the verdict changed: verify=%d verify --sandbox=%d\n%s\n%s",
				plain, withSandbox, plainOut, sandboxOut)
		}
		// 常规那部分输出必须完全一致；沙箱只会多出一段
		if !strings.Contains(sandboxOut, plainOut[:min(len(plainOut), 40)]) {
			t.Errorf("the ordinary part of the report changed:\nplain:\n%s\nsandbox:\n%s", plainOut, sandboxOut)
		}
	})
}

// TestV03SandboxRealDeno 用**真实 Deno**验收沙箱在 CLI 里的行为。
//
// 与 internal/security 的沙箱测试分工：那里证明 flag 与逃逸阻断，
// 这里证明**接线**——脚本从哪找、权限从哪来、失败如何变成退出码。
func TestV03SandboxRealDeno(t *testing.T) {
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno is not installed; TestV03SandboxAcceptance covers the wiring without it")
	}

	t.Run("a passing self-check keeps the verdict", func(t *testing.T) {
		proj := v3SandboxProject(t, map[string]string{
			"verify.js": `console.log("checking myself"); Deno.exit(0);` + "\n",
		})

		code, out := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if code != 0 {
			t.Fatalf("a passing self-check must keep exit 0, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "self-check passed") {
			t.Errorf("the report should say the self-check ran and passed:\n%s", out)
		}
		// 脚本输出必须带上"这是依赖的自述"这一标注，不能被读成 ngm 的结论
		if !strings.Contains(out, "does not verify it") {
			t.Errorf("the report must label the script's output as unverified:\n%s", out)
		}
	})

	// 脚本只能**否决**：它失败会让 verify 失败，而且归到"完整性"（exit 2）。
	t.Run("a failing self-check fails the run with exit 2", func(t *testing.T) {
		proj := v3SandboxProject(t, map[string]string{
			"verify.js": `console.error("i am not what i claim"); Deno.exit(3);` + "\n",
		})

		code, out := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if code != 2 {
			t.Fatalf("a failing self-check must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "github:v3/sbx") {
			t.Errorf("the error should name the dependency:\n%s", out)
		}
	})

	// `--allow-drift` 只降级漂移，不能降级完整性——自检失败属后者。
	t.Run("--allow-drift must not downgrade a self-check failure", func(t *testing.T) {
		proj := v3SandboxProject(t, map[string]string{
			"verify.js": `Deno.exit(1);` + "\n",
		})

		code, out := runCaptureCode(t, "verify", "--sandbox", "--allow-drift", "--dir="+proj)
		if code != 2 {
			t.Fatalf("--allow-drift must not downgrade an integrity failure, got %d:\n%s", code, out)
		}
	})

	// 边界在 CLI 这一层同样成立：脚本读不到自己那棵树之外的任何东西。
	t.Run("the boundary holds through the CLI", func(t *testing.T) {
		proj := v3SandboxProject(t, map[string]string{
			"verify.js": `let blocked = false;
try {
  Deno.readTextFileSync("../../../../ngm.json");
} catch (e) {
  blocked = e.name === "PermissionDenied";
}
console.log(blocked ? "outside: blocked" : "outside: READABLE");
Deno.exit(blocked ? 0 : 1);
` + "\n",
		})

		code, out := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if code != 0 {
			t.Fatalf("the sandbox did not hold (exit %d):\n%s", code, out)
		}
		if !strings.Contains(out, "outside: blocked") {
			t.Errorf("the script read outside its own subtree:\n%s", out)
		}
	})

	// 脚本卡住时不能被当成"通过"：必须超时并失败。
	t.Run("a hanging self-check is a failure, not a pass", func(t *testing.T) {
		if testing.Short() {
			t.Skip("a 30s timeout is not a short test")
		}
		proj := v3SandboxProject(t, map[string]string{
			"verify.js": "while (true) {}\n",
		})

		code, out := runCaptureCode(t, "verify", "--sandbox", "--dir="+proj)
		if code != 2 {
			t.Fatalf("a hanging self-check must exit 2, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "did not finish") {
			t.Errorf("the report should distinguish a timeout from a failing script:\n%s", out)
		}
	})
}

// 依赖目录里的脚本名固定为 verify.js：它是**依赖作者**写的，
// 因此位置必须由协议规定，而不是由 ngm 猜。
func TestV03Sandbox_ScriptLocationIsFixed(t *testing.T) {
	proj := v3SandboxProject(t, map[string]string{
		"verify.js": "Deno.exit(0);\n",
	})
	dir := sandboxDepDir(t, proj)
	if _, err := os.Stat(filepath.Join(dir, "verify.js")); err != nil {
		t.Fatalf("verify.js should have been vendored to %s: %v", dir, err)
	}
}
