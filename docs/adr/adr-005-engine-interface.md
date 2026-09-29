# ADR-005：为什么引擎统一接口动态选择

- **状态**：已定（v2 修订：第三方永久优先，自研不做生产级）
- **日期**：2026-09-29（v2 修订：2026-09-29）
- **范围**：构建/类型/CSS 引擎架构

> **结论**：所有引擎（transform / bundle / typeCheck / typeDecl / css）实现同一个 Go interface，
> 通过 adapter 动态选择；**第三方永久优先**，自研引擎不做生产级实现。
> **代价**：能力**受限于外部工具**——例如 esbuild 不做类型检查，ngm 的选择是明确拒绝（`exit 5`），
> 而不是自己补一个"看起来像"的检查；用户需自行安装引擎；接口必须跟随外部工具的版本演进。

---

## 背景

v1 方案曾规划"第三方优先 + 自研 fallback"，并预留 self 引擎接口。v2 竞品分析发现：

- esbuild 已覆盖 transform + bundle + tree-shaking，自研无优势
- tsc 的 type checker 经过十年打磨，自研追赶无意义
- Vite 的 HMR 已是行业标杆，自研 LSP/Dev Server 不具竞争力
- Bun 已实现 install/build/test/docs/HMR 一体化

**结论：ngm core 不做自研引擎。所有引擎通过 adapter 调用外部工具。**

---

## 决策

**所有引擎（transform / bundle / typeCheck / typeDecl / css）实现同一 Go interface，通过 adapter 动态选择。**

**第三方永久优先，自研引擎不做生产级实现。**

---

## 引擎接口（Go interface）

```go
// 所有引擎实现同一套接口
type TransformEngine interface {
    Name() string
    Transform(input []byte, opts TransformOptions) (*TransformResult, error)
}

type BundleEngine interface {
    Name() string
    Bundle(entry string, opts BundleOptions) (*BundleResult, error)
}

type TypeCheckEngine interface {
    Name() string
    Check(entry string, opts TypeCheckOptions) (*TypeCheckResult, error)
}

type TypeDeclEngine interface {
    Name() string
    GenerateTypeDecl(entry string, opts TypeDeclOptions) (*TypeDeclResult, error)
}

type CSSEngine interface {
    Name() string
    Compile(input []byte, opts CSSOptions) (*CSSResult, error)
}
```

---

## Adapter 类型

| adapter | 例子 | ngm 角色 | 适用场景 |
|---------|------|---------|---------|
| `embed` | 未来自研 Go transformer | 直接调用 | 兜底、dry-run、离线 stub |
| `subprocess` | esbuild / tsc / deno / postcss | spawn CLI | **默认方式** |
| `wasm` | 某些 JS 引擎 wasm 构建 | 沙箱内跑 | 安全敏感场景 |
| `remote` | 自托管构建服务 | 网络调用 | 企业级共享构建 |

---

## 引擎清单与运行时选择

引擎清单（`ngm.engines.json`）与运行时选择（`primary` / `fallbacks`）的字段与示例，唯一维护在：

- 引擎模型与清单 schema：[引擎 adapter 模型](../architecture/engine-adapter.md)
- 用户配置写法（简写 / 完整写法）：[配置详解](../guides/configuration.md)

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

## 后果

- ngm core 不做构建，只做依赖证明
- 构建/类型/CSS 全部走 adapter
- 不自研追赶 esbuild/tsc/Vite
- v0.3 的 LSP/Dev Server/test/docs 从 milestone 中移除，交给外部工具

---

## 相关文档

- [引擎 adapter 模型](../architecture/engine-adapter.md)
- [运行时模型](../architecture/runtime-model.md)
- [ADR-001：为什么用 Go](./adr-001-go.md)
