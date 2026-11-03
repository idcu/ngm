# 阶段地图（逐版交付记录）

> **这份文件是什么**：`docs/development/README.md` 原「阶段地图」一节。
> **为什么单独成文**：它是一张 160 行的逐版表，占原文件 100KB；索引要先回答"怎么工作、怎么验收"。
> **判据**：`TestV63MilestoneTablesListEveryVersionExactlyOnce` 要求每个版本恰好一行、顺序正确（起点 v0.5）。
> 本目录整体是**快照**（复盘不改写），这张表是**活**的：新增一版就加一行。

### v0.1（详细计划：[v0.1-plan.md](./v0.1-plan.md)）

| 阶段 | 名称 | 前置 | 关键交付 |
|------|------|------|---------|
| M0 | 工程骨架与配置层 | — | CLI 骨架 / 错误码 / 配置三级合并 / `init` / CI |
| M1 | Git 层 | M0 | 4 协议归一化 / refType 解析 / mirror / `add` |
| M2 | 内容清单与 archiveDigest | M1 | ADR-008 实现 / digest 向量冻结 / content store |
| M3 | 依赖图 / 冲突 / lock | M2 | 传递依赖 / root wins / 确定性 lock / `install` |
| M4 | vendor 4 层与 mappings | M3 | link tree（linkMode）/ cache / `cache clean` / mappings 生成与校验 |
| M5 | verify | M4 | 三级检查 / driftKind / 退出码 / `--offline`·`--deep`·`--json` |
| M6 | adapter 与构建命令 | M1 | esbuild adapter / `build`·`typecheck`·`css` / `engines` |
| M7 | 端到端验收与发布 | M5+M6 | 三平台 CI / 四问实测 / 发布产物 |

### v0.2（计划：[v0.2-plan.md](./v0.2-plan.md)）

供应链防护完整：OSV + audit、策略引擎（minimumReleaseAge / 白名单 / postInstallPolicy）、why·tree·outdated、tsc·deno·postcss adapter、install 的 `--frozen-lockfile` / `--offline` CI 模式。

### v0.3（计划：[v0.3-plan.md](./v0.3-plan.md)）

引擎生态与协议：wasm adapter（`remote` 经 [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布）、
集成脚手架（Vite / esbuild / Deno / Webpack + tsconfig paths）、Deno sandbox、权限与凭证、mappings 子路径扩展。

### v0.4（计划：[v0.4-plan.md](./v0.4-plan.md)）

验证与收敛：把上一版留下来的开放问题收敛掉，并把"人工纪律"换成机器检查——
`ngm typedecl` 给 `typeDecl` 一个入口（真 tsc 验收）、`verify --signatures` / `--require-signed`
（[ADR-014](../adr/adr-014-self-report-signatures.md)）、配置字段接线的机械检查。

### v0.5（复核：[v0.5-review.md](./v0.5-review.md) / 计划：[v0.5-plan.md](./v0.5-plan.md) / **复盘：[v0.5-retrospective.md](./v0.5-retrospective.md)**）

**已交付**（唯一的残项见下）。**范围**：把三件"已经量出来但仍挂着"的事推到结论——
在线 verify 的成本与方差（含两条需先立 ADR 的改动）、ADR-013 翻案条件的**判定**、
权限施加点的机械核对；外加一项代码之外的交付：**v0.2 ~ v0.4 补发布**。

**三条与预期相反或需特别注意的结论**（详读复盘）：

1. **在线 verify 的 3s 目标线在今天的机器上不可判别**——受控对照（未优化）最大 2.433s 也过线，
   而跨机器状态的方差（≥0.3s）大于本轮收益（0.45s）。因此纪律改为**次数优先于秒数**。
2. **跨机器可复现性已实测**（3 平台 × 7 形态字节一致），但**翻案条件成立不等于要发布
   `remote` adapter**——那是一个需要另立 ADR 的新决策。
3. **`read:` / `write:` 裁决为"不在 ngm 自身路径上施加"**——这是裁决，不是遗漏。

**当时的残项（已办）**：Gitee 侧 21 个附件曾待人工上传——那次补传于 **2026-10-02**（v0.6 期间）完成：
21 个附件逐个从 Gitee 取回复核 sha256，两源 `SHA256SUMS` 逐字节相同，
**"国内推荐源取不到"这个问题不再存在**（见[发布清单](./README.md#补发记录2026-10-01)）。

开工前复核已完成，并**继续做掉了它自己列出的未做项**：

- 配置字段接线核对：新查出 4 项确认未接线 + 3 项弱接线并逐项处置，首次运行又多点出 3 项"已接线、没断言"
- 性能回归复测：**在线 verify 贴着 3s 目标线**（3 次采样里 1 次超过），"单次读数达标"的写法已删除
- 文档与实现一致性：逐页核对，修掉 4 类失效陈述 + 2 处引用了不存在/已失效东西的用户可见提示
- 安全模型：沙箱在**真实 Deno 1.45.2 与 2.4.0** 上重测，修掉 4 处**假失败夹具**与 1 处弱断言
  （Deno 2 把权限错误的类名从 `PermissionDenied` 改成 `NotCapable`，而 CI 当时钉在 1.x）
- 挂账两项：内部并行哈希**已做**（两个可复现基准）；deno **`typeCheck` 已覆盖**、`bundle` 仍等上游
- `ngm transform`：补上最后一处"有能力、无入口"，并修掉一处会**静默丢弃** `--format` / `--minify` /
  `--sourcemap` 的协议缺陷

### v0.6（计划：[v0.6-plan.md](./v0.6-plan.md) / **复盘：[v0.6-retrospective.md](./v0.6-retrospective.md)**）

**已交付**（两处未完成都不是工程：缺凭据、读不到 CI）。

v0.5 把挂着的事推到了结论，但其中三处是"**测了，但判不了或数据不够**"：3s 目标在噪声里
不可判别、可复现性只证到合成夹具、GC 缺磁盘数据。**v0.6 补的是可判别性**：

| 组 | 内容 | 状态 |
|----|------|------|
| A | 真实项目形态的可复现证据（[ADR-017](../adr/adr-017-remote-adapter-release-decision.md) 门槛 a） | ✅ **已交付**：本机 20 轮全稳定 + **跨机器三平台同值**（`treeA=124f03b1de58091a`）→ **门槛 a 成立** |
| B | **spawn 预算**：把"次数优先于秒数"变成 CI 门禁 | ✅ **已交付**（开门第一天抓到一次**记账错误**：v0.5 C 组的重复记账让数字虚高 1/依赖） |
| C | store / mirror 的磁盘增长数据（只测不做，为 GC 排期提供数据） | ✅ **已交付**（**160.2 KiB/commit**，是源码真实增量的 **20×**；结论：做 GC，但下一步是 ADR 而不是代码） |
| D | Gitee 附件补传（把手工步骤降到一条命令） | ✅ **已完成**（2026-10-02：三版各 **7/7**，两源 `SHA256SUMS` 逐字节相同、抽查二进制与清单一致） |
| E | 挂账：`deno bundle` 等上游 | ✅ 已核查（条件未变；**间接证据**，见[复盘](./v0.6-retrospective.md) §1.5） |

> 本版**先行完成**的一项：[ADR-017](../adr/adr-017-remote-adapter-release-decision.md)
> —— remote adapter 的发布决策（结论仍不发布，但把剩下的问题写成可判定的门槛）。
> 收尾时又补上 [ADR-018](../adr/adr-018-store-reclaim.md)
> ——内容寻址 store 的回收与去重（**不做**按可达性自动删除；写入侧去重是长期解法）。

### v0.11（计划：[v0.11-plan.md](./v0.11-plan.md) / 复盘：[v0.11-retrospective.md](./v0.11-retrospective.md)）— **已交付**

**把挂着的决策结掉，并第一次真正回收**：v0.10 之后剩下的事几乎全是"等决策"——
数据都已就位，缺的是拍板。这一版把四件拍完（三件写 ADR、一件写 ADR **并实现**），
再补一处"改坏了会静默失效"的门禁，最后**当一次用户**走完整条路。

| 组 | 内容 | 状态 |
|----|------|------|
| A | 四项决策结项：[ADR-020](../adr/adr-020-remote-adapter-shelved.md)（`remote` 搁置——它在代码里**不存在**）· [ADR-021](../adr/adr-021-symlink-link-mode.md)（symlink 保留降级）· [ADR-022](../adr/adr-022-verify-performance-target.md)（3s 降级为观测值）· [ADR-023](../adr/adr-023-orphan-reclaim.md)（**回收孤儿，含实现**） | ✅ **已交付** |
| A4 | `ngm store prune --orphans [--older-than] [--dry-run]`：层 2 第一次真回收。判据单点（`scanManifests` 被 `usage` 与回收**共用**）+ 年龄门槛 + **有清单读不出来时拒绝删除** | ✅ **已交付**（实测：回收 21 B 后 `verify` 仍 `1 ok`） |
| B | workflow 的 **YAML 有效性门禁**（CI job `workflow-lint`，判据用真实解析器 js-yaml） | ✅ **已交付** |
| C | 端到端可用性复核（干净环境 + 真实 git + 真实 esbuild） | ✅ **已交付**——6 步闭环全通，**抓到 6 个问题**（C 组 3 个：1 已知限制 / 2 已修；全量测试 3 个：`WaitDelay` 三处同缺、不 hermetic 的测试、CI 超时把"慢"读成"坏"） |

> **本版最值得记住的两条**：
> ① **ADR-023 的洞察可迁移**——当一条设计卡在"缺项目注册表"时，先问有没有
> **更弱但由构造可判定**的版本（"无人引用"不需要注册表，而它覆盖了大部分实际收益）；
> ② **C 组抓到的三个问题，在锚点/链接/单测/门禁全绿时依然存在**——
> 内部一致性检查证明不了"对外真的能用"（详见[复盘 §5.1](./v0.11-retrospective.md)）。

### v0.10（计划：[v0.10-plan.md](./v0.10-plan.md) / 复盘：[v0.10-retrospective.md](./v0.10-retrospective.md)）— **已交付**

**把检查推到最外圈**：v0.9 让 store 的读数说了真话；这一版补两个**没人看的角落**——
最外圈的文档（根 `README.md`）与用户真会遇到的一种失败（store 不完整）。

| 组 | 内容 | 状态 |
|----|------|------|
| A | "store 里少了一个 blob"时的报错与残骸行为 | ✅ **已交付**（两条承诺钉进测试：点名条目 + 给出路；残骸必须被 `verify` 认出） |
| B | 检查边界：根 `README.md` 的链接与锚点 | ✅ **已交付**（显式根缺失 = 失败；53 个锚点；双向牙齿测试） |

### v0.9（计划：[v0.9-plan.md](./v0.9-plan.md) / 复盘：[v0.9-retrospective.md](./v0.9-retrospective.md)）— **已交付**

**让 store 的读数说真话**：v0.8 换了布局之后，这一层的数字开始需要解释
（逻辑体积 / 共享 / 独占是三个不同的东西），而"能不能回收"至今没有数据。

| 组 | 内容 | 状态 |
|----|------|------|
| A | `store usage` 把 blob 池切成**共享 / 独占 / 孤儿**，每棵树报"丢掉它能回收多少" | ✅ **已交付**（断言是不变量，已验证有牙齿：把共享算进独占立刻红） |
| B | 锚点检查（`[label](x.md#anchor)` 的锚点此前**无人校验**） | ✅ **已交付**（Go 测试，46 个锚点全验；放回死锚点即红并给出正确答案） |
| C | blob 池的规模（10 万级，**只测不做**） | ✅ **已交付**（10 万 blob：单目录 341~437 文件、`usage` 244ms） |

### v0.8（计划：[v0.8-plan.md](./v0.8-plan.md)）— **已交付**

层 2 换布局（[ADR-019](../adr/adr-019-content-addressed-blobs.md)：blob 池 + 树清单）分三阶段落地。
顺序之所以是"先解耦、再换布局"：`verify` 是安全关键路径，它的偏差都是"看起来通过"，
不能与布局改动同时发生。

| 阶段 | 内容 | 状态 |
|------|------|------|
| A | 消费方与布局解耦（`Entries` + `compareTrees` + `VerifyVendorAgainstEntries`） | ✅ 等价重构，既有测试一字未改全部通过 |
| B | `Put` 写 v2（blobs + manifest），读路径 v2 优先、回落 v1 | ✅ 含双布局验收测试 |
| C | 收敛与观测（v1 被重写时回收；`usage` 分别报告两种布局） | ✅ |

**结果**：同一把尺子（12 个 commit、每个只改 1 个文件）**20.0× → 0.6×**
（占用 1.88 MiB → **59.9 KiB**）；那把尺子的断言已翻转为"不得接近 N 棵树"，
于是它同时成了去重的**回归门禁**。

**三处实施时才发现、ADR 没写到的点**（都记进 [ADR-019 §修订](../adr/adr-019-content-addressed-blobs.md#修订v08-实现时发现并改掉的五处)）：

1. **可执行位无法只靠"内容寻址"承载**——落地层用 hardlink，而 hardlink 共享 inode，
   于是 blob 的身份改为 `H(mode, 内容)`（代价：同内容两种 mode 存两份）。
   少了这一步，`scripts/*.sh` 落地后会**丢可执行位**，而且是静默的。
2. **`symlink` 落地模式在 v2 下没有整棵树可链**——退化为逐条目 hardlink 并如实报告实际 mode。
   这是本版**唯一一处用户可见的行为变化**。
3. **收敛不是自动的**——`install` 在 `Has` 为真时短路（那是"无网络也能安装"的承诺），
   而健康的 v1 条目 `Has` 正是真，所以**旧条目不会被自动迁移**。处置是把出路交给用户
   （`usage` 打印 `rm -rf <root>/sha256 && ngm install`），而不是为了迁移去动那条短路。

### v0.7（计划：[v0.7-plan.md](./v0.7-plan.md)）— **已交付**

范围 = [ADR-018](../adr/adr-018-store-reclaim.md) 决策 2 的落地：让 content store 的
**占用可见**（`ngm store usage`，只读）与**残骸可回收**（`ngm store prune`，只清解包残骸）。
两组均已交付 ✅。

**剩下的候选**（按证据强度排序，详见 [v0.6 复盘 §7](./v0.6-retrospective.md#7-v07-候选按证据强度排序非路线图承诺)）：

| # | 候选 | 前置 |
|---|------|------|
| 1 | ~~层 2 **写入侧去重**（blob 池 + 树清单）~~ | ✅ **v0.8 已交付**（[ADR-019](../adr/adr-019-content-addressed-blobs.md)）：20.0× → 0.6×。**它改的是斜率，不是终点**——回收仍受 ADR-018 那两个条件约束 |
| 2 | `remote` adapter 门槛 b：抽样构建的**成本数字** | **门槛 a 已满足** → 这是"要不要发布"**唯一**剩下的条件 |
| 3 | `deno bundle` 真引擎覆盖 | 上游把 bundle 标为稳定（截至 2026-10-02 未变） |
| 4 | 3s 目标的口径重定义 | 等次数门禁稳定运行几个版本 |

---
