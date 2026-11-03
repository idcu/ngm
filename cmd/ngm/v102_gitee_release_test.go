package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// v0.102：Gitee 上传脚本的**可离线验证**部分。
//
// 真实 Gitee 写操作需要 token，本机没有；但“发什么请求、按什么顺序、失败时
// 退出码是什么”可以用一个假 API 完整判定。判据覆盖：
//
//	① 不存在 release ⇒ POST 创建一次，再上传 7 个附件并逐个回读校验；
//	② 已存在 release ⇒ PATCH 更新而不是再建一份；已有附件跳过，缺的补上；
//	③ --dry-run ⇒ **零请求**，只打印计划；
//	④ 产物与 SHA256SUMS 不配对 / 缺 token ⇒ 用法错误 exit 2，不动远端。
//
// 这些判据用真实 multipart（curl -F）与真实回读，而不是只比对脚本输出。

type giteeFake struct {
	mu       sync.Mutex
	exists   bool
	requests int
	created  int
	patched  int
	deleted  int
	uploaded []string
	assetID  int
	byID     map[int]string
	contents map[string][]byte
	base     string
}

func newGiteeFake(t *testing.T) (*giteeFake, *httptest.Server) {
	t.Helper()
	f := &giteeFake{byID: map[int]string{}, contents: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	f.base = srv.URL
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *giteeFake) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++

	const prefix = "/repos/idcu/ngm/releases"
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, prefix+"/tags/"):
		if !f.exists {
			fmt.Fprint(w, "null")
			return
		}
		fmt.Fprint(w, f.releaseJSON())
	case r.Method == http.MethodPost && r.URL.Path == prefix:
		f.exists = true
		f.created++
		fmt.Fprint(w, `{"id":1}`)
	case r.Method == http.MethodPatch && r.URL.Path == prefix+"/1":
		f.patched++
		fmt.Fprint(w, `{"id":1}`)
	case r.Method == http.MethodPost && r.URL.Path == prefix+"/1/attach_files":
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file", http.StatusBadRequest)
			return
		}
		body, _ := io.ReadAll(file)
		_ = file.Close()
		name := header.Filename
		f.contents[name] = body
		f.assetID++
		f.byID[f.assetID] = name
		f.uploaded = append(f.uploaded, name)
		fmt.Fprintf(w, `{"id":%d,"name":%q,"browser_download_url":%q}`,
			f.assetID, name, f.base+"/assets/"+name)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/assets/"):
		name := strings.TrimPrefix(r.URL.Path, "/assets/")
		body, ok := f.contents[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/attach_files/"):
		idText := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var id int
		fmt.Sscanf(idText, "%d", &id)
		if name, ok := f.byID[id]; ok {
			delete(f.byID, id)
			delete(f.contents, name)
			f.deleted++
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *giteeFake) releaseJSON() string {
	names := make([]string, 0, len(f.byID))
	for id, name := range f.byID {
		names = append(names, fmt.Sprintf(`{"id":%d,"name":%q,"browser_download_url":%q}`,
			id, name, f.base+"/assets/"+name))
	}
	return `{"id":1,"assets":[` + strings.Join(names, ",") + `]}`
}

func (f *giteeFake) setExisting(name string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists = true
	f.assetID++
	f.byID[f.assetID] = name
	f.contents[name] = body
}

// giteeAssets 造 6 个平台二进制 + 6 个版本化别名 + SHA256SUMS。
func giteeAssets(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	targets := []string{
		"linux/amd64", "linux/arm64",
		"darwin/amd64", "darwin/arm64",
		"windows/amd64", "windows/arm64",
	}
	var sums strings.Builder
	for _, target := range targets {
		osName, arch := strings.Split(target, "/")[0], strings.Split(target, "/")[1]
		legacy := "ngm-" + osName + "-" + arch
		versioned := "ngm-0.1.0-" + osName + "-" + arch
		if osName == "windows" {
			legacy += ".exe"
			versioned += ".exe"
		}
		body := []byte("bytes for " + legacy)
		sum := sha256.Sum256(body)
		hexSum := hex.EncodeToString(sum[:])
		for _, name := range []string{legacy, versioned} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&sums, "%s  %s\n", hexSum, name)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func giteeEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GITEE_TOKEN=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func runGiteeRelease(t *testing.T, args ...string) (string, int) {
	t.Helper()
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "gitee-release.sh")
	cmd := exec.Command(bash, append([]string{filepath.ToSlash(script)}, args...)...)
	cmd.Env = giteeEnv("GITEE_TOKEN=test-token")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run gitee-release: %v\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestV102GiteeReleaseCreatesAndVerifies(t *testing.T) {
	f, srv := newGiteeFake(t)
	dir := giteeAssets(t)
	out, code := runGiteeRelease(t, "--tag", "v0.1.0", "--assets", dir,
		"--repo", "idcu/ngm", "--api-base", srv.URL)
	if code != 0 {
		t.Fatalf("new release should succeed, got exit %d:\n%s", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.exists || f.created != 1 {
		t.Errorf("expected exactly one create, exists=%v created=%d", f.exists, f.created)
	}
	if f.patched != 0 {
		t.Errorf("new release must not be patched, got %d", f.patched)
	}
	if len(f.uploaded) != 7 {
		t.Errorf("expected 7 uploads (6 binaries + SHA256SUMS), got %d: %v", len(f.uploaded), f.uploaded)
	}
}

func TestV102GiteeReleaseUpdatesExistingAndSkipsPresent(t *testing.T) {
	f, srv := newGiteeFake(t)
	dir := giteeAssets(t)
	// 预置一个附件：它应被跳过，其余 6 个补上；release 本身走 PATCH。
	f.setExisting("ngm-linux-amd64", []byte("bytes for ngm-linux-amd64"))
	out, code := runGiteeRelease(t, "--tag", "v0.1.0", "--assets", dir,
		"--repo", "idcu/ngm", "--api-base", srv.URL)
	if code != 0 {
		t.Fatalf("existing release should succeed, got exit %d:\n%s", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != 0 {
		t.Errorf("existing release must not be created again, got %d", f.created)
	}
	if f.patched != 1 {
		t.Errorf("existing release should be patched once, got %d", f.patched)
	}
	if len(f.uploaded) != 6 {
		t.Errorf("only the 6 missing assets should be uploaded, got %d: %v", len(f.uploaded), f.uploaded)
	}
	for _, name := range f.uploaded {
		if name == "ngm-linux-amd64" {
			t.Errorf("present asset should have been skipped, not re-uploaded")
		}
	}
}

func TestV102GiteeReleaseForceReplacesExisting(t *testing.T) {
	f, srv := newGiteeFake(t)
	dir := giteeAssets(t)
	f.setExisting("ngm-linux-amd64", []byte("stale bytes"))
	out, code := runGiteeRelease(t, "--tag", "v0.1.0", "--assets", dir,
		"--repo", "idcu/ngm", "--api-base", srv.URL, "--force")
	if code != 0 {
		t.Fatalf("--force should succeed, got exit %d:\n%s", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleted != 1 {
		t.Errorf("--force should delete the stale attachment first, got %d", f.deleted)
	}
	body := f.contents["ngm-linux-amd64"]
	if string(body) != "bytes for ngm-linux-amd64" {
		t.Errorf("--force should replace the stale bytes with the local ones, got %q", body)
	}
}

func TestV102GiteeReleaseDryRunMakesNoRequests(t *testing.T) {
	f, srv := newGiteeFake(t)
	dir := giteeAssets(t)
	out, code := runGiteeRelease(t, "--tag", "v0.1.0", "--assets", dir,
		"--repo", "idcu/ngm", "--api-base", srv.URL, "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run should exit 0, got %d:\n%s", code, out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests != 0 {
		t.Errorf("dry-run must not touch the network, got %d request(s)", f.requests)
	}
	for _, name := range []string{
		"ngm-linux-amd64", "ngm-linux-arm64", "ngm-darwin-amd64",
		"ngm-darwin-arm64", "ngm-windows-amd64.exe", "ngm-windows-arm64.exe", "SHA256SUMS",
	} {
		if !strings.Contains(out, name) {
			t.Errorf("dry-run plan should list %s:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "no request was sent") {
		t.Errorf("dry-run must say it sent nothing:\n%s", out)
	}
}

func TestV102GiteeReleaseRejectsBadInputs(t *testing.T) {
	bash := findBash(t)
	script := filepath.ToSlash(filepath.Join(repoRoot(t), "scripts", "gitee-release.sh"))
	dir := giteeAssets(t)

	runNoToken := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bash, append([]string{script}, args...)...)
		cmd.Env = giteeEnv() // 显式去掉 GITEE_TOKEN
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if !asExitError(err, &ee) {
				t.Fatalf("run: %v\n%s", err, out)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}

	out, code := runNoToken("--tag", "v0.1.0", "--assets", dir)
	if code != 2 {
		t.Fatalf("missing token must exit 2, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "GITEE_TOKEN") {
		t.Errorf("missing-token error must name the missing env var:\n%s", out)
	}

	if out, code := runNoToken("--tag", "v0.1.0"); code != 2 {
		t.Fatalf("missing --assets must exit 2, got %d:\n%s", code, out)
	}
	if out, code := runNoToken("--tag", "v0.1.0", "--assets", dir, "--bogus"); code != 2 {
		t.Fatalf("unknown flag must exit 2, got %d:\n%s", code, out)
	}

	// 清单里列了一个不存在的文件 ⇒ exit 2，且点名缺的是谁。
	// 用 --dry-run：dry-run 不需要 token，但照样做完整的产物校验。
	if err := os.Remove(filepath.Join(dir, "ngm-linux-amd64")); err != nil {
		t.Fatal(err)
	}
	out, code = runNoToken("--tag", "v0.1.0", "--assets", dir, "--dry-run")
	if code != 2 {
		t.Fatalf("missing listed asset must exit 2, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ngm-linux-amd64") {
		t.Errorf("missing-asset error must name the file:\n%s", out)
	}
}

func TestV102GiteeReleaseRejectsBadChecksumLine(t *testing.T) {
	dir := giteeAssets(t)
	path := filepath.Join(dir, "SHA256SUMS")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 12 {
		t.Fatalf("fixture should have 12 checksum lines, got %d", len(lines))
	}
	lines[0] = "not-a-sha256  ngm-linux-amd64"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runGiteeRelease(t, "--tag", "v0.1.0", "--assets", dir)
	if code != 2 {
		t.Fatalf("bad checksum line must exit 2, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "SHA256SUMS") {
		t.Errorf("checksum error should mention SHA256SUMS:\n%s", out)
	}
}
