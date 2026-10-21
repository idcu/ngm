# 架构总览

ngm 的核心设计是**把 Git 依赖的可证明性做成默认闭环**，而不是又一个包管理器或一体化工具链。

---

## 一句话架构

```
refType 声明 → commit 解析 → 内容 digest 锁定（archive / module）
  → vendor 落地（mirror / module cache / content store / link tree）
  → verify 漂移 + binding + 重放 + 落地四查
  → OSV/白名单门禁 → 可复现构建
```

这条链路目前没有主流工具完整覆盖，但**ngm 的差异化不在单项能力，而在流程闭环与共享状态模型**。

---

## 三层运行时模型

```
┌─────────────────────────────┐
│  Host runtime               │  Node.js / Deno
│  （用户代码实际运行的环境）   │
└────────────┬────────────────┘
             │  ngm build / ngm typecheck
             ▼
┌─────────────────────────────┐
│  Engine adapters            │  esbuild / tsc / deno / postcss
│  embed / subprocess /       │
│  wasm / remote              │
└────────────┬────────────────┘
             │
             ▼
┌─────────────────────────────┐
│  ngm core (Go)              │  依赖图 / lock v2 / 内容证明 / vendor
│  internal/core，无 JS 运行时 │  module cache / hardlink / verify / gate
└─────────────────────────────┘
```

详见 [运行时模型](./runtime-model.md)。

---

## 核心子系统

| 子系统 | 职责 | 关键设计 |
|--------|------|---------|
| 依赖解析 | Git URL 归一化、refType 解析、commit 解析、模块目标 | refType 仅对完整 commit 窄默认；tag/branch 仍显式；4 种协议归一化 |
| 模块集合 | 一个仓库多个模块；模块自己的依赖/版本 | 模块清单优先、根清单回退 warning；模块身份 `repo + subPath` |
| 锁定机制 | commit + 内容 digest + resolvedAt | 根用 `archiveDigest`；模块用 `moduleDigest + subtreeOid`（lock v2） |
| vendor / 缓存 | mirror / module cache / content store / hardlink tree / cache | partial clone 只服务 lock 钉住的模块；filter 不支持明确回退 |
| 引擎层 | 统一 interface，第三方优先 | adapter 类型与内置清单唯一维护在[引擎 adapter](./engine-adapter.md)（内置 esbuild / typescript / postcss + wasm；`remote` 按 ADR-013 排除） |
| 供应链策略 | JSON-first 策略引擎 + audit(OSV) + 白名单 | v0.2：白名单与 minimumReleaseAge 已生效；postinstall 执行入口收窄到 v0.3（[ADR-009](../adr/adr-009-supply-chain-policy.md)） |
| 可观测性 | `verify`、`audit` / `why` / `tree` / `outdated` | verify 四查：ref / binding / digest / landing；区分预期漂移与非预期漂移 |

---

## 与竞品的分界（诚实版）

> 逐项对比（refType / archiveDigest / 漂移检测 / postinstall 等控制点的覆盖矩阵）的唯一事实源是
> [竞品分析](../COMPETITIVE-ANALYSIS.md)；此处只保留分界结论，不复制明细。

| 工具 | 已有能力 | ngm 不重复造 | ngm 的窄缺口 |
|------|---------|-------------|-------------|
| npm | Git commit 记录、signature、provenance、audit | npm 已是 registry 包标准答案 | refType + archiveDigest + verify 闭环 |
| pnpm | content store、minimumReleaseAge、blockExoticSubdeps | pnpm 已是磁盘管理标杆 | 统一流程 + 共享状态模型 |
| Yarn | approvedGitRepositories、checksum、immutable | Yarn 的 Git 审批已成熟 | archive 级策略 + 漂移复核 |
| Bun | 一体化（install/build/test/docs/HMR/runtime） | Bun 已是速度/一体化标杆 | 供应链可证明性闭环 |
| Vite | dev server + HMR + Rollup 生产构建 | Vite 是构建生态标杆 | ngm 只做依赖，构建交给 Vite |

**ngm 的潜在价值**：把上述分散的控制点收束为同一默认流程。若这些策略只是分别调用外部命令，ngm 就只是配置与编排层。

---

## 诚实边界

- **性能优势已经商品化**：Bun 无变更重装约 12ms，Biome 格式化约快 35×，SWC 单线程约快 20×。"快"不是稀缺卖点
- **vendor 不必然省磁盘**：pnpm 通过 content-addressable store 与 hardlink 复用，ngm 全量副本更易跨项目重复
- **统一 Go interface 不等于底层引擎统一**：接口抽象若只覆盖命令执行，得到的只是薄封装
- **"无 registry"不等于"去信任"**：仍需面对 Git host、凭证、force push、archive 一致性、私有 repo、postinstall 脚本
- **ngm 是窄场景工具**：Git 依赖可证明性。不是通用包管理器替代品

---

## 子系统文档

- [运行时模型](./runtime-model.md)
- [信任模型](./trust-model.md)
- [模块集合](./module-collections.md)
- [语言无关核心与适配器](./core-and-adapters.md)
- [vendor / module cache 层](./vendor-layers.md)
- [引擎 adapter](./engine-adapter.md)
- [供应链防护](./supply-chain.md)
- [依赖解析](./dependency-resolution.md)
- [锁定机制](./locking.md)
- [安全模型](./security-model.md)
- [可观测性](./observability.md)

---

## 相关 ADR

- [ADR-001：为什么用 Go](../adr/adr-001-go.md)
- [ADR-002：为什么直接拉 Git 仓库](../adr/adr-002-git-direct.md)
- [ADR-003：为什么纯 vendor/ 目录](../adr/adr-003-vendor.md)
- [ADR-004：为什么声明用 refType 锁定用 commit](../adr/adr-004-reftype.md)
- [ADR-005：为什么引擎统一接口动态选择](../adr/adr-005-engine-interface.md)
- [ADR-006：为什么 lytd 并入 ngm](../adr/adr-006-lytd-merge.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)
- [ADR-031：一仓多模块](../adr/adr-031-module-collections.md)
- [ADR-032：模块级内容证明](../adr/adr-032-scoped-content-proof.md)
- [ADR-033：语言无关核心](../adr/adr-033-language-neutral-core.md)
- [ADR-034：module cache 边界](../adr/adr-034-module-cache.md)
- [ADR-035：refType 窄默认](../adr/adr-035-refType-narrow-default.md)
- [ADR-036：证据式 refType 推断](../adr/adr-036-infer-ref-type-opt-in.md)
- [ADR-037：逐模块 publish 覆盖](../adr/adr-037-per-module-publish-override.md)
