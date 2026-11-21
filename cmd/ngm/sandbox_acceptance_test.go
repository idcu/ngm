package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/vendor"
	"github.com/idcu/ngm/internal/testutils"
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

	// 项目目录**不**从 t.Name() 派生：`t.TempDir()` 会把子用例名写进路径，而沙箱
	// 对含逗号的路径是 fail-closed 的（`--allow-read` 用逗号分隔、无法转义）。
	// 于是"某个子用例的名字里有个逗号"会让 `verify --sandbox` 退 3，而判据报出来的
	// 是"没有超时"——2026-11-19 的 CI 就是这么红的，查了半天才发现是夹具在自己
	// 坑自己（那条子用例名是 "a hanging self-check is a failure, not a pass"）。
	//
	// 用固定前缀的临时目录：路径里没有标点，也就与用例名解耦。
	proj, err := os.MkdirTemp("", "ngm-sbx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(proj) })
	if code, out := runCaptureCode(t, "init", "github.com:my-org/app", "--runtime=node", "--dir="+proj); code != 0 {
		t.Fatalf("init: %s", out)
	}
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
		// 常规那部分输出必须**逐字**是前缀；沙箱只会多出一段（审计 T-05：
		// 旧版只 Contains 前 40 字节，40 字节之后的结论被改也不会红）。
		// 两次调用各自新建上游仓库，commit 短 SHA 必然不同，先归一化再比。
		normSHA := regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
		plainN := normSHA.ReplaceAllString(plainOut, "<sha>")
		sandboxN := normSHA.ReplaceAllString(sandboxOut, "<sha>")
		// 沙箱段落插在逐条结论与总结之间，不是整体追加在末尾；把它整块摘掉后
		// 剩余内容必须与常规报告逐字相同。
		const block = "— dependency self-checks (--sandbox) —\n" +
			"sandbox: no dependency provides verify.js — nothing to execute\n\n"
		if stripped := strings.Replace(sandboxN, block, "", 1); stripped != plainN {
			t.Errorf("the ordinary part of the report changed:\nplain:\n%s\nsandbox:\n%s", plainOut, sandboxOut)
		}
		if !strings.Contains(sandboxOut, "— dependency self-checks (--sandbox) —") {
			t.Errorf("the appended sandbox section is missing:\n%s", sandboxOut)
		}
	})
}

// TestV03SandboxRealDeno 用**真实 Deno**验收沙箱在 CLI 里的行为。
//
// 与 internal/security 的沙箱测试分工：那里证明 flag 与逃逸阻断，
// 这里证明**接线**——脚本从哪找、权限从哪来、失败如何变成退出码。
func TestV03SandboxRealDeno(t *testing.T) {
	requireSupportedDeno(t, "TestV03SandboxAcceptance covers the wiring without it")

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
let why = "no error at all";
try {
  Deno.readTextFileSync("../../../../ngm.json");
} catch (e) {
  // **两种类名都要认**：Deno 2 把权限错误的 name 从 PermissionDenied 改成了 NotCapable
  // （v0.5 在真实 2.4.0 上实测）。只认旧名字的后果不是"测试失败"这么简单——
  // 它会把**已经被正确拦下**的读取报成 "outside: READABLE"，也就是把一次假失败
  // 说成"沙箱坏了"。把实际类名打出来，是为了让下一次改名一眼可见，
  // 而不是又花一轮去猜是沙箱的问题还是命名的问题。
  why = e.name;
  blocked = e.name === "PermissionDenied" || e.name === "NotCapable";
}
console.log(blocked ? "outside: blocked (" + why + ")" : "outside: READABLE (" + why + ")");
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
