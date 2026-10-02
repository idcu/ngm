# vendor 4 层模型

## 推翻"vendor = 拷文件"

ngm 的 vendor 不是一团目录拷贝，而是四层协作：mirror 提供对象库，content store 提供不可变内容树，link tree 把内容带进项目，cache 只做加速。

---

## 四层结构

```
~/.ngm/
├── mirror/        # 层 1：Git 裸仓库镜像（完整对象库）
├── content/       # 层 2：内容寻址的不可变内容树（已解包）
└── cache/         # 层 4：可丢弃缓存（metadata / OSV / tmp）

project/
├── ngm.json
├── ngm.lock
└── ngm.vendor/    # 层 3：链接树（逐文件 hardlink 指向层 2）
    └── github.com/my-org/utils/
```

---

## 层 1：mirror（Git 裸仓库镜像）

**职责**：避免重复 clone；为 archiveDigest 的本地重放提供完整对象库。

```
~/.ngm/mirror/
├── github.com/my-org/utils.git/    # bare repo
├── github.com/my-org/logger.git/
└── gitee.com/other-org/lib.git/
```

- `git clone --mirror` 拉取，后续 `git fetch` 增量更新
- 4 种协议归一化（https/ssh/git/shorthand）
- 认证信息管理（SSH key / token / netrc）
- mirror 缺失且在线时自动补拉；离线且缺失时 digest 生成/校验报错（exit 4）
- v0.1 不提供 shallow / partial clone：mirror 保留完整历史（大仓库成本列入后续探索）

**对应竞品思想**：git remote 镜像。

---

## 层 2：content store（内容寻址存储）

**职责**：按 archiveDigest 存放**不可变内容**，去重。

```
~/.ngm/content/
├── schema                               # 新写路径使用的布局版本（"2"）
├── blobs/<aa>/<sha>                     # 文件内容，只存一份（两位 hex 分片）
├── trees/<digest-hex>/
│   ├── manifest.json                    # 内容树 = 一份清单（路径 / mode / blob / 大小）
│   └── meta.json                        # 首次写入者的来源（repo / commit / subPath）+ 清单规范版本
└── sha256/<digest-hex>/tree/            # v1（旧布局）遗留：只读，不再新增
```

**布局 v2（blob 池 + 树清单）自 v0.8 起**，见 [ADR-019](../adr/adr-019-content-addressed-blobs.md)：
按文件内容寻址，同一份字节在层 2 只存一份。v1 的"每个 digest 一棵完整解包树"
在实测中把 12 个 commit 的占用放大到源码真实增量的 **20×**（[metrics](../internals/metrics.md#磁盘增长内容寻址-storev06)）；
v2 之后同一场景约 **0.6×**，且差距只来自每个 commit 多出的那份清单。

- digest 定义与清单规范化规则见 [ADR-008](../adr/adr-008-archive-digest.md)：digest 基于规范化内容清单，**不是** tar/zip 归档字节流
- 条目键是 **digest（内容）而不是 commit**：内容相同的不同 commit——乃至不同仓库、不同依赖——**共用同一个条目**。去重的粒度是字节，不是提交
- 因此 `meta.json` 记录的是**首次写入者的来源**；当条目被共享时，它的 `repo` / `commit` / `subPath` 与实际取用者并不一致，这是预期行为。它只是审计线索，**不是身份**；身份由目录名（digest）与内容本身承载。校验实现不得把"来源字段与当前依赖不同"判为异常，否则内容去重会被误报成完整性破坏
- **blob 的身份里含 mode**（`sha = H(mode, 内容)`）：落地层用 hardlink，而 hardlink 共享 inode，
  所以"同一份字节一处可执行、一处不可执行"只能靠两个 blob 表达。代价是这种情况下存两份——
  在"同一文件在多个 commit 里不变"这个主导场景下不受影响（ADR-019 §修订）
- 任何时刻可"重建清单 → 重算 digest"校验内容是否被篡改/损坏（verify 的本地重放）
- 借鉴 pnpm 的 content-addressable store；差别在于 ngm 的清单是**规范化且可重放**的（digest 由它算出）

---

## 层 3：link tree（项目落地）

**职责**：让项目里看到"普通目录"，但不占多份磁盘。

```
project/ngm.vendor/
└── github.com/my-org/utils/     # 普通目录
    ├── index.ts                 # 文件：hardlink → content/blobs/<aa>/<sha>
    └── src/…
```

**目录不能 hardlink**，所以落地方式是"逐文件 hardlink + 真实目录结构"；symlink 条目按目标字符串原样重建为 symlink（不 hardlink）。

| linkMode | 行为 | 适用 |
|---------|------|------|
| `auto`（默认） | hardlink 优先；跨卷/文件系统不支持时**整树复制** | 大多数场景 |
| `hardlink` | 强制 hardlink，失败即报错 | 要求零重复磁盘 |
| `copy` | 普通复制 | 网络文件系统、要提交 vendor 的保守场景 |
| `symlink` | 依赖目录整体 symlink 指向内容树（不逐文件链接） | 磁盘最省；Windows 需开发者模式 |

> **`symlink` 在 v2 布局下退化为逐条目 hardlink**（v0.8 / ADR-019 §修订）：层 2 不再有
> "一棵已物化的树"可以整目录链接。**磁盘收益不变**（hardlink 与目录 symlink 同样不复制内容），
> 而实际使用的 mode 会被 CLI 如实打印（"所有链接失败路径必须可观测"）。
> 仍然只有 v1 数据的旧 store 保持"整目录链接"的老语义。

（linkMode 配置见[配置详解](../guides/configuration.md)。）

**平台注意事项**：

- Windows：hardlink 需要 NTFS 同卷，**不需要管理员权限**；跨卷自动降级 copy
- 目录 symlink 在 Windows 需要开发者模式或管理员权限——不默认启用
- **填充 store 不再需要 symlink 权限**（v0.8 起）：v2 的清单直接记录 symlink 的目标字符串，
  store 里不会创建 symlink。只有 `symlink` 落地模式才需要它（且如上所述在 v2 下不生效）
- 提交 vendor/ 时：hardlink / copy 落地在 Git 视角无差别（都是普通文件）；symlink 会被 Git 记录为链接，跨平台消费方可能无法正确检出

---

## 层 4：cache（缓存层）

**职责**：可丢弃的加速层，不承担可证明性。

```
~/.ngm/cache/
├── metadata/      # Git ref 信息、tag 列表
├── osv/           # OSV.dev 查询结果
└── tmp/           # 临时目录
```

- **cache 可以在任何时刻整层删除**——可证明性由 mirror + content + lock 保证，与 cache 无关
- `ngm cache clean` 清空缓存层
- content store 与 mirror 的 GC：由 [ADR-018](../adr/adr-018-store-reclaim.md) 裁定——
  **不做**按可达性自动删除的 GC（它依赖跨项目引用索引，而 ngm 没有，也不该去扫用户的磁盘）；
  代之以 `ngm store usage`（只读占用报告）与 `ngm store prune`（**只清**解包残骸），
  而真正能改变增长曲线的是**层 2 的写入侧去重**——布局改为 blob 池 + 树清单，
  schema 与迁移方案见 [ADR-019](../adr/adr-019-content-addressed-blobs.md)，**已由 v0.8 落地**。
  **注意它改的是斜率，不是终点**：blob 同样只增不减，回收仍受 ADR-018 那两个条件约束。
  纪律不变：[CLI 参考](../guides/cli.md) 不会预告一个不存在的 `ngm store gc`

---

## vendor 的落地位置与是否提交

| 模式 | 配置 | 适用场景 | 代价 |
|------|------|---------|------|
| 全局缓存 | `"mode": "global"` | 节省磁盘，多项目共享 | 重新引入 pnpm 式缓存管理问题，削弱隔离收益 |
| 本地 vendor/ | `"mode": "local"`（默认） | 隔离清晰，CI 可复现 | 跨项目重复存储 |

**是否把 `ngm.vendor/` 提交进仓库是 Git 侧的选择**（`git add ngm.vendor`），
不是 ngm 的配置项：ngm 从不执行 `git add` / `git commit`。配置项 `vendor.commit`
因此从未有过任何读取点，已于 v0.5 移除（见[配置详解 §vendor](../guides/configuration.md)）。

**没有普遍最优解**。构建镜像、离线交付和审计门禁才需要提交 vendor/。

---

## 诚实说明

1. **vendor 不必然省磁盘**：pnpm 的 content store + hardlink 通常更省空间
2. **全量副本跨项目重复**：每个项目一份 vendor 副本，monorepo 下膨胀明显
3. **提交 vendor 让 git 膨胀**：大依赖（如带 native 模块的包）不适合提交
4. **hardlink 有平台限制**：需要同卷文件系统（Windows 需 NTFS），跨卷/网络文件系统自动降级复制
5. **content store 只增不减**：v0.1 无 store GC，`ngm cache clean` 只能清缓存层
6. **hardlink 与 content store 共享 inode（就地写入会污染层 2）**：`auto`（默认）落地的是指向 content store 的**硬链接**，因此**就地修改 `ngm.vendor/` 中的文件会同时改写 content store**（同一 inode，不是副本）。更棘手的是后续 `ngm install` **不会察觉**——该 digest 目录已存在，写入被直接跳过，污染被原样保留。需要就地编辑依赖代码的场景请改用 `linkMode: "copy"`（写入副本，不影响层 2）。检测路径是 `ngm verify --deep`：从 mirror 重建规范化清单，并与**实际字节**逐文件比对（清单规则见 [ADR-008](../adr/adr-008-archive-digest.md)）

---

## 相关文档

- [ADR-003：为什么纯 vendor/ 目录](../adr/adr-003-vendor.md)
- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)
- [信任模型](./trust-model.md)
- [配置详解](../guides/configuration.md)