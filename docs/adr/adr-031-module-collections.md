# ADR-031：一个仓库承载多个模块（Module Collections）

- **状态**：已定
- **日期**：2026-10-20
- **范围**：依赖身份、模块清单、集合解析、版本/发布模型
- **相关**：[ADR-032](./adr-032-scoped-content-proof.md)（模块级证明）、[ADR-033](./adr-033-language-neutral-core.md)（核心抽离）

> **结论**：一个 Git 仓库可以承载多个模块；模块身份是 `repo + subPath`，模块自己的 `ngm.json` 是它依赖声明的权威来源。仓库根 `ngm.json` 只在子路径没有自己的清单时回退，并必须输出 warning。
>
> **代价**：仓库成为内容与信任的边界（详见 ADR-032）；同一依赖在消费者图里仍只能有一个版本；每一层新增的“模块”概念都必须有机械判据，否则会退化成第二套事实源。

---

## 背景

ngm 的 Git 层从一开始就是“仓库 = 依赖”。`subPath` 在 v0.94 已能端到端消费一个 monorepo 子包：过滤子目录、写 `subPath`、按子包自己的 `exports` 推入口。但传递依赖展开仍固定读仓库根 `ngm.json`，所以“每个模块有自己的依赖”并不成立。

同时，AI 辅助开发让“一个人维护几十上百个模块”成为现实。系统必须允许一个仓库承载一个公司/个人的模块集合，而不是迫使每个小模块单独建仓库。

## 决策

1. **模块身份是 `repo + subPath`**；`subPath` 为空表示整仓依赖。
2. **模块清单优先**：解析子路径依赖时先读 `<subPath>/ngm.json`；存在即权威，不再混入根清单。
3. **根清单回退**：子路径没有自己的清单时，回退到仓库根 `ngm.json`，并输出一条 warning，点明缺失的路径与“如何变成自包含模块”。
4. **不做 workspace**：没有共享 lock、没有 hoisting、没有“一次 install 所有模块”。每个消费者项目仍只有一张 lock。
5. **版本是标签，commit 是真相**：模块可以有自己的 semver；tag/branch/index 都只是找到 commit 的方式。正式验证仍以 `commit + subPath` 为锚。
6. **tag 不是必需**：支持 `commit` / `branch` / `version` 三种消费模式；快速验证不需要发布。
7. **索引是派生缓存**：集合索引只做发现与 `outdated`，不参与内容证明；它可以滞后，不能领先（见 ADR-032 的发布纪律）。
8. **不做兼容**：项目尚无用户，直接采用 v2 语义；不保留 v1 过渡态，不写迁移命令。
9. **非目标**：registry 式裸名解析、同一依赖多版本共存、模块级签名、跨仓库搜索、workspace 编排。

## 后果

- 每个被消费的子路径在 lock 中是一条独立条目，key 为 `name#subPath`。
- 解析器必须按 commit 批处理模块清单与 digest；逐模块整树扫描在 AI 规模下不可接受（见 ADR-032）。
- CLI 必须能按模块寻址（`repo#path=...`）；只给仓库名且匹配多个模块时，必须报歧义而不是静默全选。
- 集合索引、版本文件、tag 都是可选的发布辅助；删掉它们不能影响“按 commit 安装/验证”。
- 仓库权限、LFS/submodule 拒绝、OSV 归属仍是仓库级；这些边界必须写进文档，不能假装模块级。

## 相关文档

- [模块集合架构](../architecture/module-collections.md)
- [语言无关核心与适配器](../architecture/core-and-adapters.md)
- [依赖解析](../architecture/dependency-resolution.md)
- [锁定机制](../architecture/locking.md)
