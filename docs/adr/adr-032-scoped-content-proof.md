# ADR-032：模块级内容证明（moduleDigest + subtreeOid）

- **状态**：已定
- **日期**：2026-10-20
- **范围**：archiveDigest 的 scope、lock v2、content store、verify、批量重放
- **上游**：[ADR-031](./adr-031-module-collections.md)

> **结论**：内容证明分成两层：`moduleDigest` 是模块作用域的规范化清单哈希（SHA-256，模块内相对路径），`subtreeOid` 是 Git 的 Merkle 绑定锚（`commit:subPath` 的 tree 对象）。根依赖继续用 `archiveDigest`（仓库相对路径）；模块依赖只记录 `moduleDigest + subtreeOid`，不再记录整仓 `archiveDigest`。
>
> **代价**：lock 升级为 v2；content store 需要按 scope 区分树清单；verify 多一层 binding 检查；模块清单规范需要新的冻结向量。没有旧用户，因此不提供 v1 迁移。
>
> **实现状态（v0.102）**：clean v2 已落地——模块条目只写 `moduleDigest`/`subtreeOid`/`version`，
> 根条目只写 `archiveDigest`；`lockfileVersion` 为 `2.0.0`；模块 content store 与 vendor 落地、
> verify binding/digest/version 均已按模块 scope 工作。

---

## 背景

现有 `archiveDigest` 是整棵 commit tree 的哈希。它有两个结构性限制：

1. 要重放它，必须拥有整仓所有 blob；只取子路径无法证明。
2. 路径包含仓库全路径；模块移动后内容不变、digest 必变，无法作为稳定的模块内容身份。

AI 规模下，1 万个模块 × 每个模块重放整仓 digest 是 `O(M × R)`，不可接受。

## 决策

### 1. 内容层：`moduleDigest`

```
header:  "ngm-module-digest/v1\0"
record:  "<path>\0<mode>\0<sha256(blob bytes)>\0"
path:    相对模块根
sort:    UTF-8 字节序
mode:    100644 / 100755 / 120000
digest:  "sha256:" + sha256(manifest)
```

- 与 `ngm-archive-digest/v1` 共用一套 `BuildManifest(scope)`，只换 header 与 pathBase；
- 模块内 gitlink/LFS 拒绝；兄弟目录的 gitlink/LFS 不进入模块清单；
- `moduleDigest` 是纯内容身份，不含 repo/commit/subPath；位置由 `subtreeOid + subPath` 绑定。

### 2. 绑定层：`subtreeOid`

- `subtreeOid = git rev-parse <commit>:<subPath>`；根 scope 是 `<commit>^{tree}`；
- 记录时带算法前缀：`sha1:<hex>` / `sha256:<hex>`；
- `blob:none` 的 partial clone 保留 tree，因此 binding 检查不需要 blob、可离线。

### 3. lock v2

- 根条目：`archiveDigest` 必填；`subtreeOid` 可选（建议记录）。
- 模块条目：`moduleDigest` + `subtreeOid` 必填；**不写 `archiveDigest`**。
- `version` 可选（测试模块可以没有）；`source` 记录解析来源（`index|tag|branch|commit|mirror`）。
- `lockfileVersion: "2.0.0"`；字段顺序固定；只有 `resolvedAt` 非确定性。

### 4. verify 四查

1. **ref**：重新解析 ref，与 lock 的 commit 比对；
2. **binding**：`rev-parse commit:subPath == subtreeOid`，不匹配 critical；
3. **digest**：`ls-tree -r <subtreeOid>` + 模块 blob → `moduleDigest` 重放，不匹配 critical；
4. **landing**：content store 按 `moduleDigest`、vendor 按 `subPath`；`--deep` 逐文件字节。

额外一条**版本绑定**：`commit:subPath/ngm.json` 的 `version` 必须等于 lock 的 `version`（若记录）。因为 `ngm.json` 在模块内容里，`moduleDigest` 已覆盖它。

### 5. 批量与 scope

- `BuildModuleMaterials(commit, subPaths)` 一次 `ls-tree -r -t`、一次 `cat-file --batch`；
- 同一 commit 的 N 个模块，树遍历只做一次；复杂度从 `O(M × R)` 降到 `O(R) + O(ΣS)`；
- content store 的树清单按 digest 区分 scope；blob 池共享；
- 完整性由模块清单给出：清单里的 blob 全在 = 可离线；缺 blob = exit 4，不是篡改。

## 后果

- `verify --deep` 的 subPath 路径口径必须与 lock 的 scope 一致；v0.101 先修整仓 `archiveDigest` 的 `--deep` 回归，v0.102 再引入模块 scope。
- content store 的 `prune` 必须同时理解 root 与 module 清单；不确定时拒绝删除。
- 报告必须写清楚证明范围：`module-scoped proof` 或 `repository-scoped proof`，不能只说 “verified”。
- tag/索引/版本文件都不是信任锚；`moduleDigest + subtreeOid + commit` 才是。

## 相关文档

- [模块集合架构](../architecture/module-collections.md)
- [依赖解析](../architecture/dependency-resolution.md)
- [锁定机制](../architecture/locking.md)
- [vendor 4 层](../architecture/vendor-layers.md)
