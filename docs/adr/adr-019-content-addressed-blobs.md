# ADR-019：层 2 改为按文件内容寻址（blob 池 + 树清单）

- **状态**：已定（**本 ADR 只定 schema 与迁移方案**；实现单列一个版本）
- **日期**：2026-10-02
- **范围**：vendor 层 2（`~/.ngm/content`）的存储布局
- **关系**：承接 [ADR-018](./adr-018-store-reclaim.md) 决策 3（写入侧去重是长期解法）；
  **不动** [ADR-008](./adr-008-archive-digest.md) 的 digest 定义

> **结论**：层 2 从"每个 digest 一份完整解包树"改为"**blob 池 + 树清单**"：
> 文件内容按 `sha256` 只存一份（`blobs/<aa>/<hex>`），内容树变成一份很小的清单
> （`trees/<digest>/manifest.json`）。
>
> 这把 v0.6 实测的 **20× 放大**从源头消掉（12 个 commit、源码真实增量 96 KiB 的场景下，
> 新增占用从 1.88 MiB 降到**约百 KiB 量级**）。
>
> **但它不改变"只增不减"的性质**——blob 的回收与整棵树的回收是同一个问题，
> 仍受 [ADR-018](./adr-018-store-reclaim.md) 那两个条件约束。**去重改的是斜率，不是终点。**

---

## 背景

v0.6 的实测（`TestV06StoreGrowthInventory`）：一个仓库、12 个 commit、每个只改 1 个文件，
层 2 涨到 **1.88 MiB**，而源码的真实增量只有 96 KiB —— **20.0×**。

原因是**布局**：`content/sha256/<digest>/tree/` 按**整棵树**寻址，同一仓库的每个 commit
都复制一遍全树。层 1（Git 裸仓）没有这个问题，因为 Git 的对象库本身就是**内容寻址**
（同一个 blob 只存一份）。层 2 是唯一一个"复制整棵树"的地方。

[ADR-018](./adr-018-store-reclaim.md) 因此把"写入侧去重"定为长期解法：**GC 是"存了再删"，
去重是"不再重复存"**——对一个只增不减的存储，后者在根源上解决问题，且不依赖
"哪些项目引用了什么"这个 ngm 没有的信息。

---

## 目标 / 非目标

**目标**

1. 同一份字节在层 2 只存一份（跨 commit、跨依赖、跨项目）。
2. `archiveDigest` 的**定义与取值不变**（ADR-008 不动）——它仍是"对整棵树的规范化哈希"，
   用户侧的 lock 与 `verify` 语义一字不改。
3. 读路径对旧布局**保持兼容**：没人需要在升级后立刻重装。
4. 落地（link）层不需要"先物化一棵树"。

**非目标**

- 不做 blob 的回收（见 ADR-018）。
- 不改层 1（mirror）、不改 lock、不改 digest 算法。
- 不做跨机器的 blob 共享（那是 `remote` 的问题，见 ADR-013/017）。

---

## 布局（schema v2）

```
~/.ngm/content/
├── schema                      # 新写路径使用的布局版本（"2"）；缺失即按 v1 处理
├── blobs/<aa>/<sha256-hex>     # 文件内容，只存一份（aa = hex 前两位，避免单目录过大）
├── trees/<digest-hex>/{manifest.json, meta.json}
└── sha256/<digest-hex>/tree/   # v1（旧）遗留：只读，不再新增
```

### `trees/<digest>/manifest.json`

```json
{
  "schemaVersion": 2,
  "manifestVersion": "ngm-archive-digest/v1",
  "digest": "sha256:<hex>",
  "entries": [
    { "path": "src/index.ts", "mode": "100644", "sha": "<blob-hex>", "size": 1234 },
    { "path": "scripts/run.sh", "mode": "100755", "sha": "<blob-hex>", "size": 42 },
    { "path": "link.txt", "mode": "120000", "sha": "<blob-hex>", "size": 7 }
  ]
}
```

- **entries 按 path 升序**：清单本身必须是确定的（可 diff、可快照），否则又会出现
  "两次构建字节不同"这类假失败（v0.5 的 metafile 事件就是这么来的）。
- `mode` 沿用 Git 的 100644 / 100755 / 120000（与 digest 层的口径一致）。
- `sha` 是 blob 内容的 sha256；symlink 的 blob 内容就是**链接目标字符串**（与 digest 层一致）。

### `meta.json`

沿用现有定义（repo / commit / digest / subPath），不新增字段。

---

## 迁移方案：**不就地迁移**

层 2 是**派生物**（层 1 才是权威，任何时刻可从 mirror 重放），因此不需要"把旧树拆成 blob"
这种就地转换——那是把一次可重建的操作变成一次不可逆的大改。

分三步：

1. **写路径只写 v2**：新的 `Put` 解包时，逐文件把内容写入 `blobs/`（临时文件 + 原子 rename），
   再写 `manifest.json` + `meta.json`。同一 blob 已存在则跳过（`AlreadyPresent` 语义不变）。
2. **读路径兼容 v1 与 v2**（关键）：`Has` / 读内容 / `verify` 的层 2 检查两条都认——
   先按 v2 找 `trees/<digest>/manifest.json`，没有再回落到 v1 的 `sha256/<digest>/tree/`。
   否则升级后旧项目会**立刻**验不过，而那正是本项目最忌的失败形态。
3. **收敛**：某个 digest 以 v2 写成功后，**删除同 digest 的 v1 目录**（内容已等价）——
   空间在它被重新安装时逐个回收。
   剩下"没人再装过"的 v1 残留：由 `ngm store usage` **单列一行**报出，
   清理路径是手工 `rm -rf ~/.ngm/content/sha256`（整层派生，删了可重建）——
   **不为此新增一个会删除内容的命令**（ADR-018 决策 5）。

> 为什么不做"一次性转换脚本"：它要遍历所有树、重写所有文件，中途失败会留下
> 半转换的 store；而"用到才写 v2"是**增量且可回退**的。真要一次到位，
> `rm -rf ~/.ngm/content` 然后重装就是一次干净的重建——代价明确、无中间态。

---

## 落地层（link）怎么改

这是本方案**最大的实现代价**，写在这里以免被低估：

- v1：vendor 直接 hardlink / copy 到 `content/sha256/<digest>/tree/` 这棵已物化的树。
- v2：**没有物化树**，所以 `LinkTree` 要按 `manifest.json` 逐条目工作——
  建目录、把 `blobs/<sha>` link（或 copy）到目标路径、按 `mode` chmod、
  `120000` 则创建 symlink（目标从 blob 内容读出）。

好处是：层 2 不再需要"一份已展开的树"，**磁盘占用再降一档**；
代价是 link 层从"整棵树"变成"逐条目"，要处理部分失败与清理。

---

## 代价与风险（如实列出）

| 项 | 说明 |
|----|------|
| 实现面 | `Put` 重写、`Has` / 读路径双支持、`LinkTree` 逐条目、`verify` 层 2 检查按清单重放、`store usage` 统计两种布局 |
| 回退 | 读路径兼容让"停在任何中间状态"都不致命；真要回退，删掉 `schema` 文件即回到只读 v1 + 写 v1？——**不行**：本 ADR 定的是单向升级（写路径只写 v2）。回退路径是"重装"（层 2 是派生物） |
| blob 也只增不减 | 去重**没有**解决回收：blob 的删除同样需要 ADR-018 的那两个条件。它把 20× 降到 ~1×，但曲线仍单调上升 |
| 并发 | 多进程同时写同一 blob → 用临时文件 + 原子 rename；读到"正在写"的 blob 只能由 `manifest` 存在性保证（清单先写、blob 先落盘） |
| 磁盘目录规模 | 大项目 blob 数可达十万级 → 用**两位 hex 分片**（`blobs/<aa>/`）避免单目录过大 |

---

## 决策

1. **采纳 v2 布局**（blob 池 + 树清单），digest 定义不变。
2. **迁移 = 不就地迁移**：写路径只写 v2、读路径兼容 v1、v1 在被重写时逐个回收；
   残留由 `store usage` 报出，清理走手工整层删除。
3. **实现单列一个版本**（不在 v0.7 内做）：本 ADR 只交付 schema、迁移与代价清单。
   实施前先出一份实施计划（含"部分失败的清理"与"双布局的验收测试"）。
4. **不新增任何删除真实内容的命令**（沿用 ADR-018 决策 5）。

---

## 什么条件下重新考虑

- 若实施中发现"逐条目 link"在 Windows（hardlink 不可用时降级为 copy）上的代价
  **超过**去重带来的收益，应先做一次实测再决定——**收益是可以量的**（同一份
  `TestV06StoreGrowthInventory` 就是尺子：12 个 commit 后应当从 1.88 MiB 降到百 KiB 量级）。
- 若 blob 数量增长带来新的问题（目录规模、inode），再评估是否需要合并/分片策略。

---

## 相关文档

- [ADR-018 内容寻址 store 的回收与去重](./adr-018-store-reclaim.md)（本 ADR 承接其决策 3）
- [ADR-008 archiveDigest 的定义](./adr-008-archive-digest.md)（本 ADR 不动它）
- [ADR-003 为什么纯 vendor 目录](./adr-003-vendor.md) · [vendor 层模型](../architecture/vendor-layers.md)
- [metrics · 磁盘增长](../internals/metrics.md#磁盘增长内容寻址-storev06)（20× 的实测依据）
- [v0.7 计划](../development/v0.7-plan.md)（本 ADR 的实施被明确排除在该版之外）
