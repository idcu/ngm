# 示例工程

> 本文是 `examples/` 下脚手架的**唯一权威指南**：四个示例的 README 只给命令与目录说明，不复制这里的正文。
> 示例只守"结构齐全 + 清单合法 + 命令成形"；它们不冒充"已经在你的机器上跑通过"。

## 四个脚手架

| 目录 | 演示什么 | 关键命令 |
|---|---|---|
| [esbuild-ts](../../examples/esbuild-ts/) | 外部 esbuild 做 bundle 与单文件 transform，mappings 交给构建工具 | `ngm build --engine=esbuild` · `ngm transform` · `ngm integrations add esbuild` |
| [local-dir-offline](../../examples/local-dir-offline/) | `local-dir` 来源 + 提交锁文件后的全离线安装与复验 | `ngm install --offline` · `ngm verify --offline` |
| [deno-engine](../../examples/deno-engine/) | Deno 不在内置清单时，用项目清单显式声明引擎 | `ngm engines validate` · `ngm typecheck --engine=deno` |
| [third-party-engine](../../examples/third-party-engine/) | 不引用内置引擎名，用最小 subprocess 引擎接入 P4 契约 | `ngm engines validate` · `ngm build --engine=thirdparty-bundle` |

## 共同的边界

1. 三个示例都用 `schemaVersion` 3 的 `ngm.json`；结构与清单合法性由 `TestV220ExamplesAreRunnableScaffolds` 守。
2. 构建、压缩、类型检查都交给外部工具，ngm 只做接入与证明：见[定位页](./positioning.md)与[能力矩阵](../internals/capability-matrix.md)。
3. `smoke.sh` 是**骨架**：不联网、不下载工具；工具缺失时打印跳过说明而不是假装成功。要跑通构建需要本机已装对应的外部引擎。
4. 示例不是第二套文档：新增示例时先更新本文，再让各 README 链回这里；不要在各 README 里复述本文段落。

## 命令对照

| 目标 | 权威文档 |
|---|---|
| 为什么这样做 | [定位页](./positioning.md) |
| 配置字段 | [配置详解](./configuration.md) |
| 引擎与 mappings | [构建指南](./build.md) |
| 依赖来源与锁 | [依赖管理](./dependency-management.md) |
| CLI 参数与退出码 | [CLI 参考](./cli.md) |
| 能力归属 | [能力矩阵](../internals/capability-matrix.md) |

## 相关文档

- [生态定位](./positioning.md) · [能力矩阵](../internals/capability-matrix.md)
