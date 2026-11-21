package runid

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, dir, name string) string {
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

// TestPathForm 固定"路径形式"与"裸名字"的分界。
//
// 这条分界是整个修复的支点：默认档位（run:git 放行）只给裸名字，
// 因此这里把四类绕过写法逐条钉住。
func TestPathForm(t *testing.T) {
	cases := []struct {
		program string
		want    bool
	}{
		{"git", false},
		{"esbuild", false},
		{"engine.wasm", false},
		{"./git", true},
		{`.\git`, true},
		{"./git.exe", true},
		{"./git.cmd", true},
		{"../x/bin/tool", true},
		{"cmd/foo.exe", true},
		{"/usr/bin/git", true},
		{".", true},
		{"..", true},
		{"", false},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			struct {
				program string
				want    bool
			}{`C:\tools\git.exe`, true},
			struct {
				program string
				want    bool
			}{`C:git`, true},
		)
	}
	for _, tc := range cases {
		id := Identity{Program: tc.program}
		if got := id.PathForm(); got != tc.want {
			t.Errorf("PathForm(%q) = %v, want %v", tc.program, got, tc.want)
		}
	}
}

// TestLocalRel 固定"项目内"的判定：看解析后的实体，不看声明。
func TestLocalRel(t *testing.T) {
	proj := t.TempDir()
	inside := writeFile(t, proj, filepath.Join("tools", "git"))
	outside := writeFile(t, t.TempDir(), "git")

	id := Identity{Program: "./tools/git", Path: inside, ProjectDir: proj}
	rel, ok := id.LocalRel()
	if !ok || rel != "tools/git" {
		t.Fatalf("LocalRel(inside) = %q, %v; want tools/git, true", rel, ok)
	}
	if got := id.DisplayTarget(); got != "./tools/git" {
		t.Fatalf("DisplayTarget = %q, want ./tools/git", got)
	}

	out := Identity{Name: "git", Program: "../git", Path: outside, ProjectDir: proj}
	if _, ok := out.LocalRel(); ok {
		t.Fatalf("an executable outside the project must not be local")
	}
	if got := out.DisplayTarget(); got != "git" {
		t.Fatalf("DisplayTarget(outside) = %q, want the name git", got)
	}

	// 项目内的软链指向项目外：判定的应该是外面的实体，不是链接所在目录。
	link := filepath.Join(proj, "tools", "link-git")
	if err := os.Symlink(outside, link); err == nil {
		viaLink := Identity{Program: "./tools/link-git", Path: link, ProjectDir: proj}
		if _, ok := viaLink.LocalRel(); ok {
			t.Fatalf("a symlink escaping the project must not count as local")
		}
	}

	// 反向：项目内的软链指向项目内文件，仍然算项目内。
	target := writeFile(t, proj, filepath.Join("bin", "real"))
	inner := filepath.Join(proj, "tools", "inner-git")
	if err := os.Symlink(target, inner); err == nil {
		viaInner := Identity{Program: "./tools/inner-git", Path: inner, ProjectDir: proj}
		if _, ok := viaInner.LocalRel(); !ok {
			t.Fatalf("a symlink resolving inside the project must count as local")
		}
	}
}

// TestNormalize 固定路径授权的归一化规则：分隔符、Clean、Windows 大小写。
func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"./git", "git"},
		{"git", "git"},
		{"tools/git", "tools/git"},
		{`tools\git`, "tools/git"},
		{"tools/../git", "git"},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			struct{ in, want string }{`C:\Tools\GIT.EXE`, "c:/tools/git.exe"},
		)
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTargetsIncludesLocalPath 固定路径授权的匹配空间：项目内文件必须能被
// `./<rel>` 形式命中，而裸名字由 security 的名字授权通道单独处理。
func TestTargetsIncludesLocalPath(t *testing.T) {
	proj := t.TempDir()
	inside := writeFile(t, proj, "git")
	bare := Identity{Name: "git", Program: "git", Path: inside, ProjectDir: proj}
	var found bool
	for _, target := range bare.Targets() {
		if target == "git" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a local executable should expose ./git as a path target: %v", bare.Targets())
	}
}
