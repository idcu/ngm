package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/lock"
	"github.com/idcu/ngm/internal/security"
	"github.com/idcu/ngm/internal/vendor"
)

// sandboxScript 是一条待执行的依赖自证脚本。
type sandboxScript struct {
	dep  string
	dir  string
	path string
}

// collectSandboxScripts 找出所有提供了 `verify.js` 的依赖（按依赖排序，输出可复现）。
func collectSandboxScripts(env *projectEnv, pf *config.ProjectFile, lf *lock.File) []sandboxScript {
	root := env.VendorRoot(pf)
	scripts := make([]sandboxScript, 0, len(lf.Dependencies))

	for i := range lf.Dependencies {
		d := &lf.Dependencies[i]
		dir := filepath.Join(root, filepath.FromSlash(vendor.VendorPathFor(d.VendorPath, d.SubPath)))
		path := security.ScriptPath(dir)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		scripts = append(scripts, sandboxScript{dep: d.Name, dir: dir, path: path})
	}
	return scripts
}

// runSandboxChecks 在沙箱里执行依赖自带的校验脚本。
//
// 三条纪律（ADR-012）：
//
//  1. 脚本**只能否决**：它失败会让 verify 失败（exit 2，`--allow-drift` 无效），
//     它成功不会改变 ngm 自己的任何结论。输出照原样展示，但不解析、不采信。
//  2. 权限从用户的词表继承，默认只读该依赖自己的 vendor 子树；没有 Deno 就 exit 5，
//     **不降级**为非沙箱执行。
//  3. 没有依赖提供脚本时不需要 Deno——那时没有任何东西要跑，
//     报"沙箱不可用"是一句与事实不符的话。
func runSandboxChecks(ctx context.Context, env *projectEnv, pf *config.ProjectFile, lf *lock.File, stdout, stderr io.Writer) error {
	scripts := collectSandboxScripts(env, pf, lf)
	if len(scripts) == 0 {
		fmt.Fprintln(stdout, "sandbox: no dependency provides "+security.SandboxScriptName+
			" — nothing to execute")
		return nil
	}

	denoPath, derr := security.FindDeno()
	if derr != nil {
		return derr
	}
	deno := security.Deno{Path: denoPath, DeniedEnv: env.Policy.DeniedEnvVars()}
	if perr := deno.Probe(ctx); perr != nil {
		return perr
	}

	fmt.Fprintf(stdout, "sandbox: %d dependency script(s) in a Deno sandbox (deno %s)\n",
		len(scripts), denoPath)
	fmt.Fprintln(stdout,
		"         their output is the dependency's own claim about itself; ngm does not verify it")

	for _, s := range scripts {
		// 需求 = 读该依赖自己的子树 + 用户已经授权过的 net/run/env。
		//
		// 不从脚本"想要什么"出发：那是不可信输入。沙箱给的是 ngm 决定给的东西，
		// 脚本要么在这个边界内完成，要么失败。
		needs := security.Needs{
			ReadDirs: []string{s.dir},
			NetHosts: env.Policy.AllowedTargets(security.Net),
			RunExes:  env.Policy.AllowedTargets(security.Run),
			EnvVars:  env.Policy.AllowedTargets(security.Env),
		}
		grants, gerr := env.Policy.PlanSandbox(needs)
		if gerr != nil {
			return gerr
		}

		res, rerr := deno.RunScript(ctx, s.path, s.dir, grants)
		if rerr != nil {
			return rerr
		}

		fmt.Fprintf(stdout, "\n  %s\n", s.dep)
		if len(res.Stdout) > 0 {
			fmt.Fprintf(stdout, "    %s", indent(string(res.Stdout), "    "))
		}
		if len(res.Stderr) > 0 {
			fmt.Fprintf(stderr, "    %s", indent(string(res.Stderr), "    "))
		}

		if serr := security.ScriptError(s.dep, res); serr != nil {
			return serr
		}
		fmt.Fprintln(stdout, "    ✓ self-check passed")
	}
	return nil
}

// indent 给多行文本统一缩进（最后一行补换行）。
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	out := ""
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out += prefix + s[start:i+1]
			start = i + 1
		}
	}
	if start < len(s) {
		out += prefix + s[start:] + "\n"
	}
	return out
}
