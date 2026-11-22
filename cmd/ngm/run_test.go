package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
)

func runWriteFile(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// grantRunScript 把 `run:script` 写进隔离全局配置——这是被测命令真实读取的那一处
// （经 EffectiveGlobalPath 换算，NGM_HOME 优先）。
func grantRunScript(t *testing.T, home string) {
	t.Helper()
	path := config.EffectiveGlobalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":{"allow":["run:script"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func platformCommand(win, posix string) string {
	if runtime.GOOS == "windows" {
		return win
	}
	return posix
}

func newRunProject(t *testing.T, scripts string) string {
	t.Helper()
	proj := t.TempDir()
	runWriteFile(t, proj, "package.json",
		`{"name":"demo","version":"1.2.3","scripts":`+scripts+`}`, 0o644)
	return proj
}

func TestRunExecutesScript(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"echo BUILD_OK"}`)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "BUILD_OK") {
		t.Errorf("script output missing:\n%s", out)
	}
}

func TestRunPassesArgsAfterDoubleDash(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"echo BUILD"}`)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj, "--", "--flag", "value")
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "BUILD --flag value") {
		t.Errorf("extra args were not appended verbatim:\n%s", out)
	}
}

func TestRunRejectsExtrasWithoutDoubleDash(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"echo BUILD"}`)

	code, out := runCaptureCode(t, "run", "build", "extra", "--dir="+proj)
	if code != 3 {
		t.Fatalf("exit=%d, want 3\n%s", code, out)
	}
	if !strings.Contains(out, "--") {
		t.Errorf("hint should mention `--`:\n%s", out)
	}
}

func TestRunPreFailureShortCircuits(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{`+
		`"prebuild":"`+platformCommand("exit /b 7", "exit 7")+`",`+
		`"build":"echo MAIN_SHOULD_NOT_RUN",`+
		`"postbuild":"echo POST_SHOULD_NOT_RUN"}`)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 7 {
		t.Fatalf("exit=%d, want the first failing step's code 7\n%s", code, out)
	}
	if strings.Contains(out, "MAIN_SHOULD_NOT_RUN") || strings.Contains(out, "POST_SHOULD_NOT_RUN") {
		t.Errorf("chain must stop at the first failure:\n%s", out)
	}
}

func TestRunIfPresent(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"echo BUILD"}`)

	code, out := runCaptureCode(t, "run", "missing", "--if-present", "--dir="+proj)
	if code != 0 {
		t.Fatalf("exit=%d, want 0 with --if-present\n%s", code, out)
	}
	if !strings.Contains(out, "not defined") {
		t.Errorf("--if-present should say the script was skipped:\n%s", out)
	}
}

func TestRunListJSON(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"prebuild":"echo pre","build":"echo main","postbuild":"echo post"}`)

	code, out := runCaptureCode(t, "run", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	var report runListReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output is not a JSON report: %v\n%s", err, out)
	}
	if report.Version != 1 || report.Project == "" {
		t.Errorf("report header = %+v", report)
	}
	var build runScriptRow
	for _, row := range report.Scripts {
		if row.Name == "build" {
			build = row
		}
	}
	if build.Command != "echo main" || build.Source != "package.json" ||
		build.Pre != "prebuild" || build.Post != "postbuild" {
		t.Errorf("build row = %+v", build)
	}
}

func TestRunMissingBinEmitsDiagnostic(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"definitely-not-a-real-binary-xyz"}`)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code == 0 {
		t.Fatalf("want non-zero when the command is missing, got 0\n%s", out)
	}
	if !strings.Contains(out, "node_modules/.bin") {
		t.Errorf("missing .bin diagnostic should be visible:\n%s", out)
	}
}

func TestRunInjectsNodeModulesBin(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"mytool"}`)

	if runtime.GOOS == "windows" {
		runWriteFile(t, filepath.Join(proj, "node_modules", ".bin"), "mytool.cmd",
			"@echo off\r\necho FROM_LOCAL_TOOL\r\n", 0o644)
	} else {
		runWriteFile(t, filepath.Join(proj, "node_modules", ".bin"), "mytool",
			"#!/bin/sh\necho FROM_LOCAL_TOOL\n", 0o755)
	}

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "FROM_LOCAL_TOOL") {
		t.Errorf("node_modules/.bin was not injected into PATH:\n%s", out)
	}
}

func TestRunRequiresRunScriptPermission(t *testing.T) {
	isolateUserEnv(t) // 故意不授予 run:script
	proj := newRunProject(t, `{"build":"echo BUILD"}`)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 3 {
		t.Fatalf("exit=%d, want 3 (permission denied)\n%s", code, out)
	}
	for _, want := range []string{"run:script", `{"permissions":{"allow":["run:script"]}}`, "postInstallPolicy"} {
		if !strings.Contains(out, want) {
			t.Errorf("permission hint must contain %q:\n%s", want, out)
		}
	}
}

// TestRunScriptDoesNotSeeDeniedEnv 是 `deny: ["env:X"]` 在脚本侧的端到端判据。
//
// 与 git / 引擎 / 沙箱共用同一个名单：变量父进程**有**、子进程**没有**。
// 用脚本自己打印出来判定，而不是读 ExecSpec——否则测的是"参数传对了"，
// 不是"策略生效了"。
func TestRunScriptDoesNotSeeDeniedEnv(t *testing.T) {
	home := isolateUserEnv(t)
	path := config.EffectiveGlobalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(
		`{"permissions":{"allow":["run:script"],"deny":["env:NGM_SCRIPT_SECRET"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NGM_SCRIPT_SECRET", "s3cr3t-value")
	t.Setenv("NGM_SCRIPT_PUBLIC", "public-value")

	// 正向对照与反向判据写在同一条脚本里：同一个子进程既要看见没被拒绝的变量，
	// 又看不见被拒绝的那个。少了正向对照，"什么都没传过去"也能让这条测试变绿。
	proj := newRunProject(t, `{"leak":`+strconv.Quote(platformCommand(
		"echo KEEP=%NGM_SCRIPT_PUBLIC% && echo VALUE=%NGM_SCRIPT_SECRET%",
		"echo KEEP=$NGM_SCRIPT_PUBLIC; echo VALUE=$NGM_SCRIPT_SECRET"))+`}`)

	code, out := runCaptureCode(t, "run", "leak", "--dir="+proj)
	if code != 0 {
		t.Fatalf("exit=%d\n%s", code, out)
	}
	if !strings.Contains(out, "KEEP=public-value") {
		t.Fatalf("the parent env was not inherited at all; the negative check below would be vacuous:\n%s", out)
	}
	if strings.Contains(out, "s3cr3t-value") {
		t.Fatalf("deny:[\"env:NGM_SCRIPT_SECRET\"] was not applied to the script child:\n%s", out)
	}
}

func TestRunRejectsDuplicateScriptSources(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"echo pkg"}`)
	runWriteFile(t, proj, "ngm.json",
		`{"schemaVersion":3,"name":"demo","version":"1.0.0","runtime":"node","dependencies":[],"scripts":{"build":"echo ngm"}}`,
		0o644)

	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 3 {
		t.Fatalf("exit=%d, want 3\n%s", code, out)
	}
	for _, want := range []string{"package.json", "ngm.json", "build"} {
		if !strings.Contains(out, want) {
			t.Errorf("conflict error must name %q:\n%s", want, out)
		}
	}
}

func TestRunPassesThroughExitCode130(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"build":"`+platformCommand("exit /b 130", "kill -INT $$")+`"}`)

	code, _ := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 130 {
		t.Errorf("exit=%d, want 130 (SIGINT/130 passthrough)", code)
	}
}

// TestRunDoubleDashShieldsHelpAndDirFromRoot 钉住 v0.136：分流层此前扫描
// `--` 之后的 token 找 --help/--version 与空 --dir=，于是
// `ngm run build -- --help` 打印了 run 的帮助（而不是把 --help 交给脚本），
// `ngm run build -- --dir=` 被误判成"空目录"而拒绝。runUsage 明确承诺
// `-- <extra args>` 原样交给脚本，这两条都是对契约的违背。
func TestRunDoubleDashShieldsHelpAndDirFromRoot(t *testing.T) {
	home := isolateUserEnv(t)
	grantRunScript(t, home)
	proj := newRunProject(t, `{"show":"echo ARGS"}`)

	for _, extra := range []string{"--help", "--version"} {
		code, out := runCaptureCode(t, "run", "show", "--dir="+proj, "--", extra, "tail")
		if code != 0 {
			t.Fatalf("run -- %s: exit=%d out=%s", extra, code, out)
		}
		if !strings.Contains(out, "ARGS "+extra+" tail") {
			t.Errorf("%s must reach the script verbatim:/n%s", extra, out)
		}
		if strings.Contains(out, "ngm run — run project scripts") {
			t.Errorf("root printed run's help instead of passing --%s through:/n%s", extra, out)
		}
	}

	// --dir= 出现在 `--` 之后是脚本参数，不是 ngm 的空目录错误
	code, out := runCaptureCode(t, "run", "show", "--dir="+proj, "--", "--dir=")
	if code != 0 || !strings.Contains(out, "ARGS --dir=") {
		t.Errorf("--dir= after -- must be a script argument: exit=%d out=%s", code, out)
	}
}
