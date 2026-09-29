# 引擎 adapter 模型

## 核心原则

**ngm core 不做自研引擎。所有引擎通过 adapter 调用外部工具。第三方永久优先。**

---

## Adapter 类型

| adapter | 调用方式 | 例子 | 适用场景 |
|---------|---------|------|---------|
| `embed` | Go 直接调用 | 未来自研 Go transformer | 兜底、dry-run、离线 stub |
| `subprocess` | spawn CLI | esbuild / tsc / deno / postcss | **默认方式** |
| `wasm` | wasm 运行时 | 某些 JS 引擎 wasm 构建 | 安全敏感场景 |
| `remote` | 网络调用 | 自托管构建服务 | 企业级共享构建 |

---

## 引擎接口（Go interface）

```go
// 所有引擎共享的基础接口
type Engine interface {
    Name() string
    Kind() EngineKind
    Version() (string, error)
    Available() bool
}

type EngineKind string

const (
    KindTransform EngineKind = "transform"
    KindBundle    EngineKind = "bundle"
    KindTypeCheck EngineKind = "typeCheck"
    KindTypeDecl  EngineKind = "typeDecl"
    KindCSS       EngineKind = "css"
)

// 各能力接口实现同一套风格
//
// 每个方法都接收 context.Context：引擎是子进程，必须能被 Ctrl+C 与超时取消——
// 一个挂住的 tsc 不该让 `ngm build` 无法中断。
type TransformEngine interface {
    Name() string
    Transform(ctx context.Context, input []byte, opts TransformOptions) (*TransformResult, error)
}

type BundleEngine interface {
    Name() string
    Bundle(ctx context.Context, entry string, opts BundleOptions) (*BundleResult, error)
}

type TypeCheckEngine interface {
    Name() string
    Check(ctx context.Context, entry string, opts TypeCheckOptions) (*TypeCheckResult, error)
}

type TypeDeclEngine interface {
    Name() string
    GenerateTypeDecl(ctx context.Context, entry string, opts TypeDeclOptions) (*TypeDeclResult, error)
}

type CSSEngine interface {
    Name() string
    Compile(ctx context.Context, input []byte, opts CSSOptions) (*CSSResult, error)
}
```

接口只覆盖"怎么调"，不覆盖"调什么"：**选项 → argv 的映射**属于 subprocess 协议，
唯一维护在 [P4 — 生态与协议](../modules/p4-ecosystem.md)。

---

## 引擎清单（JSON）

> **成熟度**：下面是**清单格式的示例**，不是 v0.1 的内置清单。
> v0.1 内置只有 **esbuild**（`bundle` + `transform`）与 `self`（仅用于 `--dry-run` / 离线 stub）；
> 示例中的 `typescript` / `deno` / `postcss` 属 `planned (v0.2)`。
> 它们**刻意不预置**——预置一个尚未适配的引擎会让 `ngm engines list` 说谎，
> 用户要到真正需要类型检查时才发现它不可用。

```json
{
  "version": 1,
  "engines": [
    {
      "name": "esbuild",
      "kind": "bundle",
      "adapter": "subprocess",
      "command": "esbuild",
      "version": "0.24.0",
      "supportedInput": [".ts", ".tsx", ".js", ".jsx"],
      "defaultOptions": {
        "format": "esm",
        "target": "es2020"
      }
    },
    {
      "name": "esbuild",
      "kind": "transform",
      "adapter": "subprocess",
      "command": "esbuild",
      "version": "0.24.0",
      "supportedInput": [".ts", ".tsx", ".js", ".jsx"],
      "defaultOptions": {
        "target": "es2020",
        "sourceMaps": true
      }
    },
    {
      "name": "typescript",
      "kind": "typeCheck",
      "adapter": "subprocess",
      "command": "tsc",
      "version": "5.6.0",
      "supportedInput": [".ts", ".tsx"],
      "defaultOptions": {}
    },
    {
      "name": "typescript",
      "kind": "typeDecl",
      "adapter": "subprocess",
      "command": "tsc --emitDeclarationOnly",
      "version": "5.6.0",
      "supportedInput": [".ts", ".tsx"],
      "defaultOptions": {}
    },
    {
      "name": "deno",
      "kind": "typeCheck",
      "adapter": "subprocess",
      "command": "deno check",
      "version": "2.0.0",
      "supportedInput": [".ts", ".tsx"],
      "defaultOptions": {}
    },
    {
      "name": "postcss",
      "kind": "css",
      "adapter": "subprocess",
      "command": "postcss",
      "version": "8.4.0",
      "supportedInput": [".css", ".scss"],
      "defaultOptions": {}
    }
  ]
}
```

**注意**：同一个引擎（esbuild）可以注册多个 kind（`transform` + `bundle`），条目按 `(name, kind)` 唯一。

**内置清单只收录已适配的引擎**；上例中的 `typescript` / `deno` / `postcss` 是 v0.2 的目标条目，
v0.1 的 `ngm engines validate` 不会因为清单格式里出现过它们就认为可用。

---

## subprocess 协议与自定义引擎

subprocess 协议的完整规范（输入/输出约定、错误映射、自定义引擎声明）唯一维护在 [P4 — 生态与协议](../modules/p4-ecosystem.md)。

---

## 运行时选择

`primary` / `fallbacks` 按顺序尝试、失败回退；配置写法唯一维护在 [配置详解](../guides/configuration.md)。

---

## 明确不做的

自研引擎与相关能力的归属清单，唯一维护在[能力矩阵](../internals/capability-matrix.md)。

`self` 引擎只做：兜底、dry-run、离线 stub。不做生产级实现。

---

## 诚实说明

1. **统一 interface ≠ 底层引擎统一**：接口抽象若只覆盖命令执行，得到的只是薄封装
2. **抽象过深有代价**：过度抽象可能破坏底层引擎的兼容性和表达能力
3. **subprocess 有启动开销**：高频调用场景（HMR）不适合反复 spawn，需用长驻 daemon 或 embed
4. **engine adapter 是配置与编排层**：若只是分别调用外部命令，ngm 的差异化不在引擎层

---

## 相关文档

- [运行时模型](./runtime-model.md)
- [ADR-005：为什么引擎统一接口动态选择](../adr/adr-005-engine-interface.md)
- [配置详解](../guides/configuration.md)
