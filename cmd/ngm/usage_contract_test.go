package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestV15UsageErrorsPrintOwnUsage 固定一条 CLI 契约：**参数错误必须给出该命令自己的用法**，
// 而且**走 stderr、以 exit 3 结束**（3 = `CodeConfigInvalid`："配置/策略/lock 错误"，
// 见 internal/errs）。
//
// 为什么需要它：
//   - **退出码是脚本唯一能看的东西**。用法错误若退成 1（通用失败）或 0，调用方就分不清
//     "我参数写错了"与"检查没通过"——这两种情形的处置完全不同。
//   - **流是契约的一部分**。用法/错误走 stderr，结果走 stdout；否则
//     `ngm verify --json > report.json` 这类用法会在报告里混进一段用法文本。
//     （audit 的 `--json --hook` 就是栽在这一点上，见 v0.12 D2。）
//   - 用法错误若打印**根帮助**而不是该命令的用法，用户拿到的是 21 个命令的清单，
//     而不是他正在用的那一个的参数表——v0.12 修掉的死代码缺陷正是它的邻居。
//
// 判据全部取自**运行结果**，且两条断言互为对照：
//
//	`ngm <cmd> --help`     → exit 0，用法在 **stdout**
//	`ngm <cmd> --zz-nope`  → exit 3，用法在 **stderr**
//
// 同一份文本、两个流、两个退出码。少了任何一半，"参数错误时打印用法"都可以靠
// "永远打印用法"或"永远退 3"来满足。
func TestV15UsageErrorsPrintOwnUsage(t *testing.T) {
	isolateUserEnv(t)
	proj := newProject(t)

	// ④ 的装置：把**进程的 os.Stderr 收进管道**，放在循环之前。
	// 这样循环里任何绕过注入 writer 的输出都会落进管道，跑完再断言它是空的。
	//
	// 这一条抓的是一个真缺陷（v0.15 修）：`flag.ContinueOnError` 把**错误行**写到
	// `fs.Output()`，而它默认是 os.Stderr——不是调用方传进来的那个 writer。
	// 终端里两者是同一个地方，所以手测时看起来完全正常；只有把 os.Stderr 换掉才看得见。
	// 本包没有 t.Parallel（已核），因此这个全局替换是安全的。
	origStderr := os.Stderr
	pr, pw, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	os.Stderr = pw
	defer func() { os.Stderr = origStderr }()

	for _, spec := range commands {
		t.Run(spec.Name, func(t *testing.T) {
			own := firstLine(spec.Usage)
			root := firstLine(rootUsage)

			// ① 错的 flag：exit 3 + 自己的用法，且**只在 stderr**。
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			code := dispatch([]string{spec.Name, "--zz-nope", "--dir=" + proj}, stdout, stderr)
			if code != 3 {
				t.Errorf("exit=%d want 3 (CodeConfigInvalid): a usage error must be distinguishable "+
					"from a failed check\n--- stdout ---\n%s\n--- stderr ---\n%s", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), own) {
				t.Errorf("the error path must print this command's own usage on stderr (want %q):\n%s", own, stderr.String())
			}
			if strings.Contains(stderr.String(), root) && !strings.Contains(spec.Usage, root) {
				t.Errorf("a usage error must not fall back to the root help:\n%s", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("nothing may be written to stdout on a usage error (it corrupts pipelines):\n%s", stdout.String())
			}

			// ② 对照：同一个命令的 --help —— exit 0，用法在 stdout，stderr 空。
			hout := &bytes.Buffer{}
			herr := &bytes.Buffer{}
			hcode := dispatch([]string{spec.Name, "--help"}, hout, herr)
			if hcode != 0 {
				t.Errorf("--help exit=%d want 0: help is what the user asked for", hcode)
			}
			if got := hout.String(); got != spec.Usage {
				t.Errorf("--help must print the command's own usage verbatim\n--- got ---\n%s\n--- want ---\n%s", got, spec.Usage)
			}
			if herr.Len() != 0 {
				t.Errorf("--help must not write to stderr:\n%s", herr.String())
			}
		})
	}

	// ④ 收尾：关掉写端再读。任何字节都意味着有输出**绕过了注入的 writer**。
	if cerr := pw.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	os.Stderr = origStderr
	leaked, rerr := io.ReadAll(pr)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(leaked) > 0 {
		t.Errorf("%d byte(s) reached the process stderr instead of the caller's writer:\n%s", len(leaked), leaked)
	}
}
