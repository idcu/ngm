package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV12TypeDeclForwardsEngineDiagnostics 固定"引擎退出 0 也可能在说话"。
//
// `GenerateTypeDecl` 此前把整个进程结果丢掉（`_ = res`）：
// 产物是"目录里出现了哪些文件"，而引擎写在 stdout/stderr 上的提示
// （tsc 会跳过某些文件并说出来）一个字都到不了用户。
// 同族的另外三条路径都转发了（Bundle / Transform / Compile），Check 也转发，
// 只有这一条在吞——所以这条测试钉的是**通道**，不是格式。
func TestV12TypeDeclForwardsEngineDiagnostics(t *testing.T) {
	testutils.MustHaveGit(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	t.Setenv("FAKE_STDERR", "warning: skipped lib.dom.d.ts (no exports)")
	// 产物不是本用例的主题：只让引擎说话。
	t.Setenv("FAKE_EMIT_NOTHING", "1")

	cat := &Catalog{Version: CatalogVersion, Engines: []Entry{
		{Name: "fake", Kind: KindTypeDecl, Adapter: AdapterSubprocess, Command: `"` + fake + `"`},
	}}
	cat.normalize()
	r := NewRunner(cat, t.TempDir())

	res, err := r.GenerateTypeDecl(context.Background(), Selection{Primary: "fake"},
		"src/index.ts", TypeDeclOptions{OutDir: t.TempDir()})
	if err != nil {
		t.Fatalf("GenerateTypeDecl: %v", err)
	}

	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "skipped lib.dom.d.ts") {
		t.Errorf("the engine's diagnostics were dropped; its stderr must reach the user:\n%q", joined)
	}
}
