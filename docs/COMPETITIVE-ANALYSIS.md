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

> **更新（2026-10-03，v0.8 之后）**：这条判断**一半被修掉了**。层 2 在 v0.8 换成按文件寻址
> （blob 池 + 树清单）后，同一场景的放大比从 **20.0× 降到 0.6×**——"跨 commit 不去重"这个
> 具体劣势不再成立。**仍然成立的一半**是：`ngm.vendor` 落地层仍是每项目一份（copy 模式下
> 真实复制字节），且**这一层没有任何人回收**（层 1 有 `git gc`，pnpm 有 `store prune`）。
> 见 [metrics · 磁盘增长](./internals/metrics.md#磁盘增长内容寻址-storev06) 与
> [项目状态评估](./internals/project-state.md#71-那页写完之后变了的)。

### 5. Git 依赖的安全控制点覆盖（ngm 的窄缺口）

| 控制点 | npm | pnpm | Yarn | Bun | Deno | ngm |
|--------|-----|------|------|-----|------|-----|
| refType 声明 | ✗ | ✗ | ✗ | ✗ | ✗ | ✓（必填） |
| commit 锁定 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| 内容摘要（archiveDigest） | ✗ | partial（checksum） | ✓（checksum） | ✓（lockfile integrity） | ✓（integrity） | ✓（规范化清单，ADR-008） |
| 漂移复核（ref 层） | ✗ | partial | partial（仅 lock 一致性） | ✗ | ✗ | ✓（区分预期/非预期） |
| Git 依赖漏洞扫描 | ✗（面向 registry） | ✗（面向 registry） | ✗（面向 registry） | ✗（面向 registry） | ✗ | done (v0.2)（`ngm audit`，OSV 按 commit 查询，覆盖率有限） |
| postinstall 控制 | partial（`--ignore-scripts`） | ✓（onlyBuiltDependencies） | partial | partial（trustedDependencies） | ✓（default deny） | ✓（默认 deny） |

> 覆盖矩阵为 2026-09 的实现观察，细节以各官方文档为准。这张表是 ngm"流程闭环"差异化论据的事实来源：单项控制点大多不独有，但没有任何竞品把它们收束为同一默认流程。

---

## 6 生态已经换了一套语言：SLSA L1–L4（2026-10 补充）

> 本节是 2026-10-03 检索后补写的。上面那张控制点矩阵是**按能力项**列的，
> 而生态如今更常用**按信任等级**描述自己——不补这一节，读者会拿ngm 去比
> 一个它本来就不在比的坐标系。

SLSA v1.2 定义四级：

| 级别 | 证明了什么 | 机制 |
|------|-----------|------|
| **L1** 完整性 | 产物没被篡改 | lock 文件 checksum |
| **L2** 溯源 | 产物由**已知来源**构建 | 加密签名 + source repo |
| **L3** 可审计 | 签名者身份可验证且记在**公开日志** | 签名者 ID + 透明日志（Rekor） |
| **L4** 全量 | 整棵依赖树（直接 + 传递）都达到 L3 | 全树验证 |

每级包含前一级。**L1 是最低门槛，L4 目前几乎没有生态达到。**

| 生态 | 达到 | 现状（2026-03~ 2026-10 观察） |
|------|------|------------------------|
| **npm** | **L3** | 最成熟：`--provenance`（SLSA v1）+ Sigstore/Rekor；Trusted Publishing 自 2025-07 GA起**默认附provenance**（opt-out）；`npm audit signatures` 一条命令验证全lockfile |
| **PyPI** | **L3** | 同一套 Sigstore 栈；PEP 740 证明；但**采用率仅约 17%** |
| pnpm | L1 | lockfile checksum + `--verify-store-integrity`（10.x） |
| Yarn (Berry) | L1 | 自有 checksum + `enableImmutableInstalls` |
| Deno | L1 | `integrity` + `minimumDependencyAge` |
| Bun | L1 | lockfile integrity |
| **ngm** | **L1+（不同维度）** | 见下——**它不在同一条轴上** |

### 关键：ngm 与 SLSA 是**正交**，不是高下

这是本节最容易被误读的地方，所以写清楚：

| | SLSA provenance 证明 | ngm archiveDigest 证明 |
|---|---|---|
| 回答的问题 | "这个 tarball 是**谁、哪次 CI、哪个 commit** 构建的" | "这份**内容**是否就是锁定的那个 commit" |
| 不证明什么 | **不证明代码无害**（恶意 commit 会被如实证明）；**不覆盖传递依赖**（只覆盖那一个包）；不证明 workflow 本身可信 | 不证明来源可信（一个被攻破的仓库照样算出稳定 digest） |

所以：
- **npm 的 L3 比 ngm 高**，在"这个发布物是谁发的"这件事上ngm 完全没有对应物；
- **但 L3 不回答 ngm 回答的问题**——tag 重打、branch 前进、上游悄悄换内容，
  这些 provenance **一概看不见**，因为它们发生在**构建之前**；
- 两者叠加才有意义：**provenance 锁"谁构建的"，archiveDigest 锁"构建出来的东西是什么"**。

> 另一条现实约束：provenance 的**采用率远未普及**（npm top500 约 1/5，
> 长尾更低；PyPI 17%）。"reject 任何没有 provenance 的包"这样的策略
> 今天会让构建失败，因此生态真正的做法是**告警**而非拒绝。
> 这意味着单靠 provenance 也不构成默认闭环——**与上面第 1 节的结论一致**。

### 诚实结论

按SLSA 这条轴，ngm **不领先**，也没有落后——**它测的是另一件事**。
它的refType 必填、ref 漂移三态、按 commit 查 OSV 三项在主流工具里仍无对应物，
但这三项都不属于 SLSA 的分级维度。

如果要往 SLSA 这条轴上走，ngm 的天然位置是 **L2 的一半**：
`archiveDigest` 已经有"内容 ↔ commit"的绑定（比 L1 强），
但**没有签名**，因而拿不到 L2 要求的"由已知来源构建"的**可验证**部分。
ADR-014已就"自证文件签名"做过决定（不做），若将来要接，
最省事的形状是给 manifest 加一层 SLSA 谓词，而不是自造一套。

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
5. **先窄后宽**：v0.1 只做依赖可证明性，v0.2 做供应链完整，v0.3 做引擎生态，v0.4 收敛验证
   （**"后续探索"里的方向——多语言 Git 依赖、SBOM 导出等——均未承诺，暂缓**）

---

## 数据限制说明

- 竞品版本与生态规模未逐项由官方发布记录复核
- 性能数字（Bun 12ms、Biome 35×、SWC 20×）来自官方 benchmark 或社区测试，环境不同可能有差异
- 本报告适合做架构决策的输入，**不适合直接用于对外宣传**

---

## 相关文档

- [架构总览](./architecture/overview.md)
- [README](./README.md)
