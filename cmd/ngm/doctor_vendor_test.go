package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV117DoctorReportsVendorGitVisibility 钉住 `ngm doctor` 的 vendor 读数。
//
// 动机（方法论 §2.3 的"意图与状态错配"）：同一个 `ngm.vendor/`，随仓库分发与
// 被 `.gitignore` 排除都合法；只有"打算分发却漏提交"会让冷克隆的 CI 在
// `install --frozen-lockfile --offline` 处以 exit 4 失败，而那条错误指向
// "缺内容"，指不到"Git 忽略了整棵树"。因此 doctor 必须把三种状态**说出来**。
//
// 三条纪律同测：
//   - 读数三态都出现（absent / ignored-by-git / present-and-trackable）；
//   - 无论哪种状态，命令仍退 0（doctor 是诊断型命令，判定在报告里）；
//   - `--json` 里有一条名为 `vendor` 的检查项（机器消费方不需要解析人读文本）。
func TestV117DoctorReportsVendorGitVisibility(t *testing.T) {
	testutils.MustHaveGit(t)

	cases := []struct {
		name       string
		ignore     string // .gitignore 内容（空串表示不建）
		mkdir      bool   // 是否创建 ngm.vendor/
		wantStatus string
		wantDetail string
	}{
		{name: "absent", wantStatus: "warn", wantDetail: "absent at ./ngm.vendor"},
		{name: "ignored-by-git", ignore: "ngm.vendor/\n", mkdir: true, wantStatus: "warn", wantDetail: "ignored by Git"},
		{name: "trackable", mkdir: true, wantStatus: "ok", wantDetail: "Git does not ignore it"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserEnv(t)
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "ngm.json"),
				`{"schemaVersion":1,"name":"github.com:acme/app","version":"0.1.0","runtime":"node"}`)
			if tc.ignore != "" {
				writeFile(t, filepath.Join(dir, ".gitignore"), tc.ignore)
			}
			if tc.mkdir {
				if err := os.MkdirAll(filepath.Join(dir, "ngm.vendor", "github.com", "acme", "lib"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// check-ignore 需要真的工作树；没有它，读数退化成"Git could not answer"。
			gitInit(t, dir)

			var out, errb bytes.Buffer
			if code := dispatch([]string{"doctor", "--json", "--dir=" + dir}, &out, &errb); code != 0 {
				t.Fatalf("doctor must stay exit 0 (diagnostic), got %d\nstdout: %s\nstderr: %s",
					code, out.String(), errb.String())
			}

			var rep doctorReport
			if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
				t.Fatalf("doctor --json is not a single JSON document: %v\n%s", err, out.String())
			}
			found := false
			for _, c := range rep.Checks {
				if c.Name != "vendor" {
					continue
				}
				found = true
				if c.Status != tc.wantStatus {
					t.Errorf("vendor status = %q, want %q (detail: %s)", c.Status, tc.wantStatus, c.Detail)
				}
				if !strings.Contains(c.Detail, tc.wantDetail) {
					t.Errorf("vendor detail = %q, want it to contain %q", c.Detail, tc.wantDetail)
				}
			}
			if !found {
				t.Fatalf("no `vendor` check in the report — the readout is not wired:\n%s", out.String())
			}
		})
	}
}

// TestV117DoctorVendorReadoutIsReadOnly 钉住"只读"这条边界本身：
// 一次诊断不得创建/删除 vendor 目录，也不得往仓库里写任何东西。
func TestV117DoctorVendorReadoutIsReadOnly(t *testing.T) {
	testutils.MustHaveGit(t)
	isolateUserEnv(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ngm.json"),
		`{"schemaVersion":1,"name":"github.com:acme/app","version":"0.1.0","runtime":"node"}`)
	gitInit(t, dir)

	before := gitStatus(t, dir)
	var out, errb bytes.Buffer
	if code := dispatch([]string{"doctor", "--dir=" + dir}, &out, &errb); code != 0 {
		t.Fatalf("doctor exit = %d, want 0\n%s", code, errb.String())
	}
	if after := gitStatus(t, dir); after != before {
		t.Fatalf("doctor changed the work tree:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(dir, "ngm.vendor")); !os.IsNotExist(err) {
		t.Fatalf("doctor created ngm.vendor/ — a diagnostic must not materialize anything")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}
