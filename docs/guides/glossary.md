# 术语表

ngm 文档中反复出现的核心术语。定义冲突时以本表与对应代码/架构文档为准。

| 术语 | 含义 | 详见 |
|------|------|------|
| **refType** | 声明层意图：`commit` / `tag` / `branch` / `local-git` / `local-dir`；仅完整 40 位 commit 可省略 | [ADR-004](../adr/README.md) · [ADR-035](../adr/README.md) |
| **commit 锁定** | 解析 refType 得到不可变 commit 锚点 | [锁定机制](../architecture/locking.md) |
| **archiveDigest** | 根依赖的规范化内容清单 sha256，对整仓"字节一致"负责 | [ADR-008](../adr/README.md) |
| **moduleDigest** | 模块作用域的规范化内容清单 sha256；路径相对模块根 | [ADR-032](../adr/README.md) |
| **subtreeOid** | `<commit>:<subPath>` 的 Git tree OID，负责"位置绑定" | [ADR-032](../adr/README.md) |
| **module** | 仓库内一个带 `ngm.json` 的子树；身份是 `repo + subPath` | [能力矩阵](../internals/capability-matrix.md) |
| **publish** | 模块级发布开关；显式 true/false 完全覆盖全局 `--releases` | [ADR-037](../adr/README.md) |
| **module cache** | `blob:none` partial clone，只服务 lock 钉住的模块 | [ADR-034](../adr/README.md) |
| **canonical manifest** | 从 Git tree 生成的排序清单（path / mode / blob-sha256） | ADR-008 / ADR-032 |
| **resolvedAt** | ngm 解析该依赖的时刻（仅审计用途，不参与门禁） | [锁定机制](../architecture/locking.md) |
| **driftKind** | verify 的漂移分类：`expected` / `unexpected` / `critical` | [安全模型](../architecture/security-model.md) |
| **vendor 层** | mirror / module cache / content store / link tree / cache | [能力矩阵](../internals/capability-matrix.md) |
| **link tree / linkMode** | 项目内 `ngm.vendor` 的落地方式（hardlink / copy / symlink） | [配置详解](./configuration.md#vendor) |
| **Host runtime** | 用户项目实际运行的 Node.js 或 Deno 环境 | [架构总览](../architecture/overview.md) |
| **engine adapter** | 调用外部引擎（esbuild / tsc / deno / postcss）的适配层 | [引擎 adapter](../architecture/engine-adapter.md) |
| **mappings** | `ngm.mappings.json`：把裸导入映射到 vendor 路径 | [构建指南](./build.md#mappings-文件) |
| **minimumReleaseAge** | 晾晒期门禁，基于 commit 的 committer date | [供应链防护](../architecture/supply-chain.md) |
| **postInstallPolicy** | 控制依赖 postinstall 是否执行（默认 deny） | [供应链防护](../architecture/supply-chain.md) |
| **frozen-lockfile** | 禁止解析新 ref / 改 lock（**不禁止网络**） | [依赖管理](./dependency-management.md) |
| **--offline** | 禁止一切网络访问，未命中本地资源即失败 | [CLI 参考](./cli.md) |

## 相关文档

- [CLI 参考](./cli.md) · [配置详解](./configuration.md) · [架构总览](../architecture/overview.md)
