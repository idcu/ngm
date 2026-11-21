# Tasks：2026-11-21 功能梳理与差距评估之后

来源：[spec.md](./spec.md) · [功能现状梳理](../../analysis/ngm-feature-inventory-2026-11-21.md) ·
[全维度差距评估](../../analysis/ngm-competitive-gap-full-2026-11-21.md)。
基线：`1dbbbe3`。状态：**全部 9 项未开始（仅规划，未授权执行）**。
每项完成标准 = 文档/案例改动 + 会红的判据 + 变异检验结果；纯文案改动也要有判据。

## P0 · 读数纠偏（2 项，无产品代码）

- [ ] **N1 三项“无对应物”措辞纠偏**（`docs/COMPETITIVE-ANALYSIS.md:204-212`）
  - 现状：§“诚实结论”写“refType 必填、ref 漂移三态、按 commit 查 OSV 三项在主流工具里仍无对应物”，
    与[差距评估 §5.2](../../analysis/ngm-competitive-gap-full-2026-11-21.md#52-三项无对应物断言的当前有效性)冲突：
    内容摘要单项不再独有（Bun 1.4 记 SHA-512、Cargo 逐文件 checksum），
    三态只部分成立（git-tag-guardian、pnpm/Deno no-downgrade）。
  - 动作：改为“唯一把这些控制点收束为**默认、可离线重放的同一状态机**”，
    加 2026-11 时间限定；保留 refType 基本成立、三态部分成立的细分判断。
  - 判据 V156（新）：`TestV156CompetitiveClaimsDoNotAssertThreeUniques`——
    文档不得出现“三项在主流工具里仍无对应物”，必须出现“同一状态机”与“内容摘要不再独有”意思的句子。
  - 变异：把纠偏后的句子改回旧断言 → 红；还原 → 绿。

- [ ] **N2 SLSA 段补 provenance 覆盖率证据**（`docs/COMPETITIVE-ANALYSIS.md:198-207`）
  - 动作：补 2026-10/11 实测读数（npm top50 约 **12%**；另一口径 **38.8%**），
    保留“provenance 采用率低 ⇒ 生态做法是告警而非拒绝”的结论，不改“ngm 是 L2 的一半”判断。
  - 判据 V157（新）：`TestV157SLSAParagraphCarriesProvenanceAdoptionReading`——
    段落必须同时含两个覆盖率数字与“告警而非拒绝”的措辞。
  - 变异：删掉任一数字 → 红；还原 → 绿。

## P1 · 闭环可复核证据（1 项，文档 + 判据）

- [ ] **N3 真实消费方端到端案例**（新文件：`docs/practice/ngm-consumer-case-study-2026-11-21.md`）
  - 内容：从 `init` → `add` → `install --frozen-lockfile --offline` → `verify --content` →
    `sbom` / `attest` → 现场 cosign 签名验证；每一步写明**证明什么、不证明什么**。
  - 必含注入演示：改动 `ngm.vendor/` 内一个字节后 `verify --content` 退 **2**，
    再恢复后退 0。这个演示是案例的核心，不是附加装饰。
  - 边界：不新增 ngm 自签命令、不固化第二份 cosign smoke 脚本；签名步骤只引用
    [signing.md](../../guides/signing.md) 的 CI 配方（延续 C1 决定）。
  - 判据 V158（新）：`TestV158ConsumerCaseStudyDemonstratesTheWholeLoop`——
    案例文件存在；命令链按序出现；含“篡改 → exit 2”的注入段；引用 signing.md；无自签命令。
  - 变异：删注入段 → 红；把 `verify --content` 换成默认 `verify` → 红；还原 → 绿。

## P2 · 采用门槛（2 项，人工/外部条件，不自动执行）

- [ ] **N4 仓库元数据**（GitHub 网页操作）
  - 任务：填写仓库 description 与 topics；许可证已是 MIT，不在本项范围。
  - 验收：网页上可见；agent 不代做，也不为它写源码判据。
  - 状态：**需要仓库所有者**；执行前不改动任何文件。

- [ ] **N5 发布回填（保持暂缓）**
  - 事实：38 个缺口是 `release-pipeline.md` 明确**不回填**的区间；GitHub 维持 63；
    当前可做的只有 Gitee 侧 3+1 个（需 `GITEE_TOKEN`）。
  - 恢复条件：见 [project-state.md §3.2](../../internals/project-state.md#32-阻塞需要人) 的两条恢复信号，
    或 token 到位后退化为机械动作。
  - 本轮动作：**只记录“暂缓 + 原因”，不排期、不执行、不新建脚本**；
    已有 `scripts/check-release-status.sh --published` 作只读门禁。

## P3 · 观察项（4 项，不承诺、不执行）

- [ ] **N6 OSV commit + 版本双查**：观察，不进路线图。触发条件：OSV 对 Git commit 覆盖显著改善，
  或出现真实漏报案例。补法需要 registry 输入，不在 ADR-044 主轴上。
- [ ] **N7 Git 非默认端口支持**：观察。触发条件：真实项目提出需求；修它要给 `Canonical` 加字段，
  是 schema 级变更，不因“别的工具支持”而启动。
- [ ] **N8 全局可达性 GC**：观察。ADR-018 明确拒绝自动全局删除；触发条件：出现项目注册表等新增事实源。
- [ ] **N9 季度端到端复核**：观察。若用户明确要求自动化，再**单独**创建定时自动化；本 spec 不擅自建。

## 全批通用收口

- [ ] 每项先判据后修改，变异结果记入本文件对应条目；不能只写“文档已更新”。
- [ ] 批次顺序：P0 → P1 → P2（人工项只记录状态）→ P3（只记录触发条件）。
- [ ] 完成后跑 `docs-links` / `discipline 13/13` / `diff-check` / `TestV114`，同步 docs/README 入口与 spec 实施补记。
- [ ] 不新增 ADR；若实施中出现源码事实推翻本计划，以“实施改判”小节记录，不重写 spec 正文。
