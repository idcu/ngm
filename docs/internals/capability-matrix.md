# 能力矩阵

> **结论**：这是能力归属与成熟度的唯一事实源。只列当前代码里的真实状态：已实现的命令与 adapter，
> 以及明确不做的目标。版本读数、竞品明细、历史评估不在本表。

---

## 已实现

命令共 29 条，全部在 `cmd/ngm/root.go` 的 `commands` 表里注册；按职责分四组：

| 分组 | 命令 | 状态 |
|------|------|------|
| 项目与依赖 | `init` / `adopt` / `add` / `install` / `update` / `migrate` / `remove` | done |
| 集合与运维 | `modules` / `doctor` / `cache` / `store` / `gc` / `config` / `engines` | done |
| 复核与观测 | `verify` / `audit` / `why` / `tree` / `outdated` | done |
| 构建与协议 | `run` / `typecheck` / `typedecl` / `build` / `transform` / `css` / `mappings` / `integrations` / `sbom` / `attest` | done |

adapter 四种：`embed`（兜底 stub）、`subprocess`（默认）、`wasm`（WASI 命令模块）已实现；
`remote` 按决定排除，不会复活。

近期新增能力均已落地：构建回执（旁车文件、不进 lock）、回执复验、本地构建缓存、只读执行计划、
引擎版本/摘要绑定、CSS 压缩 fail-closed、过期 lock 默认拒绝导出。回执只记录事实，不承诺跨工具可复现；
产物证明"谁说的"，不证明"内容无害"。

## 非目标

不做 registry / semver / publish / workspace / 多版本共存；不自研 bundler / minifier / test runner /
LSP / dev server / HMR；不做通用包管理器生态（见 [ADR 汇总表](../adr/README.md) ADR-044）。
`remote` adapter 因源码要离开本机、产物无法本地证明而永久排除（ADR-013 / ADR-020）。
边界细节见[架构总览](../architecture/overview.md)与[安全模型](../architecture/security-model.md)。

---

## 机器可读索引（V222 门禁）

> `TestV222CapabilityMatrixMatchesCode` 只读下面标记之间的三列表格，与 `root.go` 的 `commands` 表和
> `catalog.go` 的 `AdapterKind` 声明双向核对：`done` 必须真注册，`excluded` 不得偷偷复活。
> 改正文时同步本节；解析边界外的说明不是门禁输入。

<!-- v222:begin -->
| 类别 | 名称 | 状态 |
|------|------|------|
| command | init | done |
| command | adopt | done |
| command | add | done |
| command | install | done |
| command | run | done |
| command | update | done |
| command | migrate | done |
| command | remove | done |
| command | modules | done |
| command | doctor | done |
| command | verify | done |
| command | audit | done |
| command | why | done |
| command | tree | done |
| command | outdated | done |
| command | typecheck | done |
| command | typedecl | done |
| command | build | done |
| command | transform | done |
| command | css | done |
| command | mappings | done |
| command | integrations | done |
| command | cache | done |
| command | store | done |
| command | gc | done |
| command | config | done |
| command | engines | done |
| command | sbom | done |
| command | attest | done |
| command | registry | excluded |
| command | semver | excluded |
| command | publish | excluded |
| command | workspace | excluded |
| command | remote-build | excluded |
| adapter | embed | done |
| adapter | subprocess | done |
| adapter | wasm | done |
| adapter | remote | excluded |
<!-- v222:end -->

## 相关文档

- [文档索引](../README.md) · [开发者入口](../DEVELOPMENT.md)
- [引擎 adapter](../architecture/engine-adapter.md) · [ADR 汇总表](../adr/README.md)
