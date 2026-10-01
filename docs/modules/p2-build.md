# P2 — 构建引擎

> engine adapter：第三方优先，自研不做生产级实现

---

## 职责

P2 提供 engine adapter 机制，让 ngm 通过统一接口调用外部构建工具（esbuild / tsc / deno / postcss）。

**ngm 不做自研构建引擎。**

以下内容的唯一维护位置（本页不再复制）：

| 内容 | 唯一维护位置 |
|------|-------------|
| 接口签名、adapter 类型、引擎清单 schema | [引擎 adapter 模型](../architecture/engine-adapter.md) |
| subprocess 协议规范、自定义引擎 | [P4 — 生态与协议](./p4-ecosystem.md) |
| 运行时选择配置（primary / fallbacks） | [配置详解](../guides/configuration.md) |
| 不做的能力 → 交给谁 | [能力矩阵](../internals/capability-matrix.md) |

---

## 模块结构

```
internal/adapter/
├── interface.go         # 统一 interface
├── registry.go          # 引擎注册表
├── subprocess.go        # subprocess adapter
├── embed.go             # embed adapter（仅兜底）
├── wasm.go              # wasm adapter（安全场景）
├── remote.go            # remote adapter（企业级）
└── engines/
    ├── esbuild.go       # esbuild adapter
    ├── typescript.go    # tsc adapter
    ├── deno.go          # deno adapter
    └── postcss.go       # postcss adapter
```

（完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## subprocess adapter 实现

```go
type SubprocessEngine struct {
    Name    string
    Command string
    Args    []string
    Env     []string
}

func (e *SubprocessEngine) run(input []byte, args ...string) ([]byte, error) {
    cmd := exec.Command(e.Command, append(e.Args, args...)...)
    cmd.Stdin = bytes.NewReader(input)
    cmd.Env = append(os.Environ(), e.Env...)
    return cmd.Output()
}
```

协议约定（输入/输出约定、错误映射）见 [P4 — 生态与协议](./p4-ecosystem.md)。

---

## 内置引擎适配器

### esbuild

```go
type EsbuildEngine struct {
    Binary string
}

func (e *EsbuildEngine) Bundle(entry string, opts BundleOptions) (*BundleResult, error) {
    args := []string{
        entry,
        "--bundle",
        "--format=" + opts.Format,
        "--target=" + opts.Target,
        "--outfile=" + opts.Outfile,
    }
    if opts.Sourcemap {
        args = append(args, "--sourcemap")
    }
    return e.run(nil, args...)
}

func (e *EsbuildEngine) Version() (string, error) {
    out, err := exec.Command(e.Binary, "--version").Output()
    // esbuild 0.24.0
    return strings.TrimSpace(string(out)), err
}
```

### TypeScript (tsc)

```go
type TypeScriptEngine struct {
    Binary string
}

func (e *TypeScriptEngine) Check(entry string, opts TypeCheckOptions) (*TypeCheckResult, error) {
    args := []string{
        "--noEmit",
        "--target", opts.Target,
        "--module", opts.Module,
        entry,
    }
    out, err := exec.Command(e.Binary, args...).CombinedOutput()
    return &TypeCheckResult{
        Output: string(out),
        Passed: err == nil,
    }, nil
}

func (e *TypeScriptEngine) GenerateTypeDecl(entry string, opts TypeDeclOptions) (*TypeDeclResult, error) {
    args := []string{
        "--emitDeclarationOnly",
        "--declaration",
        "--outDir", opts.OutDir,
        entry,
    }
    _, err := exec.Command(e.Binary, args...).Output()
    return &TypeDeclResult{}, err
}
```

### Deno

```go
type DenoEngine struct {
    Binary string
}

// 注意：deno bundle 需要 Deno 2.4+（实验特性，底层为 esbuild）；Deno 2.0–2.3 不可用
func (e *DenoEngine) Bundle(entry string, opts BundleOptions) (*BundleResult, error) {
    args := []string{
        "bundle",
        entry,
        opts.Outfile,
    }
    return e.run(nil, args...)
}

func (e *DenoEngine) Check(entry string, opts TypeCheckOptions) (*TypeCheckResult, error) {
    args := []string{"check", entry}
    out, err := exec.Command(e.Binary, args...).CombinedOutput()
    return &TypeCheckResult{
        Output: string(out),
        Passed: err == nil,
    }, nil
}
```

### postcss

```go
type PostCSSEngine struct {
    Binary string
}

func (e *PostCSSEngine) Compile(input []byte, opts CSSOptions) (*CSSResult, error) {
    args := []string{
        "--use", opts.Plugins,
        "--output", opts.Outfile,
    }
    out, err := e.run(input, args...)
    return &CSSResult{Output: out}, err
}
```

---

## 引擎注册表

```go
type Registry struct {
    engines map[EngineKind]map[string]Engine
}

func NewRegistry() *Registry {
    r := &Registry{
        engines: make(map[EngineKind]map[string]Engine),
    }

    // 注册内置引擎
    r.Register(KindBundle, "esbuild", &EsbuildEngine{Binary: "esbuild"})
    r.Register(KindTransform, "esbuild", &EsbuildEngine{Binary: "esbuild"})
    r.Register(KindTypeCheck, "typescript", &TypeScriptEngine{Binary: "tsc"})
    r.Register(KindTypeDecl, "typescript", &TypeScriptEngine{Binary: "tsc"})
    r.Register(KindTypeCheck, "deno", &DenoEngine{Binary: "deno"})
    r.Register(KindBundle, "deno", &DenoEngine{Binary: "deno"})
    r.Register(KindCSS, "postcss", &PostCSSEngine{Binary: "postcss"})

    // self 引擎（兜底，仅占位）
    // r.Register(KindBundle, "self", &SelfBundleEngine{})

    return r
}
```

---

## 运行时选择

```go
func (m *EngineManager) Transform(input []byte, opts TransformOptions) (*TransformResult, error) {
    primary := m.config.Engines.Transform.Primary
    engine, ok := m.registry.Get(KindTransform, primary)
    if !ok {
        return nil, fmt.Errorf("engine %q not found", primary)
    }

    result, err := engine.Transform(input, opts)
    if err != nil && len(m.config.Engines.Transform.Fallbacks) > 0 {
        for _, name := range m.config.Engines.Transform.Fallbacks {
            if fallback, ok := m.registry.Get(KindTransform, name); ok {
                return fallback.Transform(input, opts)
            }
        }
    }
    return result, err
}
```

---

## 成熟度

| 子模块 | 成熟度 | 备注 |
|--------|--------|------|
| 统一 interface | done (v0.1) | 5 种引擎类型 |
| subprocess adapter | done (v0.1) | 默认方式 |
| esbuild adapter | done (v0.1) | `bundle` + `transform` |
| typescript adapter | planned (v0.2) | `typeCheck` + `typeDecl` |
| deno adapter | planned (v0.2) | `typeCheck`；`bundle` 需 Deno ≥ 2.4 |
| postcss adapter | planned (v0.2) | `css` |
| wasm adapter | **done (v0.3)** | wazero + WASI 命令模块；argv / 产物 / 退出码与 subprocess 同一套语义（[ADR-011](../adr/adr-011-wasm-runtime.md)） |
| remote adapter | planned (v0.3) | 企业级 |
| self 引擎 | done (v0.1) | 仅 `--dry-run` / 离线 stub，不做生产级 |

> 成熟度口径与唯一事实源[能力矩阵](../internals/capability-matrix.md)一致。

---

## 相关文档

- [P0 — ngm core](./p0-core.md)
- [P1 — 依赖管理](./p1-dependency.md)
- [P3 — 供应链](./p3-supplychain.md)
- [P4 — 生态与协议](./p4-ecosystem.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [ADR-005：为什么引擎统一接口动态选择](../adr/adr-005-engine-interface.md)