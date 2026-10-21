# ADR-034：模块缓存与 partial clone 的边界

- **状态**：已定
- **日期**：2026-10-20
- **范围**：抓取、缓存、离线完整性、mirror 的关系
- **上游**：[ADR-030](./adr-030-mirror-reclaim.md)（mirror 的占用与整理）、[ADR-032](./adr-032-scoped-content-proof.md)

> **结论**：把“mirror”和“module cache”分成两种东西。mirror 仍是完整裸仓（ADR-030 的拒绝不变）；module cache 只服务已被 lock 钉住的 `(commit, subPath)`，可以使用 `--filter=blob:none`、`--depth=1` 和定向 tag/commit fetch。module cache 从不宣称“任意 ref 可离线重放”，它的完整性只由模块清单给出。
>
> **代价**：多一层缓存语义与失败模式；服务端 filter 能力必须探测；`verify` 必须在读取前做完整性检查，缺对象只能退出 4，不能惰性联网。
>
> **实现状态（v0.104）**：`git.ClonePartial/FetchPartial/FetchPartialCommit`、`vendor.ModuleCache`、
> `Layout.ModuleCacheRoot` 已落地；`install-from-lock` 的模块条目优先使用它，filter 不支持时明确回退完整 mirror；
> `verify --offline` 在 mirror 缺失时对模块条目复用 module cache 完成 binding/digest/landing，
> 并用 `GIT_NO_LAZY_FETCH` 禁止惰性联网。

---

## 背景

ADR-030 拒绝 `--single-branch` 与部分克隆，理由是 mirror 必须保留 refType 完整性与离线 digest 重放。这个理由对 mirror 成立，但对“只消费一个模块”的消费者不成立：消费者不需要整仓历史、不需要兄弟模块 blob，也不宣称任意 ref 可离线解析。

如果继续用 mirror 服务所有消费者，1 万模块的集合会让每个消费者付整仓成本；这违反 ADR-031 的规模目标。

## 决策

1. **mirror 语义不变**：维护者/CI 仍可使用完整裸仓；ADR-030 的拒绝继续适用于 mirror。
2. **新增 module cache**：
   - key 是 `(commit, subPath)`；
   - 只取该 commit 的 tree 图 + 该模块的 blob；
   - 默认 `--filter=blob:none`、`--depth=1`；`tree:0` 仅在实测必要时使用；
   - 完整性 = `moduleDigest` 清单列出的 blob 全在；缺一个就是缓存不完整（exit 4）。
3. **抓取优先定向**：
   - 精确 tag/branch 用 `ls-remote` 定向查询；
   - 发布用 tag 时，`git fetch <tag>` 只取该 ref；
   - 批量 fetch 多个 tag/commit 时共享未变对象；
   - 不要求消费者 `git clone --mirror`。
4. **禁止隐式联网**：
   - `verify` / `install --offline` 先检查模块清单完整性；
   - 缺对象 → exit 4，绝不触发 promisor lazy fetch；
   - 在线安装可以显式补取，但必须是单独、可观测的一步。
5. **服务端能力探测**：
   - 检查 `remote.origin.promisor` / `partialclonefilter` 是否真的生效；
   - 不支持 filter 时明确回退全量或拒绝，不能静默全量克隆。
6. **ref index 与 module cache 分离**：
   - ref index 缓存 `tag/branch → commit`，可过期、可标 stale；
   - module cache 只存内容；
   - 索引分支（`ngm-index`）是 ref index 的一种载体，不是信任锚。

## 后果

- 消费者不再需要完整 mirror；维护者仍可保留 mirror 以做完整校验。
- `store usage` / `prune` 需要同时报告 mirror、ref index、module cache、content store 的占用。
- 离线能力变成“按 lock 条目声明”：能证明已缓存的模块，不证明任意 ref。
- 服务端不支持 partial clone 时，模块级证明仍然有效，只是抓取退回全量；这是性能降级，不是正确性降级。
- 未来若要把 module cache 升级为“可离线解析任意 ref”，那是另一个 ADR，不在本决策内。

## 相关文档

- [模块集合架构](../architecture/module-collections.md)
- [vendor 4 层](../architecture/vendor-layers.md)
- [锁定机制](../architecture/locking.md)
