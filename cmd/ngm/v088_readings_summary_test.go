package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.88：**读数要放在会被看见的地方**——CI 的 job summary（`$GITHUB_STEP_SUMMARY`）。
//
// 仓库里不缺读数（v0.86 把它们换成了"随时可再测的命令"），缺的是**有人看**：
// 埋在流水线日志里的读数，和没有差不多。`scripts/write-readings-summary.sh`
// 把两个源的读数写成一段 markdown，CI 一次调用就贴到摘要页上。
//
// 两条牙（都是本项目反复吃的亏）：
//
//	① **读不出必须说出来**：某个源没读成，摘要里必须有 ⚠️ 那一段——静默少一块
//	   等于让人以为"没问题"；
//	② `--out` 是**追加**：`$GITHUB_STEP_SUMMARY` 本来就是追加写的地方，
//	   截断会把同一个 job 里别的步骤写的内容抹掉。
func TestV88ReadingsSummaryIsWritten(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "write-readings-summary.sh")

	// 合成仓库：已交付版本数由综述文件数派生（与既有两条读法同源）。
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "test")
	for i := 1; i <= 3; i++ {
		writeFileAt(t, dir, fmt.Sprintf("docs/development/v0.%d-retrospective.md", i), "# retro\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "3 retrospectives")

	// 假 API：2 个 tag + 1 个 release（已交付 3 ⇒ 未推送 1）。
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

	run := func(apiBase, out string) (string, int) {
		cmd := exec.Command(bash, filepath.ToSlash(script),
			"--repo", "idcu/ngm", "--api-base", apiBase, "--out", out)
		cmd.Dir = dir
		combined, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("running %s: %v\n%s", script, err, combined)
			}
		}
		return string(combined), code
	}

	t.Run("两个源都在摘要里，且带现场读数", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "summary.md")
		if err := os.WriteFile(outFile, []byte("> 前一个步骤写的内容\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		out, code := run(srv.URL, outFile)
		if code != 0 {
			t.Fatalf("读数 ≠ 门禁：本脚本应当永远 exit 0（除非用法错），got %d\n%s", code, out)
		}
		body, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("摘要没写成文件：%v（stdout=%s）", err, out)
		}
		got := string(body)

		for _, want := range []string{
			"## ngm 读数摘要",
			"### GitHub 镜像",
			"### Gitee",
			"2 tag(s)",           // 假 API 实测
			"UNPUSHED: 1 tag(s)", // 3 − 2
			"> 前一个步骤写的内容",        // 追加，不是截断
		} {
			if !strings.Contains(got, want) {
				t.Errorf("摘要里缺 %q：\n%s", want, got)
			}
		}
		if strings.Contains(got, "这一段**没读成**") && !strings.Contains(got, "### Gitee") {
			t.Errorf("⚠️ 出现却没有对应的段落：\n%s", got)
		}
	})

	t.Run("读不出必须说出来（假 API 地址没人听）", func(t *testing.T) {
		outFile := filepath.Join(t.TempDir(), "summary.md")
		out, code := run("http://127.0.0.1:1", outFile)
		if code != 0 {
			t.Fatalf("镜像读不成也不该让本脚本非 0（那是读数不是门禁），got %d\n%s", code, out)
		}
		body, err := os.ReadFile(outFile)
		if err != nil {
			t.Fatalf("摘要没写成文件：%v", err)
		}
		got := string(body)
		if !strings.Contains(got, "这一段**没读成**") {
			t.Errorf("镜像没读成时摘要必须显式写\"没读成\"（静默少一块正是要防的）：\n%s", got)
		}
		if !strings.Contains(got, "### GitHub 镜像") {
			t.Errorf("^即使读不成，那一段也必须存在（写明读不成，而不是消失）：\n%s", got)
		}
	})

	t.Run("用法错 exit 2（只有与读数无关的错误才非 0）", func(t *testing.T) {
		cmd := exec.Command(bash, filepath.ToSlash(script), "--nope")
		cmd.Dir = dir
		combined, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			}
		}
		if code != 2 {
			t.Errorf("未知参数应当 exit 2，got %d：\n%s", code, combined)
		}
		if !strings.Contains(string(combined), "usage:") {
			t.Errorf("用法错要打印 usage：\n%s", combined)
		}
	})
}
