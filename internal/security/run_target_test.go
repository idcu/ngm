package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/runid"
)

func writeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCheckRunTarget 固定"身份判定"的核心规则。
//
// 背景：`run:git` 默认放行。修复前判定只看 filepath.Base(program)，
// 于是仓库自带的 `./git` 也能通过；这里逐条钉住修复后的边界。
func TestCheckRunTarget(t *testing.T) {
	proj := t.TempDir()
	localGit := writeExe(t, proj, "git")
	outsideGit := writeExe(t, t.TempDir(), "git")
	projSlash := filepath.ToSlash(proj)

	cases := []struct {
		name    string
		allow   []string
		deny    []string
		id      runid.Identity
		wantErr bool
		// wantKey / wantHint 只在 wantErr 时检查。
		wantKey  string
		wantHint string
	}{
		{
			name: "a bare git outside the project keeps the default allowance",
			id:   runid.Identity{Name: "git", Program: "git", Path: outsideGit, ProjectDir: proj},
		},
		{
			name:     "a project-local git is refused even though run:git is default-allowed",
			id:       runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
			wantErr:  true,
			wantKey:  "run:./git",
			wantHint: "resolves inside this project",
		},
		{
			name:    "a bare name that resolves inside the project is refused too",
			id:      runid.Identity{Name: "git", Program: "git", Path: localGit, ProjectDir: proj},
			wantErr: true,
			wantKey: "run:./git",
		},
		{
			name:    "a name grant does not unlock a project-local file",
			allow:   []string{"run:git"},
			id:      runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
			wantErr: true,
			wantKey: "run:./git",
		},
		{
			name:  "a relative path grant unlocks exactly that file",
			allow: []string{"run:./git"},
			id:    runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
		},
		{
			name:  "an absolute path grant also unlocks it",
			allow: []string{"run:" + projSlash + "/git"},
			id:    runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
		},
		{
			name:     "deny wins over an explicit path grant",
			allow:    []string{"run:./git"},
			deny:     []string{"run:./git"},
			id:       runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
			wantErr:  true,
			wantKey:  "run:./git",
			wantHint: "denied by `permissions.deny`",
		},
		{
			name:    "a name deny also refuses the local file",
			deny:    []string{"run:git"},
			id:      runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
			wantErr: true,
			wantKey: "run:git",
		},
		{
			name:     "a traversal path outside the project does not inherit run:git",
			id:       runid.Identity{Name: "git", Program: "../git", Path: outsideGit, ProjectDir: proj},
			wantErr:  true,
			wantKey:  "run:" + filepath.ToSlash(filepath.Join(proj, "..", "git")),
			wantHint: "explicit path",
		},
		{
			name:  "an explicit name grant still covers a path outside the project",
			allow: []string{"run:git"},
			id:    runid.Identity{Name: "git", Program: "../git", Path: outsideGit, ProjectDir: proj},
		},
		{
			name:    "a prefix-similar grant must not match",
			allow:   []string{"run:./git-helper"},
			id:      runid.Identity{Name: "git", Program: "./git", Path: localGit, ProjectDir: proj},
			wantErr: true,
			wantKey: "run:./git",
		},
		{
			name:    "a deny on the name still applies to a bare external git",
			deny:    []string{"run:git"},
			id:      runid.Identity{Name: "git", Program: "git", Path: outsideGit, ProjectDir: proj},
			wantErr: true,
			wantKey: "run:git",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pol := mustPolicy(t, tc.allow, tc.deny)
			err := pol.CheckRunTarget(tc.id)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("CheckRunTarget = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckRunTarget = nil, want a denial")
			}
			human := errs.FormatHuman(err)
			if tc.wantKey != "" && !strings.Contains(human, tc.wantKey) {
				t.Fatalf("denial should name %q:\n%s", tc.wantKey, human)
			}
			if tc.wantHint != "" && !strings.Contains(human, tc.wantHint) {
				t.Fatalf("denial should explain %q:\n%s", tc.wantHint, human)
			}
			if code := errs.ExitCode(err); code != int(errs.CodeConfigInvalid) {
				t.Fatalf("denial exit code = %d, want %d", code, int(errs.CodeConfigInvalid))
			}
		})
	}
}

// TestCheckRunTargetNilPolicy 固定"没有策略时按默认档位"的退化方向：
// git 仍可用，但项目内文件不会因此变成可执行。
func TestCheckRunTargetNilPolicy(t *testing.T) {
	proj := t.TempDir()
	localGit := writeExe(t, proj, "git")
	outsideGit := writeExe(t, t.TempDir(), "git")

	var pol *Policy
	if err := pol.CheckRunTarget(runid.Identity{
		Name: "git", Program: "git", Path: outsideGit, ProjectDir: proj,
	}); err != nil {
		t.Fatalf("a bare external git must still pass: %v", err)
	}
	if err := pol.CheckRunTarget(runid.Identity{
		Name: "git", Program: "./git", Path: localGit, ProjectDir: proj,
	}); err == nil {
		t.Fatalf("a project-local git must not pass without a policy")
	}
}

// TestCheckRunTargetWindowsCase 在 Windows 上固定大小写不敏感：
// 同一份路径的不同大小写必须命中同一条路径授权。
func TestCheckRunTargetWindowsCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only case folding")
	}
	proj := t.TempDir()
	localGit := writeExe(t, proj, "git")
	pol := mustPolicy(t, []string{"run:./GIT"}, nil)
	if err := pol.CheckRunTarget(runid.Identity{
		Name: "GIT", Program: "./git", Path: localGit, ProjectDir: proj,
	}); err != nil {
		t.Fatalf("case-insensitive path grant should match: %v", err)
	}
}
