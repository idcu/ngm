package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// v0.89：**交付脚本必须"被谁跑"说得清**——跑过的、或写明为什么跑不了。
//
// 起因是 v0.88 抓到的那两个 `.ps1`：它们是交付给 Windows 用户的入口，
// 却在仓库里躺了很久**根本不能被解析**（无 BOM ⇒ PS 5.1 按 ANSI 解码 ⇒ 换行被吞）。
// 它们为什么没人发现？因为**没有任何东西在跑它们**——Go 侧只有一个判据
// 读了它的文本（用来对齐 Find-Bash 顺序），"读过"与"跑过"在这里差了整整一个层次。
//
// 这条网把"没人跑"这件事变成**必须显式登记**：每个 `scripts/*.sh|*.ps1` 要么
// 能沿着"判据 / 工作流 / 其它脚本"的引用链走到某处，要么在下面的登记表里
// 写明为什么跑不了。新增一个脚本而两样都没有 ⇒ 红。
//
// **记账口径**：本网按**引用**记账（名字出现在判据、工作流或别的脚本里），
// 因此它是筛子而不是证明——一个名字被写进某段说明文字也算"被提到"。
// 它的价值在于让"从没被提到"这件事不可能悄悄发生；"真的跑过一次"由
// 各脚本自己的判据守着（`TestV8x…` 里那些真的 `exec.Command` 它们的地方）。
func TestV89ScriptsArtifactsAreEitherRunOrExplained(t *testing.T) {
	root := repoRoot(t)
	refs := append(
		readAllTexts(t, filepath.Join(root, "cmd"), ".go"),
		readAllTexts(t, filepath.Join(root, ".github", "workflows"), ".yml")...,
	)

	unexplained, stale := auditArtifacts(t, filepath.Join(root, "scripts"), refs, explainedNotAutomated)
	if len(stale) > 0 {
		t.Errorf("登记表里有已经不存在的文件（会让人以为还有人在跑它）：%v", stale)
	}
	if len(unexplained) > 0 {
		t.Errorf("这些交付脚本既跑不到、也没有登记原因：\n  %s\n"+
			"（要么把它接进某条判据/工作流，要么在 explainedNotAutomated 里写明为什么跑不了）",
			strings.Join(unexplained, "\n  "))
	}
}

// explainedNotAutomated：**不可能在自动化里跑**的交付物，逐条写明原因。
//
// 判断标准（v0.89 定）：能跑的就接进判据/工作流；不能跑的，原因必须是
// "缺环境/缺凭据"或"它是某个已被跑的实现的入口"这两类之一——
// "忘了接"不是原因，它正是这条网要抓的东西。
var explainedNotAutomated = map[string]string{
	// 需要凭据：CI 里没有 GITEE_TOKEN，只有人工回填历史发行版时才跑。
	"upload-gitee-assets.ps1": "需要 GITEE_TOKEN（只在人工回填时跑）；静态部分由 v0.88 的 ps1 解析网守着",
	// 四个 Windows 入口：它们**转调**的实现都被跑着（判据或 CI），
	// 而"在 Windows 上真跑一次 .ps1"目前是人工的——这是 v0.90 该接进判据的对象。
	"check-mirror-coverage.ps1":      "Windows 入口，转调 check-mirror-coverage.sh（那个 .sh 被判据与 CI 跑）；本文件由 ps1 解析网守着",
	"check-vendored-standard.ps1":    "Windows 入口，转调 check-vendored-standard.sh（那个 .sh 被判据跑）；本文件由 ps1 解析网守着",
	"write-readings-summary.ps1":     "Windows 入口，转调 write-readings-summary.sh（那个 .sh 被判据与 CI 跑）；本文件由 ps1 解析网守着",
	"check-gitee-release-status.ps1": "Windows 入口，转调 check-gitee-release-status.sh（那个 .sh 被 write-readings-summary.sh 跑，而后者被判据与 CI 跑）；本文件由 ps1 解析网守着",
}

// auditArtifacts 返回（既跑不到也没登记的文件，登记表里已不存在的键）。
//
// 覆盖 = 被 refs（判据/工作流）直接提到，**或在引用闭包里**：
// A 被某条判据跑、A 的正文里提到 B ⇒ B 也算覆盖（传递闭包，直到不动点）。
// 少了闭包，一个"只被另一个脚本调用"的脚本会被误判成没人跑。
func auditArtifacts(t *testing.T, scriptDir string, refs []string, explained map[string]string) (unexplained, stale []string) {
	t.Helper()

	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		t.Fatalf("读不出 %s：%v", scriptDir, err)
	}
	bodies := map[string]string{}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".ps1") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(scriptDir, name))
		if err != nil {
			t.Fatalf("读不出 %s：%v", name, err)
		}
		bodies[name] = string(b)
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("scripts/ 里一个交付脚本都没有 —— 这条网会静默地什么都不守")
	}
	for key := range explained {
		found := false
		for _, n := range names {
			if n == key {
				found = true
				break
			}
		}
		if !found {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	covered := map[string]bool{}
	for _, n := range names {
		for _, r := range refs {
			if strings.Contains(r, n) {
				covered[n] = true
				break
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, n := range names {
			if covered[n] {
				continue
			}
			for other, body := range bodies {
				if other == n || !covered[other] {
					continue
				}
				if strings.Contains(body, n) {
					covered[n] = true
					changed = true
					break
				}
			}
		}
	}

	for _, n := range names {
		if covered[n] {
			continue
		}
		if _, ok := explained[n]; ok {
			continue
		}
		unexplained = append(unexplained, n)
	}
	return unexplained, stale
}

// readAllTexts 读取一个目录下所有指定扩展名文件的文本（递归）。
func readAllTexts(t *testing.T, dir, ext string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ext) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, string(b))
		return nil
	})
	if err != nil {
		t.Fatalf("读 %s 下的 %s 失败：%v", dir, ext, err)
	}
	return out
}

// v0.89：`.sh` 与 `.ps1` 一样，**先要能被解析**（对交付物来说这是最低限度）。
// `bash -n` 只解析不执行，因此不需要任何夹具、也不会碰网络。
func TestV89ShellScriptsParse(t *testing.T) {
	bash := findBash(t)
	scripts, err := filepath.Glob(filepath.Join(repoRoot(t), "scripts", "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("scripts/ 下没有 .sh（err=%v）—— 路径变了？", err)
	}
	for _, s := range scripts {
		name := filepath.Base(s)
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(bash, "-n", filepath.ToSlash(s))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s 语法不过（bash -n）：%v\n%s", name, err, out)
			}
		})
	}
}

var reScriptRef = regexp.MustCompile(`scripts/([A-Za-z0-9._-]+\.(?:sh|ps1))`)

// v0.89：**工作流里点名的脚本必须真的在**。
//
// 这一条守的是一类很安静的腐烂：CI 里写 `bash scripts/xxx.sh`，而那个文件被改名或删掉——
// 直到下一次有人推 tag / 开 PR 时才发现（而本仓库的镜像最后一次推送是 v0.65.0，
// 也就是说这条路径可能几个月没人走）。
func TestV89WorkflowReferencesExistingScripts(t *testing.T) {
	root := repoRoot(t)
	workflows := readAllTexts(t, filepath.Join(root, ".github", "workflows"), ".yml")
	if len(workflows) == 0 {
		t.Fatal("没有读到任何工作流 —— 这条网会静默地什么都不守")
	}

	seen := map[string]bool{}
	var missing []string
	for _, w := range workflows {
		for _, m := range reScriptRef.FindAllStringSubmatch(w, -1) {
			if seen[m[1]] {
				continue
			}
			p := filepath.Join(root, "scripts", m[1])
			if _, err := os.Stat(p); err != nil {
				seen[m[1]] = true
				missing = append(missing, m[1])
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("工作流点名了不存在的脚本：%v（改过名？删过？）", missing)
	}
}
