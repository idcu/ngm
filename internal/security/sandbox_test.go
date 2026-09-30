package security

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

func sandboxPolicy(t *testing.T, allow, deny []string) *Policy {
	t.Helper()
	pol, err := NewPolicy(allow, deny, "~/.ngm/config.json")
	if err != nil {
		t.Fatal(err)
	}
	return pol
}

// 权限映射是沙箱的全部安全语义：需求与策略求交，未获授权一律拒绝。
func TestPlanSandbox(t *testing.T) {
	needs := Needs{
		ReadDirs: []string{"/vendor/org/repo"},
		NetHosts: []string{"example.com"},
		RunExes:  []string{"git"},
		EnvVars:  []string{"GITHUB_TOKEN"},
	}

	t.Run("nothing is granted without permission", func(t *testing.T) {
		pol := sandboxPolicy(t, nil, nil)
		// 读自己那棵树是默认允许的（read 档位为"允许"），其余都要显式授权
		g, err := pol.PlanSandbox(Needs{ReadDirs: []string{"/vendor/org/repo"}})
		if err != nil {
			t.Fatalf("reading the dependency's own tree should be allowed by default: %v", err)
		}
		if len(g.Read) != 1 || len(g.Net) != 0 || len(g.Run) != 0 {
			t.Errorf("unexpected grants: %+v", g)
		}

		for _, n := range []Needs{{NetHosts: []string{"example.com"}}, {RunExes: []string{"bash"}}} {
			if _, err := pol.PlanSandbox(n); err == nil {
				t.Errorf("these needs must be refused without permission: %+v", n)
			}
		}
	})

	t.Run("granted needs are mapped through", func(t *testing.T) {
		pol := sandboxPolicy(t, []string{"net:example.com", "run:git", "env:GITHUB_TOKEN"}, nil)
		g, err := pol.PlanSandbox(needs)
		if err != nil {
			t.Fatalf("PlanSandbox: %v", err)
		}
		if len(g.Net) != 1 || g.Net[0] != "example.com" {
			t.Errorf("net grants = %v", g.Net)
		}
		if len(g.Run) != 1 || g.Run[0] != "git" {
			t.Errorf("run grants = %v", g.Run)
		}
		if len(g.Env) != 1 || g.Env[0] != "GITHUB_TOKEN" {
			t.Errorf("env grants = %v", g.Env)
		}
	})

	// deny 优先于 allow：沙箱是 deny 最需要生效的地方
	t.Run("deny wins inside the sandbox too", func(t *testing.T) {
		pol := sandboxPolicy(t, []string{"net:example.com"}, []string{"net:example.com"})
		if _, err := pol.PlanSandbox(Needs{NetHosts: []string{"example.com"}}); err == nil {
			t.Error("a denied host must not be granted")
		}
	})

	// 需要项去重并排序：flag 列表必须可复现，否则同一次执行的 argv 每次都不同
	t.Run("grants are deduped and sorted", func(t *testing.T) {
		pol := sandboxPolicy(t, []string{"net:b.com", "net:a.com", "run:z", "run:a"}, nil)
		g, err := pol.PlanSandbox(Needs{
			NetHosts: []string{"b.com", "a.com", "a.com"},
			RunExes:  []string{"z", "a"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(g.Net, ",") != "a.com,b.com" || strings.Join(g.Run, ",") != "a,z" {
			t.Errorf("grants are not sorted/deduped: %+v", g)
		}
	})
}

// TestGrants_DenoArgs 固定"边界必须写在 argv 里"这条纪律。
func TestGrants_DenoArgs(t *testing.T) {
	t.Run("everything ungranted is denied explicitly", func(t *testing.T) {
		args := Grants{}.DenoArgs()
		joined := strings.Join(args, " ")
		for _, want := range []string{"--deny-read", "--deny-net", "--deny-run", "--deny-env", "--deny-write"} {
			if !strings.Contains(joined, want) {
				t.Errorf("argv must state the denial explicitly (%s is missing): %v", want, args)
			}
		}
		// 不从网络加载模块：否则 import "https://..." 会把网络访问变成模块加载的副作用
		if !strings.Contains(joined, "--no-remote") {
			t.Errorf("argv must prevent module loading from the network: %v", args)
		}
		// 写权限从不授予
		if strings.Contains(joined, "--allow-write") {
			t.Errorf("write must never be granted: %v", args)
		}
	})

	t.Run("grants become allow flags", func(t *testing.T) {
		args := Grants{Read: []string{"/a", "/b"}, Net: []string{"h"}}.DenoArgs()
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--allow-read=/a,/b") {
			t.Errorf("read grants = %v", args)
		}
		if !strings.Contains(joined, "--allow-net=h") {
			t.Errorf("net grants = %v", args)
		}
		if strings.Contains(joined, "--deny-read") {
			t.Errorf("a granted category must not also be denied: %v", args)
		}
	})
}

// 失败语义：脚本只能否决，不能证明；超时与脚本报错必须分开说。
func TestScriptError(t *testing.T) {
	if err := ScriptError("github:o/r", &Result{ExitCode: 0}); err != nil {
		t.Errorf("a passing script is not an error: %v", err)
	}

	failed := ScriptError("github:o/r", &Result{ExitCode: 3, Stderr: []byte("bad")})
	if failed == nil {
		t.Fatal("a failing script must fail the check")
	}
	if got := errs.ExitCode(failed); got != 2 {
		t.Errorf("a dependency's own check failing is an integrity failure (exit 2), got %d", got)
	}
	if !strings.Contains(failed.Error(), "exit 3") {
		t.Errorf("the error should carry the script's exit code: %v", failed)
	}

	timedOut := ScriptError("github:o/r", &Result{TimedOut: true, ExitCode: -1})
	if timedOut == nil {
		t.Fatal("a timeout must fail the check")
	}
	if got := errs.ExitCode(timedOut); got != 2 {
		t.Errorf("a timeout must also be exit 2, got %d", got)
	}
	// "没能得出结论"与"它说自己不过关"是两件事，报告里必须区分
	if !strings.Contains(errs.FormatHuman(timedOut), "did not finish") {
		t.Errorf("a timeout must be reported as such:\n%s", errs.FormatHuman(timedOut))
	}
}

func TestEnvWithout(t *testing.T) {
	env := []string{"PATH=/bin", "GITHUB_TOKEN=secret", "HOME=/home/x"}
	got := envWithout(env, []string{"github_token"})
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "secret") {
		t.Errorf("the denied variable survived: %v", got)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "HOME=/home/x") {
		t.Errorf("unrelated variables must survive: %v", got)
	}
	if len(envWithout(env, nil)) != len(env) {
		t.Error("with nothing denied the environment must pass through")
	}
}

// ---- 真实 Deno 验收 ----
//
// 上面那些证明"我们生成了正确的 flag"，只有真 Deno 能证明"这些 flag 真的拦住了"。
// 与引擎侧同样的分工：缺工具时跳过，CI 的 engine-integration job 会装上 Deno。

func realDeno(t *testing.T) Deno {
	t.Helper()
	path, err := FindDeno()
	if err != nil {
		t.Skip("deno is not installed; the flag assertions above cover the mapping")
	}
	d := Deno{Path: path, Timeout: 30 * time.Second}
	if err := d.Probe(context.Background()); err != nil {
		t.Skipf("this deno does not support the required flags: %v", err)
	}
	return d
}

// TestSandbox_RealDeno_BlocksEscapes 是 C 组验收的核心：
// 沙箱内读不到 `~/.ssh`、`~/.git-credentials`，也访问不了网络。
func TestSandbox_RealDeno_BlocksEscapes(t *testing.T) {
	d := realDeno(t)

	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(sshDir, "id_rsa")
	if err := os.WriteFile(secretPath, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	allowed := t.TempDir()
	if err := os.WriteFile(filepath.Join(allowed, "readable.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 脚本尝试三件事：读允许目录（应当成功）、读 ~/.ssh（应当被拦）、访问网络（应当被拦）。
	// 只有三者的结果都符合预期时它才 exit 0——于是"沙箱守住了"本身成为一次可断言的结果。
	script := filepath.Join(allowed, "probe.js")
	body := `const expected = {
  readable: "ok",
  secret: "blocked",
  net: "blocked",
};
const results = {};
try { results.readable = Deno.readTextFileSync("./readable.txt").trim(); }
catch (e) { results.readable = "blocked"; }
try { Deno.readTextFileSync(` + jsString(secretPath) + `); results.secret = "READ"; }
catch (e) { results.secret = "blocked"; }
try { await fetch("https://example.com"); results.net = "REACHED"; }
catch (e) { results.net = "blocked"; }

const ok = Object.keys(expected).every(k => results[k] === expected[k]);
console.log(JSON.stringify(results));
Deno.exit(ok ? 0 : 1);
`
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	pol := sandboxPolicy(t, nil, nil)
	g, err := pol.PlanSandbox(Needs{ReadDirs: []string{allowed}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.RunScript(context.Background(), script, allowed, g)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("the sandbox did not hold (exit %d).\nstdout: %s\nstderr: %s",
			res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), `"secret":"blocked"`) {
		t.Errorf("the private key was reachable:\n%s", res.Stdout)
	}
}

// 网络只有在显式授权时才可达——这是 net 权限在沙箱里的落点。
// 用不可路由的地址而不是真站点：**测试不依赖公网**，而这里要验证的是
// "被允许访问主机"，不是"那个主机可达"（DNS/连接失败同样证明网络层开了）。
func TestSandbox_RealDeno_NetOnlyWhenGranted(t *testing.T) {
	d := realDeno(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "net.js")
	if err := os.WriteFile(script, []byte(`try {
  await fetch("http://127.0.0.1:1/");
  console.log("net: reached");
} catch (e) {
  console.log("net:", e.name === "PermissionDenied" ? "blocked" : "allowed-but-unreachable");
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("denied by default", func(t *testing.T) {
		pol := sandboxPolicy(t, nil, nil)
		g, err := pol.PlanSandbox(Needs{ReadDirs: []string{dir}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := d.RunScript(context.Background(), script, dir, g)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(res.Stdout), "net: blocked") {
			t.Errorf("without net: permission the network must be unreachable:\n%s", res.Stdout)
		}
	})

	t.Run("granted host opens the network layer", func(t *testing.T) {
		pol := sandboxPolicy(t, []string{"net:127.0.0.1"}, nil)
		g, err := pol.PlanSandbox(Needs{ReadDirs: []string{dir}, NetHosts: []string{"127.0.0.1"}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := d.RunScript(context.Background(), script, dir, g)
		if err != nil {
			t.Fatal(err)
		}
		// 关键在于**不是** PermissionDenied：端口 1 连不上是环境事实，
		// 而"权限被拒"说明授权根本没生效。
		if strings.Contains(string(res.Stdout), "blocked") {
			t.Errorf("a granted host must pass the permission layer:\n%s", res.Stdout)
		}
	})
}

// 超时必须被识别为超时，而不是"脚本报错"。
func TestSandbox_RealDeno_TimeoutIsReported(t *testing.T) {
	d := realDeno(t)
	d.Timeout = 2 * time.Second

	dir := t.TempDir()
	script := filepath.Join(dir, "hang.js")
	if err := os.WriteFile(script, []byte("while (true) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pol := sandboxPolicy(t, nil, nil)
	g, err := pol.PlanSandbox(Needs{ReadDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	res, err := d.RunScript(context.Background(), script, dir, g)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("an infinite loop must be killed (exit %d after %s)", res.ExitCode, time.Since(start))
	}
	if err := ScriptError("github:o/r", res); err == nil {
		t.Error("a timeout must fail the check")
	} else if !strings.Contains(errs.FormatHuman(err), "did not finish") {
		t.Errorf("a timeout must not be reported as a script failure:\n%s", errs.FormatHuman(err))
	}
}

func jsString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
