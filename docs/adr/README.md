# 架构决策记录（ADR）

> ADR 记录"已定的重大决策与理由"。状态流转：`提议` → `已定` → `已废弃` / `被取代`。

---

## 索引

| 编号 | 决策 | 状态 |
|------|------|------|
| [ADR-001](./adr-001-go.md) | 为什么用 Go | 已定（v2 修订） |
| [ADR-002](./adr-002-git-direct.md) | 为什么直接拉 Git 仓库 | 已定 |
| [ADR-003](./adr-003-vendor.md) | 为什么纯 vendor/ 目录 | 已定（v2 修订） |
| [ADR-004](./adr-004-reftype.md) | 为什么声明用 refType 锁定用 commit | 已定 |
| [ADR-005](./adr-005-engine-interface.md) | 为什么引擎统一接口动态选择 | 已定（v2 修订） |
| [ADR-006](./adr-006-lytd-merge.md) | 为什么 lytd 并入 ngm | 已定（v2 修订） |
| [ADR-007](./adr-007-runtime-node-deno.md) | Node 还是 Deno？ | 已定 |
| [ADR-008](./adr-008-archive-digest.md) | archiveDigest 的定义 | 已定 |
| [ADR-009](./adr-009-supply-chain-policy.md) | 供应链策略的执行时机与失败语义 | 已定（v0.2 首个决策） |
| [ADR-010](./adr-010-online-verify-fetch-policy.md) | 在线 verify 的抓取策略 | 已定 |
| [ADR-011](./adr-011-wasm-runtime.md) | wasm adapter 的运行时与 ABI | 已定（v0.3 A 组） |
| [ADR-012](./adr-012-sandbox.md) | 沙箱与执行边界 | 已定（v0.3 D 组之后） |
| [ADR-013](./adr-013-remote-adapter.md) | remote adapter 的信任边界 | 已定（v0.3 A 组，**结论：不发布**） |
| [ADR-014](./adr-014-self-report-signatures.md) | 依赖自述的签名检查 | 已定（v0.4 复核，**结论：不做**；身份交回 Git 的签名机制） |
| [ADR-015](./adr-015-commit-ref-resolution.md) | commit 型依赖不做 ref 解析 | 已定（v0.5 A 组）：commit 型 **0 次 git 子进程**；tag/branch 不变 |
| [ADR-016](./adr-016-mirror-url-local-read.md) | mirror 远端地址改为读 config 文件 | 已定（v0.5 A4）：形状不认识即回退子进程；tag 型 4.00 → **3.00 次/依赖** |
| [ADR-017](./adr-017-remote-adapter-release-decision.md) | remote adapter 的**发布决策** | 已定（v0.6）：**仍不发布**；剩下的问题写成可判定的门槛（修订 [ADR-013](./adr-013-remote-adapter.md)） |
| [ADR-018](./adr-018-store-reclaim.md) | 内容寻址 store 的**回收与去重** | 已定（v0.6）：**不做**按可达性自动删除；`store usage` / `store prune` 排期；**写入侧去重**是长期解法 |
| [ADR-019](./adr-019-content-addressed-blobs.md) | 层 2 改为**按文件内容寻址**（blob 池 + 树清单） | 已定（v0.7）**并已实施（v0.8）**：digest 定义不变、blob 里含 mode（§修订）、`symlink` 落地模式退化；实测 20.0× → 0.6× |
| [ADR-020](./adr-020-remote-adapter-shelved.md) | `remote` adapter **正式搁置** | 已定（2026-10-03）：该 adapter **在代码里不存在**（"发布"= 从零写协议）；三条触发条件（真实需求 / 本地算力证据 / 跨生态标准） |
| [ADR-021](./adr-021-symlink-link-mode.md) | `symlink` 落地模式**保留降级** | 已定（2026-10-03）：不实现"按需物化一棵 hardlink 树"；触发条件是"具体的工具/流程要求 + 实测数字" |
| [ADR-022](./adr-022-verify-performance-target.md) | verify 的性能目标改为**次数门禁 + 秒数观测** | 已定（2026-10-03）：3s 不再是验收目标（v0.5 实测它在噪声里不可判别）；门禁是 spawn 预算 |
| [ADR-023](./adr-023-orphan-reclaim.md) | 回收**无人引用的 blob**（`store prune --orphans`） | 已定**并已实施**（2026-10-03）：只删"没有任何清单引用 + 比门槛更旧"的字节；**有清单读不出来时拒绝删除**；仍不做按可达性删除 |
| [ADR-024](./adr-024-bounded-explanations.md) | 解释输出**必须有界**（`why` 的路径枚举 / `tree` 的展开） | 已定**并已实施**（2026-10-04）：`why` 默认最多 **64** 条路径、`tree` 默认最多 **4096** 个条目；达到上界**必须说出来**（`pathsTruncated` / `entriesTruncated` + 文字里那句），`--all` 显式解除，退出码不变。实测：**41 个节点 → 1 048 576 条路径 / 626 MB**，而图的形状来自上游清单 |

---

## 流程

1. 复制模板，编号为下一个可用编号（`adr-XXX-<slug>.md`）
2. 状态先标 `提议`，讨论定稿后改 `已定`
3. 决策被推翻时：新 ADR 标注"取代 ADR-XXX"，旧 ADR 状态改为 `被取代`，**不删除**

## 模板

```markdown
# ADR-XXX：<决策标题>

- **状态**：提议 / 已定 / 已废弃 / 被取代（by ADR-YYY）
- **日期**：YYYY-MM-DD
- **范围**：<影响的领域>

> **结论**：<一句话，读完就知道选了什么>
> **代价**：<诚实写出这个决策付出了什么>

---

## 背景
<面临的问题与约束>

## 决策
<明确的一句话决策；可与上方"结论"重复，也可更精确>

## 理由
<支持点；以及诚实的代价。若上方"结论/代价"两行已写清，本节可省略>

## 后果
<决策带来的影响与后续约束>

## 相关文档
- <链接>
```

> **为什么把"结论 / 代价"提到最前面**：ADR 的价值是让后来者几秒钟内理解"选了什么、付出什么"。
> **代价必须写**——只列好处的 ADR 是宣传，不是决策记录。

## 约定

- ADR 是**决策快照**，不随实现频繁修改；协议、清单、接口的"活文档"在各自唯一事实源位置（见 [README](../README.md) 的文档约定）
- 与 ADR 冲突的实现必须先修订 ADR，再改代码