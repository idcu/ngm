package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newProject 创建含 ngm.json 的临时项目目录并返回其路径。
func newProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out, err := runCapture(t, "init", "github.com:my-org/app", "--runtime=node", "--dir="+dir)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	return dir
}

// runCapture 运行 dispatch 并返回 (合并输出, error)。
func runCapture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := dispatch(args, stdout, stderr)
	if code != 0 {
		return stdout.String() + stderr.String(), &exitError{code: code, msg: stderr.String()}
	}
	return stdout.String(), nil
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

// runCaptureCode 返回退出码与合并输出（不把非零当错误）。
func runCaptureCode(t *testing.T, args ...string) (int, string) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := dispatch(args, stdout, stderr)
	return code, stdout.String() + stderr.String()
}

// readDeps 读回项目依赖列表。
func readDeps(t *testing.T, dir string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "ngm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pf struct {
		Dependencies []map[string]any `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("parse ngm.json: %v\n%s", err, data)
	}
	return pf.Dependencies
}

func TestAdd_TagDependency(t *testing.T) {
	dir := newProject(t)

	out, err := runCapture(t, "add", "github:my-org/utils@v1.2.3", "--ref-type=tag", "--dir="+dir)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if !strings.Contains(out, "added github:my-org/utils@v1.2.3 (tag)") {
		t.Errorf("unexpected output: %q", out)
	}

	deps := readDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("deps=%v", deps)
	}
	if deps[0]["name"] != "github:my-org/utils" {
		t.Errorf("name=%v", deps[0]["name"])
	}
	if deps[0]["ref"] != "v1.2.3" {
		t.Errorf("ref=%v", deps[0]["ref"])
	}
	if deps[0]["refType"] != "tag" {
		t.Errorf("refType=%v", deps[0]["refType"])
	}
}

func TestAdd_RequiresRefType(t *testing.T) {
	dir := newProject(t)

	code, out := runCaptureCode(t, "add", "github:my-org/utils@v1.2.3", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "--ref-type is required") {
		t.Errorf("missing required message: %s", out)
	}
	// Hint 应基于推断给出建议（v1.2.3 → tag）
	if !strings.Contains(out, "tag") {
		t.Errorf("hint should suggest tag for v1.2.3: %s", out)
	}
	if len(readDeps(t, dir)) != 0 {
		t.Errorf("ngm.json should be unchanged on failure")
	}
}

func TestAdd_InfersBranchHintForNonSemver(t *testing.T) {
	dir := newProject(t)
	code, out := runCaptureCode(t, "add", "github:my-org/logger@main", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "branch") {
		t.Errorf("hint should suggest branch: %s", out)
	}
}

func TestAdd_InfersCommitHintForHex(t *testing.T) {
	dir := newProject(t)
	code, out := runCaptureCode(t, "add", "github:my-org/legacy@abc1234", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "commit") {
		t.Errorf("hint should suggest commit: %s", out)
	}
}

func TestAdd_InvalidRefType(t *testing.T) {
	dir := newProject(t)
	code, out := runCaptureCode(t, "add", "github:o/r@main", "--ref-type=banana", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "invalid --ref-type") {
		t.Errorf("out=%s", out)
	}
}

func TestAdd_MissingRef(t *testing.T) {
	dir := newProject(t)
	code, out := runCaptureCode(t, "add", "github:my-org/utils", "--ref-type=tag", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "missing ref") {
		t.Errorf("out=%s", out)
	}
}

func TestAdd_MonorepoSubPath(t *testing.T) {
	dir := newProject(t)
	_, err := runCapture(t, "add", "github:my-org/monorepo#path=packages/core@v2.0.0", "--ref-type=tag", "--dir="+dir)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	deps := readDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("deps=%v", deps)
	}
	if deps[0]["path"] != "packages/core" {
		t.Errorf("path=%v", deps[0]["path"])
	}
}

func TestAdd_MonorepoSubPathViaFlag(t *testing.T) {
	dir := newProject(t)
	_, err := runCapture(t, "add", "github:my-org/monorepo@v2.0.0",
		"--ref-type=tag", "--path=packages/web", "--dir="+dir)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	deps := readDeps(t, dir)
	if deps[0]["path"] != "packages/web" {
		t.Errorf("path=%v", deps[0]["path"])
	}
}

func TestAdd_UpdatesExistingDependency(t *testing.T) {
	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@v1.0.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatal(err)
	}
	out, err := runCapture(t, "add", "github:my-org/utils@v2.0.0", "--ref-type=tag", "--dir="+dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("expected 'updated' in output: %q", out)
	}
	deps := readDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("should not duplicate: %v", deps)
	}
	if deps[0]["ref"] != "v2.0.0" {
		t.Errorf("ref=%v", deps[0]["ref"])
	}
}

// TestAdd_NormalizesEquivalentNames 验证 `github:x/y` 与 `github.com:x/y`
// 归一化后被视为同一依赖（不会产生重复条目）。
func TestAdd_NormalizesEquivalentNames(t *testing.T) {
	dir := newProject(t)
	if _, err := runCapture(t, "add", "github:my-org/utils@v1.0.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatal(err)
	}
	if _, err := runCapture(t, "add", "github.com:my-org/utils@v1.1.0", "--ref-type=tag", "--dir="+dir); err != nil {
		t.Fatal(err)
	}
	deps := readDeps(t, dir)
	if len(deps) != 1 {
		t.Fatalf("equivalent names should merge: %v", deps)
	}
	// 写回的 name 使用规范 slug 形式
	if deps[0]["name"] != "github:my-org/utils" {
		t.Errorf("name should be canonical slug: %v", deps[0]["name"])
	}
}

func TestAdd_DryRunDoesNotWrite(t *testing.T) {
	dir := newProject(t)
	before, _ := os.ReadFile(filepath.Join(dir, "ngm.json"))

	out, err := runCapture(t, "add", "github:my-org/utils@v1.2.3", "--ref-type=tag", "--dir="+dir, "--dry-run")
	if err != nil {
		t.Fatalf("add --dry-run: %v", err)
	}
	if !strings.Contains(out, `"name": "github:my-org/utils"`) {
		t.Errorf("dry-run should print resulting json: %s", out)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "ngm.json"))
	if !bytes.Equal(before, after) {
		t.Errorf("--dry-run must not modify ngm.json")
	}
}

func TestAdd_NoProjectFile(t *testing.T) {
	dir := t.TempDir() // 空目录，无 ngm.json
	code, out := runCaptureCode(t, "add", "github:o/r@v1", "--ref-type=tag", "--dir="+dir)
	if code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "ngm init") {
		t.Errorf("hint should point to `ngm init`: %s", out)
	}
}

func TestAdd_SortedDeterministicOutput(t *testing.T) {
	dir := newProject(t)
	for _, spec := range []string{
		"github:z-org/zeta@v1 --ref-type=tag",
		"github:a-org/alpha@v1 --ref-type=tag",
		"github:m-org/mid@v1 --ref-type=tag",
	} {
		args := append(strings.Fields(spec), "--dir="+dir)
		if _, err := runCapture(t, append([]string{"add"}, args...)...); err != nil {
			t.Fatalf("add %v: %v", args, err)
		}
	}
	deps := readDeps(t, dir)
	if len(deps) != 3 {
		t.Fatalf("deps=%v", deps)
	}
	// 按 name 字节序
	got := []string{deps[0]["name"].(string), deps[1]["name"].(string), deps[2]["name"].(string)}
	want := []string{"github:a-org/alpha", "github:m-org/mid", "github:z-org/zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: %s want %s (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestAdd_UsageOnNoArgs(t *testing.T) {
	code, out := runCaptureCode(t, "add")
	if code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "USAGE:") {
		t.Errorf("should print usage: %s", out)
	}
}
