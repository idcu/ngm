package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV19ConfigValidatesTheDirectoryItIsToldTo 固定 `ngm config` 的三条契约（v0.19 修）。
//
// 修复前的实测（探针输出，见 v0.19 复盘）：
//
//	config validate --dir=<有坏 ngm.json 的目录>   exit=0   ngm.json OK
//
// 三处缺陷叠在一起才产生这一行：
//
//	① `--dir` **不存在**——本命令是唯一只认"进程 CWD"的项目命令，
//	   于是那个 flag 变成多余位置参数；而 Go 的 flag 在第一个位置参数处停止解析。
//	② 多余位置参数**被静默丢弃**（只检查了 NArg()==0）。
//	③ 目标目录里**没有** ngm.json 时照样打印 `ngm.json OK`——CI 在错的目录里会拿到绿色。
//
// 这三条合起来是一次**假通过**，而假通过比报错更糟：它让错的人以为自己对。
func TestV19ConfigValidatesTheDirectoryItIsToldTo(t *testing.T) {
	isolateUserEnv(t)

	good := newProject(t) // 有合法的 ngm.json
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "ngm.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir() // 什么都没有

	// ① `--dir` 真的被使用：坏清单在**另一个**目录里，也必须被抓到。
	code, out := runCaptureCode(t, "config", "validate", "--dir="+bad)
	if code != 3 {
		t.Errorf("a malformed ngm.json in --dir must exit 3; got %d:\n%s", code, out)
	}
	if strings.Contains(out, "ngm.json OK") {
		t.Errorf("it must not print OK for a manifest that does not parse:\n%s", out)
	}

	// ② 目标目录没有 ngm.json → 3，且点名它看的是哪个目录。
	code, out = runCaptureCode(t, "config", "validate", "--dir="+empty)
	if code != 3 {
		t.Errorf("validate with nothing to validate must not pass; got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "no ngm.json") {
		t.Errorf("the failure must say the manifest is missing:\n%s", out)
	}

	// ③ 多余位置参数被拒绝，而不是被丢掉（否则 `--dir=x` 这类笔误会静默改行为）。
	code, out = runCaptureCode(t, "config", "validate", "--dir="+good, "extra-positional")
	if code != 3 {
		t.Errorf("a stray positional argument must be refused; got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ngm config") {
		t.Errorf("the refusal must print the command's usage:\n%s", out)
	}

	// 正常路径：把 --dir 指到一个合法项目上 → 0 + 可解析的那一行。
	code, out = runCaptureCode(t, "config", "validate", "--dir="+good)
	if code != 0 || !strings.Contains(out, "ngm.json OK") {
		t.Errorf("a valid project must pass; exit=%d:\n%s", code, out)
	}

	// 对照：`config show` 是**查看**，不设门禁——没有清单时照常打印，但**写明**它没有。
	code, out = runCaptureCode(t, "config", "show", "--dir="+empty)
	if code != 0 {
		t.Errorf("show inspects; it does not gate. exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, "no ngm.json in") {
		t.Errorf("show must say that the project level is absent:\n%s", out)
	}
}
