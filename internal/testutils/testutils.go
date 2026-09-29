// Package testutils 提供 ngm 全项目的测试基建。
//
// 三大支柱（来自 development/README.md "测试策略"）：
//  1. Fixture 仓库：用 t.TempDir() 现场创建本地 Git 仓库；
//     禁止测试依赖公网。
//  2. Golden 文件：所有可复现输出（lock / digest / mappings / help）必须 golden 化；
//     测试支持 `-update` 重生成；变更必须在 PR 说明。
//  3. 表驱动：URL 归一化、退出码、schema 校验、漂移分类全部表驱动。
//
// 本包提供：Golden 文件比对助手、Git fixture 创建助手、表驱动用例 helper。
package testutils

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// UpdateGolden 是 -update 命令行 flag 的 hook。
//
// 用法：在 TestMain 中调用
//
//	flag.Parse()
//	if *updateGolden { ... }
//
// 或者直接调用 UpdateGoldenForTest(t) 把它绑到 test 标志。
//
// 默认 true（CI 安全）：仅当显式传入 -update=true 时更新。
// 这是为了避免开发者误用本地覆盖；正式 CI 上应不传此 flag。
var updateGolden = flag.Bool("update", false, "rewrite golden files for failing assertions")

// GoldenPath 返回仓库根目录下 testdata/<name> 的绝对路径。
//
// Go test 运行时，cwd 是包目录（不是仓库根）。本函数向上找到含 go.mod 的目录
// 作为锚点，把 golden 集中放到仓库根 testdata/。
//
// 环境变量 NGM_TESTDATA_DIR 可覆盖根目录——供 testutils 自身的自测使用，
// 避免把临时 golden 写进真实仓库的 testdata/。
//
// 约定：所有 golden 集中放在仓库根 testdata/ 下（与 fixtures/vectors 平级）。
func GoldenPath(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("NGM_TESTDATA_DIR")
	if root == "" {
		root = filepath.Join(moduleRoot(), "testdata")
	}
	return filepath.Join(root, name)
}

// IsolateUserEnv 把 ngm 的用户态环境指向一个临时目录，使被测代码与开发者的
// 真实环境完全隔离：
//
//	NGM_HOME    ngm 用户态根（mirror / content / cache）
//	HOME        类 Unix 的主目录（全局配置 ~/.ngm/config.json）
//	USERPROFILE Windows 的主目录
//
// 返回隔离后的"主目录"路径。
//
// 必要性：以下命令会读用户主目录，若不隔离，开发者本地存在
// `~/.ngm/config.json` 就会让测试结果依赖机器状态。
func IsolateUserEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("NGM_HOME", filepath.Join(home, "ngm-home"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// BuildHelperBinary 把仓库内的一个 main 包编译成临时可执行文件，返回其绝对路径。
//
// 用途：引擎 adapter 的协议测试需要一个**可控的假引擎**——不装真 esbuild、
// 不联网，且能模拟任意 argv / 退出码 / stderr / stdin。
//
// 用 Go main 包而不是 shell 脚本，是为了三平台一致：Windows 上没有 /bin/sh，
// .bat 的引号与退出码语义又与 POSIX 不同，用它做协议测试会把"平台差异"
// 混进"协议差异"里。
//
// pkgPath 是相对仓库根的包路径（如 ./internal/adapter/testdata/fakeengine）。
// 注意 Go 工具链会跳过 testdata 目录，因此 ./... 不会编译它，也不会被 vet 覆盖；
// 这里用显式路径构建。
func BuildHelperBinary(t *testing.T, pkgPath, name string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" {
		// Windows 上可执行文件必须有扩展名，否则 exec.LookPath 找不到它
		out += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", out, pkgPath)
	cmd.Dir = moduleRoot()
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkgPath, err, stderr.String())
	}
	return out
}

// WriteFile 在 dir 下写入文件（自动创建父目录，LF 结尾不做转换）。
//
// 供需要落盘 ngm.json / ngm.engines.json 的测试使用。
func WriteFile(t *testing.T, dir, rel, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// moduleRoot 找到包含 go.mod 的最近祖先目录。
//
// 实现：从调用者文件所在目录向上搜索，直至发现 go.mod；找不到就 panic（M0 测试基建
// 错误应当致命，避免后续读 / 写错位置）。
func moduleRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("testutils: runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("testutils: go.mod not found above " + file)
		}
		dir = parent
	}
}

// Golden 比对 got 字节与 golden 文件。
//
// 行为：
//   - 文件不存在：首次断言将生成并失败，提示开发者运行 -update。
//   - 内容一致：通过
//   - 内容不一致：报告差异（用字符串首部），提示开发者：
//     "re-run with -update to regenerate"（需在 PR 中说明变更）
//
// 注意：内部用 t.Errorf（不调用 Fatal/FailNow），便于子测试场景中调用本函数
// 不会因为 FailNow 终止父测试（Go testing 的 Fatalf 会调用 Goexit）。
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := GoldenPath(t, name)
	if *updateGolden {
		if err := writeGolden(path, got); err != nil {
			t.Errorf("update golden %s: %v", path, err)
			return
		}
		t.Logf("updated golden %s (%d bytes)", path, len(got))
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("golden %s not found; run with -update to create (this is the first run)\nGOT:\n%s", path, head(got, 64))
		return
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden mismatch for %s\n%s\n\nto update: re-run with -update=true\n--- want (first 64 lines):\n%s\n--- got:\n%s",
			path,
			diffLine(want, got),
			head(want, 64),
			head(got, 64),
		)
	}
}

// GoldenString 比对 got 字符串与 golden 文件。
func GoldenString(t *testing.T, name string, got string) {
	t.Helper()
	Golden(t, name, []byte(got))
}

func writeGolden(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func head(b []byte, lines int) string {
	all := strings.Split(string(b), "\n")
	if len(all) <= lines {
		return string(b)
	}
	return strings.Join(all[:lines], "\n") + fmt.Sprintf("\n... (%d more lines)", len(all)-lines)
}

func diffLine(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	max := len(w)
	if len(g) > max {
		max = len(g)
	}
	var sb strings.Builder
	for i := 0; i < max; i++ {
		var ws, gs string
		if i < len(w) {
			ws = w[i]
		}
		if i < len(g) {
			gs = g[i]
		}
		if ws != gs {
			fmt.Fprintf(&sb, "line %d:\n  want: %q\n  got:  %q\n", i+1, ws, gs)
			if sb.Len() > 4096 {
				sb.WriteString("... diff truncated\n")
				break
			}
		}
	}
	return sb.String()
}

// ---- Git fixture 基建 ----

// GitInit 在 dir 内初始化一个 git 仓库并完成一次空 commit。
//
// 返回值：
//   - dir: 仓库根
//   - headCommit: HEAD commit hash（用于断言）
//
// 默认参数：user.name / user.email；分支 main。
//
// 环境无关性（关键）：fixture 必须不随开发者的全局 git 配置漂移，因此这里
// 在**仓库级**显式关闭所有会影响字节或导致失败的全局行为：
//
//	commit.gpgsign false  用户全局开了签名时，空 commit 会因缺 key/口令而失败
//	tag.gpgsign    false  同上，annotated tag（M1 的核心用例）会失败
//	core.autocrlf  false  否则 LF 写入工作树后被转成 CRLF 入库，
//	                      破坏 M2 的"CRLF 原样参与哈希"纪律
//	core.filemode  false  在无 exec-bit 的文件系统（部分网络盘）上保持一致性
//	core.hooksPath <空>   屏蔽开发者全局配置的 hooks（如带 lint/校验的 pre-commit），
//	                      它们会让 fixture 的 commit 意外失败
//	gc.auto        0      禁止后台 gc 在测试中途搬动对象
//	advice.*       false  抑制提示噪音（保持 stderr 干净，便于错误断言）
//
// `git init --template=` 让新仓库不复制 init.templatedir 中的模板 hooks。
func GitInit(t *testing.T, dir string) (headCommit string) {
	t.Helper()
	// 空模板：不继承 init.templatedir 里的 hooks
	runGit(t, dir, "init", "-q", "-b", "main", "--template=")
	runGit(t, dir, "config", "user.name", "ngm test")
	runGit(t, dir, "config", "user.email", "ngm@test.local")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "config", "tag.gpgsign", "false")
	runGit(t, dir, "config", "core.autocrlf", "false")
	runGit(t, dir, "config", "core.safecrlf", "false")
	runGit(t, dir, "config", "gc.auto", "0")
	runGit(t, dir, "config", "advice.detachedHead", "false")

	// 屏蔽全局 hooks：把 hooksPath 指向仓库内一个空目录
	emptyHooks := filepath.Join(dir, ".git", "ngm-empty-hooks")
	if err := os.MkdirAll(emptyHooks, 0o755); err != nil {
		t.Fatalf("create empty hooks dir: %v", err)
	}
	runGit(t, dir, "config", "core.hooksPath", emptyHooks)

	// 空 commit：避免 requireZeroCommit 等额外配置
	runGit(t, dir, "commit", "--allow-empty", "-q", "-m", "initial")
	headCommit = strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	return headCommit
}

// GitWriteFile 在 dir/<relpath> 写入内容并 git add；不自动 commit。
//
// 返回值：写入的绝对路径，便于断言。
func GitWriteFile(t *testing.T, dir, relpath, content string) string {
	t.Helper()
	full := filepath.Join(dir, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	// 强制 LF 写入（Windows 默认 CRLF），保证三平台字节级一致
	body := strings.ReplaceAll(content, "\r\n", "\n")
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", relpath)
	return full
}

// GitCommit 在 dir 内提交所有已 add 的变更；返回 commit hash。
func GitCommit(t *testing.T, dir, msg string) string {
	t.Helper()
	runGit(t, dir, "commit", "-q", "-m", msg)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
}

// GitTag 在 dir 内创建 tag（annotate=true 时创建 annotated tag）。
//
// 返回值是被 tag 指向的 **commit**（对 annotated tag 做 `^{commit}` 解引用），
// 与 ngm 的语义一致——lock 记录的是 commit，不是 tag object。
func GitTag(t *testing.T, dir, name string, annotate bool) string {
	t.Helper()
	args := []string{"tag"}
	if annotate {
		args = append(args, "-a", "-m", name)
	}
	args = append(args, name)
	runGit(t, dir, args...)
	return strings.TrimSpace(runGit(t, dir, "rev-parse", name+"^{commit}"))
}

// GitTagObjectSHA 返回 annotated tag 的 tag object sha（用于断言"不是 commit"）。
func GitTagObjectSHA(t *testing.T, dir, name string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, dir, "rev-parse", "refs/tags/"+name))
}

// GitOutput 在 dir 内执行 `git <args...>` 并返回 stdout；失败即 t.Fatal。
//
// 供测试直接查询 git 状态（如 `rev-parse`、`cat-file`）。
func GitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return runGit(t, dir, args...)
}

// GitOutputErr 在 dir 内执行 `git <args...>`，返回 (stdout, error)，不 t.Fatal。
//
// 用于"允许失败"的场景，如 tag 已存在时的清理、探测性查询。
func GitOutputErr(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

// GitWriteFileRaw 写入**不做任何换行转换**的字节内容并 git add。
//
// 与 GitWriteFile 的区别：后者把 CRLF 归一为 LF（多数测试想要的行为）；
// 本函数保留原始字节，用于构造 CRLF 文件等需要精确字节的场景——
// M2 的 digest 向量依赖"CRLF 原样参与哈希"。
func GitWriteFileRaw(t *testing.T, dir, relpath string, content []byte) string {
	t.Helper()
	full := filepath.Join(dir, relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", relpath)
	return full
}

// GitHashObject 把内容写入对象库并返回 blob sha（`git hash-object -w --stdin`）。
//
// 用途：构造那些不便经文件系统产生的条目（symlink 的目标字符串等）。
func GitHashObject(t *testing.T, dir string, content []byte) string {
	t.Helper()
	return strings.TrimSpace(runGitStdin(t, dir, content, "hash-object", "-w", "--stdin"))
}

// GitAddSymlinkEntry 在索引中插入一个 symlink 条目（mode 120000）。
//
// 关键：**不依赖文件系统的符号链接能力**。Git 把 symlink 的目标路径字符串
// 存在 blob 里，因此可以用 `hash-object` 造出该 blob，再用
// `update-index --cacheinfo 120000,...` 写索引——在 Windows（无开发者模式）
// 上同样可用，使 M2 的 symlink 向量成为三平台一致的测试。
func GitAddSymlinkEntry(t *testing.T, dir, relpath, target string) {
	t.Helper()
	sha := GitHashObject(t, dir, []byte(target))
	runGit(t, dir, "update-index", "--add", "--cacheinfo", "120000,"+sha+","+relpath)
}

// runGitStdin 在 dir 内执行 git 并通过 stdin 提供数据，返回 stdout。
func runGitStdin(t *testing.T, dir string, stdin []byte, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s (stdin %d bytes) in %s failed: %v\nstderr: %s",
			strings.Join(args, " "), len(stdin), dir, err, stderr.String())
	}
	return stdout.String()
}

// runGit 在 dir 内执行 git <args...>，失败则 t.Fatal。
//
// 跨平台注意：避免把 stdout/stderr 直接拼到 t.Log——某些 git 提示走 stderr。
// 本工具统一走 cmd.Output()，并把 stderr 包裹到错误中。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s in %s failed: %v\nstderr: %s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return stdout.String()
}

// blockedGitEnvVars 是必须从 fixture 的 git 子进程环境中剔除的变量。
//
// 它们会**重定向 git 对仓库的定位**，从而让 `cmd.Dir` 失效：若开发者 shell 里
// 残留了 GIT_DIR（常见于钩子脚本、复杂 CI、`git worktree` 会话），fixture 会把
// 对象写进完全无关的仓库，表现为难以理解的随机失败。
//
// 变量名比较统一转大写（Windows 环境变量名不区分大小写）。
var blockedGitEnvVars = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_COMMON_DIR":                   true,
	"GIT_NAMESPACE":                    true,
	"GIT_PREFIX":                       true,
	"GIT_TEMPLATE_DIR":                 true, // 防止用户 init.templatedir 注入 hooks
	"GIT_CONFIG_COUNT":                 true, // 防止 `git -c` 的注入式环境配置
}

// testGitEnv 构造 fixture 专用环境：
//   - 剔除 blockedGitEnvVars（见上）
//   - 禁止交互提示（无 tty 时会挂起）
//   - 固定为 C locale（git 消息稳定为英文，便于断言）
func testGitEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, e := range os.Environ() {
		name := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			name = e[:i]
		}
		if blockedGitEnvVars[strings.ToUpper(name)] {
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"LANG=C",
	)
}

// MinGitMinor 是 fixture 需要的最低 git 版本（major=2 下的 minor 号）。
//
// 依据 development/README.md 的"开发环境"表：Git >= 2.30。
// 实际硬性下限更低——`git init -b <branch>`（本包依赖）需要 2.28——
// 这里取文档值，留出余量。
const MinGitMinor = 30

// MustHaveGit 确保系统存在满足版本要求的 git。
//
// 行为：
//   - git 不在 PATH → skip（环境不具备，跳过而非误报失败）
//   - git 版本低于 development/README.md 要求的 2.30 → **fail**，
//     并给出升级建议。刻意不 skip：静默跳过会让"验收通过"变成假象。
func MustHaveGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH; skipping fixture-based test")
	}
	ok, version := gitVersionAtLeast(t, 2, MinGitMinor)
	if !ok {
		t.Fatalf("git %s is too old; development/README.md requires git >= 2.%d "+
			"(upgrade git, then re-run)", version, MinGitMinor)
	}
}

// gitVersionAtLeast 解析 `git version` 输出并比较 (major, minor)。
//
// 输出形如 `git version 2.56.0.windows.1` 或 `git version 2.39.3 (Apple Git-145)`。
// 解析失败时返回 (true, raw)——宁可放行也不因解析差异误杀测试。
func gitVersionAtLeast(t *testing.T, wantMajor, wantMinor int) (bool, string) {
	t.Helper()
	out, err := GitOutputErr(".", "version")
	if err != nil {
		return true, "unknown"
	}
	raw := strings.TrimSpace(out)
	fields := strings.Fields(raw)
	for _, f := range fields {
		if f == "" || f[0] < '0' || f[0] > '9' {
			continue
		}
		parts := strings.SplitN(f, ".", 3)
		if len(parts) < 2 {
			continue
		}
		major, e1 := strconv.Atoi(parts[0])
		minorStr := parts[1]
		// 去掉可能的后缀（如 `39-beta1`）
		for i, r := range minorStr {
			if r < '0' || r > '9' {
				minorStr = minorStr[:i]
				break
			}
		}
		minor, e2 := strconv.Atoi(minorStr)
		if e1 != nil || e2 != nil {
			continue
		}
		if major != wantMajor {
			return major > wantMajor, raw
		}
		return minor >= wantMinor, raw
	}
	return true, raw
}

// SkipOnWindows 标记测试在 Windows 上跳过——给已知未处理的差异场景使用。
//
// 用法：
//
//	testutils.SkipOnWindows(t, "Windows hardlink semantics differ; covered in M4")
func SkipOnWindows(t *testing.T, reason string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip(reason)
	}
}

// SkipUnless 在条件不满足时跳过测试；用于按需启用的 fixture 套件。
func SkipUnless(t *testing.T, cond bool, reason string) {
	t.Helper()
	if !cond {
		t.Skip(reason)
	}
}
