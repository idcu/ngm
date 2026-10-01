package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/testutils"
)

// writeModule 把模块写到 dir 下的 name，返回对应的清单条目。
func writeModule(t *testing.T, dir, name string, bin []byte) Entry {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	return wasmEntry(name, KindBundle)
}

func wasmEntry(program string, kind EngineKind) Entry {
	return Entry{
		Name:    "testwasm",
		Kind:    kind,
		Adapter: AdapterWasm,
		Command: program,
		Program: program,
		Builtin: true,
	}
}

func newTestWasmEngine(t *testing.T, dir, name string, bin []byte) *wasmEngine {
	t.Helper()
	entry := writeModule(t, dir, name, bin)
	eng, err := NewEngine(entry, dir)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	we, ok := eng.(*wasmEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T for adapter=wasm", eng)
	}
	return we
}

// 最基本的一条：模块真的跑起来了，产物真的从 stdout 拿到了。
func TestWasmEngine_RunsModuleAndCapturesStdout(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "engine.wasm", testutils.WASIModuleStdout("bundle output\n"))

	if !eng.Available() {
		t.Fatal("the module exists, so Available must be true")
	}
	res, err := eng.Bundle(context.Background(), "src/index.ts", BundleOptions{})
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if got := string(res.Code); got != "bundle output\n" {
		t.Errorf("stdout = %q, want the module's output", got)
	}
}

// 非零退出必须是引擎失败，且**带上模块的退出码**——
// 那正是用户与 CI 判断"引擎为什么失败"的依据。
func TestWasmEngine_NonZeroExitIsAnEngineFailure(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "failing.wasm", testutils.WASIModuleExit(3))

	_, err := eng.Bundle(context.Background(), "src/index.ts", BundleOptions{})
	if err == nil {
		t.Fatal("a module exiting 3 must fail")
	}
	var ee *EngineError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *EngineError, got %T: %v", err, err)
	}
	if ee.Code != 3 {
		t.Errorf("exit code = %d, want 3 (the module's own code must survive)", ee.Code)
	}
	if !ee.Fallback {
		t.Error("a failing engine must be fallback-eligible, same as a failing subprocess")
	}
}

// 退出码 0 是成功，不是"wazero 报了个 ExitError 所以失败"。
func TestWasmEngine_ZeroExitIsSuccess(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "ok.wasm", testutils.WASIModuleExit(0))

	if _, err := eng.Bundle(context.Background(), "src/index.ts", BundleOptions{}); err != nil {
		t.Fatalf("a module exiting 0 must succeed: %v", err)
	}
}

// 内存上限**在实例化阶段**生效——这是 wasm adapter 相对 subprocess 的实质优势，
// 也是 ADR-011 决策 4 写明的行为。
func TestWasmEngine_EnforcesMemoryLimit(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "hungry.wasm", testutils.WASIModuleMemoryPages(wasmMemoryLimitPages+64))

	_, err := eng.Bundle(context.Background(), "src/index.ts", BundleOptions{})
	if err == nil {
		t.Fatal("a module exceeding the memory limit must be refused")
	}
	if !strings.Contains(err.Error(), "memory") {
		t.Errorf("the error should mention memory, not just 'trapped': %v", err)
	}

	// 对照：同样结构但不超限的模块能跑起来——否则这条测试可能只是
	// "所有模块都失败"，而那种情况下它什么都没证明。
	ok := newTestWasmEngine(t, dir, "ok.wasm", testutils.WASIModuleMemoryPages(1))
	if _, oerr := ok.Bundle(context.Background(), "src/index.ts", BundleOptions{}); oerr != nil {
		t.Fatalf("a small module must run (otherwise this test proves nothing): %v", oerr)
	}
}

// ADR-011 的核心承诺：**argv 语义与 subprocess 完全一致**。
//
// 与其让模块回显参数（那要给夹具写 argv 解析），不如直接断言两种 adapter
// 对同一份输入生成同一条 argv——前者只能证明"某一个参数传对了"，
// 后者证明的是"整套翻译没有分叉"。
func TestWasmEngine_ArgvMatchesSubprocess(t *testing.T) {
	wasm := wasmEntry("engine.wasm", KindBundle)
	sub := wasm
	sub.Adapter = AdapterSubprocess

	req := buildRequest{
		Options: BundleOptions{
			Outfile: "dist/out.js",
			Target:  "es2020",
			Minify:  true,
			Alias:   map[string]string{"github:org/repo": "./ngm.vendor/github.com/org/repo"},
		},
		EntryFile: "src/index.ts",
	}

	invWasm, err := buildInvocation(wasm, req)
	if err != nil {
		t.Fatal(err)
	}
	invSub, err := buildInvocation(sub, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(invWasm.Args, " ") != strings.Join(invSub.Args, " ") {
		t.Errorf("the two adapters must translate options identically:\n wasm: %v\n  sub: %v",
			invWasm.Args, invSub.Args)
	}
	if invWasm.ReadsStdin != invSub.ReadsStdin {
		t.Errorf("stdin convention differs: wasm=%v subprocess=%v", invWasm.ReadsStdin, invSub.ReadsStdin)
	}
}

// 缺少模块时 Available 为假——与"可执行文件不在 PATH"是同一类事实。
func TestWasmEngine_UnavailableWhenModuleMissing(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewEngine(wasmEntry("nope.wasm", KindBundle), dir)
	if err != nil {
		t.Fatal(err)
	}
	if eng.Available() {
		t.Error("a missing module must not be Available")
	}
	if _, verr := eng.Version(); verr == nil {
		t.Error("Version must fail when the module is missing")
	}
}

// 构造期就拒绝空 command（与 subprocess 同一条规则）。
func TestWasmEngine_EmptyCommandIsRejected(t *testing.T) {
	if _, err := NewEngine(wasmEntry("", KindBundle), t.TempDir()); err == nil {
		t.Error("an empty command must be rejected at construction")
	}
}

// `--outfile` 由 **ngm** 代写：模块没有写权限（ADR-011 决策 3），
// 而用户给了 outfile 就期待文件出现。
func TestWasmEngine_WritesArtifactOnBehalfOfTheModule(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "engine.wasm", testutils.WASIModuleStdout("artifact\n"))

	res, err := eng.Bundle(context.Background(), "src/index.ts", BundleOptions{Outfile: "dist/out.js"})
	if err != nil {
		t.Fatalf("Bundle: %v", err)
	}
	if res.Outfile != "dist/out.js" {
		t.Errorf("Outfile = %q", res.Outfile)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "dist", "out.js"))
	if rerr != nil {
		t.Fatalf("the artifact was not written: %v", rerr)
	}
	if string(data) != "artifact\n" {
		t.Errorf("artifact = %q", data)
	}
}

// typeDecl 在 wasm 形态下明确拒绝：多文件输出需要写权限，而沙箱不给。
//
// 静默返回空列表会让调用方以为"没有声明要生成"——那正是本项目最反对的
// 一类失败（看起来成功了）。
func TestWasmEngine_TypeDeclIsRefusedWithAReason(t *testing.T) {
	dir := t.TempDir()
	eng := newTestWasmEngine(t, dir, "engine.wasm", testutils.WASIModuleStdout("x\n"))

	_, err := eng.GenerateTypeDecl(context.Background(), "src/index.ts", TypeDeclOptions{})
	if err == nil {
		t.Fatal("typeDecl must be refused, not silently empty")
	}
	// 用户看到的是 message + hint，所以"指向 subprocess"那句话必须在**渲染后**的输出里。
	if !strings.Contains(errs.FormatHuman(err), "subprocess") {
		t.Errorf("the rendered error should point at the workable alternative:\n%s", errs.FormatHuman(err))
	}
}
