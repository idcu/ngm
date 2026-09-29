# 竞品对比分析

> **事实截止 2026-09-29**——内容来自各竞品公开文档与行为观察，细节以各官方文档为准。
> 本文是 ngm 设计决策的**事实依据**：差异化论据取自这里的逐项对比，而不是印象。
> 逐项对比的唯一事实源就是本文；`README.md` 与 `architecture/overview.md` 只保留结论并引用本页。

---

## 竞品全景

### 依赖管理

| 工具 | 语言 | 分发 | Git 依赖 | 内容寻址 | 锁定 | 漂移检测 |
|------|------|------|---------|---------|------|---------|
| npm | JS | npm 包 | ✓（记录 commit） | ✗ | ✓ | ✗ |
| pnpm | JS | npm 包 | ✓ | ✓ | ✓ | partial |
| Yarn | JS | npm 包 | ✓（审批） | ✓ | ✓ | partial（lock 一致性） |
| Bun | Zig | 单二进制 | ✓ | ✓ | ✓ | ✓ |

### 构建链路

| 工具 | 语言 | 热更新 | 生产构建 | 类型检查 | 测试 | 文档 | LSP |
|------|------|--------|---------|---------|------|------|-----|
| Vite | TS | ✓（极快） | Rollup | tsc | Vitest | 外部 | 外部 |
| esbuild | Go | ✗ | ✓（快） | ✗ | ✗ | ✗ | ✗ |
| Rollup | JS | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Webpack | JS | ✓ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Turbopack | Rust | ✓ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Bun | Zig | ✓ | ✓ | bun-types | ✓ | ✗ | ✗ |
| SWC | Rust | ✗ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Biome | Rust | ✗ | ✗ | ✓（部分） | ✓ | ✗ | ✓ |
| tsgo | Go | ✗ | ✗ | ✓（预览） | ✗ | ✗ | ✗ |

### 历史方案（已淘汰或边缘化）

- Component（早期 JS 包管理）
- git-deps（Git 依赖实验）
- Snowpack（ESM 构建，已停止维护）
- Pika（Snowpack 前身）
- Rome（现 Biome）

---

## 关键洞察

### 1. "Git 依赖可证明性闭环"并非完全空白

- npm 记录 Git commit
- pnpm 有 minimumReleaseAge
- Yarn 有 approvedGitRepositories + immutable
- Deno 有 minimumDependencyAge + integrity
- Bun 有 lockfile + integrity

**ngm 不能声称"无人覆盖"，只能说"未形成默认闭环"。**

### 2. 速度与一体化已商品化

- Bun 无变更重装约 12ms
- Biome 格式化约快 35×（对比 Prettier）
- SWC 单线程约快 20×（对比 Babel）
- Bun 已实现 install/build/test/docs/HMR/runtime 一体化

**ngm 若只宣传"快"或"一体化"，无法形成有效差异化。**

### 3. 自研追赶无意义

- esbuild 覆盖 transform + bundle + tree-shaking
- tsc 经过十年打磨
- Vite 的 HMR 是行业标杆
- Vitest/Deno test 已成熟
- tsserver/Deno LSP 已成熟

**ngm 不自研这些能力，全部通过 adapter 调用。**

### 4. vendor 不必然省磁盘

pnpm 的 content-addressable store + hardlink 已经很高效。ngm 的 vendor 全量副本更容易跨项目重复。

**vendor 的价值必须重新定义为：可审计、可提交、可离线。**

### 5. Git 依赖的安全控制点覆盖（ngm 的窄缺口）

| 控制点 | npm | pnpm | Yarn | Bun | Deno | ngm |
|--------|-----|------|------|-----|------|-----|
| refType 声明 | ✗ | ✗ | ✗ | ✗ | ✗ | ✓（必填） |
| commit 锁定 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 内容摘要（archiveDigest） | ✗ | partial（checksum） | ✓（checksum） | ✓（lockfile integrity） | ✓（integrity） | ✓（规范化清单，ADR-008） |
| 漂移复核（ref 层） | ✗ | partial | partial（仅 lock 一致性） | ✗ | ✗ | ✓（区分预期/非预期） |
| Git 依赖漏洞扫描 | ✗（面向 registry） | ✗（面向 registry） | ✗（面向 registry） | ✗（面向 registry） | ✗ | planned (v0.2)（OSV 按 commit，覆盖率有限） |
| postinstall 控制 | partial（`--ignore-scripts`） | ✓（onlyBuiltDependencies） | partial | partial（trustedDependencies） | ✓（default deny） | ✓（默认 deny） |

> 覆盖矩阵为 2026-09 的实现观察，细节以各官方文档为准。这张表是 ngm"流程闭环"差异化论据的事实来源：单项控制点大多不独有，但没有任何竞品把它们收束为同一默认流程。

---

## ngm 的真实差异化

ngm 的潜在价值在于**流程闭环**和**共享状态模型**：

> 依赖图、lock file、vendor 布局、verify 机制、audit 报告、供应链策略
> 共享同一状态模型，任一环节变更都能触发正确的增量更新，
> 产物能够由锁定的 commit 和 archiveDigest 完整解释。

若这些策略只是分别调用外部命令，ngm 就只是配置与编排层。

---

## 结论

1. **ngm 定位为"Git-first 依赖证明层"**，不是通用包管理器替代品
2. **core 用 Go**，宿主运行时支持 Node/Deno
3. **所有引擎通过 adapter 调用外部工具**，不自研追赶
4. **差异化在流程闭环**，不在单项性能
5. **先窄后宽**：v0.1 只做依赖可证明性，v0.2 做供应链完整，v0.3 做引擎生态

---

## 数据限制说明

- 竞品版本与生态规模未逐项由官方发布记录复核
- 性能数字（Bun 12ms、Biome 35×、SWC 20×）来自官方 benchmark 或社区测试，环境不同可能有差异
- 本报告适合做架构决策的输入，**不适合直接用于对外宣传**

---

## 相关文档

- [架构总览](./architecture/overview.md)
- [README](./README.md)
