# ngm 文档

> **ngm 只解决一个问题**：当依赖直接来自 Git 仓库时，证明"我正在运行的代码"就是"我审过的那份代码"。
> 它不是 npm / pnpm / Yarn / Bun / Vite 的通用替代品。

本页是**保留文档的唯一索引**。同一件事只在一个地方维护：能力归属看[能力矩阵](./internals/capability-matrix.md)，
退出码看[可观测性](./architecture/observability.md)，决策看[ADR 汇总表](./adr/README.md)。

---

## 从这里开始

| 目的 | 文档 |
|------|------|
| 第一次接触：装好、跑通、查词、懂配置 | [安装](./guides/installation.md) → [快速上手](./guides/quickstart.md) → [术语表](./guides/glossary.md) → [配置详解](./guides/configuration.md) |
| 查命令与 flag | [CLI 参考](./guides/cli.md) |
| 增删改依赖、处理冲突 | [依赖管理](./guides/dependency-management.md) |
| 构建 / 类型检查 / 编译 CSS | [构建](./guides/build.md) |
| CI 与发布产物 | [发布](./guides/publishing.md) |
| 给语句签名（ngm 不自签） | [签名配方](./guides/signing.md) |
| 从 npm / pnpm 迁过来 | [迁移指南](./guides/migration.md) |
| 给项目配测试 | [测试](./guides/test.md) |
| 先搞懂"ngm 不是 npm" | [定位说明](./guides/positioning.md) · [可跑示例](./guides/examples.md) |

---

## 最小闭环

```
ngm init → ngm add → ngm install → ngm verify → ngm build
```

声明用 refType，解析得到 commit，内容 digest 写进 lock，vendor 落地，verify 复核漂移与完整性。
CI 上用锁死"装什么"和"怎么拿"的组合；两个 flag 的分工见[锁定机制](./architecture/locking.md)。

---

## 文档地图

| 目录 | 内容 |
|------|------|
| [guides/](./guides/) | 使用者文档：安装、上手、配置、依赖管理、构建、CLI、术语、发布、迁移、签名、测试、定位、示例 |
| [architecture/](./architecture/) | 贡献者文档：数据流 / 状态 / 失败语义 / 边界（见下表） |
| [adr/](./adr/README.md) | 架构决策汇总表 ADR-001 ~ ADR-058（唯一 ADR 汇总文件） |
| [internals/](./internals/) | 能力矩阵（能力归属与成熟度的唯一事实源） |
| [development/](./development/README.md) | 开发总览（历史开发线的索引） |
| [DEVELOPMENT.md](./DEVELOPMENT.md) | **开发者唯一入口**：构建、测试、加命令、加引擎、纪律、文档规则 |
| [deletions.md](./deletions.md) | 大删除记录（本次历史文档清理的逐条台账） |

### 架构六篇 + 可观测性

| 主题 | 回答什么 |
|------|---------|
| [架构总览](./architecture/overview.md) | 整体数据流、三层运行时、子系统与失败语义 |
| [依赖解析](./architecture/dependency-resolution.md) | 声明怎么变成 commit 与 digest，传递依赖与冲突怎么处理 |
| [锁定机制](./architecture/locking.md) | lock 的状态字段、可复现定义、frozen/offline 的失败语义 |
| [引擎 adapter](./architecture/engine-adapter.md) | 引擎怎么选、怎么调、三类失败怎么区分 |
| [安全模型](./architecture/security-model.md) | 权限施加点、沙箱边界、凭证与输入校验 |
| [供应链防护](./architecture/supply-chain.md) | 晾晒期、白名单、OSV、postinstall、verifyOnLock |
| [可观测性](./architecture/observability.md) | 退出码契约、verify/why/tree/outdated/audit/doctor 的判定与输出 |

---

## 文档规则

- 每篇先结论后细节，不写历史叙事、性能读数、版本回顾；单文件 ≤400 行
- 同一事实只在一处维护，其余引用；引用用文件级链接，锚点只在目标标题真实存在时使用
- 决策的当前结论在 [ADR 汇总表](./adr/README.md)；实现与决策冲突时先修订决策，再改代码
- 删除历史文档留账在 [deletions.md](./deletions.md)；验证命令见 [DEVELOPMENT.md](./DEVELOPMENT.md)

## 许可证

MIT，见仓库根目录的 [`LICENSE`](../LICENSE)。
