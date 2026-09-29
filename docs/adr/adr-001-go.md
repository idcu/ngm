# ADR-001：为什么用 Go

- **状态**：已定（v2 修订）
- **日期**：2026-09-29（v2 修订：2026-09-29）
- **范围**：ngm 实现语言与分发形态

---

## 背景

ngm 前身 jsmod 原计划用 Go 实现。后并入 lytd（TypeScript 工具链）时曾讨论是否改用 TS。最终决定 ngm 核心仍用 Go。

v2 阶段进一步明确：ngm 定位从"一体化工具链"收缩为"Git-first 依赖证明层"，构建/测试/文档/Dev Server/LSP 全部交给外部引擎。这强化了 Go 作为 core 语言的合理性。

---

## 决策

**ngm core 用 Go 实现，以单二进制分发。**

**用户项目运行时支持 Node.js 与 Deno，ngm core 不替用户选择。**

---

## 理由

### 支持点

1. **单二进制分发**：用户安装 ngm 不需要先装 Node 或 Deno。依赖管理器是基础设施，单二进制显著降低分发摩擦
2. **文件系统语义强**：ngm core 的核心是 vendor 4 层（worktree / content store / hardlink tree / mirror）、hardlink、cache、lock file——这些 Go 的标准库处理得比 JS 更可靠
3. **并发模型适合 IO 密集**：拉取多个 Git 仓库、并行计算 archiveDigest、并行校验——goroutine 天然适合
4. **无运行时依赖**：ngm 本身不背 Node 或 Deno 的升级包袱，不会被"依赖漂移"反噬
5. **交叉编译**：`GOOS=linux GOARCH=amd64` 一行命令出多平台二进制，CI 分发简单

### 代价与诚实说明

1. **Go 单二进制不天然更快**：底层仍需调用外部引擎（esbuild / tsc / deno / postcss），性能优势只在 core 路径上体现
2. **对 JS 生态适配需走 adapter**：ngm 不直接 import npm 包，所有 JS 能力通过 subprocess / embed / wasm adapter 调用
3. **JSON schema 与配置管理**：Go 的 JSON 处理够用但不够灵活，复杂配置可能需要自定义 Unmarshal 逻辑
4. **esbuild 嵌入不再是核心卖点**：v2 把 esbuild 降级为 adapter 之一，Go 能嵌入 esbuild 这个理由被削弱——但 core 仍受益于 Go 的 IO 与文件系统能力

---

## 对比参考

| 工具 | 语言 | 分发形态 | 与 ngm 的关系 |
|------|------|---------|--------------|
| esbuild | Go | npm 包 / 二进制 | ngm 通过 adapter 调用，不嵌入 |
| Bun | Zig | 单二进制 | 一体化标杆，ngm 不与其竞争 |
| Biome | Rust | npm 包 | lint/format 已商品化，ngm 不涉足 |
| SWC | Rust | npm 包 | 底层可靠性已被大型框架验证 |
| tsgo | Go | 预览包 | ngm typeCheck adapter 的备选 |

---

## 后果

- ngm core 负责：依赖解析、lock、vendor 4 层、cache、hardlink、verify、audit
- 构建/类型/CSS 全部通过 engine adapter 调用外部引擎
- ngm 不宣称"自研引擎替代 esbuild"——第三方优先是永久策略，不是过渡策略
- `self` 引擎只做兜底、dry-run、离线 stub，不做生产级实现

---

## 相关文档

- [架构总览](../architecture/overview.md)
- [运行时模型](../architecture/runtime-model.md)
- [引擎 adapter 模型](../architecture/engine-adapter.md)
- [ADR-007：Node 还是 Deno？](./adr-007-runtime-node-deno.md)
