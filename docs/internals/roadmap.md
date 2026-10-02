# 路线图

> ngm 的产品路线。**v0.1 ~ v0.8 均已交付**（证据见各版[复盘](../development/)与计划）；
> `v0.1.0` ~ `v0.4.0` 均已打 tag 并发布，**GitHub 与 Gitee 两个源都可取到**
> （2026-10-01 补齐后三版，2026-10-02 补齐 Gitee 侧附件，两源已比对）。
> **`v0.5.0` ~ `v0.8.0` 的 tag 已于 2026-10-02 补打**（此前这四版"已交付但取不到"，
> 见 [v0.8 复盘 §5.6](../development/v0.8-retrospective.md)）；但镜像转发 tag **没有触发**
> `release.yml`，那四版的 GitHub release 尚未生成——补发走手动入口（Actions → Release →
> Run workflow），Gitee 侧附件随后待上传。
> **"已交付 ⇔ 已打 tag"现在有机械检查**（`scripts/check-release-status.sh`）——
> 但"release 是否真的出来了"还没有，见[发布清单](../development/README.md#发布清单每个版本)。
> v0.6 / v0.7 / v0.8 的计划与复盘见[开发总览](../development/README.md)。
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
v0.2~v0.4 **三版从未发布**（发行源上只有 `v0.1.0`；**2026-10-01 补发 GitHub 侧、
2026-10-02 补齐 Gitee 侧**）。

| 组 | 任务 | 状态 |
|----|------|------|
| A | 在线 verify 的 spawn 成本与方差 | ✅ 已交付（[ADR-015](../adr/adr-015-commit-ref-resolution.md) / [ADR-016](../adr/adr-016-mirror-url-local-read.md)：commit 3→2、tag 4→3 次/依赖；**目标线在噪声里不可判别**，见复盘 §2.3） |
| B | ADR-013 翻案条件的**判定** | ✅ 已交付：3 平台 × 7 形态**字节一致**；翻案条件的技术前提成立，**仍不发布**（发布须另立 ADR） |
| C | 权限施加点的机械核对 | ✅ 已交付：查出两处缺口并接线；三张机械检查的网（`internal/security/enforcement_test.go` 等） |
| D | **v0.2 ~ v0.4 补发布** | ✅ 已完成：GitHub 侧 3 × 7（2026-10-01）、**Gitee 侧 21 个附件（2026-10-02）**，两源已比对 |
| E | 挂账项：`deno bundle` 实验性警告必须转达；store/mirror GC 等数据 | ✅ E1 已交付（两条消息通道此前各丢一半）；E2 按计划不做（缺磁盘增长数据） |

> 复盘见 [v0.5 复盘](../development/v0.5-retrospective.md)；实施计划是
> [v0.5 计划](../development/v0.5-plan.md)；范围来源是 [v0.5 开工前复核](../development/v0.5-review.md)。
> 复核期间**顺手交付**的部分（`ngm transform`、并行哈希、deno `typeCheck` 真引擎覆盖、
> Deno 2 假失败夹具修复、CI 的 deno 版本对齐声明下限）不计入本版范围，已登记在该复核里。

---

## v0.6 — 让结论可判别

**目标**：v0.5 留下的三处都是"测了，但判不了或数据不够"——3s 目标在噪声里不可判别、
可复现性只证到合成夹具、GC 缺磁盘数据。本版补的是**可判别性**，不是新能力。

| 组 | 任务 | 状态 |
|----|------|------|
| A | 真实项目形态的可复现证据（代码分割 / 资产指纹 / 多插件链；[ADR-017](../adr/adr-017-remote-adapter-release-decision.md) 门槛 a） | ✅ **已交付**：本机 10 个门禁形态 × 20 轮全稳定 + 对照 20 个取值；**跨机器三平台 + 本机同值**（`treeA=124f03b1de58091a`）→ **门槛 a 成立** |
| B | **spawn 预算**：把"次数优先于秒数"变成 CI 门禁 | ✅ **已交付**（门禁 + 两条扫源码的机械网；开门第一天抓到 v0.5 遗留的一次**重复记账**） |
| C | store / mirror 的磁盘增长数据（**只测不做**，为 GC 排期提供数据） | ✅ **已交付**（每 commit 一整棵树、无跨 commit 去重：源码增量的 **20×**；结论：**做 GC，但先立 ADR**） |
| D | Gitee 附件补传（把手工步骤降到一条命令） | ✅ **已完成**（2026-10-02：三版各 **7/7**；两源 `SHA256SUMS` 逐字节相同、抽查二进制与清单一致） |
| E | 挂账：`deno bundle`（条件未变，等上游） | ✅ 已核查（2026-10-02，**间接证据**：能直接引用的仍是 2025-08 的 issue 输出；2.8 发布说明未提及） |

> **复盘**见 [v0.6 复盘](../development/v0.6-retrospective.md)；计划见 [v0.6 计划](../development/v0.6-plan.md)。
> 本版**先行完成**两项 ADR：[ADR-017](../adr/adr-017-remote-adapter-release-decision.md)
> （remote adapter 的发布决策，结论仍不发布，但剩下的问题已写成可判定的门槛）、
> [ADR-018](../adr/adr-018-store-reclaim.md)（store 的回收与去重）。
> **唯一未完成的是 A4（跨机器结论）**：探针已触发，但本环境**读不到 CI 结果**——
> 这一处也不是工程，是凭据。Gitee 侧的 21 个附件已于 2026-10-02 补齐。

---

## v0.8 — 层 2 换布局（ADR-019 分阶段落地）

**计划**：[v0.8 计划](../development/v0.8-plan.md)。**复盘**：[v0.8 复盘](../development/v0.8-retrospective.md)。
顺序是**先让消费方与布局解耦，再换布局**——
因为 `verify` 是安全关键路径，不能与布局改动同时发生。

| 阶段 | 内容 | 状态 |
|------|------|------|
| A | 消费方与布局解耦（`Entries` + `compareTrees` + `VerifyVendorAgainstEntries`） | ✅ **已交付**（等价重构：既有测试一字未改全部通过） |
| B | `Put` 写 v2（blobs + manifest），读路径 v2 优先、回落 v1；`LinkTree` 按条目落地 | ✅ **已交付**（含双布局验收） |
| C | 收敛与观测（v1 被重写时回收；`usage` 分别报告两种布局） | ✅ **已交付** |

**尺子已量**（[ADR-019](../adr/adr-019-content-addressed-blobs.md) 写死的那个）：
12 个 commit 的占用 **1.88 MiB → 59.9 KiB（20.0× → 0.6×）**。
`TestV06StoreGrowthInventory` 的断言已从"至少要有 N 棵树"**翻转**为"不得接近 N 棵树"——
同一段代码，量的从"放大得有多严重"变成"去重是否真的生效"。

**三处实施时才发现、ADR 没写到的点**（记在 [ADR-019 §修订](../adr/adr-019-content-addressed-blobs.md#修订v08-实现时发现并改掉的五处)）：

1. **可执行位无法只靠"内容寻址"承载**（hardlink 共享 inode）→ blob 身份改为 `H(mode, 内容)`；
   不这么做，可执行文件落地后会**静默丢掉可执行位**。
2. **`symlink` 落地模式在 v2 下退化为逐条目 hardlink**（如实报告实际 mode）——
   本版**唯一一处用户可见的行为变化**。
3. **收敛不是自动的**：`install` 在 `Has` 为真时短路（"无网络也能安装"的承诺），
   健康的 v1 条目 `Has` 正是真 → **旧条目不会被自动迁移**；出路由 `usage` 打印
   （删 `sha256/` 整层再重装，层 2 是派生物）。

> **它改的是斜率，不是终点**：blob 同样只增不减，回收仍受
> [ADR-018](../adr/adr-018-store-reclaim.md) 那两个条件约束。

---

## v0.7 — store 的占用可见、残骸可回收

**计划**：[v0.7 计划](../development/v0.7-plan.md)（范围 = [ADR-018](../adr/adr-018-store-reclaim.md) 决策 2）——**已交付**；
**复盘**：[v0.7 复盘](../development/v0.7-retrospective.md)（**补写**：交付时跳过了这一步，v0.8 收尾时补齐）。

| 组 | 内容 | 状态 |
|----|------|------|
| A | `ngm store usage`（只读占用报告） | ✅ 已交付 |
| B | `ngm store prune`（只清解包残骸，`--dry-run` 可用、幂等） | ✅ 已交付 |

**剩下的候选**（按证据强度排序，来源：[v0.6 复盘 §7](../development/v0.6-retrospective.md#7-v07-候选按证据强度排序非路线图承诺)）。
候选池不是承诺：

| # | 候选 | 前置 |
|---|------|------|
| 1 | ~~`ngm store usage` + `ngm store prune`~~ ✅ **已在 v0.7 完成** | —— |
| 2 | ~~层 2 **写入侧去重**（blob 池 + 树清单）~~ ✅ **已在 v0.8 完成**（[ADR-019](../adr/adr-019-content-addressed-blobs.md)：20.0× → 0.6×；改的是斜率，不是终点） | —— |
| 3 | ~~补上可复现性的**跨机器结论**~~ ✅ **已在 v0.6 完成**（三平台 + 本机同值） | —— |
| 4 | `remote` adapter 门槛 b：抽样构建的**成本数字** | **门槛 a 已满足** → b 是"要不要发布"唯一剩下的条件，也是决定性的一问 |
| 5 | `deno bundle` 真引擎覆盖 | 上游把 bundle 标为稳定（截至 2026-10-02 未变） |
| 6 | 3s 目标的口径重定义 | 等次数门禁稳定运行几个版本（v0.6 有意推迟） |

---

## v0.9 — 让 store 的读数说真话

**目标**：v0.8 换了布局之后，层 2 的数字开始需要解释（逻辑体积 / 共享 / 独占是三个不同的
东西），而"能不能回收"至今没有数据。本版**不新增能力，只补读数与判别**。

| 组 | 任务 | 状态 |
|----|------|------|
| A | `store usage` 把 blob 池切成**共享 / 独占 / 孤儿**，每棵树报"丢掉它能回收多少" | ✅ **已交付**（断言是不变量：把共享算进独占立刻红） |
| B | **锚点检查**（`[label](x.md#anchor)` 的锚点此前无人校验） | ✅ **已交付**（Go 测试，46 个锚点全验） |
| C | blob 池的规模（10 万级，**只测不做**） | ✅ **已交付**（两位分片仍有余量；usage 毫秒级） |

> 计划见 [v0.9 计划](../development/v0.9-plan.md)。本版明确**不做**：
> 任何按可达性删除 blob 的代码（[ADR-018](../adr/adr-018-store-reclaim.md) 的两个条件未变）、
> v1 → v2 的就地迁移（[ADR-019](../adr/adr-019-content-addressed-blobs.md) 已否）。

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
- [开发计划与复盘（v0.1 ~ v0.6）](../development/)
