# 模块集合（Module Collections）

> 本文是 [ADR-031](../adr/adr-031-module-collections.md) 与 [ADR-032](../adr/adr-032-scoped-content-proof.md) 的活文档：一个仓库如何承载多个模块，以及每个模块如何被独立解析、证明、缓存。

## 1. 概念

| 概念 | 定义 |
|------|------|
| Repository | Git 仓库，canonical slug，例如 `github.com:acme/modules` |
| Module | `(repo, subPath)`；子树内有 `ngm.json` |
| Collection | 一个仓库里的模块集合；不是新的信任单位 |
| Release | `(module, version, commit, tag?)`；commit 是锚，tag 可选 |
| Index | 从 release/tag/模块清单派生的目录；缓存，不是权威 |
| Proof | `moduleDigest`（内容）+ `subtreeOid`（绑定）+ `commit`（锚） |

三条铁律：

1. **commit 是唯一锚点**；tag/branch/index 都是找到 commit 的方式。
2. **内容身份与来源分离**：`moduleDigest` 回答“字节是什么”，`subtreeOid` 回答“属于哪个 commit 的哪个子树”。
3. **索引可以滞后，不能领先**；冲突必须报错。

## 2. 模块清单

`packages/utils/ngm.json`：

```json
{
  "schemaVersion": 2,
  "name": "acme-utils",
  "version": "1.2.3",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [
    { "name": "github:acme/lib", "ref": "v2.0.0", "refType": "tag" }
  ]
}
```

- `version` 对开发模块可选；发布模块由 `modules validate --release` 要求 semver。
- `version` 是模块内容的一部分；`moduleDigest` 覆盖它。
- 没有 devDependencies、没有安装期钩子、没有 registry 依赖。
- 解析子路径依赖时，先读 `<subPath>/ngm.json`；存在即权威。缺失时回退仓库根 `ngm.json` 并输出 warning。

## 3. 模块 digest

```
header:  "ngm-module-digest/v1\0"
record:  "<path>\0<mode>\0<sha256(blob bytes)>\0"
path:    相对模块根
sort:    UTF-8 字节序
mode:    100644 / 100755 / 120000
digest:  "sha256:" + sha256(manifest)
```

- 与 `ngm-archive-digest/v1` 共用一套清单实现，只换 header 与 pathBase。
- 模块内 gitlink/LFS 拒绝；兄弟目录的不进入模块清单。
- `moduleDigest` 不含 repo/commit/subPath，是纯内容身份；位置由 `subtreeOid + subPath` 绑定。

## 4. 绑定与验证

`subtreeOid = git rev-parse <commit>:<subPath>`（根 scope 是 `<commit>^{tree}`），记录时带 `sha1:`/`sha256:` 前缀。

模块依赖的四查：

| 检查 | 内容 | 失败 |
|------|------|------|
| ref | 重新解析 ref，与 lock 的 commit 比对 | 漂移：expected/unexpected |
| binding | `rev-parse commit:subPath == subtreeOid` | critical，exit 2 |
| digest | `ls-tree -r <subtreeOid>` + 模块 blob → `moduleDigest` | critical，exit 2 |
| landing | content store 按 `moduleDigest`；vendor 按 `subPath`；`--deep` 逐文件 | 缺对象 exit 4；字节不符 exit 2 |

额外一条版本绑定：`commit:subPath/ngm.json` 的 `version` 必须等于 lock 的 `version`（若记录）。因为 `ngm.json` 在模块内容里，`moduleDigest` 已覆盖它。

### lock v2

- 根条目：`archiveDigest`（整仓），不写模块字段；
- 模块条目：`moduleDigest + subtreeOid + version`（version 可选），**不写 `archiveDigest`**；
- `lockfileVersion: "2.0.0"`；没有旧用户，因此不提供 v1 迁移；
- 模块条目的 content store 键是 `moduleDigest`，条目路径相对模块根。

## 5. 集合索引

人类可读的根 `ngm.collection.json` 可选；机器入口是本地 `ngm-index` 分支，只放索引，不碰代码历史（推送由 CI/用户显式完成）。

```json
{
  "schemaVersion": 1,
  "source": { "repo": "github.com/acme/modules", "branch": "main", "commit": "<code tip>" },
  "modules": [
    { "name": "@acme/utils", "path": "packages/utils", "version": "1.2.4" }
  ],
  "releases": {
    "packages/utils": {
      "1.2.4": { "tag": "packages/utils/v1.2.4", "commit": "<commit>" }
    }
  }
}
```

- `ngm modules index` 生成该文件，并默认更新本地 `ngm-index` 分支；
- `--shards` 时 releases 不内联，改为 `releases/<module>.json`，根索引写 `releaseDir: "releases"`；`ngm-index` 分支用一次 `hash-object --stdin-paths` + `update-index --index-info` 写入整棵分片树；
- `source.commit` 把索引锚到代码 commit；`source` 在非 Git 目录或无 HEAD 时省略；
- 逐模块 `publish` 完全覆盖全局 `--releases`：显式 true/false 直接决定，nil 才跟随全局（ADR-037）；
- `releases` 来自本地 `<modulePath>/v<version>` tag，只按模块前缀收窄；
- `ngm modules validate --index=...` 检查索引与目录一致：缺失、多余、name/version 不一致都失败；
- 索引只做发现、`outdated`、避免枚举全仓 tag；不是信任锚；
- 索引滞后安全；索引领先危险；
- 条目必须记录 `version + tag + commit`；只记录 branch 会失去版本的可复现性；
- 发布用原子 push 同时推代码、tag、索引；不支持原子 push 时先推 tag，再推索引。

## 6. 解析优先级

| 输入 | 1 | 2 | 3 | 冲突 |
|------|---|---|---|------|
| 完整 SHA | 本地 object | 远端 commit（宿主允许） | 分支包含检查 | 报错 |
| 精确 tag | exact tag（定向 `ls-remote`） | 索引条目 | — | tag/索引不一致 → 报错 |
| 无 tag version | 索引 release | 明确允许的 branch | 报错 | — |
| 分支 | 定向 `ls-remote` | 索引 heads（离线） | 本地 mirror | 缓存旧 → stale |
| `latest` | 索引 latest | 定向 tag 前缀查询 | 报错 | 索引旧 → 警告 |
| 未给 ref | 报错 | — | — | — |

快速验证不需要 tag：commit 模式（精确）或 branch 模式（浮动，lock 钉 commit）。正式发布再引入 version/tag/索引。

## 7. 抓取与缓存

- 消费者不再要求完整 mirror；mirror 留给维护者/CI（见 [ADR-034](../adr/adr-034-module-cache.md)）。
- ref index：缓存 `tag/branch → commit`，可过期、可标 stale。
- module cache：按 `(commit, subPath)` 只取 commit + path trees + 模块 blob；默认 `blob:none`、`--depth=1`。
- **v0.104 已接入 `install-from-lock` 与 `verify --offline` 的模块条目**：优先建/复用 partial clone，filter 不支持时明确回退完整 mirror；verify 离线复用 module cache 完成 binding/digest/landing。
- 完整性由 `moduleDigest` 清单给出：清单里的 blob 全在 = 可离线；缺 blob = exit 4，不是篡改。
- `verify` 绝不惰性联网；先查完整性，再读 blob；离线模块 proof 走 `GIT_NO_LAZY_FETCH`。
- 服务端不支持 filter 时明确回退全量或拒绝，不能静默全量克隆。

## 8. 按 commit 批处理

1 万模块不能逐模块整树扫描。`BuildModuleMaterials(commit, subPaths)`：

1. `git ls-tree -r -t -z <commit>` 一次；
2. 按 subPath 前缀分组、去前缀；
3. 每个模块构建 `ngm-module-digest/v1` 清单；
4. `git cat-file --batch` 一次读所有 manifest/blob；
5. `subtreeOid` 从 tree 条目取；
6. 批量写 content store。

复杂度：`O(R)` 一次树遍历 + `O(ΣS)` 模块 blob 哈希，而不是 `O(M × R)`。

## 9. 规模边界

- 1 万模块、10 万版本：索引分片；定向 `ls-remote`；不 `--mirror`；输出分页；store 加派生索引。
- lock 只锁消费者闭包；集合校验用索引/worktree，不生成巨型 lock。
- 模块缓存按 commit 共享 tree；不同 commit 复用未变 tree 对象。
- Windows 路径/大小写、路径深度/长度必须有明确错误。
- 合成基准先量：1k/10k 模块、10k/100k tag，再决定 tags 还是索引入口。

## 10. 非目标

workspace、同一依赖多版本共存、模块级签名/信任、跨仓库搜索、registry 式裸名、宿主 archive API。

## 相关文档

- [ADR-031：模块集合](../adr/adr-031-module-collections.md)
- [ADR-032：模块级证明](../adr/adr-032-scoped-content-proof.md)
- [ADR-034：module cache](../adr/adr-034-module-cache.md)
- [依赖解析](./dependency-resolution.md)
- [锁定机制](./locking.md)
- [vendor 4 层](./vendor-layers.md)
