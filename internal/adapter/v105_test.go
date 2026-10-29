package adapter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestMain 在本包里兼任一个"假引擎"。
//
// 当进程被以 NGM_ENV_PROBE=1 启动时，它把 SECRET_TOKEN 的值打到 stdout 后退出。
// 为什么不用 shell 脚本：Windows 上是 .cmd、Unix 上是 .sh，两套写法两套失败模式，
// 而**测试进程自己**在两个平台上都起得来，也不需要 shell。
func TestMain(m *testing.M) {
	if os.Getenv("NGM_ENV_PROBE") == "1" {
		fmt.Print("TOKEN=" + os.Getenv("SECRET_TOKEN"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// engineEnvProbe 跑一次"假引擎"，返回它的 stdout。
func engineEnvProbe(t *testing.T, denied []string) string {
	t.Helper()
	res, err := runProcessEnv(context.Background(), os.Args[0], nil, "", nil, nil, denied)
	if err != nil {
		t.Fatalf("runProcessEnv: %v", err)
	}
	return string(res.Stdout)
}

// `deny: ["env:X"]` 的第三个施力点（v0.105）。
//
// 缺陷形态：git 侧有 buildEnv、沙箱侧有 envWithout，而引擎子进程是裸
// `append(os.Environ(), extraEnv...)`。于是用户写下 `deny: ["env:GITHUB_TOKEN"]`
// 之后，esbuild / tsc / 自定义引擎照旧读得到 token——"声明了但施加点没覆盖"。
func TestV105EngineSubprocessDropsDeniedEnv(t *testing.T) {
	t.Setenv("SECRET_TOKEN", "s3cr3t")
	t.Setenv("NGM_ENV_PROBE", "1")

	got := engineEnvProbe(t, []string{"SECRET_TOKEN"})
	if strings.Contains(got, "s3cr3t") {
		t.Errorf("the denied variable reached the engine child process: %q", got)
	}
	// 探针必须真的跑过：否则上面那条断言在"什么都没执行"时也成立。
	if !strings.Contains(got, "TOKEN=") {
		t.Fatalf("the probe engine did not run at all: %q", got)
	}

	// 对照：不剔除时必须读得到。没有这一条，"删掉了整份环境"也能让上面通过。
	if control := engineEnvProbe(t, nil); !strings.Contains(control, "s3cr3t") {
		t.Errorf("the control run cannot see the variable either (%q): "+
			"the test proves nothing about filtering", control)
	}

	// 大小写不敏感：Windows 的环境变量名不区分大小写，用户写小写也必须生效。
	if got := engineEnvProbe(t, []string{"secret_token"}); strings.Contains(got, "s3cr3t") {
		t.Errorf("a lower-case deny name must still drop the variable: %q", got)
	}
}

// Runner 必须把策略里的拒绝列表**传到**引擎实例上。
// 这一条挡的是"接线接了一半"：字段加上了，构造时却还传 nil。
func TestV105RunnerWiresDeniedEnvIntoEngines(t *testing.T) {
	entry := Entry{
		Name:    "probe",
		Kind:    KindBundle,
		Adapter: AdapterSubprocess,
		Program: "probe",
	}
	eng, err := newEngine(entry, "", []string{"SECRET_TOKEN", "GITHUB_TOKEN"})
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	se, ok := eng.(*subprocessEngine)
	if !ok {
		t.Fatalf("engine type = %T, want *subprocessEngine", eng)
	}
	if len(se.deniedEnv) != 2 {
		t.Errorf("deniedEnv = %v, want the two names from the policy", se.deniedEnv)
	}

	// 导出的 NewEngine 保持旧语义（不剔除）：engines list / 版本探测走它。
	plain, perr := NewEngine(entry, "")
	if perr != nil {
		t.Fatalf("NewEngine: %v", perr)
	}
	if ps, ok := plain.(*subprocessEngine); !ok || len(ps.deniedEnv) != 0 {
		t.Errorf("NewEngine must not filter anything (it has no policy): %T %v", plain, ps)
	}
}

// "引擎退出 0"不是产物存在的证据（v0.105）。
//
// 缺陷形态：`Bundle` 把 opts.Outfile 原样抄进结果，从不看它是否真的写出来。
// 引擎写到了别处、或什么都没写，`ngm build` 仍报成功，下游拿到一个不存在的文件。
func TestV105BundleFailsWhenTheOutfileIsMissing(t *testing.T) {
	// 用测试进程自己当引擎：它退出 0，但什么都不写。
	//
	// 打开探针开关是必要的：否则测试进程会按 Go 的 flag 规则解析引擎参数
	// （`--outfile` 之类）并以 exit 2 失败——那样测到的是"引擎失败"，
	// 而这条判据要测的是"引擎**成功**却没写文件"。
	t.Setenv("NGM_ENV_PROBE", "1")
	entry := Entry{
		Name:    "noop",
		Kind:    KindBundle,
		Adapter: AdapterSubprocess,
		Program: os.Args[0],
	}
	eng, err := newEngine(entry, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	b, ok := eng.(BundleEngine)
	if !ok {
		t.Fatalf("engine does not implement BundleEngine: %T", eng)
	}
	_, berr := b.Bundle(context.Background(), "entry.js", BundleOptions{
		Outfile: "dist/out.js",
	})
	if berr == nil {
		t.Fatal("a bundle that was never written must not be reported as success")
	}
	if !strings.Contains(berr.Error(), "produced no bundle") {
		t.Errorf("the error must say what is wrong: %v", berr)
	}
}
