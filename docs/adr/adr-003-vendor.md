# ADR-003：为什么纯 vendor/ 目录

- **状态**：已定（v2 修订：4 层 vendor 模型）
- **日期**：2026-09-29（v2 修订：2026-09-29）
- **范围**：依赖落地方式与目录布局

> **结论**：用 `vendor/` 落地依赖，但拆为 4 层（mirror / content store / link tree / cache）。
> vendor 的价值是**可审计、可提交、可离线**——不是省磁盘。
> **代价**：跨项目重复存储（省磁盘这一项 pnpm 的 content store 做得更好）；提交 vendor 会让仓库体积膨胀；
> 默认的 hardlink 落地与 content store **共享 inode**，就地编辑 vendor 文件会同时污染层 2，
> 只能靠 `ngm verify --deep` 发现。

---

## 背景

原方案把 vendor/ 当作"一团目录拷贝"。v2 竞品分析发现：

- pnpm 已有 content-addressable store + hardlink，磁盘效率高于全量 vendor 副本
- npm 2026 支持 `file:` 本地包 + workspace
- Bun 的 install 已经极快（无变更约 12ms）

所以"vendor/ 省磁盘"不成立。vendor 的价值必须重新定义。

---

## 决策

**ngm 使用 vendor/ 目录落地依赖，但拆为 4 层模型。**

vendor 的价值不是"省磁盘"，而是**可审计、可提交、可离线**。

---

## 4 层 vendor 模型

```
~/.ngm/
├── mirror/        # 层 1：Git 裸仓库镜像（origin fetch 结果）
├── content/       # 层 2：内容寻址的不可变内容树（按 archiveDigest 寻址，已解包）
└── cache/         # 层 4：可丢弃缓存（ref metadata / OSV 响应 / tmp）

project/
├── ngm.json
├── ngm.lock
└── ngm.vendor/    # 层 3：链接树（逐文件 hardlink 指向层 2）
    └── github.com/my-org/utils/   # 普通目录，文件 hardlink 自 content
```

| 层 | 作用 | 对应竞品思想 |
|---|---|---|
| mirror | 避免重复 clone；为 digest 重放提供完整对象库 | git remote 镜像 |
| content store | 内容寻址的内容树，去重 | pnpm content-addressable store |
| link tree | 项目里看到普通目录（逐文件 hardlink，失败按 linkMode 降级） | hardlink，不占多份磁盘 |
| cache | 可随时清空，不影响可证明性 | pnpm store / yarn cache |

> 目录不能 hardlink，因此 link tree 是"逐文件 hardlink + 真实目录结构"；详见 [vendor 4 层](../architecture/vendor-layers.md)。

---

## vendor 的三种模式

| 模式 | 配置 | 适用场景 | 代价 |
|------|------|---------|------|
| 全局缓存 | `"mode": "global"` | 节省磁盘，多项目共享 | 重新引入 pnpm 式缓存管理问题，削弱隔离收益 |
| 本地 vendor/ | `"mode": "local"`（默认） | 隔离清晰，CI 可复现 | 跨项目重复存储 |
| 提交 vendor/ | **Git 侧的 `git add ngm.vendor`**（原 `"commit": true` 配置项已于 v0.5 移除：它没有任何读取点，而文档曾把它描述为会生效） | 离线交付、审计门禁、镜像 | 仓库体积、clone 时间、依赖更新 diff |

**没有普遍最优解**。构建镜像、离线交付和审计门禁才需要提交 vendor/。

---

## 诚实说明

1. **vendor/ 不必然省磁盘**：pnpm 的 content store + hardlink 通常更省空间
2. **全量副本跨项目重复**：每个项目一份 vendor 副本，monorepo 下膨胀明显
3. **提交 vendor/ 让 git 膨胀**：大依赖（如带 native 模块的包）不适合提交
4. **hardlink 有平台限制**：需要同卷文件系统（Windows 需 NTFS），不支持时自动降级为复制
5. **content store 只增不减**：不提供 store GC，`ngm cache clean` 只能清缓存层
   > **补录（v0.5 复核）**：本条原写"v0.2 规划 `ngm store gc`"，**该计划未成立**——v0.2 ~ v0.4 均未排期
   > store GC，且 [CLI 参考](../guides/cli.md) 明确**不预告**这个命令名。补录而非改写，是为了保留
   > "曾经预告过一个没做的东西"这一事实。
   > **再补录（v0.6）**：挂账理由（"缺实测的磁盘增长数据"）**已经补上**——
   > 实测为**每个 commit 一整棵树、无跨 commit 去重**（20 文件 × 8 KiB 的树，
   > 12 个 commit 涨到 1.88 MiB，是源码真实增量的 **20.0×**、是 Git 对象库的 **187×**）。
   > 数字与方法见 [metrics · 磁盘增长](../internals/metrics.md#磁盘增长内容寻址-storev06)。
   > **那一步（先立 ADR）已经办了**：[ADR-018](./adr-018-store-reclaim.md) 决定
   > **不做按可达性自动删除的 GC**（缺项目注册表，误删会让别的项目某天突然验不过），
   > 当下只做低风险的两件事（`store usage` / `store prune`），
   > 并把**层 2 的写入侧去重**（按文件内容寻址）记为长期解法——它把 20× 从源头消掉。
   > 本条的"只增不减"因此仍是**事实**，但已经是**有意的、有 ADR 支撑的**取舍。

---

## 后果

- vendor 不是"拷文件"，而是 4 层协作
- 默认 local 模式，不强制提交
- 提交 vendor 是"审计/离线"的可选项，不是默认答案
- content store 层借鉴 pnpm 思路，不宣称创新

---

## 相关文档

- [架构：vendor 4 层](../architecture/vendor-layers.md)
- [配置详解](../guides/configuration.md)
- [ADR-002：为什么直接拉 Git 仓库](./adr-002-git-direct.md)
