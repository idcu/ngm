package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// denoFloorMajor / denoFloorMinor 是**文档声明的下限**（`guides/installation.md`
// 的"Deno >= 2.0"），不是随手挑的数。
//
// 为什么真实引擎测试必须看它（v0.116）：这些测试此前只检查 `exec.LookPath("deno")`
// ——只要 deno 在 PATH 上就跑。而本机装的是 **1.45.2**，低于自己声明的下限，
// 四组真实引擎测试照样全绿。那次全绿**什么也没证明**：它证明的是"在 ngm 不支持
// 的环境上碰巧能过"。
//
// CI 早就把版本钉在 v2.4.0，理由写在 ci.yml 里：v0.5 时 CI 跑 1.x 而文档写 2.0，
// 于是 Deno 2 把权限错误的类名从 `PermissionDenied` 改成 `NotCapable` 这件事
// 被藏了很久——验收夹具只认旧名字，2.x 上会把"已被正确拦下"报成"沙箱坏了"。
// 那条注释管住了 CI，没管住本地：本地跑一次 1.x 仍然给出一片绿。
const (
	denoFloorMajor = 2
	denoFloorMinor = 0
)

var reDenoVersion = regexp.MustCompile(`^deno (\d+)\.(\d+)`)

// requireSupportedDeno 在 deno 缺失或**低于文档下限**时跳过，并说清为什么。
//
// 跳过而不是失败：一台只装了 1.x 的机器确实无法验证受支持的那一段，
// 而把它变成失败会让"没装对版本"和"产品坏了"看起来一样。但这一支绝不能安静——
// 消息里要同时给出找到的版本、声明的下限、以及 CI 钉的版本，
// 否则"跳过"会被读成"这条测试本来就不重要"。
func requireSupportedDeno(t *testing.T, why string) {
	t.Helper()
	if _, err := exec.LookPath("deno"); err != nil {
		t.Skipf("deno is not installed; %s", why)
	}
	out, err := exec.Command("deno", "--version").Output()
	if err != nil {
		t.Skipf("could not run `deno --version` (%v); %s", err, why)
	}
	m := reDenoVersion.FindStringSubmatch(string(out))
	if m == nil {
		// 读不出版本号时**不跳过**：那说明 deno 的输出形态变了，
		// 而"读数解析不了"正是本仓库反复吃亏的地方，应当被看见。
		t.Fatalf("cannot parse a version out of `deno --version`:\n%s", out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < denoFloorMajor || (major == denoFloorMajor && minor < denoFloorMinor) {
		t.Skipf("deno %d.%d is below the documented floor %d.%d (guides/installation.md); "+
			"CI pins v2.4.0. A run below the floor does not exercise the supported range — "+
			"v0.5 was hidden for a long time by exactly this. %s",
			major, minor, denoFloorMajor, denoFloorMinor, why)
	}
}

// TestV116DenoFloorIsTheDocumentedOne 盯住上面那两个数：它们是从文档抄来的，
// 而抄来的数会与文档分叉（这个仓库已经因此吃过几次）。
//
// 判据只做一件事：`guides/installation.md` 里出现的那句下限，必须与常量一致。
func TestV116DenoFloorIsTheDocumentedOne(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "guides", "installation.md"))
	if err != nil {
		t.Fatalf("read installation.md: %v", err)
	}
	re := regexp.MustCompile(`Deno\s*>=\s*(\d+)\.(\d+)`)
	m := re.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("installation.md 里找不到形如 `Deno >= X.Y` 的下限声明——判据失去依据")
	}
	if m[1] != strconv.Itoa(denoFloorMajor) || m[2] != strconv.Itoa(denoFloorMinor) {
		t.Errorf("文档声明的下限是 Deno >= %s.%s，而 engine_floor_test.go 里写的是 %d.%d —— "+
			"两个数必须一起改", m[1], m[2], denoFloorMajor, denoFloorMinor)
	}
}
