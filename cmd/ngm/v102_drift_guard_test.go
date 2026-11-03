package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.102：漂移守卫的**离线判定**。
//
// 联网那半在 CI 里跑（GITHUB_TOKEN + 真实 API）；本判据用 fixture 造出
// “附件齐全 / 缺一个 / SHA 行数少一行 / pin 指向的版本没有 release”，
// 验证的是判定逻辑本身，而不是网络。
//
// 判定规则（与脚本一致）：
//   最近 3 个正式 tag（排除已声明不回填的 v0.5~v0.8 / v0.65~v0.98）
//   的 release 必须存在，附件数 = 矩阵项 × 2 + SHA256SUMS，
//   SHA256SUMS 行数 = 矩阵项 × 2；
//   ngm-repo 的 .ngm-version 指向的 tag 也必须有同样的可下载产物。

type driftAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type driftRelease struct {
	Assets []driftAsset `json:"assets"`
}

func driftMatrix(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "release-matrix.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func driftNames(tag string, matrix []string) (all []string, binaries []string) {
	version := strings.TrimPrefix(tag, "v")
	for _, target := range matrix {
		osName, arch := strings.Split(target, "/")[0], strings.Split(target, "/")[1]
		legacy := "ngm-" + osName + "-" + arch
		versioned := "ngm-" + version + "-" + osName + "-" + arch
		if osName == "windows" {
			legacy += ".exe"
			versioned += ".exe"
		}
		binaries = append(binaries, legacy, versioned)
	}
	all = append(binaries, "SHA256SUMS")
	return all, binaries
}

// driftFixture 造 releases/<tag>.json、SHA256SUMS-<tag>、ngm-version。
func driftFixture(t *testing.T, tags []string, pin string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "releases"), 0o755); err != nil {
		t.Fatal(err)
	}
	matrix := driftMatrix(t)
	for _, tag := range tags {
		names, binaries := driftNames(tag, matrix)
		rel := driftRelease{}
		for _, name := range names {
			rel.Assets = append(rel.Assets, driftAsset{
				Name:               name,
				BrowserDownloadURL: "https://example.invalid/releases/download/" + tag + "/" + name,
			})
		}
		body, err := json.Marshal(rel)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "releases", tag+".json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		var sums strings.Builder
		for _, name := range binaries {
			fmt.Fprintf(&sums, "%064x  %s\n", 0, name)
		}
		if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS-"+tag), []byte(sums.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ngm-version"), []byte(pin+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func driftRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "test")
	// 用 v0.99/0.100/0.101：守卫显式排除 v0.5~v0.8 与 v0.65~v0.98 的
	// 声明不回填区间，因此候选必须是 v0.99 之后的正式 tag。
	for _, minor := range []string{"99", "100", "101"} {
		writeFileAt(t, dir, "docs/development/v0."+minor+"-retrospective.md", "# retro\n")
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-q", "-m", "v0."+minor)
		gitIn(t, dir, "tag", "-a", "v0."+minor+".0", "-m", "v0."+minor+".0")
	}
	return dir
}

func runDrift(t *testing.T, dir, fixtures string) (string, int) {
	t.Helper()
	bash := findBash(t)
	script := filepath.Join(repoRoot(t), "scripts", "check-release-status.sh")
	cmd := exec.Command(bash, filepath.ToSlash(script),
		"--drift-guard", "--fixtures", filepath.ToSlash(fixtures), "--repo", "idcu/ngm")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !asExitError(err, &ee) {
			t.Fatalf("run drift guard: %v\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestV102ReleaseDriftGuardPassesOnCompleteFixtures(t *testing.T) {
	dir := driftRepo(t)
	fixtures := driftFixture(t, []string{"v0.99.0", "v0.100.0", "v0.101.0"}, "0.100.0")
	out, code := runDrift(t, dir, fixtures)
	if code != 0 {
		t.Fatalf("complete fixtures should pass, got exit %d:\n%s", code, out)
	}
	for _, tag := range []string{"v0.99.0", "v0.100.0", "v0.101.0"} {
		if !strings.Contains(out, tag) {
			t.Errorf("report should mention %s:\n%s", tag, out)
		}
	}
	if !strings.Contains(out, "pin:0.100.0") {
		t.Errorf("report should mention the pinned version:\n%s", out)
	}
}

func TestV102ReleaseDriftGuardFailsOnMissingAsset(t *testing.T) {
	dir := driftRepo(t)
	fixtures := driftFixture(t, []string{"v0.99.0", "v0.100.0", "v0.101.0"}, "0.100.0")
	// 从 v0.3.0 的 JSON 里删掉一个附件。
	path := filepath.Join(fixtures, "releases", "v0.101.0.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rel driftRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		t.Fatal(err)
	}
	missing := "ngm-linux-amd64"
	var kept []driftAsset
	for _, a := range rel.Assets {
		if a.Name != missing {
			kept = append(kept, a)
		}
	}
	rel.Assets = kept
	body, _ = json.Marshal(rel)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runDrift(t, dir, fixtures)
	if code != 1 {
		t.Fatalf("missing asset must be a gate failure, got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "v0.101.0") || !strings.Contains(out, missing) {
		t.Errorf("failure must name the tag and the missing asset:\n%s", out)
	}
}

func TestV102ReleaseDriftGuardFailsOnShortChecksums(t *testing.T) {
	dir := driftRepo(t)
	fixtures := driftFixture(t, []string{"v0.99.0", "v0.100.0", "v0.101.0"}, "0.100.0")
	path := filepath.Join(fixtures, "SHA256SUMS-v0.100.0")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 12 {
		t.Fatalf("fixture should have 12 lines, got %d", len(lines))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines[:11], "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := runDrift(t, dir, fixtures)
	if code != 1 {
		t.Fatalf("short SHA256SUMS must be a gate failure, got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "SHA256SUMS 11 lines") {
		t.Errorf("failure must report the line count:\n%s", out)
	}
}

func TestV102ReleaseDriftGuardFailsWhenPinHasNoRelease(t *testing.T) {
	dir := driftRepo(t)
	fixtures := driftFixture(t, []string{"v0.99.0", "v0.100.0", "v0.101.0"}, "0.9.0")
	out, code := runDrift(t, dir, fixtures)
	if code != 1 {
		t.Fatalf("pin without a release must be a gate failure, got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "pin:0.9.0") || !strings.Contains(out, "MISSING release") {
		t.Errorf("failure must name the pinned version and the missing release:\n%s", out)
	}
}

func TestV102ReleaseDriftGuardRefusesUnknown(t *testing.T) {
	dir := driftRepo(t)
	fixtures := driftFixture(t, []string{"v0.99.0", "v0.100.0", "v0.101.0"}, "0.100.0")
	if err := os.Remove(filepath.Join(fixtures, "ngm-version")); err != nil {
		t.Fatal(err)
	}
	out, code := runDrift(t, dir, fixtures)
	if code != 3 {
		t.Fatalf("missing pin file must be exit 3 (not success, not 'missing'), got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "COULD NOT CHECK") {
		t.Errorf("unknown must say COULD NOT CHECK:\n%s", out)
	}
}
