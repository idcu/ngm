package main

import (
	"bytes"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestHelpGolden 锁定 `ngm --help` 的输出文本。
//
// 当新增子命令、改名、调整 help 格式时，必须：
//  1. 同步 guides/cli.md
//  2. 同步 installation.md 中的 help 片段
//  3. 重生成 golden（`go test -update ./cmd/ngm`）并在 PR 说明
//
// 这是 development/README.md "文档-实现对照"纪律的机械化实现。
func TestHelpGolden(t *testing.T) {
	testutils.GoldenString(t, "help.golden", rootUsage)
}

// TestVersionGolden 锁定 `--version` 输出的非版本号部分（即 "ngm " 前缀）。
//
// 版本号本身每次构建都不同，不参与 golden。
func TestVersionGolden(t *testing.T) {
	out := &bytes.Buffer{}
	if code := dispatch([]string{"--version"}, out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("dispatch --version: %d", code)
	}
	got := out.String()
	// 只校验 "ngm " 前缀 + 末尾换行
	const prefix = "ngm "
	if len(got) < len(prefix)+1 {
		t.Fatalf("output too short: %q", got)
	}
	if got[:len(prefix)] != prefix {
		t.Errorf("missing prefix %q", prefix)
	}
	if got[len(got)-1] != '\n' {
		t.Errorf("missing trailing LF: %q", got)
	}
}
