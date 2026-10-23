# 语言无关核心与适配器

> 本文是 [ADR-033](../adr/adr-033-language-neutral-core.md) 的活文档：哪些能力属于核心，哪些属于语言适配器，以及如何在不复制产品的前提下支持更多语言。

## 1. 分层

```
CLI / 产品层
  ngm（JS-first）        ngm-core CLI（未来）
        │
        ▼
ngm-core：语言无关
  git / commit-tree / canonical manifest
  archiveDigest / moduleDigest / subtreeOid
  lock / content store / resolver / verify / index
        │ adapter interface
        ▼
语言适配器
  js（当前）：package.json / exports / TS / 引擎
  future：python / go / rust / ...
```

## 2. 核心认识什么

- `repo`（canonical slug）、`commit`、`subPath`；
- 规范化清单（path / mode / sha256）；
- `archiveDigest` / `moduleDigest` / `subtreeOid`；
- lock v2、content store、module cache、ref index；
- 依赖声明的最小形状：`name / ref / refType / path`；
- 解析、冲突、校验、批量、缓存、离线完整性。

## 3. 核心不认识什么

- `package.json`、`exports`、`main`/`types` 的 JS 语义；
- npm/pnpm registry 概念；
- TypeScript、esbuild、tsc、postcss；
- Node/Deno runtime；
- 任何语言的对象模型与包管理器配置。

## 4. 适配器接口（概念）

| 接口 | 职责 | JS 示例 |
|------|------|---------|
| ManifestReader | 读模块清单，返回依赖与元数据 | `ngm.json` 的 `dependencies` |
| EntryResolver | 推模块入口/类型入口 | `package.json#exports`、`main`、`types` |
| Runtime | 语言运行时约束 | Node / Deno |
| EngineCatalog | 构建/类型/CSS 外部工具 | esbuild / tsc / postcss |
| FileClassifier | 哪些文件属于模块内容 | `files`、忽略规则（例外必须进入 digest 规则） |

接口以数据为主，不返回语言对象；核心不执行依赖代码。

## 5. 包边界

```
internal/core/git
internal/core/digest
internal/core/lock
internal/core/store
internal/core/resolve
internal/core/verify
internal/core/index
internal/core/cache
internal/lang/js
internal/lang/registry
cmd/ngm
cmd/ngm-core        （未来）
```

迁移纪律：

- 新代码进 `internal/core/...`；
- `internal/core` 不得 import `internal/lang/js`、`internal/adapter`、`internal/mappings`；
- 旧包在边界稳定后逐步移动，不做一次性大搬迁；
- `internal/mappings` 目前含 JS 入口推断，未来拆成“核心映射数据 + JS 入口适配器”。

这条边界由 `TestCoreBoundaryForbidsLanguageSpecificImports` 守住，否则“核心不认识 JS”只是注释。
当前进度：`atomicfile`、`digest`、`errs`、`git`、`lock`、`resolve`、`vendor`、`verify`
已全部迁入 `internal/core/`；物理搬迁完成。

### 孵化中的外部模块：`modproof`

- 仓库：`gitee.com/idcu-go/modproof`；
- 模块路径：`gitee.com/idcu-go/modproof`（已对齐 idcu-go 组织规范）；
- idcu-go 登记：`meta/modules/modules.yaml` 中 `state: planned`，标注单消费者例外；
- 当前内容：从 ngm `internal/core/{modproof,git,digest,errs}` 复制的 facade + 测试 + 冻结向量；
- 测试已平移并通过；模块总覆盖率约 **84.2%**，已达到 84% 门禁；
- `module.yaml` / `docs/api.md` / `docs/api.sig` / Makefile / CI 已补齐，`v0.1.1` 已打 tag；
- 唯一未满足的是**双消费者门禁**；idcu-go 仍登记 `planned`；
- ngm 仍使用自己的 `internal/core/modproof`，**尚未切换到外部依赖**。

这是“特事特办”的显式记录：可以继续用、可以继续推进，但在第二消费者出现前不宣称通用、不作为常态。

## 6. 规格与版本

- `ngm-archive-digest/v1`、`ngm-module-digest/v1`、lock v2、index schema v1 是独立规格；
- 规格升级必须附冻结向量与兼容策略；
- 核心 API 语义化版本；错误码、JSON 形状按跨语言公共契约设计；
- 多语言适配器共用一个 conformance suite：语言无关 fixture + 黄金向量 + 适配器契约测试。

## 7. 抽取节奏

1. 模块集合与模块级证明先落在现有仓库（v0.101–v0.104）；
2. 同期识别 JS 专属逻辑，新增代码不再进核心；
3. 边界稳定后，把 `internal/core` 抽成独立 Go module；
4. 提供 `ngm-core` 库/CLI；JS 的 `ngm` 是第一个消费者；
5. 新语言适配器只实现接口，不复制 Git/digest/lock/verify。

## 8. 非目标

- 核心不管理语言包注册表；
- 核心不执行依赖代码或安装期钩子；
- 核心不内置构建工具；
- 核心不承诺“跨语言依赖解析”本身；它只提供 Git 模块的证明与分发。

## 相关文档

- [ADR-033：语言无关核心](../adr/adr-033-language-neutral-core.md)
- [模块集合架构](./module-collections.md)
- [引擎适配器](./engine-adapter.md)
- [运行时模型](./runtime-model.md)
