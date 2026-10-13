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

// v0.86：镜像侧覆盖是**按需实测的读数**，不是写死的历史数字。
//
// 仓库里那几处"GitHub 有 65 个 tag / 60 个 release"是 2026-10-10 问过 API 的快照；
// 快照会过期，而它过期的方式很安静（"未推送数"那处就漏过两次）。
// `scripts/check-mirror-coverage.sh` 让同一件事随时可再测：两次请求，
// 把"镜像真实覆盖"与"未推送 = 已交付 − 镜像 tag"算出来。
//
// 它是**读数不是门禁**（v0.85 那条教训的第二处应用）：推送是仓库所有者的动作，
// 缺口不判失败；只有"问不到"才退出 3。
//
// 判据用一个**本地假 API**（httptest）喂它，因此不碰网络：
//
//	① 65 个 tag + 60 个 release ⇒ 打印 UNPUSHED（= 已交付 − 65）且 exit 0；
//	② 假 API 回 500 ⇒ exit 3（"不下结论"）；
//	③ 指向一个没人听的地址 ⇒ 同样 exit 3（连不上 ≠ 覆盖率 0）。

func TestV86MirrorCoverageIsAMeasuredReading(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-mirror-coverage.sh")

	// 合成仓库：已交付版本数由综述文件数派生（与既有的两条读法同源）。
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "test")
	for i := 1; i <= 70; i++ {
		writeFileAt(t, dir, fmt.Sprintf("docs/development/v0.%d-retrospective.md", i), "# retro\n")
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "70 retrospectives")

	run := func(apiBase string) (string, int) {
		cmd := exec.Command(bash, filepath.ToSlash(script), "--repo", "idcu/ngm", "--api-base", apiBase)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("running %s: %v\n%s", script, err, out)
			}
		}
		return string(out), code
	}

	// ① 假 API：65 个 tag（refs）+ 60 个 release（最新 v0.64.0）
	tags := strings.Builder{}
	tags.WriteString("[")
	for i := 0; i < 65; i++ {
		if i > 0 {
			tags.WriteString(",")
		}
		fmt.Fprintf(&tags, `{"ref":"refs/tags/v0.%d.0","object":{"sha":"deadbeef"}}`, i+1)
	}
	tags.WriteString("]")
	rels := strings.Builder{}
	rels.WriteString("[")
	for i := 0; i < 60; i++ {
		if i > 0 {
			rels.WriteString(",")
		}
		fmt.Fprintf(&rels, `{"tag_name":"v0.%d.0"}`, 64-i)
	}
	rels.WriteString("]")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/git/refs/tags"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, tags.String())
		case strings.Contains(r.URL.Path, "/releases"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, rels.String())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, code := run(srv.URL)
	if code != 0 {
		t.Fatalf("读到了就该 exit 0（缺口不判失败），got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "65 tag(s)") || !strings.Contains(out, "60 release(s)") {
		t.Errorf("应当报出镜像的 tag / release 数：\n%s", out)
	}
	if !strings.Contains(out, "v0.64.0") {
		t.Errorf("应当报出最新 release：\n%s", out)
	}
	// 70 份复盘 − 65 个镜像 tag = 5
	if !strings.Contains(out, "UNPUSHED: 5") {
		t.Errorf("未推送数应等于 已交付 − 镜像 tag（70 − 65 = 5）：\n%s", out)
	}

	// ② 假 API 回 500 ⇒ 不下结论
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	out, code = run(bad.URL)
	if code != 3 {
		t.Fatalf("问不到必须 exit 3（不能报成功），got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "refusing to report success") {
		t.Errorf("exit 3 时要把话说出来：\n%s", out)
	}

	// ③ 连不上（没人听的地址）⇒ 同样 exit 3
	out, code = run("http://127.0.0.1:1")
	if code != 3 {
		t.Fatalf("连不上必须 exit 3，got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "could not read the mirror") {
		t.Errorf("连不上时的说明要在：\n%s", out)
	}
	_ = os.Getenv
}
