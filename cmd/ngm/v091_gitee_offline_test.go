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

// v0.91：Gitee 侧读数**离线可测**——把最后一个"跑不了"的登记条目接进判据。
//
// `scripts/check-gitee-release-status.sh` 早就支持 `GITEE_API_BASE`（指向别的 API 基址），
// 也就是说它一直可以对着**假 API** 跑；只是没人这么做过（它一直在登记表里躺着，
// 理由写的是"没有离线模式"——那条理由**是错的**，是这条网写出来时才发现的）。
//
// 三种结论各要一条断言（这正是这个读数的全部价值）：
//
//	① 附件齐  ⇒ exit 0 并报 `OK (7 attachments …)`；
//	② 缺附件  ⇒ exit 1 并**点名**缺的是哪一个；
//	③ 请求失败 ⇒ exit 3 且写 `NOT CHECKED`（"查不动"既不算缺、也**不报成功**）。
func TestV91GiteeReadingRunsOffline(t *testing.T) {
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-gitee-release-status.sh")

	if !hasCommand(bash, "curl") || !hasCommand(bash, "python3") {
		t.Skip("读数的依赖（curl / python3）在这台机器上不全——它自己会 exit 3，这里就不重复断言了")
	}

	expected := []string{
		"ngm-darwin-amd64", "ngm-darwin-arm64",
		"ngm-linux-amd64", "ngm-linux-arm64",
		"ngm-windows-amd64.exe", "ngm-windows-arm64.exe",
		"SHA256SUMS",
	}

	// releaseJSON 造 Gitee 的响应形状：assets 里既有我们传的 7 个，也有它自动附的源码包。
	//
	// **`id` 必须有**：脚本的规则是"没有 id = 没有 release"（`d.get("id")` 为空 ⇒ MISSING）。
	// 第一版夹具没写 id，于是三条断言全错成 MISSING——这也是"假 API 要照着实现的判据造"的
	// 一个实例：不是随便一个 JSON 都算 release。
	releaseJSON := func(tag string, assets []string) string {
		var b strings.Builder
		fmt.Fprintf(&b, `{"id":4711,"tag_name":%q,"assets":[`, tag)
		for i, a := range assets {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"name":%q,"browser_download_url":"https://example.invalid/%s"}`, a, a)
		}
		b.WriteString(`]}`)
		return b.String()
	}
	withSource := append(append([]string{}, expected...), "v0.2.0.zip", "v0.2.0.tar.gz")

	// 服务器行为随 tag 变：v0.2.0 齐 · v0.3.0 缺 SHA256SUMS · v0.4.0 让请求失败。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v0.2.0"):
			fmt.Fprint(w, releaseJSON("v0.2.0", withSource))
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v0.3.0"):
			var incomplete []string
			for _, a := range expected {
				if a != "SHA256SUMS" {
					incomplete = append(incomplete, a)
				}
			}
			fmt.Fprint(w, releaseJSON("v0.3.0", append(incomplete, "v0.3.0.zip")))
		default:
			http.Error(w, "rate limited", http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	run := func(t *testing.T, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bash, append([]string{filepath.ToSlash(script)}, args...)...)
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GITEE_API_BASE="+srv.URL)
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("跑 %s 失败：%v\n%s", script, err, out)
			}
		}
		return string(out), code
	}

	t.Run("① 附件齐 ⇒ exit 0 且报 7 个附件", func(t *testing.T) {
		out, code := run(t, "v0.2.0")
		if code != 0 {
			t.Fatalf("附件齐时应当 exit 0，got %d：\n%s", code, out)
		}
		if !strings.Contains(out, "OK (7 attachments") {
			t.Errorf("输出里应当报 7 个附件（源码包单独计数）：\n%s", out)
		}
	})

	t.Run("② 缺附件 ⇒ exit 1 且点名缺的是哪一个", func(t *testing.T) {
		out, code := run(t, "v0.3.0")
		if code != 1 {
			t.Fatalf("缺附件时应当 exit 1（这是读数里的「有缺」），got %d：\n%s", code, out)
		}
		if !strings.Contains(out, "missing: SHA256SUMS") {
			t.Errorf("必须点名缺的附件：\n%s", out)
		}
	})

	t.Run("③ 请求失败 ⇒ exit 3 且 NOT CHECKED（查不动既不报缺也不报成功）", func(t *testing.T) {
		out, code := run(t, "v0.4.0")
		if code != 3 {
			t.Fatalf("查不动时应当 exit 3，got %d：\n%s", code, out)
		}
		if !strings.Contains(out, "NOT CHECKED") {
			t.Errorf("必须写明哪一条没查成：\n%s", out)
		}
	})

	t.Run("④ Windows 入口（.ps1）也能离线跑：同一份实现的同一个结论", func(t *testing.T) {
		ps := findPowerShell(t)
		if ps == "" {
			t.Skip("没有 powershell/pwsh")
		}
		cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-File",
			filepath.ToSlash(filepath.Join(repoRoot(t), "scripts", "check-gitee-release-status.ps1")),
			"-Tags", "v0.2.0")
		cmd.Dir = repoRoot(t)
		cmd.Env = append(os.Environ(), "GITEE_API_BASE="+srv.URL)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Windows 入口跑不动（%v）：\n%s", err, out)
		}
		if !strings.Contains(string(out), "OK (7 attachments") {
			t.Errorf("入口的输出应当与实现一致：\n%s", out)
		}
	})
}

// hasCommand 在 bash 里问一句某个命令在不在（跨平台，不猜文件名）。
func hasCommand(bash, name string) bool {
	out, err := exec.Command(bash, "-c", "command -v "+name).CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) != ""
}
