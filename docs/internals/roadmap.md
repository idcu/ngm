# 路线图

> ngm 的产品路线。**v0.1 ~ v0.4 均已交付**（证据见各版[复盘](../development/)；
> `v0.1.0` ~ `v0.4.0` 均已打 tag 并发布（2026-10-01 补齐后三版；Gitee 侧附件待上传））；
> **v0.5 开工前复核已完成**（[v0.5 复核](../development/v0.5-review.md)），**v0.5 范围尚未成文**。
>
> 本文件是**范围**的唯一事实源；各版本的验收标准与实测结论在对应的计划与复盘里。

---

## 总原则

**先窄后宽**：先把"Git 依赖可证明性"做深做透，再扩展生态集成。

**第三方优先**：不自研追赶 esbuild/tsc/Vite/Deno，全部通过 adapter 调用。

**核心精简**：ngm core 只做依赖证明层，不做构建/测试/文档/Dev Server/LSP。

---

## v0.1 — Git 依赖可证明性闭环

**目标**：`ngm install && ngm verify` 能证明 Git 依赖的可追溯性。

**必须完成**：

| 模块 | 任务 |
|------|------|
| Git | 4 种协议归一化、凭证管理 |
| 解析 | refType 解析（commit/tag/branch）、依赖图、冲突检测 |
| 锁定 | refType + commit + archiveDigest + resolvedAt |
| vendor | mirror / content store / hardlink tree / cache |
| verify | ref 漂移检测 + digest 检查 + 退出码 |
| adapter | esbuild subprocess adapter |
| mappings | 生成 ngm.mappings.json |
| 维护 | `ngm cache clean`（缓存层可随时清空） |

**退出标准**（四条**均已达成**，证据见 [v0.1 复盘](../development/v0.1-retrospective.md) §1）：

- 能在真实项目上跑通 `ngm install` ✅
- lock file 跨平台字节级一致 ✅
- verify 能检测出 tag 重打和 branch 前进 ✅
- digest 重放不匹配能阻断构建（exit 2）✅

**尚未达成的非阻塞项**：性能目标 5 条中 2 条未达标（`verify` 在 100 依赖规模上超目标
6.3× / 2.2×），根因是每个依赖 4 次 git 子进程，列入 v0.2 优化项。

> **v0.2 更新**：其中 **offline verify 已达标**（11.12s → 1.97s）；在线 verify 仍差 1.1×，
> 缺口性质与去向见 [v0.2 复盘 §2.1](../development/v0.2-retrospective.md)。

---

## v0.2 — 供应链防护完整

**目标**：把 refType + commit + digest + vendor + verify + OSV 收束为统一流程。

| 模块 | 任务 | 状态 |
|------|------|------|
| OSV | OSV.dev 查询 + 缓存 + 报告 | ✅ 按 commit 查询 + 24h 缓存 |
| 策略 | minimumReleaseAge、白名单、postInstallPolicy | ✅ 三项均生效；**postInstallPolicy 的执行入口自 v0.3 起存在**（沙箱内、仅 JS 钩子，见 ADR-009 决策 5/5a） |
| audit | `ngm audit` 命令 + 报告格式化 | ✅ |
| 可观测性 | why / tree / outdated | ✅ |
| adapter | tsc / deno / postcss adapter | ✅（deno 需自行声明。**真引擎 CI 覆盖**：tsc / postcss 自 v0.3，**deno 的 `typeCheck` 自 v0.5**；`deno bundle` 仍等上游脱离实验，见 [v0.5 复核 §挂账](../development/v0.5-review.md)） |
| CI | `--frozen-lockfile` / `--offline` 模式 | ✅ |
| （追加） | `verifyOnLock` + verify 性能 | ✅ 已接线；offline 达标、在线差 1.1× |

> **验收与实测**见 [v0.2 实施计划](../development/v0.2-plan.md) 与
> [v0.2 复盘](../development/v0.2-retrospective.md)（含 6 条设计偏离与 6 处已修缺陷）。

---

## v0.3 — 引擎生态与协议

**目标**：让 ngm 能接入主流构建工具。

| 模块 | 任务 | 状态 |
|------|------|------|
| adapter | wasm adapter | ✅（[ADR-011](../adr/adr-011-wasm-runtime.md)：wazero v1.9.0 + WASI，argv / 产物 / 退出码同 subprocess） |
| adapter | remote adapter | ❌ **已决定不发布**（[ADR-013](../adr/adr-013-remote-adapter.md)：产物无法被用户本地证明） |
| 集成 | Vite / esbuild / Deno / Webpack 集成脚手架 | ✅（含 tsconfig `paths`；真 esbuild / tsc 验收） |
| sandbox | Deno 沙箱模式（**`postInstallPolicy` 执行入口的前置**） | ✅（[ADR-012](../adr/adr-012-sandbox.md)；postinstall 与 audit hook 已在沙箱内执行） |
| 凭证 | `~/.ngm/config.json` 权限管理 | ✅（`read:`/`write:`/`net:`/`run:`/`env:` 全部**真的施加**） |
| mappings | monorepo 子路径（可选 `path`，**版本号保持 1**） | ✅（[P4](../modules/p4-ecosystem.md)） |

> **验收与实测**见 [v0.3 实施计划](../development/v0.3-plan.md) 与
> [v0.3 复盘](../development/v0.3-retrospective.md)（11 条设计偏离、8 处已修缺陷——
> 其中 2 处在已发布代码里）。复盘记录的那 1 项未结项
> （沙箱自述文件签名检查）已由 [ADR-014](../adr/adr-014-self-report-signatures.md)
> **以"决定不做"结项**；复盘本身是快照，不随之后的决定修改。

> **候选（不构成承诺）**：[v0.3 复盘 §6](../development/v0.3-retrospective.md) 按证据强度列出
> 自述文件签名检查、可复现构建、deno 真引擎覆盖、单依赖内并行哈希。
> `remote` adapter **不再列为候选**——它的翻案条件是"产物可复现且用户能抽样本地复现"。

---

## v0.4 — 验证与收敛

**目标**：把上一版留下来的开放问题收敛掉，并把"人工纪律"换成机器检查。

| 模块 | 任务 | 状态 |
|------|------|------|
| 配置 | 端到端断言：每个配置键真的被读过 | ✅ `TestConfigKeysAreExercisedByTests`（首次运行即抓到第 4 例缺陷 ✓） |
| 引擎 | `ngm typedecl`：给 typeDecl 能力一个入口 | ✅ + **真 tsc** 验收 ✓ |
| verify | `--signatures` / `--require-signed` | ✅（ADR-014 决策 3）✓ |
| adapter | remote adapter | ❌ **已决定不发布**（[ADR-013](../adr/adr-013-remote-adapter.md)） |

> **验收与实测**见 [v0.4 计划](../development/v0.4-plan.md) 与 [v0.4 复盘](../development/v0.4-retrospective.md)。
> v0.3 计划里最后一个未打勾的框（沙箱自述文件签名检查）由 [ADR-014](../adr/adr-014-self-report-signatures.md)
> 以"决定不做"结项 ✓；并在同一份 ADR 里记下 ADR-013 翻案条件的实测进展 ✓。

---

## v0.5 — 收敛与交付

**目标**：把已经量出来但还剩着的三件事各自推到结论——性能贴着线、ADR-013 翻案条件未判、
v0.2~v0.4 **三版从未发布**（发行源上只有 `v0.1.0`；**2026-10-01 已补发 GitHub 侧**，
Gitee 附件待人工上传）。

| 组 | 任务 | 状态 |
|----|------|------|
| A | 在线 verify 的 spawn 成本与方差（先立 ADR：`refType=commit` 跳过 `ls-remote`） | 计划 |
| B | ADR-013 翻案条件的**判定**（新形态探针三平台结果 + 机器比对） | 计划 |
| C | 权限施加点的机械核对（`read:`/`net:`/`run:`/`env:`，缺断言即失败） | 计划 |
| D | **v0.2 ~ v0.4 补发布**（打 tag → 重建六平台产物 → 双源核对 `SHA256SUMS`） | ✅ GitHub 侧已完成（3 × 7 附件，2026-10-01）；**Gitee 附件待人工上传** |
| E | 挂账项：`deno bundle` 固定"实验性警告必须转达"；store/mirror GC 等数据 | 计划 |

> 实施计划见 [v0.5 计划](../development/v0.5-plan.md)；范围来源是
> [v0.5 开工前复核](../development/v0.5-review.md)（已完成，含逐项证据）。
> 复核期间**顺手交付**的部分（`ngm transform`、并行哈希、deno `typeCheck` 真引擎覆盖、
> Deno 2 假失败夹具修复、CI 的 deno 版本对齐声明下限）不计入本版范围，已登记在该复核里。

---

## 后续探索（不承诺）

| 方向 | 前提 |
|------|------|
| 多语言 Git 依赖（Go/Rust/Python） | JS/TS 场景证明可行 |
| 自研 transformer（兜底） | subprocess 有不可接受短板 |
| 远程构建缓存 | 企业级需求；**须先回答 [ADR-013](../adr/adr-013-remote-adapter.md) 的第 2 问**（把源码送出本机后，产物如何被本地证明） |
| SBOM 导出（SPDX/CycloneDX） | 合规需求 |

---

## 里程碑依赖关系

```
v0.1（可证明性闭环）
  │
  ├── 必须优先：refType + commit + digest + vendor + verify
  │
  ▼
v0.2（供应链完整）
  │
  ├── 依赖 v0.1 的 lock schema 和 verify 机制
  │
  ▼
v0.3（引擎生态）
     │
     └── 依赖 v0.1 的 adapter 协议和 mappings 协议
```

**关键路径**：v0.1 的 lock schema 和 adapter protocol 一旦稳定，后续里程碑才能展开。

---

## 诚实边界

1. **里程碑可能延期**：这是早期项目的常态
2. **范围可能收缩**：如果 v0.1 证明某些设计不成立，会调整
3. **不做大承诺**：ngm 是窄场景工具，不追求通用替代品

---

## 相关文档

- [能力矩阵](./capability-matrix.md)
- [健康度指标](./metrics.md)
- [模块分解（P0~P8）](../modules/)
- [开发计划与复盘（v0.1 ~ v0.5）](../development/)
