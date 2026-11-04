package runscript

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func ngmManifest(scripts string) string {
	return `{"schemaVersion":1,"name":"demo","version":"1.0.0","runtime":"node","dependencies":[]` + scripts + `}`
}

func TestLoadSourcesAndFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"demo","version":"1.2.3","scripts":{"build":"echo from-pkg"}}`)
	writeFile(t, dir, "ngm.json", ngmManifest(`,"scripts":{"test":"echo from-ngm"}`))

	set, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if set.PackageName != "demo" || set.PackageVersion != "1.2.3" {
		t.Errorf("package identity = %q@%q", set.PackageName, set.PackageVersion)
	}
	build, ok := set.Lookup("build")
	if !ok || build.Source != SourcePackageJSON || build.Command != "echo from-pkg" {
		t.Errorf("build = %+v (ok=%v)", build, ok)
	}
	test, ok := set.Lookup("test")
	if !ok || test.Source != SourceNgmJSON || test.Command != "echo from-ngm" {
		t.Errorf("test = %+v (ok=%v)", test, ok)
	}
}

func TestLoadRejectsDuplicateDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"demo","scripts":{"build":"echo pkg"}}`)
	writeFile(t, dir, "ngm.json", ngmManifest(`,"scripts":{"build":"echo ngm"}`))

	_, err := Load(dir)
	if err == nil {
		t.Fatal("want duplicate-source error")
	}
	msg := err.Error()
	for _, want := range []string{"build", "package.json", "ngm.json"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error must name %q: %s", want, msg)
		}
	}
}

func TestLoadNoScripts(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir); err == nil {
		t.Fatal("want an error when neither source defines scripts")
	}
}

func TestPrePostFollowNpmPrefixConvention(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json",
		`{"scripts":{"prebuild":"echo pre","build":"echo main","postbuild":"echo post"}}`)
	set, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	build, _ := set.Lookup("build")
	if build.Pre != "prebuild" || build.Post != "postbuild" {
		t.Errorf("build pre/post = %q/%q", build.Pre, build.Post)
	}
	// pre/post 本身也是普通脚本，可被直接运行（与 npm 一致）。
	if _, ok := set.Lookup("prebuild"); !ok {
		t.Error("prebuild should be listed as its own script")
	}
}

func TestBinDirsNearestFirst(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(filepath.Join(root, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sub, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := BinDirs(sub)
	if len(got) != 2 {
		t.Fatalf("BinDirs = %v, want 2 entries", got)
	}
	if !strings.HasPrefix(got[0], sub) || !strings.HasPrefix(got[1], root) {
		t.Errorf("nearest must come first: %v", got)
	}
}

func TestChildEnvIsTheDocumentedMinimum(t *testing.T) {
	bin := filepath.Join("x", "node_modules", ".bin")
	env := ChildEnv(ExecSpec{
		Command: "echo hi", Event: "build",
		PackageName: "demo", PackageVersion: "1.0.0",
		BinDirs: []string{bin},
	})
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"npm_lifecycle_event=build",
		"npm_lifecycle_script=echo hi",
		"npm_package_name=demo",
		"npm_package_version=1.0.0",
		"INIT_CWD=",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("child env missing %q", want)
		}
	}
	if strings.Contains(joined, "npm_execpath=") {
		t.Error("npm_execpath must not be set")
	}
	if strings.Contains(joined, "npm_config_") {
		t.Error("npm_config_* must not be promised (nor set)")
	}
	for _, line := range env {
		if strings.HasPrefix(line, "PATH=") && !strings.HasPrefix(strings.TrimPrefix(line, "PATH="), bin) {
			t.Errorf("PATH must start with node_modules/.bin: %q", line)
		}
	}
}

func TestAppendArgsVerbatim(t *testing.T) {
	if got := AppendArgs("echo hi", nil); got != "echo hi" {
		t.Errorf("no extra args must not change the command: %q", got)
	}
	if got := AppendArgs("echo hi", []string{"--flag", "a b"}); got != "echo hi --flag a b" {
		t.Errorf("AppendArgs = %q", got)
	}
}

func TestExecPassesThroughExitCode(t *testing.T) {
	tmp := t.TempDir()
	cmd := "exit 7"
	file := "run.sh"
	if runtime.GOOS == "windows" {
		cmd = "exit /b 7"
		file = "run.cmd"
	}
	writeFile(t, tmp, file, "ignored")
	code, err := Exec(context.Background(), ExecSpec{Dir: tmp, Command: cmd}, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7 passed through", code)
	}
}

func TestExecReportsInterruptAs130(t *testing.T) {
	tmp := t.TempDir()
	cmd := "kill -INT $$"
	if runtime.GOOS == "windows" {
		// Windows 无 POSIX 信号语义：用子进程自身产生的 130 验证“原样透传”。
		cmd = "exit /b 130"
	}
	code, err := Exec(context.Background(), ExecSpec{Dir: tmp, Command: cmd}, os.Stdout, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 130 {
		t.Errorf("SIGINT/130 mapping = %d, want 130", code)
	}
}
