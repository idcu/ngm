# 引擎 adapter 模型

## 核心原则

**ngm core 不做自研引擎。所有引擎通过 adapter 调用外部工具。第三方永久优先。**

---

## Adapter 类型

| adapter | 调用方式 | 例子 | 适用场景 |
|---------|---------|------|---------|
| `embed` | Go 直接调用 | 未来自研 Go transformer | 兜底、dry-run、离线 stub |
| `subprocess` | spawn CLI | esbuild / tsc / deno / postcss | **默认方式** |
| `wasm` | wazero 内执行 WASI 模块 | 以 WASI 命令模块形式发布的构建器 | 安全敏感场景（**已实现**，见 [ADR-011](../adr/adr-011-wasm-runtime.md)） |
| `remote` | 网络调用 | 自托管构建服务 | 企业级共享构建——**已由 [ADR-013](../adr/adr-013-remote-adapter.md) 排除**：源码要离开本机，而产物无法被用户本地证明 |

> **`wasm` 的边界**：只跑 **WASI preview1 命令模块**。期待 JS 宿主（`env.*` 一类导入）的
> wasm 构建**不在支持范围内**——那类模块需要一个 JS 运行时，属 `subprocess` + node 的范畴。
> 写清楚这条，比让用户拿一个跑不起来的模块去猜原因要好。

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

> **成熟度**：下面是**清单的格式示例**，已接近 v0.2 的内置清单。
>
> v0.2 E 组已适配 `typescript`（tsc）与 `postcss`，两者**进入内置清单**（见下）。
> `deno` 已适配但**刻意不进内置清单**：它的 bundle 是 Deno ≥ 2.4 的实验特性，
> 且一旦内置就会在 typeCheck 的默认顺序里排到 tsc 前面（条目按名字排序），
> 让多数 TS 项目意外用上 deno。需要它就在 `ngm.engines.json` 里显式声明——
> argv 翻译已经支持。
>
> 新增的内置条目一律 `optional`：**没装不算 issue**。"你没装 tsc"不该让
> `ngm engines validate` 对所有用户报 exit 5——不装 tsc 的人根本没打算类型检查。
> 真正用到却没装时，命令本身会在那一刻失败并给出安装提示。

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

**内置清单只收录已适配的引擎**——因此 v0.2 起 `typescript` 与 `postcss` 才进来（此前它们只是示例条目）。
`ngm engines validate` 不会因为清单里出现过就认为可用：它真的去 PATH 找可执行文件，
只是对 `optional` 条目"没找到"不报为 issue（见上）。

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
