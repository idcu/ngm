package security

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// SandboxScriptName 是依赖自证脚本的文件名（放依赖根目录）。
//
// 沿用安全模型里写的名字。它是**依赖作者**写的、随依赖进入 vendor 的代码，
// 因此执行它必须走沙箱——详见 ADR-012。
const SandboxScriptName = "verify.js"

// SandboxTimeout 是沙箱执行的上限。
//
// 超时是唯一被施加的资源限制：Deno 没有稳定的跨平台内存上限开关，
// 用一个近似手段会造成"已经限制住了"的错觉（ADR-012 决策 5）。
const SandboxTimeout = 30 * time.Second

// Needs 描述一次沙箱执行**想要**什么。它是需求，不是授权。
//
// 注意**没有**"要执行某个程序"这一项：沙箱里一律不派生进程，见 DenoArgs 的说明。
type Needs struct {
	// ReadDirs 是脚本需要读取的目录。
	ReadDirs []string
	// NetHosts 是脚本需要访问的主机。
	NetHosts []string
	// EnvVars 是脚本需要读取的环境变量名。
	EnvVars []string
}

// Grants 是**实际授予**的权限（Needs 与策略求交后的结果）。
//
// 同样没有"可执行的程序"：这是**类型层面**的保证——调用方无法表达
// "给这个脚本 --allow-run"，因为那会让沙箱形同虚设（见 DenoArgs）。
type Grants struct {
	Read []string
	Net  []string
	Env  []string
}

// PlanSandbox 依据权限策略决定这次沙箱能拿到什么；任一需求未被放行即返回错误。
//
// 它是"沙箱权限从 ngm 词表映射过来"这句话的可执行形式：
// 沙箱**不新造**权限语言（ADR-012 决策 3），只把同一套词表翻译成 Deno 的 flag。
//
// 但**默认档位不照搬**，两处刻意更严：
//
//   - 读：默认允许（否则没有任何脚本能跑起来——它连自己都读不到）
//   - 网络与环境变量：**必须显式出现在 `allow` 里**。
//     不照搬 `env:` 的"默认允许透传"，是因为那条默认值是为 **git** 定的
//     （ngm 不解析 token，只把环境交给 git 去认证）；而沙箱里跑的是一段
//     不受信任的脚本——把 token 默认交给它，与"默认拒绝"的整个设计相悖。
func (pol *Policy) PlanSandbox(n Needs) (Grants, error) {
	var g Grants

	for _, dir := range n.ReadDirs {
		if err := pol.Check(Permission{Namespace: Read, Target: dir}); err != nil {
			return Grants{}, err
		}
		g.Read = append(g.Read, dir)
	}
	for _, host := range n.NetHosts {
		if err := pol.CheckExplicit(Permission{Namespace: Net, Target: host}); err != nil {
			return Grants{}, err
		}
		g.Net = append(g.Net, host)
	}
	for _, name := range n.EnvVars {
		if err := pol.CheckExplicit(Permission{Namespace: Env, Target: name}); err != nil {
			return Grants{}, err
		}
		g.Env = append(g.Env, name)
	}

	// 去重并排序：flag 列表要可复现，否则同一次执行的 argv 每次都可能不同
	g.Read = dedupSorted(g.Read)
	g.Net = dedupSorted(g.Net)
	g.Env = dedupSorted(g.Env)
	return g, nil
}

// DenoArgs 返回把这次授权表达成 Deno flag 的参数（不含 `deno run` 与脚本本身）。
//
// **未授予的类别一律显式拒绝**，即使 Deno 本来就是默认拒绝：
// `--deny-net` 出现在 argv 里，读日志的人能直接看到边界；
// 而"它默认应该是拒绝的"是一句需要相信的话（ADR-012 决策 3）。
func (g Grants) DenoArgs() []string {
	args := make([]string, 0, 8)

	if len(g.Read) > 0 {
		args = append(args, "--allow-read="+strings.Join(g.Read, ","))
	} else {
		args = append(args, "--deny-read")
	}
	if len(g.Net) > 0 {
		args = append(args, "--allow-net="+strings.Join(g.Net, ","))
	} else {
		args = append(args, "--deny-net")
	}
	// 派生进程**一律禁止**，没有例外，也不从用户授权里继承 `run:`。
	//
	// 理由经实测：`--allow-run` 允许的进程**不受 Deno 权限约束**——
	// 给 Deno 脚本 `--allow-run=cmd` 后，它通过 `cmd /c type <path>`
	// 读到了脚本自己被禁止读取的文件。也就是说，一旦允许派生，
	// 沙箱在文件系统与网络两个维度上的约束都可以被一次性绕开。
	// 沙箱的意义就在于这两条约束，因此这里不给任何开口。
	args = append(args, "--deny-run")
	if len(g.Env) > 0 {
		args = append(args, "--allow-env="+strings.Join(g.Env, ","))
	} else {
		args = append(args, "--deny-env")
	}

	// 写权限**从不**授予：依赖的自证脚本没有任何正当的写入需求，
	// 而"它能写哪里"是最难审的一类授权。
	args = append(args, "--deny-write")

	// 不从网络加载模块：否则 `import "https://..."` 会把网络访问
	// 从"权限"变成"脚本的副作用"，而 --deny-net 拦不住模块加载。
	args = append(args, "--no-remote")

	return args
}

// Result 是一次沙箱执行的结果。
type Result struct {
	// ExitCode 是脚本的退出码；-1 表示没能启动。
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// TimedOut 为 true 表示是超时被杀的，而不是脚本自己退出。
	//
	// 单独标出来是因为两者的处理完全不同：脚本报错是"它说自己不过关"，
	// 超时是"它没能给出结论"——报告里必须区分。
	TimedOut bool
}

// Deno 是与 Deno 交互的最小封装。
type Deno struct {
	// Path 是 deno 可执行文件。
	Path string
	// Timeout 覆盖默认超时（测试用）。
	Timeout time.Duration
	// DeniedEnv 是不交给子进程的环境变量名（来自策略的 `deny: ["env:X"]`）。
	//
	// 与 git 侧同一套做法：`--deny-env` 拦的是 Deno 的 API 层，
	// 而子进程环境本身应当在**源头**就干净。
	DeniedEnv []string
}

// FindDeno 在 PATH 上查找 deno。
//
// 找不到时返回 exit 5（工具缺失）并给出安装提示——**不降级**：
// 安全语义不允许"没有沙箱就普通执行"（ADR-012 决策 6）。
func FindDeno() (string, error) {
	path, err := exec.LookPath("deno")
	if err != nil {
		return "", errs.New(errs.CodeEngineNotFound,
			"deno is required to run this script but was not found on PATH",
			"install Deno (https://deno.com), or drop the flag that asked for it; "+
				"ngm will not run a script outside a sandbox")
	}
	return path, nil
}

// Probe 确认这一版 Deno 既接受我们要用的 flag，也能执行一个脚本文件。
//
// 用**真实的脚本文件**而不是内联表达式：`deno run` 并不接受 `-e`
// （那是 `deno eval` 的形式），而沙箱真正要走的路径就是"跑一个脚本文件"。
// 用别的形式探测，等于探测了一件与实际不同的事——第一版正是这样，
// 结果在完全够用的 Deno 1.45.2 上判定为"不支持"，把功能整个挡在门外。
//
// 按能力探测而不是按版本号：写死版本会在能跑的版本上拒绝运行，
// 也会在不能跑的版本上放行（ADR-012 决策 6）。
func (d Deno) Probe(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "ngm-sandbox-probe-")
	if err != nil {
		return errs.Wrap(errs.CodeEngineNotFound, "cannot create a probe directory", "", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	script := filepath.Join(dir, "probe.js")
	if werr := os.WriteFile(script, []byte("Deno.exit(0);\n"), 0o600); werr != nil {
		return errs.Wrap(errs.CodeEngineNotFound, "cannot write the probe script", "", werr)
	}

	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	args := append([]string{"run"}, Grants{Read: []string{dir}}.DenoArgs()...)
	args = append(args, script)

	cmd := exec.CommandContext(runCtx, d.Path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Deno 的原话放进 message 而不是 hint：用户（与测试）第一眼就能看到
		// 究竟是哪个 flag 被拒绝了，而不是一句"不支持"。
		return errs.New(errs.CodeEngineNotFound,
			"this build of Deno cannot run the sandbox: "+firstLine(strings.TrimSpace(stderr.String())),
			"ngm needs --allow-read=<dir> and the --deny-* flags; run `deno --version` to see what you have")
	}
	return nil
}

// ScriptRequest 是一次沙箱执行的输入。
type ScriptRequest struct {
	// Script 是脚本的绝对路径。
	Script string
	// Dir 是工作目录（脚本的相对导入从它解析）。
	Dir string
	// Grants 是这次执行实际获得的权限。
	Grants Grants
	// Stdin 是可选的输入（如 audit 报告）。
	//
	// 用 stdin 而不是临时文件：临时文件要落到磁盘上，而"沙箱内不写任何东西"
	// 是这套边界的一部分——为传一份数据破例，下次就会为别的破例。
	Stdin []byte
}

// RunScript 在沙箱里执行脚本。
func (d Deno) RunScript(ctx context.Context, req ScriptRequest) (*Result, error) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = SandboxTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string{"run"}, req.Grants.DenoArgs()...)
	args = append(args, req.Script)

	cmd := exec.CommandContext(runCtx, d.Path, args...)
	cmd.Dir = req.Dir
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}
	cmd.Env = envWithout(os.Environ(), d.DeniedEnv)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}
	if err == nil {
		return res, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		res.ExitCode = -1
		res.TimedOut = true
		return res, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	if errors.Is(err, context.Canceled) {
		return res, errs.New(errs.CodeEngineNotFound, "sandbox run was cancelled", "")
	}
	return res, errs.Wrap(errs.CodeEngineNotFound, "failed to start deno", "", err)
}

// ScriptError 把失败的沙箱执行转成 verify 的错误（exit 2）。
//
// 归到"完整性/信任"一类而不是配置或网络类：这份代码没有通过它**自己声称**的检查，
// 与 digest 不匹配同级——`--allow-drift` 对两者都无效（ADR-012 决策 4）。
func ScriptError(dep string, res *Result) error {
	switch {
	case res.TimedOut:
		return errs.New(errs.CodeDigestMismatch,
			fmt.Sprintf("%s: its %s did not finish within %s (no verdict was produced)",
				dep, SandboxScriptName, SandboxTimeout),
			"a self-check that cannot conclude is not a pass; "+
				"run it by hand to see what it is doing")
	case res.ExitCode != 0:
		return errs.New(errs.CodeDigestMismatch,
			fmt.Sprintf("%s: its %s failed with exit %d", dep, SandboxScriptName, res.ExitCode),
			"the dependency did not pass its own check; "+
				"inspect the output above before trusting or removing it")
	}
	return nil
}

func dedupSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// envWithout 从环境里剔除指定变量（大小写不敏感，Windows 环境变量名不区分大小写）。
func envWithout(env, drop []string) []string {
	if len(drop) == 0 {
		return env
	}
	dropSet := make(map[string]bool, len(drop))
	for _, d := range drop {
		dropSet[strings.ToUpper(d)] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		if dropSet[strings.ToUpper(name)] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ScriptPath 返回依赖目录下的自证脚本路径。
//
// 调用方自己判断它是否存在：脚本是**可选**的，没有它就没这一层检查，
// 那不是错误（ADR-012 决策 2）。
func ScriptPath(depDir string) string {
	return filepath.Join(depDir, SandboxScriptName)
}
