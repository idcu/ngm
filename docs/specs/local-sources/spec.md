# Spec：本地来源（local-git / local-dir）与通用包管理器定位

- **基线**：`2165127`（ngm 仓，2026-11-21；工作树仅有预存未跟踪目录 `2026-11-11/`）
- **依据**：[ADR-052](../../adr/adr-052-local-sources.md)（双证明模型、版本策略、边界）
- **范围**：ADR-052 的配套改造规格；**A→E 五批已授权实施**（2026-11-22）
- **规模**：8 类、约 50+ 文件（其中 Go 源码 ~28、测试 ~15、文档 ~10）

> **实施状态（2026-11-22）**：A（版本契约 3/3.0.0）、B（localfs）、C（refType/config/resolve/lock/install 分流）、
> D（verify 第二条证明链）、E（文档）**均已落地**；两条端到端判据通过：
> `TestLocalDirInstallVerifyE2E`（install → verify → 改目录 → exit 2）与
> `TestLocalGitInstallVerifyE2E`（本地 git 仓 → install → verify）。
> **实施完成（2026-11-22）**：A/B/C/D/E 五批全部落地；`ngm add local:*`（含 dry-run）、
> `ngm update --all` 刷新 local-dir digest 均已实现。四条端到端判据通过：
> `TestLocalDirInstallVerifyE2E`、`TestLocalGitInstallVerifyE2E`、`TestAddLocalSourceE2E`、
> `TestUpdateRefreshesLocalDirDigest`。

> **结论**：改造的难点不在 schema，而在 **resolve/graph 的路径分流** 与 **verify 的第二条证明链**。
> 按"先冻结契约与判据 → 再改 schema/lock → 再改解析与 vendor → 最后改 verify"的顺序推进，
> 每一批先写会红的判据，再实现，避免"装得进去但验不出结论"的中间态。

## 0. 不变量（任何批次不得破坏）

1. **定位不变量**：来源通用 ≠ 生态兼容——不引入 registry 协议、不解析 semver/peer deps、
   不接管 `node_modules`、不做 publish、不为 vendored 依赖生成 `.bin` shim（ADR-043 补记）。
2. **可证明性不变量**：每条 lock 条目必须有可离线重放的内容锚点（git 来源 commit、local-dir 来源 dirDigest）。
   禁止出现"装得进去但 verify 无结论"的条目。
3. **信任网关不变量**：local-git 仍走 `internal/core/git`（ADR-038）；local-dir 走新增 `internal/core/localfs`，
   两者不混用。
4. **确定性不变量**：lock / 清单 / SBOM / attest 同输入同字节；dirDigest 的排除集是契约（ADR-052 §3 边界 4）。
5. **权限闸门不变量**：本地路径读取不申请 `net:*`，但必须过路径逃逸校验（`run:script` 等不变）。
6. **文档同源**：README 现 599 行（上限 600；本轮为恢复 discipline 第 11 问合并一行，ADR 计数仍在 :593），以改写为主；ADR 计数 52，三处同步由 V71/V142 守。

---

## 1. schema 与声明（Difficulty: 中）

| 文件 | 位置 | 改造点 |
|---|---|---|
| `internal/core/resolve/reftype.go` | `:11-35` | 加 `RefTypeLocalGit="local-git"` / `RefTypeLocalDir="local-dir"`；`ValidRefTypes()` 与 `IsValid()` 纳入 |
| `internal/config/config.go` | `:38,43,111-118` | `SchemaVersion = 3`；`SupportedSchemaVersion` 只接受 3；`Dependency` 加 `SourcePath`/`LocalGitRepo`/`DirDigest` 字段（注意 `decodeProject` 的 `DisallowUnknownFields`，`:412`） |
| `internal/config/validate.go` | `:130-161,385-392` | `Dependency.Validate` 按 refType 分支：local-git/local-dir 不调 `ParseSlug`、不要求 40-hex ref；校验 `sourcePath` 为安全相对路径且存在；local-dir 的 ref 必须是 `sha256:<64hex>` |
| `internal/core/lock/schema.go` | `:42-54,70-116,258-330,527-532` | `FileVersion = "3.0.0"`；`Dependency` 加 `sourcePath`/`dirDigest`/`localRepo`（均 omitempty）；`:268/:272/:279/:296/:319` 按 refType 分支放宽；`ContentDigest()` 加 local-dir 分支；`validateUnique` 的键纳入来源命名空间 |
| `internal/core/lock/io.go` | `:19,62,76-77` | 通常无需改（`Validate` 自动生效），补往返测试 |

**硬门禁**：`schemaVersion 1/2` 与 `lockfileVersion 2.x` 必须 exit 3，提示"升级声明 / 删 lock 重装"。
现有 `majorVersion` 判据（`schema.go:141-164`）已具备，只需改常量与提示文案。

## 2. 本地来源识别与路径安全（Difficulty: 中高）

新增 `internal/core/localfs/` 包（ADR-052 §3 边界 2）：

| 新文件 | 职责 |
|---|---|
| `internal/core/localfs/path.go` | `ValidateSourcePath` / `ResolveSourcePath` — 拒绝绝对路径、盘符、UNC、URL、`..` 逃逸；纵深防御二次校验 |
| `internal/core/localfs/manifest.go` | `CollectRecords(root)` — 目录遍历 + 排除集（`.git/`、`node_modules/`、`ngm.vendor/`、`.tmp*/`、`ngm.json`）+ mode 映射；`BuildDirDigest(root)` 复用 `digest.BuildManifestWithVersion("ngm-local-digest/v1", records)` |
| mode 映射的**跨平台补充** | Unix 执行位（mode&0111）**或** Windows 可执行扩展名（`.sh/.bash/.cmd/.bat/.ps1/.exe`）。NTFS 不保留 Unix 执行位，仅靠 mode 会让同一目录在两台机器算出不同 digest；模式为 `100644/100755/120000`。这是 ADR-052 §2.3 的实施细化 |
| `internal/core/localfs/hash.go` | `sha256Hex` — 原始字节 → 小写十六进制（digest.Record 口径） |
| `internal/core/localfs/manifest_test.go` | 确定性：同集合不同遍历序 ⇒ 同 digest；改一字节 ⇒ 不同；排除集变更 ⇒ 必须升版本（冻结向量） |

**与 git 层边界**：local-git 不走此包的 digest 计算，只在 `resolveOne` 前用它校验 `sourcePath` 安全，
然后交 `internal/core/git` 做 mirror/refs/tree（ADR-038 不变量）。

## 3. 解析（Difficulty: 高——核心路径分流）

| 文件 | 位置 | 改造点 |
|---|---|---|
| `internal/core/resolve/normalize.go` | `:92-97,213-249` | 新增 local 名称形态：`local:<name>`（独立命名空间，不进 `ParseSlug` 的 `<host>:<path>` 校验） |
| `internal/core/resolve/addspec.go` | `:42-66,73-125` | `ParseAddSpec` 支持本地路径形式（`./dir` 等）；`../` 与绝对路径一律拒绝（§10 决策 1，2026-11-23 纠正本行早期表述），local-git 同样受 `ValidateSourcePath` 约束；`@ref` 语义按 refType 分流 |
| `internal/core/resolve/graph.go` | `:481-511 resolveOne` | 三路分流：git / local-git（跳过 `EnsureMirror`，用 `localRepo` 路径做 mirror）/ local-dir（`ResolveRef` 返回 dirDigest 锚，跳过 git）。`:569-659 readUpstreamDeps` 对 local 从 `sourcePath/ngm.json` 读 |
| `internal/core/resolve/reftype.go` | `:54-81` | `ResolveRef` 加 local-git（本地 `ls-remote`/`rev-parse`）与 local-dir（`localfs.BuildDirDigest`）分支 |
| `cmd/ngm/env.go` | `:350-362 toDepSpecs`, `:160-214 RemoteRefResolver`, `:578-613 MaterializeVendorAndMappings` | `DepSpec` 加源类型；local 不注入远端 resolver；materialize 按 digest 来源分支 |

## 4. vendor（Difficulty: 高——新 content 来源）

| 文件 | 位置 | 改造点 |
|---|---|---|
| `internal/core/vendor/content.go` | `:270-353 Put`, `:337-341` | 新增 `PutFromLocalDir(root, dirDigest)` — 用 `localfs` 清单构建 content store 条目；现有 `Put(mirror, commit)` 保持 |
| `internal/core/vendor/integrity.go` | `:395-402 VendorPathFor` | local 命名空间 `local/<name>`，与 `host/org/repo` 隔离；`IsSafeVendorPath` 覆盖新前缀 |
| `internal/core/vendor/links.go` | `:128-253 materialize` | 基本可复用（输入是 digest key）；`validateEntryPaths` 覆盖 local 源条目 |
| `internal/core/vendor/module_cache.go` | 全文 | local 无 partial clone，明确短路 |
| `internal/core/vendor/mirror.go` | `:199-279` | local-dir 不走 `Ensure`；local-git 复用 `EnsureLocal` 思路但要求 `localRepo` 显式给出 |

## 5. verify（Difficulty: 高——第二条证明链）

| 文件 | 位置 | 改造点 |
|---|---|---|
| `internal/core/verify/verify.go` | `:212-374 verifyOne`, `:457-502 checkRef`, `:606-647 checkDigest`, `:735-987 checkLanding` | `verifyOne` 三路分流：git/local-git 沿用 `checkRef`+`checkDigest`；local-dir 用新增 `checkLocalSnapshot`（重算 `BuildDirDigest(sourcePath)` 与 lock 的 `dirDigest` 比对，不等 ⇒ `DriftCritical`）。`checkLanding` 的权威形状对 local-dir 来自本地清单而非 `git.ListTree`（`:1088`） |
| `internal/core/verify/report.go` | `:61-86,229-260` | 目录内容变化 ⇒ `DriftCritical` ⇒ exit 2；`--offline` 下 local 仍可验（天然离线） |
| `cmd/ngm/verify.go` | `:114-322` | opts 加 `LocalRoot`；local 不注入 `ResolveRemoteRef` |

## 6. cmd / 输出 / 其他消费者（Difficulty: 中）

| 文件 | 改造点 |
|---|---|
| `cmd/ngm/add.go` | 新 usage + `--ref-type local-git\|local-dir` + 本地路径参数；跳过 `InferRefTypeFromRemote` |
| `cmd/ngm/install.go` | `installFresh` / `installFromLock` 两路径加 local 分支；`VendorPath` 生成适配 `local/` 命名空间 |
| `cmd/ngm/update.go` | 写 lock 时 local 字段；local-dir 的 ref 随目录内容更新**自动重算**（实施改判 2026-11-22：不引入 `--refresh-digest` flag） |
| `tree.go` / `why.go` / `sbom.go` / `attest.go` / `doctor.go` | 消费 `refType`/`commit` 处降级处理；SBOM purl 用 `local:<name>` 方案 |
| `internal/attest/attest.go` | `:54,122` 的 ref 注解支持 local 值；签名/颗statement 形状不变（ADR-027 不变量） |

## 7. 测试矩阵（先写红判据，再实现）

| # | 判据名 | 断言 | 批次 |
|---|---|---|---|
| T1 | `TestLocalDirDigestDeterministic` | 同集合不同遍历序同 digest；改一字节不同；排除集冻结向量 | B |
| T2 | `TestLocalPathRejectsEscape` | 绝对路径 / `..` / 盘符 / scheme 被拒 | B |
| T3 | `TestSchemaV3RejectsV12` | schemaVersion 1/2 → exit 3 | A |
| T4 | `TestLockV3RejectsV2` | lockfileVersion 2.x → exit 3 | A |
| T5 | `TestLocalGitReusesCommitProof` | local-git lock 条目与 git 来源逐字段一致（除 name/vendorPath） | C |
| T6 | `TestLocalDirInstallVerify` | local-dir 声明 → install → verify exit 0；vendor 落地字节与源一致 | C/D |
| T7 | `TestLocalDriftIsCritical` | install 后改本地目录 → verify exit 2（DriftCritical） | D |
| T8 | `TestLocalOfflineVerify` | `--offline` 下 local-dir verify 仍可给出结论 | D |
| T9 | `TestLocalNotWorkspace` | 两个 local 依赖不共享 lock、不 hoisting | C |
| T10 | `TestLocalVendorPathNamespace` | `local/` 与 `host/org/repo` 不冲突、无逃逸 | C |
| T11 | `TestOldGitProjectReinstall` | 存量 git 项目升 3 后重装，依赖字节不变 | E |
| T12 | `TestAdrIndexAndCounts`（计划名；实际落为 `TestV71AdrIndexCoversEveryAdrFile` / `TestV142AdrCountsAgreeWithFiles`） | 52 个 ADR、索引连续、三处计数同步 | A |

**变异验证**：每条判据实现后做一次反向变异（改回旧行为）确认判据变红，否则判据无效（ADR-049 纪律）。

## 8. 文档改造（以改写为主，README ≤600 行）

| 文件 | 改造点 |
|---|---|
| `README.md` | `:3` 定位句 → 通用包管理器；`:5` "锁定 commit" → "锁定 commit 或 dirDigest"；`:7` "管理 Git 仓库依赖" → "git + 本地来源"；`:63` "只接受 Git 地址" → "Git 地址或本地目录"；`:593` ADR 计数已改 52；README 共 599 行（上限） |
| `docs/guides/configuration.md` | schemaVersion 1..2 → 3；refType 三值 → 五值；写明迁移命令仍不存在（守 `doc_examples_test.go:150`） |
| `docs/architecture/dependency-resolution.md` | 新增"来源分流"一节；local-git / local-dir 解析时序 |
| `docs/architecture/locking.md` | lockfileVersion 3.0.0；dirDigest 字段契约；排除集是契约 |
| `docs/guides/cli.md` | `ngm add` 本地路径示例；`--ref-type` 五值 |
| `CHANGELOG.md` | 新增条目：定位升级 + 本地来源 + 破坏性版本变更 |

## 9. 分批实施顺序（每批先红判据、后实现、再变异）

| 批 | 内容 | 判据 | 交付门槛 |
|---|---|---|---|
| **A** | 契约冻结：ADR-052 + 补记（**已交付**）；schemaVersion 3 / lockfileVersion 3.0.0 常量与拒绝旧格式 | T3 T4 T12 | `go test ./...` 绿；旧格式 exit 3 |
| **B** | `internal/core/localfs`（路径安全 + dirDigest） | T1 T2 | 冻结向量通过；变异红 |
| **C** | resolve 三路分流 + lock 新字段 + vendor local 来源 + add/install 分支 | T5 T9 T10 | local-git 与 local-dir 都能 install；vendor 字节正确 |
| **D** | verify 第二条证明链 + report 漂移语义 + update/tree/why/sbom/attest 适配 | T6 T7 T8 | 改动本地目录必被 exit 2 抓住；offline 可验 |
| **E** | 文档改造 + 存量项目重装演练（pivoise 试点） | T11 | README ≤600 行；pivoise schemaVersion 3 + lock 3.0.0 全绿 |

**试点建议**：E 批用 pivoise 做真实消费方——它同时有 11 个 git 模块（ngm-repo）和 11 个本地子包
（`@pivoise/*`），正好覆盖双模型。试点通过后再宣布通用。

## 10. 未决问题（2026-11-22 已拍板）

> 用户授权"按分析决策拍板并全量推进"，以下为实施基线，按此执行不再改判。

1. **跨项目根的 `sourcePath`**：**拒绝**。`ValidateSourcePath` 拒绝绝对路径、`..` 逃逸、盘符与 scheme，
   解析后必须仍在项目根内。跨仓本地依赖不在本轮范围；若未来需要，另立 `local-ext` 类型并显式授权
   （那会引入新的信任边界，不是放宽校验）。
2. **local-git 的 `ref`**：**必填**。沿用 ADR-004 的 refType/ref 必填纪律，不允许省略默认 HEAD——
   "声明自描述、不靠推断"是 ngm 的核心不变量，本地来源不例外。
3. **dirDigest 的排除集**：**包含 `test/` 与 `docs/`**（即只排除 `.git/`、`node_modules/`、
   `ngm.vendor/`、`.tmp*/`、`ngm.json` 自身）。理由：本地模块的测试与文档也是内容身份的一部分；
   本地目录哈希成本可控，且排除集越小越不容易出现"同一目录不同机器算出不同 digest"的歧义。
   排除集的任何变更必须升 `LocalManifestVersion` 并走 lock MAJOR（ADR-052 §3 边界 4）。
