# 大删除记录

> 本次清理把 242 篇历史文档移出工作树，保留文档按当前代码重写。每条记录写全六字段；
> 路径以历史目录/通配符表示，历史正文用 git 查，不在活文档里复制读数。

## 旧 ADR 单篇

- 删除：`docs/adr/adr-*.md`（ADR-001 ~ ADR-058 的单篇正文）
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：单篇是决策当时的快照，结论分散且互相链接；改一次结论要同步多处，读者也难判断哪篇是当前口径。
- 影响：`docs/adr/README.md` 改为唯一汇总表，三列 `ADR-NNN / 决策 / 结论`，编号连续；架构文档里的单篇链接改指汇总表。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 竞品分析档案

- 删除：`docs/COMPETITIVE-ANALYSIS*`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：竞品逐项明细与性能读数是某一时刻的快照，随时间变旧且误导读者；活文档只保留分界结论。
- 影响：架构文档不再引用竞品明细，能力归属以 `docs/internals/capability-matrix.md` 为准。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 分析报告目录

- 删除：`docs/analysis/`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：全量评估、差距分析、特性盘点、生态路线等报告是带日期的一次性读数，混在活文档里会被当成当前事实。
- 影响：`docs/README.md` 移除对应索引项；架构文档只写当前代码的数据流、状态、失败语义与边界。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 规格与任务快照

- 删除：`docs/specs/`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：按评估 Follow-up 生成的 spec / tasks / checklist 已完成历史使命，留着会让读者误以为仍在待办。
- 影响：开发入口只保留 `docs/DEVELOPMENT.md` 的当代表；判据继续从文档与源码派生，不依赖旧 spec。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 逐版计划与复盘

- 删除：`docs/development/v0.*-plan.md`、`docs/development/v0.*-retrospective.md`、`docs/development/v0.*-review.md`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：v0.1 ~ v0.101 的计划、复盘与复核是版本叙事，数量大且读数过期；重写目标明确排除历史叙事与版本回顾。
- 影响：`docs/development/README.md` 改为开发与验收口径的索引，历史用 git log 查；不再保留逐版链接。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 开发线台账与阶段表

- 删除：`docs/development/ledger*.md`、`docs/development/stages*.md`、`docs/development/acceptance*.md`、`docs/development/*-plan.md`、`docs/development/ci-red-*.md`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：大表与专项台账是历史抄本，维护成本高且与当前代码无对照关系。
- 影响：验收口径收敛到 `docs/DEVELOPMENT.md` 的本地命令与判据纪律；能力状态收敛到能力矩阵。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## internals 历史评估

- 删除：`docs/internals/assessment-*.md`、`docs/internals/*-assessment.md`、`docs/internals/progress*.md`、`docs/internals/*state*.md`、`docs/internals/*roadmap*.md`、`docs/internals/*metrics*.md`、`docs/internals/*fixtures*.md`、`docs/internals/*-paths.md`、`docs/internals/*audit*.md`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：评估、路线、指标与进度快照是过去某一天的读数，出现在活文档目录会被当成当前状态。
- 影响：`docs/internals/` 只保留能力矩阵；`docs/README.md` 与架构文档的相关链接同步改为能力矩阵或 git 历史。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 方法论与案例

- 删除：`docs/practice/`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：工程纪律方法论与真实案例是跨项目的方法论沉淀，不属于本项目"用了什么、怎么开发"的活文档。
- 影响：判据纪律要点搬进 `docs/DEVELOPMENT.md`，其余结论以 ADR 汇总表为准，不保留旧目录链接。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 模块分解档案

- 删除：`docs/modules/`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：按 P0 ~ P8 拆分的模块文档描述的是早期规划结构，与当前 `cmd/ngm` 和 `internal/` 的组织不再一一对应。
- 影响：包结构事实以源码为准，开发者入口说明 `cmd/ngm` 与 `internal/core` 的职责，不引用旧模块文档。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 里程碑账本

- 删除：`docs/milestones*.md`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：逐版里程碑与发布状态表是读数快照，包含大量已过期的 tag / release 计数。
- 影响：文档索引删除对应条目与 release 计数快照；不再维护任何版本计数，历史用 git 查。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿

## 指南中的过期单篇

- 删除：`docs/guides/*library-standard*.md`、`docs/guides/node-vs-*.md`、`docs/guides/release-*.md`
- 日期：2026-11-24
- 批准：用户明确授权（历史 commit 保留）
- 为什么：这三篇分别依赖已移除的标准检查、已合并的运行时选型叙事与已过期的发布流水线读数。
- 影响：`docs/README.md` 只列保留的指南文件；不保留指向这三篇的链接。
- 验证：git log --oneline -- docs/ 可查历史 + go test 全绿
