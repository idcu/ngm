package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/testutils"
)

// writeAllowRunConfig 把 `allow: ["run:<target>"]` 写进隔离 HOME 的全局配置。
func writeAllowRunConfig(t *testing.T, home string, targets ...string) {
	t.Helper()
	allow := make([]string, 0, len(targets))
	for _, target := range targets {
		allow = append(allow, "run:"+target)
	}
	body, err := json.MarshalIndent(map[string]any{
		"permissions": map[string]any{"allow": allow},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := config.EffectiveGlobalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// copyExecutable 把构建好的假引擎放到项目里，模拟"仓库自带一个可执行文件"。
func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func platformExe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// TestV128ProjectLocalEngineNeedsPathGrant 是 P0 的回归判据：
// 仓库自带的 `./git` 不能借 `run:git` 的默认放行通过权限门禁。
//
// 判定对象从"名字"改成"解析后的文件"，因此：
//   - 裸名字授权（含默认档位）不覆盖项目内文件；
//   - 只有精确到文件的路径授权才能放行。
func TestV128ProjectLocalEngineNeedsPathGrant(t *testing.T) {
	proj := m6Project(t, `{"bundle":"fake"}`)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	name := platformExe("git")
	localGit := filepath.Join(proj, name)
	copyExecutable(t, fake, localGit)
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"./` + name + `"`,
	})

	buildArgs := []string{"build", "src/index.ts", "--engine=fake", "--dir=" + proj}

	t.Run("denied by default even though the name is git", func(t *testing.T) {
		isolateUserEnv(t)
		marker := filepath.Join(t.TempDir(), "probed.txt")
		t.Setenv("FAKE_VERSION_MARKER", marker)

		var out, errb bytes.Buffer
		code := dispatch(buildArgs, &out, &errb)
		if code != 3 {
			t.Fatalf("build exit = %d, want 3 (denied)\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("the project-local git was executed")
		}
		human := out.String() + errb.String()
		for _, want := range []string{"run:./" + name, "resolves inside this project", "permissions.allow"} {
			if !strings.Contains(human, want) {
				t.Fatalf("denial should explain %q:\n%s", want, human)
			}
		}
	})

	t.Run("a name grant does not unlock it", func(t *testing.T) {
		// 先声明再隔离：WriteUserConfig 在隔离时按声明写入 allow。
		testutils.AllowEngines(t, "git")
		isolateUserEnv(t)
		marker := filepath.Join(t.TempDir(), "probed.txt")
		t.Setenv("FAKE_VERSION_MARKER", marker)

		var out, errb bytes.Buffer
		code := dispatch(buildArgs, &out, &errb)
		if code != 3 {
			t.Fatalf("a bare-name grant must not cover a project-local file, exit = %d\n%s",
				code, out.String()+errb.String())
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("the project-local git ran under a name grant")
		}
	})

	t.Run("an explicit path grant unlocks exactly that file", func(t *testing.T) {
		home := isolateUserEnv(t)
		writeAllowRunConfig(t, home, "./"+name)
		marker := filepath.Join(t.TempDir(), "probed.txt")
		t.Setenv("FAKE_VERSION_MARKER", marker)

		var out, errb bytes.Buffer
		if code := dispatch([]string{"engines", "list", "--json", "--dir=" + proj}, &out, &errb); code != 0 {
			t.Fatalf("engines list exit = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("the engine was not probed after the path grant: %v", err)
		}
		var rows []struct {
			Name      string `json:"name"`
			Available bool   `json:"available"`
		}
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, out.String())
		}
		var checked bool
		for _, row := range rows {
			if row.Name != "fake" {
				continue
			}
			checked = true
			if !row.Available {
				t.Fatalf("the granted engine should be available: %+v", row)
			}
		}
		if !checked {
			t.Fatalf("the fake engine is missing from the listing:\n%s", out.String())
		}
	})

	t.Run("engines list reports the path, not the bare name", func(t *testing.T) {
		isolateUserEnv(t)
		var out, errb bytes.Buffer
		if code := dispatch([]string{"engines", "list", "--json", "--dir=" + proj}, &out, &errb); code != 0 {
			t.Fatalf("engines list exit = %d\n%s", code, errb.String())
		}
		var rows []struct {
			ProbeDenied bool   `json:"probeDenied"`
			RunTarget   string `json:"runTarget"`
		}
		if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
			t.Fatalf("invalid json: %v\n%s", err, out.String())
		}
		var found bool
		for _, row := range rows {
			if row.RunTarget == "./"+name {
				found = true
				if !row.ProbeDenied {
					t.Fatalf("a project-local engine must be reported as denied")
				}
			}
		}
		if !found {
			t.Fatalf("runTarget should be ./%s, got:\n%s", name, out.String())
		}
	})
}

// TestV128TraversalPathIsNotDefaultAllowed 覆盖 `../x/bin/git`：
// 路径形式声明不能继承 `run:git` 的默认放行。
func TestV128TraversalPathIsNotDefaultAllowed(t *testing.T) {
	proj := m6Project(t, `{"bundle":"fake"}`)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	outside := filepath.Join(t.TempDir(), platformExe("git"))
	copyExecutable(t, fake, outside)
	rel, err := filepath.Rel(proj, outside)
	if err != nil {
		t.Fatal(err)
	}
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"` + rel + `"`,
	})

	isolateUserEnv(t)
	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	code := dispatch([]string{"build", "src/index.ts", "--engine=fake", "--dir=" + proj}, &out, &errb)
	if code != 3 {
		t.Fatalf("a traversal path must be denied, exit = %d\n%s", code, out.String()+errb.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("the traversal target was executed")
	}
	if human := out.String() + errb.String(); !strings.Contains(human, "explicit path") {
		t.Fatalf("denial should explain the path-form rule:\n%s", human)
	}
}

// TestV128SymlinkEscapeIsDenied 覆盖软链：项目内的链接指向项目外时，
// 判定看解析后的实体（项目外），但路径形式声明仍不享受默认档位。
func TestV128SymlinkEscapeIsDenied(t *testing.T) {
	proj := m6Project(t, `{"bundle":"fake"}`)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	outside := filepath.Join(t.TempDir(), platformExe("git"))
	copyExecutable(t, fake, outside)

	link := filepath.Join(proj, "tools", platformExe("git"))
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"./tools/` + platformExe("git") + `"`,
	})

	isolateUserEnv(t)
	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	code := dispatch([]string{"build", "src/index.ts", "--engine=fake", "--dir=" + proj}, &out, &errb)
	if code != 3 {
		t.Fatalf("a symlinked path must be denied, exit = %d\n%s", code, out.String()+errb.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("the symlink target was executed")
	}
}

// TestV128WindowsCmdVariant 覆盖 PATHEXT 变体：`./git.cmd` 与 `./git.exe` 一样
// 属于路径形式，不能借名字放行。只在 Windows 上有意义（Unix 不认 .cmd）。
func TestV128WindowsCmdVariant(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT variants are windows-only")
	}
	proj := m6Project(t, `{"bundle":"fake"}`)
	testutils.WriteFile(t, proj, "git.cmd", "@echo off\r\n")
	m6WriteCatalog(t, proj, m6CatalogEntry{
		Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"./git.cmd"`,
	})

	isolateUserEnv(t)
	marker := filepath.Join(t.TempDir(), "probed.txt")
	t.Setenv("FAKE_VERSION_MARKER", marker)

	var out, errb bytes.Buffer
	if code := dispatch([]string{"engines", "list", "--json", "--dir=" + proj}, &out, &errb); code != 0 {
		t.Fatalf("engines list exit = %d\n%s", code, errb.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("git.cmd was spawned")
	}
	var rows []struct {
		ProbeDenied bool   `json:"probeDenied"`
		RunTarget   string `json:"runTarget"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	for _, row := range rows {
		if row.RunTarget == "./git.cmd" && !row.ProbeDenied {
			t.Fatalf("git.cmd must be denied, got %+v", row)
		}
	}
}
