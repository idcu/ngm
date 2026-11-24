# ADR-055：保守的 v1/v2→v3 迁移与只读的 digest 反查

- **状态**：已定（2026-11-24）
- **范围**：`ngm migrate`（项目清单 + lock）；`ngm why --digest`
- **依据**：[竞品全维度差距评估 2026-11-24 §6 P1 前两项](../analysis/ngm-competitive-gap-full-2026-11-24.md)
- **与 ADR-052 的关系**：ADR-052 §2.4 写的是"仍不提供迁移命令"。本 ADR 是该条目的
  **显式修订**：提供一个只搬可证明字段的保守命令；"不猜"的原则不变，
  变的只是把"删掉 lock 重装"这件事拆成可审计的半自动路径。

## 结论

1. **migrate 只搬能证明的字段。** v1/v2 清单 → `schemaVersion 3`、2.x lock →
   `lockfileVersion 3.0.0`。能确定映射的字段原样保留：`name` / `ref` / `refType` /
   `commit` / `archiveDigest` / `moduleDigest` / `subtreeOid` / `version` /
   `resolvedAt` / `vendorPath` / `subPath` / `requiredBy`；清单侧的
   `name` / `version` / `runtime` / `main` / `types` / `publish` / `dependencies` /
   `engines` / `vendor` / `supplyChain` / `scripts` 同名保留。
2. **digest 缺失必须人工重装。** 任何条目只要内容锚点不全、形状不合法或语义发生变化，
   就进入结构化的 `skipped[]`（`file` / `index` / `entry` / `reason` / `fix`）；
   **整次迁移一个字节都不写**，包括其它文件的备份，退出 3。命令不补摘要、不缩短哈希、
   不把模块条目的 `archiveDigest` 当模块摘要用。
3. **digest 反查是只读审计面。** `ngm why --digest=<sha256[:]…>` 只扫 `ngm.lock`，
   回答"哪个条目 / subPath / requiredBy 引用了这个内容摘要"；不联网、不起 git 子进程、
   不写文件，也不产生新的信任断言。

## 决策

### 1. migrate 的可证明边界

lock 条目按 v2 规范分流：

- 根条目（`subPath` 为空）：必须有完整 `commit`（40 hex）与完整 `sha256:archiveDigest`；
  同时带 `moduleDigest` / `subtreeOid` 的 v2 过渡态形状一律 skipped。
- 模块条目：必须有完整 `moduleDigest` 与合法 `subtreeOid`，且**不得**带 `archiveDigest`
  （已废止的过渡态）；两摘要并存的条目语义歧义，skipped。
- `vendorPath` / `subPath` 必须仍通过格式层的安全路径判定；`requiredBy` 原样保留
  （缺失时按既有契约归一为 `[]`，表示"图未知"，不伪造成空图）。
- 出现 v3-only 的本地来源字段（`sourcePath` / `localRepo` / `dirDigest`）或本地 `refType`，
  说明来源语义已经变化，2.x lock 无法证明，skipped。
- 未知字段整份拒绝：迁移器不替用户决定未知字段的 v3 含义。

清单侧只改 `schemaVersion` 并原样携带同名字段；本地来源字段、未知字段、非法
`name` / `ref` / `refType` / `path` 逐条 skipped，不推断、不补齐。

### 2. 写文件的安全性

- 支持 `--dir` 与 `--dry-run`。dry-run 只输出将发生的变更、备份路径与 skipped，
  不写文件也不创建备份。
- 写前把原文件复制到 `<file>.bak`，并在人读/JSON 两份输出里给出备份路径；
  写锁使用原子写，且先校验生成的 v3 lock 可读。
- 目标已是 v3 时明确输出"无需迁移"并退出 0（幂等）；manifest 与 lock 可以分别迁移
  （一份已 v3 时只处理另一份）。
- skipped 为空才写；否则报告并退出 3，提示逐条重装后重试。

### 3. digest 反查的只读契约

- 输入可为裸 hex 或 `sha256:<hex>`，至少 12 位 hex；更短或不合法的值按 errs 契约
  返回 `ConfigInvalid`（exit 3）并带可操作 hint。
- 对每个条目取其 `ContentDigest()`：`local-dir` 看 `dirDigest`，模块条目看
  `moduleDigest`，根条目看 `archiveDigest`。输出条目的 `name` / `key` / `subPath` /
  `digest` / `digestField` / `requiredBy`。
- 报告同时给出 `matchCount` 与 `distinctDigests`；**前缀**查询命中多个不同摘要时
  `ambiguous=true`，人读输出明说"前缀歧义: 是"。精确匹配不谈歧义；无匹配输出"无匹配"。
- 四种结果都退出 0（查询完成即成功）；只有查询本身非法才退出 3。
  与 `--json` 契约兼容：同一份报告字段，2 空格缩进、末尾换行。

## 理由与代价

- 不把 digest 当作可计算字段：它绑定的是内容，v2 lock 只记录旧规范下的摘要，
  重新拼一个 v3 摘要等于凭空背书；人工重装是唯一诚实的路径。
- 迁移器只处理"格式确定"的版本区间（清单 1/2、lock 2.x）；更旧/更新的文件明确拒绝，
  避免把未知语义拖进 v3。
- digest 反查刻意不接入 `resolve` / `mirror`：它是内容摘要的索引查询，
  不是为了回答引用漂移或漏洞——那两类问题各有 `verify` / `audit` 的既有路径。

## 判据

- V215：最小 v2 lock + 清单可迁移；缺 digest 条目 skipped 且不写坏文件；
  dry-run 不改文件；已 v3 幂等。
- V216：精确匹配、前缀唯一、前缀歧义、无匹配四条路径；`--json` 同报告；
  非法输入走 errs + hint。
