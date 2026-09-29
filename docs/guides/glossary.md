# 术语表

> ngm 文档中反复出现的核心术语。出现定义冲突时，以本表与"唯一事实源"文档为准（见 [README](../README.md) 的文档约定）。

| 术语 | 含义 | 详见 |
|------|------|------|
| **refType** | 声明层意图：`commit` / `tag` / `branch`，必填 | [ADR-004](../adr/adr-004-reftype.md) |
| **commit 锁定** | 解析 refType 得到不可变 commit 锚点 | [锁定机制](../architecture/locking.md) |
| **archiveDigest** | 规范化内容清单（canonical manifest）的 sha256，对"字节一致"负责 | [ADR-008](../adr/adr-008-archive-digest.md) |
| **canonical manifest** | 从 Git tree 生成的排序清单（path / mode / blob-sha256） | ADR-008 |
| **resolvedAt** | ngm 解析该依赖的时刻（仅审计用途，不参与门禁） | [锁定机制](../architecture/locking.md) |
| **driftKind** | verify 的漂移分类：`expected` / `unexpected` / `critical` | [信任模型](../architecture/trust-model.md) |
| **vendor 4 层** | mirror / content store / link tree / cache | [vendor 4 层](../architecture/vendor-layers.md) |
| **link tree / linkMode** | 项目内 `ngm.vendor` 的落地方式（hardlink / copy / symlink） | vendor 4 层 |
| **Host runtime** | 用户项目实际运行的 Node.js 或 Deno 环境 | [运行时模型](../architecture/runtime-model.md) |
| **engine adapter** | 调用外部引擎（esbuild / tsc / deno / postcss）的适配层 | [引擎 adapter](../architecture/engine-adapter.md) |
| **mappings** | `ngm.mappings.json`：把裸导入映射到 vendor 路径 | [P4 — 生态与协议](../modules/p4-ecosystem.md) |
| **minimumReleaseAge** | 晾晒期门禁，基于 commit 的 committer date | [供应链防护](../architecture/supply-chain.md) |
| **postInstallPolicy** | 控制依赖 postinstall 是否执行（默认 deny） | 供应链防护 |
| **frozen-lockfile** | 禁止解析新 ref / 改 lock（**不禁止网络**） | [依赖管理](./dependency-management.md) |
| **--offline** | 禁止一切网络访问，未命中本地资源即失败 | [CLI 参考](./cli.md) |

---

## 相关文档

- [CLI 参考](./cli.md)
- [配置详解](./configuration.md)
- [架构总览](../architecture/overview.md)