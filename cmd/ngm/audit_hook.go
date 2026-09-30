package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/security"
	"github.com/idcu/ngm/internal/supplychain"
)

// runAuditHook 在沙箱里执行用户自定义的 audit hook。
//
// 契约：
//
//	stdin   `ngm audit` 的报告（JSON，与 `--json` 的输出一致）
//	退出码  0 = 通过；非 0 = 否决
//
// 它是**用户自己的代码**（不是依赖的），因此读权限给整个项目目录——审计脚本需要
// 看 ngm.json / ngm.lock / vendor。但它仍然在沙箱里跑：审计脚本没有理由写文件，
// 也没有理由默认拿到网络与环境变量（与依赖脚本同一条纪律，同一套权限映射）。
//
// 否决的退出码是 1（与 verify 的"非预期漂移"同码）：退出码 1 在这个项目里的语义是
// "有需要人去处理的东西"——verify 用它是漂移，audit 用它是发现漏洞，
// 而这里是用**自己的策略**说"我不接受这个结果"。它不该是 2：2 表示完整性失败。
func runAuditHook(
	ctx context.Context,
	env *projectEnv,
	rep *supplychain.AuditReport,
	hook string,
	baseCode int,
	stdout, stderr io.Writer,
) int {
	if strings.TrimSpace(hook) == "" {
		return baseCode
	}

	script := hook
	if !filepath.IsAbs(script) {
		script = filepath.Join(env.ProjectDir, filepath.FromSlash(hook))
	}
	if info, err := os.Stat(script); err != nil || info.IsDir() {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeConfigInvalid,
			"audit hook not found: "+hook,
			"pass a path to a JS file (relative paths resolve against the project directory)"))
	}

	// 与依赖脚本同一条规矩：要执行就必须有沙箱，缺了不降级。
	denoPath, derr := security.FindDeno()
	if derr != nil {
		return runErr(ctx, stdout, stderr, derr)
	}
	deno := security.Deno{Path: denoPath, DeniedEnv: env.Policy.DeniedEnvVars()}
	if perr := deno.Probe(ctx); perr != nil {
		return runErr(ctx, stdout, stderr, perr)
	}

	grants, gerr := env.Policy.PlanSandbox(security.Needs{
		ReadDirs: []string{env.ProjectDir},
		NetHosts: env.Policy.AllowedTargets(security.Net),
		EnvVars:  env.Policy.AllowedTargets(security.Env),
	})
	if gerr != nil {
		return runErr(ctx, stdout, stderr, gerr)
	}

	payload, merr := rep.Marshal()
	if merr != nil {
		return runErr(ctx, stdout, stderr, merr)
	}

	fmt.Fprintf(stdout, "\naudit hook: %s (Deno sandbox; the report arrives on stdin)\n", hook)
	res, rerr := deno.RunScript(ctx, security.ScriptRequest{
		Script: script,
		Dir:    env.ProjectDir,
		Grants: grants,
		Stdin:  payload,
	})
	if rerr != nil {
		return runErr(ctx, stdout, stderr, rerr)
	}
	if len(res.Stdout) > 0 {
		fmt.Fprintf(stdout, "  %s", indent(string(res.Stdout), "  "))
	}
	if len(res.Stderr) > 0 {
		fmt.Fprintf(stderr, "  %s", indent(string(res.Stderr), "  "))
	}

	if res.TimedOut {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeRefDrift,
			fmt.Sprintf("the audit hook did not finish within %s", security.SandboxTimeout),
			"a hook that cannot conclude is not a pass; run it by hand to see what it is doing"))
	}
	if res.ExitCode != 0 {
		return runErr(ctx, stdout, stderr, errs.New(errs.CodeRefDrift,
			fmt.Sprintf("the audit hook rejected this dependency set (exit %d)", res.ExitCode),
			"your own policy objected; the report above is what it was given"))
	}

	fmt.Fprintln(stdout, "  ✓ audit hook passed")
	return baseCode
}
