# ADR-041：按当前项目 lock 的显式回收（`ngm gc`）

- **状态**：已定并已实施
- **日期**：2026-11-03
- **范围**：层 1（`~/.ngm/mirror`）、层 2（`~/.ngm/content`）、层 3（vendor，含 global vendor 的例外）
- **前置**：[ADR-018 内容寻址 store 的回收与去重](./adr-018-store-reclaim.md)、
  [ADR-019 blob 池](./adr-019-content-addressed-blobs.md)、
  [ADR-023 孤儿回收](./adr-023-orphan-reclaim.md)、
  [ADR-030 mirror 回收](./adr-030-mirror-reclaim.md)

> **结论**：新增 `ngm gc`，以**当前项目的 `ngm.lock`** 为唯一可达性来源，报告 store / mirror /
> vendor 里不被它命名的对象；**默认 dry-run**，只有显式 `--apply` 才删。
> global vendor 条目无法判定是否被其它项目引用，一律**保留并 warning**。
>
> **代价**：层 1/2 是全机共享的，当前项目看不到别的项目的 lock；被回收的 mirror 要在下次
> install 时重新 clone，content 要重新 install。可恢复，但**离线 CI 会失败**——所以它必须
> 是显式、有人盯着的一步，而不是后台定时任务。

---

## 背景

[ADR-018](./adr-018-store-reclaim.md) 明确拒绝了"按可达性自动删除"，理由是 ngm 没有项目
注册表：项目散落在磁盘任意位置，"这个 digest 还有没有人要"无法在本机判定。它同时写下了
重新考虑的条件：

> 若要重新考虑它，先要给出**可达性的来源**（项目注册表，或"GC 必须显式给出要保留的 lock
> 集合"），以及"删错了怎么恢复"的答案。

`ngm gc` 走的是**第二条**：不建注册表，而是让"要保留什么"由**在哪个项目里跑**这件事显式
给出；恢复路径写在命令输出里。

## 决策

1. **默认 dry-run**。`ngm gc` 不带 `--apply` 只打印候选；`--apply` 与 `--dry-run` 同时给出
   是用法错误（exit 3）。方向不能反过来：这个命令会删字节，必须被要求两次。
2. **可达性口径**：lock 里的每一条依赖都在图上，因此"选择集"（间接依赖、monorepo 模块）
   天然包含在内。具体三张集合：
   - store：每个依赖的 `archiveDigest` / `moduleDigest`；
   - mirror：每个依赖 `name` 对应的裸仓库路径；
   - vendor：每个依赖的 `vendorPath`（含 `subPath`）。
   lock 的 `requiredBy` 图不逐边遍历——它只描述"谁引用谁"，不改变"这些条目都要保留"。
3. **vendor 的父目录只是容器**：一个目录只要含有可达路径（按路径分量前缀判断），就保留并
   继续向下看；只有整棵子树都没有可达路径经过时才是候选。少了这条规则，monorepo 的兄弟
   目录会跟着被删。
4. **层 2 的 blob**：只回收"仅被候选树引用"的 blob；仍被可达树引用的共享 blob 不动。
   比 `UnpackResidueGrace`（1 分钟）新的 blob 留下并报出来——`Put` 先写 blob、后发布清单，
   刚写下的字节可能是另一个正在进行的 install。
5. **global vendor 一律保留**：`vendor.mode=global` 时，不可达条目只进 `kept` 列表并给
   warning，不删除。ngm 无法证明别的项目不引用它们；"无法判定时保留、不要猜"。
6. **删除失败汇总**：逐对象记录，其余继续删，最终退出码 4；不中途退出。
7. **不做的事**：不扫描磁盘、不建注册表、不做后台/自动回收、不动 module cache（它的残骸由
   `store prune` 管）。

## 为什么这不推翻 ADR-018

ADR-018 拒绝的形状是"**自动**、**全局**判定、删错了要到几天后才发现"。`ngm gc` 三点都不同：

| ADR-018 的顾虑 | `ngm gc` 的回答 |
|---|---|
| 可达性从哪来 | 从用户**显式选择**的项目 lock；命令在哪个项目里跑，就以哪份 lock 为准 |
| 误删怎么恢复 | 层 2 由层 1 重建；层 1 由远端重建；vendor 由 `ngm install` 重建。输出里写明 |
| 会不会悄悄发生 | 默认 dry-run；只有 `--apply` 才删；候选清单逐条可见 |

它仍是**弱于注册表**的方案：另一个项目可能因为这次回收而需要重新拉取。这条代价写在
命令的 SAFETY 段与 `docs/guides/cli.md` 里，不靠用户自己想到。

## 边界

- **不处理**：module cache（层 1.5）、`cache/`、孤儿 blob（交给 `store prune --orphans`
  的 24h 年龄门槛）、以及任何不属于这三层的目录。
- **并发**：层 1 没有跨进程锁，命令帮助明确要求不要与另一个 install/update 同时跑。
- **`store usage` 与 `store prune` 的措辞同步更新**：它们不再说"ngm 永远不做按可达性删除"，
  而是说明自动/全局那类仍然没有，显式项目范围的那类在 `ngm gc`。
