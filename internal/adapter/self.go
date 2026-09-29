package adapter

import (
	"context"
	"fmt"
)

// selfEngine 是 `self` 兜底引擎（ADR-005：只做兜底、dry-run、离线 stub）。
//
// 它对任何能力调用都返回明确错误，**不产出任何产物**。
// 这一条是刻意的："静默降级"（例如把输入原样返回、或产出一个空文件）会让 CI
// 在完全没构建的情况下报成功，那是本引擎唯一必须防住的事。
//
// 它唯一被允许的用途是 `--dry-run`——而 dry-run 由 Runner 在调用引擎之前
// 就短路了（见 Runner.Plans），根本不会走到这里。
type selfEngine struct {
	entry Entry
}

func newSelfEngine(entry Entry) *selfEngine { return &selfEngine{entry: entry} }

// Name 实现 Engine。
func (e *selfEngine) Name() string { return e.entry.Name }

// Kind 实现 Engine。
func (e *selfEngine) Kind() EngineKind { return e.entry.Kind }

// Available 实现 Engine。恒为 true：它是进程内的 stub，永远"存在"——
// 但存在不等于能干活，具体能力方法会明确拒绝。
func (e *selfEngine) Available() bool { return true }

// Version 实现 Engine。
func (e *selfEngine) Version() (string, error) { return "stub", nil }

// stubError 构造统一的"stub 不能干活"错误。
//
// Code 为 -1（进程从未运行），因此映射到退出码 5（引擎不可用）：
// 语义上正确的描述是"没有可用的引擎来完成这件事"，而不是"引擎运行失败"。
func (e *selfEngine) stubError(verb string) *EngineError {
	return &EngineError{
		Code: -1,
		Message: fmt.Sprintf("the `%s` engine is a dry-run stub and cannot %s",
			SelfEngineName, verb),
		Fallback: true,
		Hint: "run with `--dry-run` to inspect the resolved command, " +
			"or select a real engine with `--engine=<name>`",
	}
}

// Transform 实现 TransformEngine：永远拒绝。
func (e *selfEngine) Transform(context.Context, []byte, TransformOptions) (*TransformResult, error) {
	return nil, e.stubError("transform")
}

// Bundle 实现 BundleEngine：永远拒绝。
func (e *selfEngine) Bundle(context.Context, string, BundleOptions) (*BundleResult, error) {
	return nil, e.stubError("bundle")
}

// Check 实现 TypeCheckEngine：永远拒绝。
func (e *selfEngine) Check(context.Context, string, TypeCheckOptions) (*TypeCheckResult, error) {
	return nil, e.stubError("type-check")
}

// GenerateTypeDecl 实现 TypeDeclEngine：永远拒绝。
func (e *selfEngine) GenerateTypeDecl(context.Context, string, TypeDeclOptions) (*TypeDeclResult, error) {
	return nil, e.stubError("emit type declarations")
}

// Compile 实现 CSSEngine：永远拒绝。
func (e *selfEngine) Compile(context.Context, []byte, CSSOptions) (*CSSResult, error) {
	return nil, e.stubError("compile CSS")
}
