package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func TestDispatch_Version(t *testing.T) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	if code := dispatch([]string{"--version"}, out, err); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
	if !strings.HasPrefix(out.String(), "ngm ") {
		t.Errorf("unexpected version line: %q", out.String())
	}
}

func TestDispatch_Help(t *testing.T) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	if code := dispatch([]string{"--help"}, out, err); code != 0 {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"init", "install", "verify", "config"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in help", want)
		}
	}
}

func TestDispatch_NoSubcommand(t *testing.T) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	if code := dispatch(nil, out, err); code != 3 {
		t.Errorf("code=%d", code)
	}
}

func TestDispatch_UnknownSubcommand(t *testing.T) {
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	if code := dispatch([]string{"nope"}, out, err); code != 3 {
		t.Errorf("code=%d", code)
	}
}

func TestDispatch_NotImplementedYet(t *testing.T) {
	// 这条纪律的内容随里程碑演进，但不变的是**清单必须与实现同步**：
	//
	//   M6 之后 build / typecheck / css / engines 已实现
	//   v0.2 audit、why / tree / outdated 已实现
	//   v0.3 integrations（最后一个）已实现 → 命令表里不再有占位项
	//
	// 所以现在该断言的不是"某个命令返回 exit 3"，而是**没有任何命令是占位的**：
	// 一旦有人再加一个 Run: nil 的条目，这里立刻红。
	//
	// 不再逐个 dispatch 每个命令：那会真的执行 init / add / cache 等有副作用的命令。
	for _, spec := range commands {
		if spec.Run == nil {
			t.Errorf("%s has no implementation: the command table must not contain placeholders", spec.Name)
		}
	}

	// 未实现的提示语不应残留在任何地方——它描述的是不存在的情况。
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	_ = dispatch([]string{"integrations"}, out, errBuf)
	if strings.Contains(errBuf.String(), "not implemented") {
		t.Errorf("integrations is implemented but still reports otherwise: %q", errBuf.String())
	}
}

func TestDispatch_Init(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	code := dispatch([]string{"init", "github.com:my-org/my-app", "--runtime=node", "--dir=" + dir}, out, err)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
	path := filepath.Join(dir, "ngm.json")
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if !strings.Contains(string(data), `"name": "github.com:my-org/my-app"`) {
		t.Errorf("name missing:\n%s", string(data))
	}
	if !strings.Contains(string(data), `"runtime": "node"`) {
		t.Errorf("runtime missing:\n%s", string(data))
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("file should end with LF")
	}
}

func TestDispatch_Init_RejectsExisting(t *testing.T) {
	dir := t.TempDir()
	// 预先写入
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	code := dispatch([]string{"init", "x", "--dir=" + dir}, out, err)
	if code != 3 {
		t.Errorf("code=%d stderr=%s", code, err.String())
	}
	if !strings.Contains(err.String(), "already exists") {
		t.Errorf("stderr=%q", err.String())
	}
}

func TestDispatch_ConfigValidate_OK(t *testing.T) {
	// config validate 会读取 ~/.ngm/config.json（三级合并的 global 层），
	// 必须隔离，否则开发者本地的全局配置会让测试结果依赖机器状态。
	testutils.IsolateUserEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(`{
		"schemaVersion": 1,
		"name": "github.com:a/b",
		"version": "0.1.0",
		"runtime": "node",
		"dependencies": []
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	code := dispatch([]string{"config", "validate"}, out, err)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
	if !strings.Contains(out.String(), "OK") {
		t.Errorf("out=%q", out.String())
	}
}

func TestDispatch_ConfigValidate_Bad(t *testing.T) {
	testutils.IsolateUserEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ngm.json"), []byte(`{"runtime":"bun"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, dir)
	out := &bytes.Buffer{}
	err := &bytes.Buffer{}
	code := dispatch([]string{"config", "validate"}, out, err)
	if code != 3 {
		t.Errorf("code=%d stderr=%s", code, err.String())
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestRootUsageContainsAllCommands(t *testing.T) {
	// help 字符串与 commands 表一致性是 hand-maintained 纪律——本测试守住它。
	want := []string{}
	for _, c := range commands {
		want = append(want, c.Name)
	}
	for _, w := range want {
		if !strings.Contains(rootUsage, w) {
			t.Errorf("help string missing command %q", w)
		}
	}
}
