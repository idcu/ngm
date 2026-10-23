# v0.101 计划：模块集合的地基

> 来源：[ADR-031](../adr/adr-031-module-collections.md)、[ADR-032](../adr/adr-032-scoped-content-proof.md)、[ADR-033](../adr/adr-033-language-neutral-core.md)、[ADR-034](../adr/adr-034-module-cache.md)。
> 一句话目标：让“一个仓库多个模块、每个模块自己的依赖”真正成立，并把后续模块级证明/缓存/多语言的地基写清楚。

---

## 1. 本版范围

### 必须做

1. **模块清单优先**：子路径依赖先读 `<subPath>/ngm.json`；缺失回退根 `ngm.json` 并 warning。
2. **`verify --deep` 的 subPath 回归**：整仓 `archiveDigest` 重放必须吃整仓条目，不能吃收窄后的 store 条目。
3. **CLI 按模块寻址**：`repo#path=<sub>` 作为命令目标；只给仓库名且多个匹配时列出候选并 exit 3。
4. **缓存**：ref 解析、manifest 读取、material/digest、verify 重放按 commit 记忆化。
5. **文档/ADR**：模块集合、模块级证明、语言无关核心、module cache 的边界。
6. **验收 fixture**：一个根 + `packages/a`（有 manifest）+ `packages/b`（无 manifest）的 monorepo。

### 不做（留给后续版本）

- lock v2 / `moduleDigest` / `subtreeOid`（v0.102）。
- 集合索引、版本文件、`modules` 命令（v0.103）。
- module cache / partial clone（v0.104）。
- workspace、多版本依赖、模块级签名/信任。

---

## 2. 已落地（本版第一增量）

| 项 | 位置 | 判据 |
|----|------|------|
| 子路径模块清单优先 + 根回退 warning | `internal/core/resolve/graph.go` | `TestResolveGraph_ModuleManifestWinsAndRootFallbackWarns` |
| package.json 提示按实际清单目录 | `internal/core/resolve/graph.go` | 同上（模块目录/根目录） |
| `verify --deep` 的 subPath 回归 | `internal/core/verify/verify.go` | `TestV94SubPathEndToEnd` 新增 `--deep` |
| 错误文本用 `name#subPath` 定位 | `internal/core/resolve/graph.go` | 解析包测试 |
| CLI 目标解析 `#path=` / `#sub` | `internal/core/resolve/target.go` | `TestParseTarget` |
| `update` / `verify` 按模块寻址、歧义报错 | `cmd/ngm/update.go`、`cmd/ngm/verify.go` | `TestSelectTargetsDistinguishesSubPaths`、`TestFilterLockDistinguishesSubPaths` |
| `remove` 按子路径精确删除 | `internal/config/io.go`、`cmd/ngm/remove.go` | `TestRemoveDependencyAtSubPath` |
| ref/manifest 解析缓存 | `internal/core/resolve/graph.go` | 解析包全量测试；同 ref 只解析一次 |
| digest/material 缓存 | `internal/core/verify/verify.go`、`cmd/ngm/material_cache.go` | subPath e2e、verify 包测试 |
| 模块级清单 `ngm-module-digest/v1` | `internal/core/digest/manifest.go` | `TestBuildModuleManifest_*`（头部、域分离、空模块） |
| `subtreeOid` 绑定原语 | `internal/core/git/tree.go` `TreeOIDAtPath` | `TestTreeOIDAtPath`（子路径/根/文件/越界） |
| 模块 material | `internal/core/git/tree.go` `BuildModuleMaterial` | `TestBuildModuleMaterial_OnlySubtree`（只收窄子树、相对路径、位置无关） |
| lock 模块证明字段 | `internal/core/lock/schema.go` | `TestV102ModuleProofFields` / `TestV102ModuleProofRoundTrip`（成对、只限子路径、序列化） |
| install/update 双写模块证明 | `cmd/ngm/module_proof.go`、`install.go`、`update.go` | v0.94 e2e 的 lock 字段断言 |
| verify 模块 binding/digest/version | `internal/core/verify/verify.go` | v0.94 e2e：篡改 `moduleDigest` 后 exit 2 |
| 批量模块材料（一次 ls-tree + 一次 cat-file） | `internal/core/git/tree.go` `BuildModuleMaterials` | `TestBuildModuleMaterials_Batch`（与单模块一致、无兄弟泄漏） |
| install/update 按 commit 批量算模块证明 | `cmd/ngm/module_proof.go` | v0.94 e2e 仍通过 |
| content store 模块 scope | `internal/core/vendor/content.go`、`links.go` | v0.94 e2e：`moduleDigest` 条目路径相对模块根 |
| vendor 从模块 scope 落地 | `cmd/ngm/env.go`、`install.go`、`update.go` | v0.94 e2e + M3/M4/M5 通过 |
| clean v2 lock | `internal/core/lock/schema.go` | `lockfileVersion 2.0.0`；模块条目只写 moduleDigest/subtreeOid |
| verify landing 模块 scope | `internal/core/verify/verify.go` | 模块 store 的 `--deep` 重放；v0.94 通过 |
| SBOM/why 使用内容 digest | `internal/sbom/entries.go`、`internal/observability/why.go` | SBOM/observability 测试 |
| refType 窄默认 | `internal/config/validate.go`、`internal/core/resolve/graph.go`、`cmd/ngm/add.go` | `TestDependencyValidate_DefaultsRefTypeForFullCommit`、`TestAdd_FullCommitDefaultsRefType` |
| `modules list/validate/index` | `cmd/ngm/modules.go` | `TestModulesListAndValidate`、`TestModulesIndexWritesDeterministicCollection` |
| 索引 releases（本地 tag） | `cmd/ngm/modules.go` | `TestModulesIndexCollectsReleaseTags` |
| 索引 branch + source 锚 | `cmd/ngm/modules.go` | `TestModulesIndexUpdatesLocalBranch` |
| 索引一致性校验 | `cmd/ngm/modules.go` | `TestModulesValidateIndexConsistency` |
| 核心 import 边界判据 | `cmd/ngm/core_boundary_test.go` | `TestCoreBoundaryForbidsLanguageSpecificImports` |
| `--infer-ref-type` 远端证据推断 | `internal/core/resolve/infer.go`、`cmd/ngm/add.go` | `TestInferRefTypeFromRemote`；ADR-036 |
| 快速验证指南 | `docs/guides/dependency-management.md` | 文档判据 |
| partial clone 模块缓存 | `internal/core/git/clone.go`、`internal/core/vendor/module_cache.go` | `TestClonePartial_*`、`TestModuleCacheEnsure`、`TestV104InstallUsesModuleCache`；ADR-034 |
| verify 离线模块缓存复用 | `internal/core/verify/verify.go`、`cmd/ngm/verify.go` | `TestV104VerifyOfflineUsesModuleCache` |
| `store usage` 报告 module cache | `internal/core/vendor/module_cache.go`、`cmd/ngm/store.go` | `TestModuleCacheEnsure`（Usage）、v104 install e2e |
| release 索引分片（本地 + 分支） | `cmd/ngm/modules.go`、`internal/core/git/exec.go` | `TestModulesIndexWritesReleaseShards`、`TestRunWithStdin` |
| 逐模块 publish 覆盖全局 | `internal/config/config.go`、`cmd/ngm/modules.go` | `TestModulesIndexPerModulePublishOverridesGlobal`；ADR-037 |
| 核心目录物理抽离 | `internal/core/{atomicfile,digest,errs,git,lock,resolve,vendor,verify}` | 全仓 import 更新；`TestCoreBoundaryForbidsLanguageSpecificImports` |
| 外部 `modproof` 发布与消费 | `gitee.com/idcu-go/modproof` | `v0.2.0`；覆盖率 84.2%；ngm 已切到外部 proof；idcu-go `planned`，仅剩双消费者门禁 |

**关键口径**：

- 模块 manifest 存在时，根依赖**不得**混入；
- 回退时 warning 必须点明 `packages/b/ngm.json` 与 “repository-root”；
- `--deep` 重放整仓 `archiveDigest` 用整仓条目；`storeEntries` 只用于 vendor 比对与逃逸 symlink。

---

## 3. 剩余任务

### 3.1 CLI 模块寻址（已完成）

- `resolve.ParseTarget`、`update` 的 `selectTargets`、`verify` 的 `filterLock`、`remove` 的精确删除；
- 剩余：`why` 统一 `#path=` 与 `#sub` 写法（它已支持 `#sub`）；
- 测试：两个子路径同仓库，分别 `update`/`verify`/`remove`；裸名歧义；错误提示含候选。

### 3.2 缓存与批处理地基（已完成缓存，批处理留给 v0.102）

- `(repo, refType, ref) → commit` 缓存；
- `(commit, manifestPath) → manifest bytes` 缓存；
- `(mirror, commit) → material/digest` 缓存；
- `verify` 的 digest 重放按 `(mirror, commit)` 记忆化；
- 目标：同 commit 的 N 个模块不重复整树扫描；同 ref 不重复 `ls-remote`。
- v0.102 再做一次 `ls-tree -r -t` + `cat-file --batch` 的 `BuildModuleMaterials`。

### 3.3 文档

- 更新 `docs/architecture/dependency-resolution.md`（已完成）；
- 更新 `docs/guides/js-git-library-standard.md`（已完成）；
- 新增 `docs/architecture/module-collections.md`（已完成）；
- 新增 `docs/architecture/core-and-adapters.md`（已完成）；
- 根 README / docs 索引在 v0.101 复盘时同步。

---

## 4. 验收

1. 一个 monorepo fixture：
   - 根 `ngm.json` 依赖 `root-dep`；
   - `packages/a/ngm.json` 依赖 `module-dep`；
   - `packages/b` 没有 manifest；
   - tag `v1`。
2. 依赖 `#path=packages/a`：只解析 `module-dep`，不解析 `root-dep`，无 warning。
3. 依赖 `#path=packages/b`：解析 `root-dep`，warning 点明 `packages/b/ngm.json`。
4. `verify --deep` 对 `#path=packages/a` 通过（整仓 digest 重放口径正确）。
5. `update repo#path=packages/a` 只更新 A；裸 `repo` 且两个子路径 → exit 3 + 候选。
6. 同 commit 的 A/B：一次 `ls-tree`、一次 `cat-file --batch`（v0.101 若未完全批处理，至少缓存命中，不重复整树）。
7. 旧仓库（只有根 manifest）：lock 除 `resolvedAt` 外字节一致。

---

## 5. 风险

| 风险 | 处置 |
|------|------|
| 回退 warning 噪声 | 只在消费子路径时出现；文案点明“如何变成自包含模块” |
| 模块 manifest 元数据不完整 | 解析只读 `dependencies`；`modules validate --release` 在 v0.103 才要求完整 |
| CLI 歧义 | 不静默全选；列出候选并 exit 3 |
| 缓存把不同 commit 混在一起 | key 必须含 `commit`/`mirror`；测试覆盖 |
| 后续 v0.102 改 lock | 本版不引入 `archiveDigest` 的替代字段；v0.102 一次性 clean v2 |

---

## 6. 下一版

v0.102 已完成：模块清单、`subtreeOid`、批量材料、lock v2、verify binding/digest/version、
模块 content store、模块 vendor 落地、verify landing 模块 scope、SBOM/why 内容 digest。
v0.103 已完成：refType 窄默认、`modules list/validate/index`、release 索引、
本地 `ngm-index` 分支与 source 锚、索引一致性校验、核心 import 边界判据。
v0.104 进行中：`--infer-ref-type`、快速验证指南、`git.LSRemote` 参数顺序修复、
partial clone 模块缓存（install-from-lock + verify --offline）、`store usage` 层 1.5 报告、
本地 release 索引分片、分片 branch 批量 tree 写入、逐模块 publish 覆盖全局已完成；
核心目录物理抽离已完成；`gitee.com/idcu-go/modproof v0.2.0` 已发布，
ngm 的模块 proof 已切换到外部依赖并删除内部 facade；idcu-go 仍登记 `planned`（仅剩双消费者门禁）。
见 [ADR-032](../adr/adr-032-scoped-content-proof.md)、
[ADR-034](../adr/adr-034-module-cache.md)、
[ADR-036](../adr/adr-036-infer-ref-type-opt-in.md)、
[ADR-037](../adr/adr-037-per-module-publish-override.md) 与
[模块集合架构](../architecture/module-collections.md)。
