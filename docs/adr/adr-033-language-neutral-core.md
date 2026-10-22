# ADR-033：语言无关的 Git 模块证明核心

- **状态**：已定
- **日期**：2026-10-20
- **范围**：包边界、适配器接口、未来多语言抽取
- **上游**：[ADR-031](./adr-031-module-collections.md)、[ADR-032](./adr-032-scoped-content-proof.md)

> **结论**：把“Git 模块的可证明分发”抽成语言无关的核心层，JS/TS 只是第一个适配器。核心只认识 `repo + commit + subPath + digest + subtreeOid + version + dependencies`，不认识 `package.json`、npm、TypeScript、esbuild。未来可以独立成 Go module/仓库，供 Python/Go/Rust 等使用。
>
> **代价**：当前仓库内需要一次渐进式分层（不是一次性大搬迁）；JS 专属逻辑必须被识别并收进适配器；核心 API 一旦发布就要遵守语义化版本，不能随 JS 需求随意变形。

---

## 背景

ngm 现在面向 JS/TS：`package.json`、`exports`、Node/Deno runtime、esbuild/tsc/postcss adapter 都写在产品里。但底层能力（Git ref/tree、规范化清单、`archiveDigest`/`moduleDigest`、lock、content store、resolver、verify）与语言无关。

AI 时代，模块集合会跨语言。如果核心继续假设 `package.json`，未来只能复制一套；如果现在就把边界切出来，后续多语言只是新增适配器。

## 决策

### 1. 分层

```
┌────────────────────────────────────────────┐
│ CLI / 产品层                                │
│  ngm（JS-first）  ngm-core CLI（未来）     │
└───────────────┬────────────────────────────┘
                │
┌───────────────▼────────────────────────────┐
│ ngm-core：语言无关                          │
│  git / commit-tree / canonical manifest     │
│  moduleDigest / subtreeOid                  │
│  lock / store / resolver / verify / index   │
└───────────────┬────────────────────────────┘
                │ adapter interface
┌───────────────▼────────────────────────────┐
│ 语言适配器                                  │
│  js（当前）：package.json/exports/TS/引擎   │
│  future：python / go / rust / ...           │
└────────────────────────────────────────────┘
```

### 2. 核心边界

核心可以依赖：

- Git 对象模型、`git` 子进程；
- 规范清单与 digest；
- lock 格式、content store；
- 解析/索引/缓存/verify 的通用算法。

核心**不得**依赖：

- `package.json`、`exports`、`main`/`types` 的 JS 语义；
- npm/pnpm registry 概念；
- TypeScript、esbuild、tsc、postcss；
- Node/Deno runtime 选择。

### 3. 适配器接口（概念）

- `ManifestReader`：给定 `(commit, subPath)`，返回模块元数据与依赖声明；
- `EntryResolver`：推断模块入口/类型入口（JS 的 `exports` 属于这里）；
- `Runtime`：语言运行时约束（Node/Deno/Python/...）；
- `EngineCatalog`：构建/类型检查/CSS 等外部工具；
- `FileClassifier`：哪些文件属于模块内容、哪些是构建产物/忽略项（核心默认全部纳入，适配器可声明例外，但例外必须进入 digest 规则）。

接口以“数据”为主，不返回语言对象；核心不执行依赖代码。

### 4. 包布局（渐进式）

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

- **不要求一次性搬迁**：新代码进 `internal/core/...`；旧包在边界稳定后逐步移动；
- 迁移期间用一个机械判据守住边界：`internal/core` 不得 import `internal/lang/js`、`internal/adapter`、`internal/mappings`；
- `internal/mappings` 目前含 JS 入口推断，未来拆成“核心映射数据 + JS 入口适配器”。

### 5. 规格与版本

- `ngm-archive-digest/v1`、`ngm-module-digest/v1`、lock v2、index schema v1 都是**独立规格**；
- 核心 API 语义化版本；规格升级必须附冻结向量与兼容策略；
- 多语言适配器共用一个 conformance suite（语言无关 fixture + 黄金向量）。

### 6. 抽取节奏

1. 先把模块集合与模块级证明落在现有仓库（v0.101–v0.104）；
2. 同期识别 JS 专属逻辑，新增代码不再进核心；
3. 边界稳定后，把 `internal/core` 抽成独立 Go module；
4. 再提供 `ngm-core` 库/CLI；JS 的 `ngm` 作为第一个消费者；
5. 新语言适配器只实现接口，不复制 Git/digest/lock/verify。

**实现状态（v0.104）**：已有 import 边界判据
`TestCoreBoundaryForbidsLanguageSpecificImports`——`atomicfile`/`digest`/`errs`/`git`/`lock`/`resolve`/`vendor`
不得 import `adapter`/`attest`/`config`/`integrations`/`mappings`/`sbom`。
物理搬迁已完成：`atomicfile` / `digest` / `errs` / `git` / `lock` / `resolve` / `vendor` / `verify`
全部在 `internal/core/`；`internal/core/modproof` 是中性 facade。

**孵化外部模块**：`gitee.com/idcu-go/modproof` 已建立，包含 facade 与 `git`/`digest`/`errs` 的初始快照；
idcu-go `modules.yaml` 登记为 `planned`（单消费者例外），尚未满足双消费者/覆盖率/`module.yaml`/tag 门禁。
ngm 仍使用自己的 `internal/core/modproof`，尚未切换外部依赖。

## 后果

- 短期会增加一层目录与接口，但避免未来复制整个产品。
- 核心的命名、错误码、JSON 形状要按“跨语言公共契约”设计，不能只服务 JS。
- 现有 `package.json` 警告、`exports` 入口推断、esbuild 适配器仍留在 JS 适配器；核心只接收解析后的数据。
- 多语言不是“再写一个 ngm”，而是“同一个核心 + 新适配器”。

## 相关文档

- [语言无关核心与适配器](../architecture/core-and-adapters.md)
- [模块集合架构](../architecture/module-collections.md)
- [引擎适配器](../architecture/engine-adapter.md)
- [运行时模型](../architecture/runtime-model.md)
