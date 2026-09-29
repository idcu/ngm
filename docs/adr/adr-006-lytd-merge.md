# ADR-006：为什么 lytd 并入 ngm（及并入后的边界）

- **状态**：已定（v2 修订：能力不平移，只吸收思路）
- **日期**：2026-09-29（v2 修订：2026-09-29）
- **范围**：项目合并策略与能力取舍

---

## 背景

lytd 是一个 TypeScript 工具链（构建 + 测试 + 文档 + Dev Server + LSP），10 个包、50+ 子模块、2042 个测试用例、78% 覆盖率（截至 v0.1.0）。

原方案：lytd 完全并入 ngm，代码全部用 Go 重写。

v2 竞品分析后重新审视：lytd 的核心能力（自研 tokenizer、自研 bundle、自研 HMR、自研 LSP、自研 test framework）在 2026 年已不具竞争力：

| lytd 能力 | 竞品现状 | 自研的必要性 |
|----------|---------|-------------|
| 自研 tokenizer | esbuild/SWC/Biome 已成熟 | 无 |
| 自研 bundle | esbuild/Rollup/Vite 已成熟 | 无 |
| 自研 HMR | Vite 已是标杆 | 无 |
| 自研 LSP | tsserver/Deno LSP 已成熟 | 无 |
| 自研 test framework | Vitest/Deno test 已成熟 | 无 |
| 自研 docs | TypeDoc/Deno doc 已成熟 | 无 |

---

## 决策

**lytd 完全并入 ngm，但只吸收架构思路，不平移能力。**

lytd 的自研能力不进入 ngm core。ngm core 只做"Git-first 依赖证明层"，构建/测试/文档/Dev Server/LSP 全部交给外部引擎。

---

## 从 lytd 吸收什么（架构思路）

| lytd 的架构决策 | ngm 怎么借鉴 |
|---------------|-------------|
| 引擎抽象层（TransformEngine/BundleEngine） | 吸收为 adapter 模型，但只做薄封装，不自研实现 |
| EngineManager 统一调度 | 吸收为 Registry + 运行时选择 |
| 第三方优先 + 自研 fallback | 吸收为"第三方永久优先"，自研只做兜底 |
| 模块分解文档（P0~P8） | 吸收为文档体系结构 |
| 测试方法论 | 吸收为 Go testing + 表驱动测试纪律 |
| 文档分层（用户/贡献者/架构师/维护者） | 吸收为文档目录结构 |
| 诚实文档（"为什么你不应该用"） | 吸收为 README 的诚实版章节 |
| 自举 | ngm 用 Go 写，天然自举 |
| 插件系统 | 降级为 adapter 协议 + hooks，不追求插件数量 |

---

## 不吸收什么

| lytd 的东西 | 为什么不吸收 |
|------------|-------------|
| 自研 tokenizer | esbuild/SWC 已覆盖，自研无优势 |
| 自研 bundle engine | esbuild/Rollup 已成熟 |
| 自研 HMR | Vite 已是标杆 |
| 自研 test framework | Vitest/Deno test 已成熟 |
| 自研 LSP | tsserver/Deno LSP 已成熟 |
| 自研 docs generator | TypeDoc/Deno doc 已成熟 |
| 10 个包的 monorepo 结构 | ngm 是单二进制，不需要分包 |
| 50+ 子模块的复杂度 | ngm core 应保持精简 |
| 2042 用例的测试体系 | 有价值，但 Go 侧需重新设计 |

---

## 后果

- ngm 不是"lytd 的 Go 重写版"
- ngm 不是"一体化工具链"
- ngm 是"Git 依赖证明层"——一个窄但深的工具
- lytd 的 v0.1.0（2026-08-10 发布，API 冻结）可作为历史参考，但不作为 ngm 的功能清单

---

## 相关文档

- [架构总览](../architecture/overview.md)
- [引擎 adapter 模型](../architecture/engine-adapter.md)
- [ADR-005：为什么引擎统一接口动态选择](./adr-005-engine-interface.md)
