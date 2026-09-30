package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/security"
	"github.com/idcu/ngm/internal/supplychain"
	"github.com/idcu/ngm/internal/vendor"
)

// PostInstallHookName 是依赖用来声明"安装后要做点什么"的文件名。
//
// 与 `verify.js` 同一套约定，理由也相同：ngm 只执行**能被沙箱约束**的东西。
// npm 风格的 shell 钩子（`package.json` 的 `scripts.postinstall`）会被检测到，
// 但**不执行**——实测：派生出去的 shell 不受 Deno 权限约束（ADR-012 决策 3），
// 执行它等于把沙箱一次性绕开。
const PostInstallHookName = "postinstall.js"

// postInstallHook 描述一个依赖声明的安装后动作。
type postInstallHook struct {
	dep string
	dir string
	// js 是可被沙箱执行的钩子路径（空表示该依赖没有 JS 钩子）。
	js string
	// shell 是 `package.json` 里 scripts.postinstall 的内容（检测到，但从不执行）。
	shell string
}

// collectPostInstallHooks 找出所有声明了钩子的依赖（按依赖顺序，输出可复现）。
func collectPostInstallHooks(env *projectEnv, pf *config.ProjectFile, items []digestNode) []postInstallHook {
	root := env.VendorRoot(pf)
	hooks := make([]postInstallHook, 0, len(items))

	for _, dn := range items {
		dir := filepath.Join(root, filepath.FromSlash(vendor.VendorPathFor(dn.Repo.MirrorRelPath(), dn.SubPath)))
		h := postInstallHook{dep: dn.Name, dir: dir}

		if path := filepath.Join(dir, PostInstallHookName); fileExists(path) {
			h.js = path
		}
		if cmd := packageJSONPostInstall(filepath.Join(dir, "package.json")); cmd != "" {
			h.shell = cmd
		}
		if h.js != "" || h.shell != "" {
			hooks = append(hooks, h)
		}
	}
	return hooks
}

// packageJSONPostInstall 读取 `package.json` 的 `scripts.postinstall`。
func packageJSONPostInstall(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var pj struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pj) != nil {
		return ""
	}
	return strings.TrimSpace(pj.Scripts["postinstall"])
}

// runPostInstall 按 `postInstallPolicy` 处理依赖声明的钩子。
//
// 语义（ADR-009 决策 5，v0.3 修订）：
//
//	deny（默认）不执行，并**明说**有钩子没被跑
//	prompt      不执行，并说清怎么开启（ngm 是非交互工具，"询问"没有真实形态）
//	allow       执行——沙箱内、只执行 JS 钩子、失败即 exit 2
func runPostInstall(
	ctx context.Context,
	env *projectEnv,
	pf *config.ProjectFile,
	items []digestNode,
	stdout, stderr io.Writer,
) error {
	hooks := collectPostInstallHooks(env, pf, items)
	if len(hooks) == 0 {
		return nil
	}

	pol, err := projectPolicy(pf)
	if err != nil {
		return err
	}

	if !pol.PostInstallIsActive() {
		reportPostInstallSkipped(pol, hooks, stdout)
		return nil
	}
	return executePostInstallHooks(ctx, env, pol, hooks, stdout, stderr)
}

// reportPostInstallSkipped 说明"有钩子，但按策略没有执行"。
//
// 这段输出是必须的：`postInstallPolicy` 在 v0.2 就存在，而它当时不执行任何东西；
// 用户配了 `prompt` / `allow` 却什么都没看到，很容易以为钩子已经跑过了。
func reportPostInstallSkipped(pol *supplychain.Policy, hooks []postInstallHook, stdout io.Writer) {
	fmt.Fprintf(stdout, "\npostinstall: %d dependency(ies) declare an install-time hook; "+
		"postInstallPolicy=%s, so nothing was executed\n", len(hooks), pol.PostInstallPolicy())
	for _, h := range hooks {
		names := make([]string, 0, 2)
		if h.js != "" {
			names = append(names, PostInstallHookName)
		}
		if h.shell != "" {
			names = append(names, "package.json scripts.postinstall")
		}
		fmt.Fprintf(stdout, "  %s (%s)\n", h.dep, strings.Join(names, ", "))
	}
	if pol.PostInstallPolicy() == "prompt" {
		fmt.Fprintln(stdout,
			"  ngm never asks interactively (there is no TTY in CI), so `prompt` means \"do not run\": "+
				"set postInstallPolicy to \"allow\" to execute them in the sandbox")
	} else {
		fmt.Fprintln(stdout,
			"  set `supplyChain.postInstallPolicy` to \"allow\" to run JS hooks in the sandbox")
	}
}

// executePostInstallHooks 在沙箱里执行 JS 钩子。
func executePostInstallHooks(
	ctx context.Context,
	env *projectEnv,
	pol *supplychain.Policy,
	hooks []postInstallHook,
	stdout, stderr io.Writer,
) error {
	jsHooks := make([]postInstallHook, 0, len(hooks))
	for _, h := range hooks {
		if h.js != "" {
			jsHooks = append(jsHooks, h)
		}
		if h.shell != "" {
			// 检测到就说清为什么不做，而不是默默跳过：
			// "我配了 allow 却什么都没发生"是最难查的一类困惑。
			fmt.Fprintf(stderr, "warning: %s declares `scripts.postinstall` (%q); ngm does not run "+
				"npm-style shell hooks — a spawned shell is not constrained by the sandbox, "+
				"so running it would defeat the sandbox entirely (ADR-012). "+
				"Provide a %s instead if you want it sandboxed\n",
				h.dep, truncateCmd(h.shell), PostInstallHookName)
		}
	}

	if len(jsHooks) == 0 {
		return nil
	}

	// 只有确实要执行时才需要 Deno——没有可执行的东西时报"沙箱不可用"与事实不符。
	denoPath, derr := security.FindDeno()
	if derr != nil {
		return derr
	}
	deno := security.Deno{Path: denoPath, DeniedEnv: env.Policy.DeniedEnvVars()}
	if perr := deno.Probe(ctx); perr != nil {
		return perr
	}

	fmt.Fprintf(stdout, "\npostinstall: %d hook(s), postInstallPolicy=%s, executed in a Deno sandbox\n",
		len(jsHooks), pol.PostInstallPolicy())
	fmt.Fprintln(stdout, "             their output is the dependency's own; ngm does not verify it")

	for _, h := range jsHooks {
		// 与 verify 的沙箱同一套边界：读该依赖自己的子树；net / env 需要显式授权；
		// 一律不派生进程、不写任何东西（vendor 是可证明的，能改写它就会让 digest 失效）。
		needs := security.Needs{
			ReadDirs: []string{h.dir},
			NetHosts: env.Policy.AllowedTargets(security.Net),
			EnvVars:  env.Policy.AllowedTargets(security.Env),
		}
		grants, gerr := env.Policy.PlanSandbox(needs)
		if gerr != nil {
			return gerr
		}

		res, rerr := deno.RunScript(ctx, h.js, h.dir, grants)
		if rerr != nil {
			return rerr
		}

		fmt.Fprintf(stdout, "  %s\n", h.dep)
		if len(res.Stdout) > 0 {
			fmt.Fprintf(stdout, "    %s", indent(string(res.Stdout), "    "))
		}
		if len(res.Stderr) > 0 {
			fmt.Fprintf(stderr, "    %s", indent(string(res.Stderr), "    "))
		}

		if res.TimedOut || res.ExitCode != 0 {
			return postInstallError(h.dep, res)
		}
	}
	return nil
}

// postInstallError 把钩子失败映射为错误（exit 2）。
//
// 与沙箱里的自检失败同级：这是"依赖自己的代码没有完成它该完成的事"，
// 属完整性问题而不是配置问题——CI 应当在这里停下来，而不是继续构建。
func postInstallError(dep string, res *security.Result) error {
	if res.TimedOut {
		return errs.New(errs.CodeDigestMismatch,
			fmt.Sprintf("%s: its %s did not finish within %s", dep, PostInstallHookName, security.SandboxTimeout),
			"a hook that cannot finish is not a pass; run it by hand to see what it is doing")
	}
	return errs.New(errs.CodeDigestMismatch,
		fmt.Sprintf("%s: its %s failed with exit %d", dep, PostInstallHookName, res.ExitCode),
		"the dependency could not complete its install step; inspect the output above")
}

// fileExists 报告路径存在且不是目录。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// truncateCmd 把命令截断到一行，避免把多行脚本原样塞进警告。
func truncateCmd(cmd string) string {
	cmd = strings.ReplaceAll(strings.TrimSpace(cmd), "\n", " ")
	const max = 60
	if len(cmd) > max {
		return cmd[:max] + "…"
	}
	return cmd
}
