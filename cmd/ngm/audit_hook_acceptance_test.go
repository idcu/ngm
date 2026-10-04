package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v3AuditProject 建一个装好依赖的项目，并把 OSV 指向本地替身。
//
// 与 v0.2 的 audit 验收同一套做法：测试不依赖公网，端点由 NGM_OSV_URL 注入。
func v3AuditProject(t *testing.T, osvBody string) string {
	t.Helper()
	home := isolateUserEnv(t)

	scUpstream(t, "github:v3/audit", "export const a = 1\n", "")
	proj := newProject(t)
	if code, out := runCaptureCode(t, "add", "github:v3/audit@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
		t.Fatalf("add: %s", out)
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, osvBody)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("NGM_OSV_URL", srv.URL)
	// 替身的主机要显式授权：`net:` 默认"需配置"（v0.5 C 组补齐了 OSV 的门禁）
	grantNetFor(t, home, srv)

	return proj
}

// TestV03AuditHookAcceptance 是"用户自定义 audit hook"的 hermetic 验收。
func TestV03AuditHookAcceptance(t *testing.T) {
	// 不带 --hook 时行为完全不变：这是"只追加"的另一处体现。
	t.Run("without --hook nothing changes", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		code, out := runCaptureCode(t, "audit", "--dir="+proj)
		if code != 0 {
			t.Fatalf("audit exit=%d:\n%s", code, out)
		}
		if strings.Contains(out, "audit hook") {
			t.Errorf("the hook line must not appear when no hook was asked for:\n%s", out)
		}
	})

	// 路径写错是配置错误（exit 3），不是"钩子否决"（exit 1）——
	// 把两者混起来，CI 会分不清"我拼错了路径"与"我的策略不接受"。
	t.Run("a missing hook file is a config error", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		code, out := runCaptureCode(t, "audit", "--hook=does-not-exist.js", "--dir="+proj)
		if code != 3 {
			t.Fatalf("a missing hook must exit 3, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "audit hook not found") {
			t.Errorf("the error should say what is missing:\n%s", out)
		}
	})

	// 要执行就必须有沙箱：缺 Deno 时 exit 5，不降级。
	t.Run("a hook without deno exits 5 instead of running unsandboxed", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		testutils.WriteFile(t, proj, "policy.js", "Deno.exit(0);\n")

		t.Setenv("PATH", t.TempDir())

		code, out := runCaptureCode(t, "audit", "--hook=policy.js", "--dir="+proj)
		if code != 5 {
			t.Fatalf("a missing sandbox must exit 5, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "deno is required") {
			t.Errorf("the error should say what is missing:\n%s", out)
		}
	})
}

// TestV03AuditHookRealDeno 用**真实 Deno**验收契约本身：报告真的从 stdin 进来。
func TestV03AuditHookRealDeno(t *testing.T) {
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skip("deno is not installed; TestV03AuditHookAcceptance covers the wiring")
	}

	// 这是这套机制存在的理由：团队可以在 OSV 之上加自己的策略。
	// 断言它真的**读到了报告**（而不是拿到一个空 stdin 也照样 exit 1）。
	t.Run("the report arrives on stdin and the hook can veto", func(t *testing.T) {
		proj := v3AuditProject(t, `{"vulns":[{"id":"GHSA-hook-1","summary":"x","severity":"HIGH"}]}`)
		testutils.WriteFile(t, proj, "policy.js", `const raw = await new Response(Deno.stdin.readable).text();
const report = JSON.parse(raw);
const ids = (report.findings ?? []).flatMap(f => (f.vulnerabilities ?? []).map(v => v.id));
console.log("hook saw:", ids.join(","));
Deno.exit(ids.includes("GHSA-hook-1") ? 1 : 3);
`)

		code, out := runCaptureCode(t, "audit", "--hook=policy.js", "--dir="+proj)
		if code != 1 {
			t.Fatalf("a rejecting hook must exit 1, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "hook saw: GHSA-hook-1") {
			t.Errorf("the hook did not receive the report on stdin:\n%s", out)
		}
		if !strings.Contains(out, "rejected this dependency set") {
			t.Errorf("the report should say the hook objected:\n%s", out)
		}
	})

	t.Run("an accepting hook keeps the audit verdict", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		testutils.WriteFile(t, proj, "policy.js", `const raw = await new Response(Deno.stdin.readable).text();
JSON.parse(raw);
console.log("hook accepted");
Deno.exit(0);
`)

		code, out := runCaptureCode(t, "audit", "--hook=policy.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("an accepting hook must keep exit 0, got %d:\n%s", code, out)
		}
		if !strings.Contains(out, "audit hook passed") {
			t.Errorf("the report should say the hook passed:\n%s", out)
		}
	})

	// 钩子是用户自己的代码，但"沙箱内不写任何东西"这条对它一样成立：
	// 一个会写盘、会联网的审计脚本，和依赖脚本一样需要被约束。
	t.Run("a hook cannot write either", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		testutils.WriteFile(t, proj, "policy.js", `let blocked = false;
let why = "no error at all";
try {
  Deno.writeTextFileSync("hook-was-here.txt", "x");
} catch (e) {
  // 两种类名都要认，理由见 sandbox_acceptance_test.go（Deno 2 改为 NotCapable）
  why = e.name;
  blocked = e.name === "PermissionDenied" || e.name === "NotCapable";
}
console.log(blocked ? "write: blocked (" + why + ")" : "write: ALLOWED (" + why + ")");
Deno.exit(blocked ? 0 : 1);
`)

		code, out := runCaptureCode(t, "audit", "--hook=policy.js", "--dir="+proj)
		if code != 0 {
			t.Fatalf("the sandbox did not hold (exit %d):\n%s", code, out)
		}
		if !strings.Contains(out, "write: blocked") {
			t.Errorf("the hook wrote to disk:\n%s", out)
		}
	})

	// `--json` 的契约是"stdout 是一份机器可读的报告"（usage 里写着 CI should use this）。
	// hook 的横幅与它自己的 stdout 只能走 stderr。verify 修过同一形态
	// （`--json --sandbox` 曾把沙箱段落混进 JSON），audit 这一处当时没跟上，
	// 而它的 JSON 同样标着"给 CI 用"。
	t.Run("--json keeps stdout free of hook output", func(t *testing.T) {
		proj := v3AuditProject(t, `{}`)
		testutils.WriteFile(t, proj, "policy.js", `await new Response(Deno.stdin.readable).text();
console.log("hook said something");
Deno.exit(0);
`)

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := dispatch([]string{"audit", "--json", "--hook=policy.js", "--dir=" + proj}, stdout, stderr)
		if code != 0 {
			t.Fatalf("audit exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout.String(), stderr.String())
		}
		if !json.Valid(stdout.Bytes()) {
			t.Errorf("stdout must be a single JSON document; the hook's output leaked into it:\n%s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "hook said something") {
			t.Errorf("the hook's own output belongs on stderr:\n%s", stderr.String())
		}
	})
}
