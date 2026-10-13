package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// v0.90：**把登记表当待办清单用**——那几个"只被解析网守着"的 `.ps1` 入口，真的跑一次。
//
// v0.89 的 run-or-explain 网让"没人跑"必须显式登记；这一版接着做的是把它**变短**：
// 能跑的就去跑，登记表留给真正跑不了的（缺凭据 `upload-gitee-assets.ps1`、
// 缺网络 `check-gitee-release-status.ps1`）。
//
// 每个 `.ps1` 都有**两条断言**，缺一不可：
//
//	① 它能跑（不再"只是能解析"）；
//	② 它**真的在委托实现**——用一个必然会失败的输入，验它的退出码与点名都传了回来。
//	   只验"退出 0"的网会被一个 `exit 0` 的坏包装器骗过去；而这四个文件存在的全部理由
//	   就是"转调同一个实现"。
func TestV90PowerShellWrappersRunTheirImplementation(t *testing.T) {
	ps := findPowerShell(t)
	if ps == "" {
		t.Skip("这台机器上没有 powershell/pwsh：Windows 入口只能靠 v0.88 的解析网守着")
	}
	findBash(t) // 包装器内部要用 bash；找不到时它会明确 exit 2（判据在下面各自断言）
	root := repoRoot(t)

	// run：dir 是工作目录——读数类脚本从 CWD 派生事实，所以它们要在**合成的仓库**里跑。
	run := func(t *testing.T, dir, script string, args ...string) (string, int) {
		t.Helper()
		argv := append([]string{"-NoProfile", "-NonInteractive", "-File", filepath.ToSlash(script)}, args...)
		cmd := exec.Command(ps, argv...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("跑 %s 失败：%v\n%s", filepath.Base(script), err, out)
			}
		}
		return string(out), code
	}

	t.Run("check-docs-links.ps1", func(t *testing.T) {
		// 只扫 README.md：包装器的职责是"把根传对"，不是把整棵树扫一遍。
		// （整树扫描的语义与代价由 TestV90DocsLinksStillCatchesBrokenLinks 与既有的
		// docs 网守着；这里再扫一遍等于白付一次成本。）
		out, code := run(t, root, filepath.Join(root, "scripts", "check-docs-links.ps1"), "-Root", "README.md")
		if code != 0 {
			t.Fatalf("README.md 的链接应当全过（与 CI 跑同一份实现），got exit %d：\n%s", code, out)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("包装器没有任何输出：它可能没真的转调实现")
		}
	})

	t.Run("check-vendored-standard.ps1（用**不合格**的夹具证明退出码真的传了回来）", func(t *testing.T) {
		dir := t.TempDir()
		writeFileAt(t, dir, "ngm.mappings.json", `{
  "version": 1,
  "mappings": [
    { "from": "gitee:idcu/bad", "to": "./ngm.vendor/gitee.com/idcu/bad", "main": "./index.js" }
  ]
}
`)
		writeVendoredBad(t, dir, "ngm.vendor/gitee.com/idcu/bad")

		out, code := run(t, root, filepath.Join(root, "scripts", "check-vendored-standard.ps1"), "-Dir", filepath.ToSlash(dir))
		if code != 1 {
			t.Fatalf("实现返回 1（有不合格依赖），包装器必须把它传回来，got %d：\n%s", code, out)
		}
		for _, want := range []string{"gitee:idcu/bad", "no-install-hooks"} {
			if !strings.Contains(out, want) {
				t.Errorf("包装器丢掉了实现输出里的 %q：\n%s", want, out)
			}
		}
	})

	t.Run("check-mirror-coverage.ps1（假 API：读数必须来自实现，不是包装器自己编的）", func(t *testing.T) {
		dir := syntheticDeliveredRepo(t, 3)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/tags"):
				fmt.Fprint(w, `[{"ref":"refs/tags/v0.1.0"},{"ref":"refs/tags/v0.2.0"}]`)
			case strings.HasSuffix(r.URL.Path, "/releases"):
				fmt.Fprint(w, `[{"tag_name":"v0.2.0"}]`)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		out, code := run(t, dir, filepath.Join(root, "scripts", "check-mirror-coverage.ps1"),
			"-Repo", "idcu/ngm", "-ApiBase", srv.URL)
		if code != 0 {
			t.Fatalf("读数命令不该判失败（缺口不是门禁），got %d：\n%s", code, out)
		}
		for _, want := range []string{"2 tag(s)", "UNPUSHED: 1 tag(s)"} {
			if !strings.Contains(out, want) {
				t.Errorf("包装器的输出里缺 %q（说明它没把参数/输出带过去）：\n%s", want, out)
			}
		}
	})

	t.Run("check-release-status.ps1（离线三条：合成一个「复盘 ⇄ tag」对称的仓库）", func(t *testing.T) {
		dir := syntheticDeliveredRepo(t, 1)
		gitIn(t, dir, "tag", "-a", "v0.1.0", "-m", "v0.1.0")

		out, code := run(t, dir, filepath.Join(root, "scripts", "check-release-status.ps1"))
		if code != 0 {
			t.Fatalf("对称的仓库应当全绿，got exit %d：\n%s", code, out)
		}
		if strings.TrimSpace(out) == "" {
			t.Error("包装器没有任何输出：它可能没真的转调实现")
		}

		// 反面：把 tag 删掉 ⇒ 实现会判"缺 tag"，包装器必须把这个结论与退出码传回来。
		gitIn(t, dir, "tag", "-d", "v0.1.0")
		out, code = run(t, dir, filepath.Join(root, "scripts", "check-release-status.ps1"))
		if code != 1 {
			t.Fatalf("缺 tag 时必须 exit 1（这是门禁），got %d：\n%s", code, out)
		}
		if !strings.Contains(out, "v0.1") {
			t.Errorf("点名缺了哪一版：\n%s", out)
		}
	})

	t.Run("write-readings-summary.ps1（-Out 转发 + 追加语义）", func(t *testing.T) {
		dir := syntheticDeliveredRepo(t, 3)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/tags"):
				fmt.Fprint(w, `[{"ref":"refs/tags/v0.1.0"},{"ref":"refs/tags/v0.2.0"}]`)
			case strings.HasSuffix(r.URL.Path, "/releases"):
				fmt.Fprint(w, `[{"tag_name":"v0.2.0"}]`)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		outFile := filepath.Join(t.TempDir(), "summary.md")
		if err := os.WriteFile(outFile, []byte("> 来自别的步骤的一行\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, filepath.Join(root, "scripts", "write-readings-summary.ps1"),
			"-ApiBase", srv.URL, "-Out", filepath.ToSlash(outFile))
		if code != 0 {
			t.Fatalf("读数 ≠ 门禁：本脚本应当永远 exit 0（除非用法错），got %d：\n%s", code, out)
		}
		body, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("包装器没有把 -Out 转过去（摘要没写成）：%v\n%s", err, out)
		}
		got := string(body)
		for _, want := range []string{"## ngm 读数摘要", "### GitHub 镜像", "### Gitee", "> 来自别的步骤的一行"} {
			if !strings.Contains(got, want) {
				t.Errorf("摘要里缺 %q：\n%s", want, got)
			}
		}
	})
}

// v0.90：文档链接检查的**负例**——它必须真的能抓到坏链接，且必须仍然忽略反引号里的示例。
//
// 为什么会在这时候加：v0.90 把它的路径规范化从"每条链接一次子进程"（`cd … && pwd`）
// 换成纯 bash 字符串处理（实测 133.72s → 33.07s）。**加速最容易顺手改坏语义**，
// 而"改坏了什么"在这种检查里会伪装成"全过"。这条网就是那次改动的对照：
// 一个存在的相对链接（过）· 一个 `..` 形式（过）· 一个反引号里的示例（忽略）
// · 一个真的坏链接（红并点名）。
func TestV90DocsLinksStillCatchesBrokenLinks(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-docs-links.sh")

	dir := t.TempDir()
	writeFileAt(t, dir, "docs/b.md", "# b\n")
	writeFileAt(t, dir, "docs/sub/c.md", "# c\n")
	writeFileAt(t, dir, "docs/a.md", strings.Join([]string{
		"# a",
		"",
		"[好的](./b.md)",
		"[好的（上一级）](../docs/b.md)",
		"[好的（子目录）](./sub/c.md)",
		"示例：`[不是链接](./nowhere.md)`",
		"[坏的](./missing.md)",
		"",
	}, "\n"))

	cmd := exec.Command(bash, filepath.ToSlash(script), "docs")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		t.Fatalf("有坏链接时必须非 0 退出：\n%s", text)
	}
	if !strings.Contains(text, "missing.md") {
		t.Errorf("坏链接必须被点名：\n%s", text)
	}
	if strings.Contains(text, "nowhere.md") {
		t.Errorf("反引号里的示例不该被当成链接（这条语义不能被加速改掉）：\n%s", text)
	}
	// 恰好一条：存在的（含 `../` 与子目录形式）都没被误判，反引号里的也没算。
	if n := strings.Count(text, "BROKEN LINK"); n != 1 {
		t.Errorf("应当恰好报 1 条坏链接，报了 %d 条：\n%s", n, text)
	}
}

var reLongOpt = regexp.MustCompile(`--[a-z][a-z-]*`)

// v0.90：**入口不许吞参数**——`.ps1` 必须转发同名 `.sh` 接受的每一个长选项。
//
// 起因是实测：`check-mirror-coverage.ps1` 只声明 `-Repo`，**不转发 `--api-base`**，
// 而那个选项存在的全部理由就是"给判据指一个本地假 API"——
// 于是在 Windows 上**没法**对这条读数做与 Linux 同样的验证（"唯一实现"的纪律在这里破了：
// 实现支持、入口不支持，两者就不再是同一个东西）。
//
// 口径：只取 `.sh` **自己**的选项解析（`case "$1" in` 块里的 `--xxx)` 分支），
// 因此不会把脚本内部调用别的工具时用的 `--foo` 也算进来。
// 没有 `.ps1` 入口的，必须在下面的登记表里写明原因。
func TestV90WrapperOptionsParity(t *testing.T) {
	root := repoRoot(t)
	scriptDir := filepath.Join(root, "scripts")
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		t.Fatal(err)
	}

	var shNames []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
			shNames = append(shNames, e.Name())
		}
	}
	if len(shNames) == 0 {
		t.Fatal("scripts/ 下没有 .sh —— 这条网会静默地什么都不守")
	}

	for _, sh := range shNames {
		base := strings.TrimSuffix(sh, ".sh")
		t.Run(base, func(t *testing.T) {
			shText, err := os.ReadFile(filepath.Join(scriptDir, sh))
			if err != nil {
				t.Fatal(err)
			}
			opts := ownLongOptions(string(shText))

			ps1Path := filepath.Join(scriptDir, base+".ps1")
			ps1Text, err := os.ReadFile(ps1Path)
			if err != nil {
				if reason, ok := noWindowsEntry[sh]; ok {
					t.Logf("%s 没有 .ps1 入口（已登记：%s）", sh, reason)
					return
				}
				t.Fatalf("%s 没有同名 .ps1 入口，也没有登记原因（Windows 用户只能自己去跑 bash？）", sh)
			}

			for _, o := range opts {
				// **带引号地**出现在入口里才算转发（`@('--api-base', $ApiBase)` 这种形状）。
				//
				// 为什么不能只查"文本里有没有这个名字"：第一次写这条网时就是这么写的，
				// 而牙齿**没红**——因为入口的**注释**里写着 `--api-base`（解释它为什么要转发），
				// 于是"提到"冒充了"转发"。这与 v0.89 那条网上写明的是同一件事：
				// 按文本记账是筛子，用在这里糊弄不过去。
				if !strings.Contains(string(ps1Text), "'"+o+"'") {
					t.Errorf("实现的选项 %s 没有被入口**转发**（只在注释里提到不算）：%s 支持它，%s.ps1 里没有 '@(...%s...)' 这样的转发",
						o, sh, base, o)
				}
			}
		})
	}

	for sh := range noWindowsEntry {
		if _, err := os.Stat(filepath.Join(scriptDir, sh)); err != nil {
			t.Errorf("登记表里的 %s 已经不存在了（表会变成「看起来还有人管它」）", sh)
		}
	}
}

// noWindowsEntry：**不需要** Windows 入口的脚本，逐条写明原因。
var noWindowsEntry = map[string]string{
	"probe-reproducibility.sh": "CI 专用探针（只在 repro-probe.yml 里、靠 push 到它自己或那个脚本触发）；" +
		"它不是给人手动跑的读数，Windows 上由那个工作流跑",
}

// ownLongOptions 取一个脚本**自己**接受的长选项：只看 `case "$1" in` 块里 `--xxx)` 形式的分支。
func ownLongOptions(shText string) []string {
	seen := map[string]bool{}
	var out []string
	for _, blk := range strings.Split(shText, `case "$1" in`)[1:] {
		body := blk
		if i := strings.Index(body, "esac"); i >= 0 {
			body = body[:i]
		}
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasSuffix(trimmed, ")") {
				continue
			}
			for _, m := range reLongOpt.FindAllString(trimmed, -1) {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// syntheticDeliveredRepo 造一个"已交付 n 个版本"的最小仓库（读数脚本从综述文件数派生）。
//
// 说明：v0.86 / v0.88 的判据各自内联了同一段夹具——这里是第三处，所以抽了出来。
// （那两处仍然内联，合并它们属于"顺手重构"，留给需要动它们的那一版。）
func syntheticDeliveredRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "test")
	for i := 1; i <= n; i++ {
		writeFileAt(t, dir, fmt.Sprintf("docs/development/v0.%d-retrospective.md", i), "# retro\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", fmt.Sprintf("%d retrospectives", n))
	return dir
}
