# ADR-010：在线 verify 的取数策略——ref 问远端、对象按需取

- **状态**：已定
- **日期**：2026-09-30
- **范围**：`ngm verify` **在线**模式（非 `--offline`）的事实源与网络访问次数

> **结论**：在线 `verify` 不再"先 fetch、再在本地 mirror 上判定"，改为
> **ref 问远端（一次 `git ls-remote`，不传对象）**，**对象按需才取**（仅在需要判定
> 漂移性质或需要重放 digest 时 `git fetch`）。`--offline` 语义与全部判定/退出码不变。
>
> **代价**：不再是"事实源单一"——ref 与对象可能来自不同时刻；本地缺 commit 也
> 不再等价于"上游丢弃了它"，必须 fetch 后才能下那个结论（见「诚实的代价」）。

---

## 背景

v0.1 复盘已测量：`verify` 的成本**几乎全在 git 子进程启动**，不是哈希或 I/O。
100 依赖规模下，每个依赖有 4 次左右的 spawn，其中在**在线**模式下包含：

| 步骤 | 网络行为 | 本机实测 |
|------|---------|---------|
| `EnsureMirror`（mirror 已存在） | `git fetch --prune` | ~112ms |
| `checkRef` | `git ls-remote`（**本地 mirror**） | ~65ms |

关键事实：**常见情形是"什么都没变"**（CI 的多数运行）。此时 fetch 的唯一作用，
是把"ref 仍然指向 Locked 的那个 commit"这个**已经在远端成立**的事实搬到本地——
而判定 ref 漂移只需要 ref 广播，不需要对象。

---

## 决策

1. **在线 `verify` 的 ref 事实源改为远端**：用一次 `git ls-remote <remote>` 取回
   `refs/*` 广播，据此重新解析 `refType → commit`。
   - 它比"刷新后的本地 mirror"**更新**（mirror 的 freshness 上限就是上一次 fetch），
     因此这不是用新鲜度换速度。
2. **对象按需取**（`git fetch` 只在两种情况下发生）：
   - **ref 已变**：要判定漂移性质（branch fast-forward vs history rewrite）需要祖先关系，
     而祖先关系需要对象；
   - **Locked 的 commit 不在本地**：digest 重放需要它。
3. **`--offline` 完全不变**：仍以本地 mirror 快照为事实源，并标记 `stale`。
4. **判定与退出码不变**：三级检查、`driftKind`、`0/1/2/3/4` 契约一字不改。

---

## 理由

**支持点**

- 常见路径的 spawn 从"1 fetch + 1 ls-remote"降为"1 ls-remote"，且后者本就要做
- 事实源没有变松：`ls-remote` 是远端自己说的，比本地镜像更接近事实
- `--offline` 与在线仍共用一个验证内核，只有"ref 从哪来"这一处注入不同

**诚实的代价**

- **"上游丢弃了该提交"需要证据**。现状的推断是：fetch 之后 commit 仍不在 mirror 里
  → 上游删了它。惰性化之后，**本地缺 commit 不再等价于上游丢弃**（可能只是从未取过）。
  因此该结论前必须先 fetch 一次；否则会把"我们没下载"误报成"上游删了"——那是**误判**，
  比慢更严重。这条是本决策最容易写错的地方，实现与测试都必须固定它。
- **ref 与对象可能来自不同时刻**。`ls-remote` 与随后的 `fetch` 之间存在竞态窗口。
  规则：**ref 类判定以 `ls-remote` 为准，内容类判定以 fetch 后的对象为准**；
  两者不一致时取**更严重**的一方（保守失败）。
- **远端不可达时在线 verify 会失败**。与现状一致（现在 fetch 失败也同样失败），
  但提示措辞会从"fetch 失败"变成"无法读取远端 refs"——需要同步文档。
- **不再有"本地镜像已被刷新"这个不变式**，后续若有人假设它存在会踩坑 → 已写入本文档。

**替代方案**

| 方案 | 为何不选 |
|------|---------|
| 保持现状（每次 fetch） | 慢，且常见路径下 fetch 不产生新信息 |
| 只对 `refType=commit` 跳过 `ls-remote` | 收益小，不解决主因（每个依赖仍要 fetch） |
| 完全改用 `ls-remote`、永不 fetch | digest 重放需要对象；这会削掉可证明性，不可接受 |

---

## 后果

- `internal/verify` 的 `Options` 增加两处注入：**远端 ref 解析**与**按需取物**；
  离线路径的注入不变（只认既有 mirror）
- `cmd/ngm/verify.go` 与 `autoverify.go` 需要在非离线时提供"远端 ref 解析"实现
- 文档同步：[observability.md §ngm verify 的网络依赖](../architecture/observability.md)、
  [locking.md](../architecture/locking.md) 中"在线 = 刷新后的 mirror"类表述
- 测试新增一条**行为断言**：ref 未变且对象已在本地时，**不得发生 fetch**
  （探针：删除 mirror 的 `FETCH_HEAD`，若 verify 后它重新出现即说明 fetch 了）

---

## 实测结果（2026-09-30，本机 8 核）

按每依赖省下一次约 112ms 的 fetch 估算为 ~2.1s。实测如下：

| 操作（100 依赖） | 目标 | v0.1 | v0.2（并发后） | 本决策后 |
|------------------|------|------|----------------|----------|
| 在线 verify | < 3s | 18.77s | 3.37s ❌ | **2.51s ✅** |
| offline verify | < 5s | 11.12s | 1.97s ✅ | 2.10s ✅ |

**在线首次达标**（此前差 1.1×）。offline 的 1.97s→2.10s 是运行间波动（同一实现多次
测量落在 1.92–2.10s），不是本决策引入的成本——离线路径不访问远端，其行为未变。

过程中被实测抓出来的一处**自造回归**，一并记录：第一版实现为了判断"要不要取对象"，
在每个依赖上先做一次 `CommitExists` 预检。这让**离线**路径凭空多了一次 spawn
（1.97s → 2.54s），而离线本来就不允许 fetch。改为"缺 commit 时返回可识别的信号、
由上层决定是否取对象"后，成功路径上不再有任何额外 spawn。

> 行为断言（不是只看数字）：`TestV03VerifyDoesNotFetchWhenNothingChanged` 用
> `FETCH_HEAD`（`fetch` 会写、`ls-remote` 不会）作探针，固定"什么都没变时一次都不 fetch"；
> `TestV03VerifyFetchesWhenTheRefMoved` 固定反向——ref 变了必须取对象，因为判性质需要祖先关系。

---

## 相关文档

- [v0.2 复盘 §2.1](../development/v0.2-retrospective.md)（未达标项与本决策的由来）
- [可观测性](../architecture/observability.md)（退出码与 verify 三级检查的唯一事实源）
- [锁定机制](../architecture/locking.md)（install / update / verify 的分工）
- [健康度指标](../internals/metrics.md)（性能目标与实测）
