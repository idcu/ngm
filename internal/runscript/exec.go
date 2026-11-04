package runscript

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// ExecSpec 描述一次脚本执行。
type ExecSpec struct {
	// Dir 是子进程的工作目录（项目根）。
	Dir string
	// Command 是脚本命令串（原样交给平台 shell）。
	Command string
	// Event 是本步的 lifecycle 事件名（`build` / `prebuild` / `postbuild`）。
	Event string
	// PackageName / PackageVersion 用于 `npm_package_name` / `npm_package_version`。
	PackageName    string
	PackageVersion string
	// ExtraArgs 是 `--` 之后的参数，原样按空格追加到 Command 末尾。
	ExtraArgs []string
	// BinDirs 是注入 PATH 前端的目录（见 BinDirs）。
	BinDirs []string
}

// Shell 返回平台 shell 及其固定参数。
//
// POSIX 用 `sh -c`，Windows 用 `cmd.exe /d /s /c`——与 npm 选择同一套，
// 因此引号、通配、`&&` 的语义与用户已有的认知一致。ngm **不**做二次引号
// 处理：自研一套引号语义只会制造第三套规则。
func Shell() (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/d", "/s", "/c"}
	}
	return "sh", []string{"-c"}
}

// AppendArgs 把额外参数按空格追加到命令串末尾。
//
// 不做引号转义：参数里若含空格，那是调用方（以及平台 shell）要处理的事。
// 与 npm 的 `script + " " + args.join(" ")` 同形。
func AppendArgs(command string, extra []string) string {
	if len(extra) == 0 {
		return command
	}
	return command + " " + strings.Join(extra, " ")
}

// ChildEnv 返回子进程环境：父环境 + 最小 lifecycle 变量集 + PATH 注入。
//
// **契约是最小集，不是“近似 npm”**：只承诺
//
//	INIT_CWD、npm_lifecycle_event、npm_lifecycle_script、
//	npm_package_name、npm_package_version
//
// 刻意不设 `npm_execpath`（ngm 不是 npm，脚本不该据此重新调用 npm），
// 也不承诺 `npm_config_*`——那是 npm 的内部实现细节，模拟得越像，
// 越容易被误认为兼容层。
func ChildEnv(spec ExecSpec) []string {
	path := injectedPath(spec.BinDirs)
	env := dropKeys(os.Environ(), "PATH", "INIT_CWD",
		"npm_lifecycle_event", "npm_lifecycle_script",
		"npm_package_name", "npm_package_version")

	cwd, _ := os.Getwd()
	env = append(env,
		"PATH="+path,
		"INIT_CWD="+cwd,
		"npm_lifecycle_event="+spec.Event,
		"npm_lifecycle_script="+spec.Command,
		"npm_package_name="+spec.PackageName,
		"npm_package_version="+spec.PackageVersion,
	)
	return env
}

// injectedPath 把 BinDirs 放到 PATH 前端（近者在前）。
func injectedPath(binDirs []string) string {
	base := os.Getenv("PATH")
	if len(binDirs) == 0 {
		return base
	}
	parts := make([]string, 0, len(binDirs)+1)
	for _, d := range binDirs {
		if strings.TrimSpace(d) != "" {
			parts = append(parts, d)
		}
	}
	if base != "" {
		parts = append(parts, base)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// dropKeys 从环境里剔除指定键（PATH 在 Windows 上大小写不敏感）。
func dropKeys(env []string, keys ...string) []string {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[strings.ToUpper(k)] = true
	}
	out := env[:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if set[strings.ToUpper(k)] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Exec 执行一步脚本并返回其退出码。
//
// 退出码规则（契约）：
//
//	· 子进程自己的退出码**原样返回**——绝不映射成 ngm 的 0–6；
//	· 被信号终止时返回 128 + signal（SIGINT→130、SIGTERM→143）。
//
// 只有“根本没能启动 shell”才返回 error（exit 5），因为那不是脚本的行为。
func Exec(ctx context.Context, spec ExecSpec, stdout, stderr io.Writer) (int, error) {
	shell, fixed := Shell()
	cmdline := AppendArgs(spec.Command, spec.ExtraArgs)
	cmd := exec.Command(shell, append(append([]string{}, fixed...), cmdline)...)
	cmd.Dir = spec.Dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = ChildEnv(spec)

	if err := cmd.Start(); err != nil {
		return 0, errs.Wrap(errs.CodeEngineNotFound,
			"cannot start "+filepath.Base(shell)+" for `ngm run`",
			"install a POSIX shell (`sh`) or run this on Windows where `cmd.exe` exists", err)
	}

	// 不用 exec.CommandContext：它的默认行为是**杀死**子进程，于是 Ctrl+C 会变成
	// “被 SIGKILL 终止”（137），而不是用户预期的 130。这里改为把中断**转发**给
	// 子进程，让平台语义（POSIX 的进程组 / Windows 的控制台组）自然发生。
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Signal(os.Interrupt)
		case <-done:
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	if waitErr == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(waitErr, &ee) {
		return exitCodeOf(ee.ProcessState), nil
	}
	return 0, errs.Wrap(errs.CodeInternal, "wait for script",
		"the script process ended without a usable status; re-run it", waitErr)
}
