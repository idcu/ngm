# ADR-052：通用包管理器定位与本地来源（local-git / local-dir）

- **状态**：已定（2026-11-22）
- **范围**：ngm 产品定位；`ngm.json` / `ngm.lock` 的来源类型与版本；依赖解析、锁定、vendor、verify 的信任模型
- **依据**：本轮 ngm 依赖链路全量源码审计（resolve/lock/vendor/verify 四层）；[ADR-044](./adr-044-independent-ecosystem.md) §重启条件③；[ADR-043](./adr-043-vendored-bin-shim.md) §重启条件③
- **配套规格**：[docs/specs/local-sources/spec.md](../specs/local-sources/spec.md)

> **结论**：ngm 的定位由「Git 依赖的证明层」升级为「**独立生态的通用包管理器**」，主轴仍是"可证明同一性"，
> 但来源从单一 Git 扩展为两类：**git 来源**（网络或本地裸仓库，锚点仍是 commit）与**本地来源**
> （本地目录，锚点是内容摘要 dirDigest）。**不引入 registry、不解析 semver、不接管 `node_modules`**——
> "通用"仅指来源通用，不指生态兼容。为此 `ngm.json` 的 `schemaVersion` 升 **3**、`ngm.lock` 的
> `lockfileVersion` 升 **3.0.0**，旧格式明确拒绝、重新 install，不提供迁移命令。

## 1. 为什么改（重启条件已经满足）

ADR-044 §重启条件逐条对照：

1. 与 npm 在同一 `node_modules` 共管 —— **不满足，本轮明确不做**；
2. 需要 registry 协议或 semver 求解 —— **不满足，本轮明确不做**；
3. 定位变成"依赖的 CLI 安装器" —— **满足**：通用包管理器覆盖并超出这一表述。

ADR-044 明文要求条件③触发时与 ADR-043 §重启条件③一起改；本 ADR 即该次重启的产物。
实际业务需求来自消费方 pivoise：其 `packages/kernel` 与 `packages/packs/*`（11 个子包）与
`ngm.vendor/` 里的 11 个 git 模块应当能用**同一套架构**管理——前者是本地来源，后者是 git 来源。

## 2. 决策

### 2.1 定位（重写 ADR-044 主轴第一句）

| | 旧 | 新 |
|---|---|---|
| 一句话 | Git 依赖的证明层 | 独立生态的通用包管理器：**来源通用，身份可证明** |
| 来源 | Git 仓库 | Git 仓库 **+ 本地目录** |
| 锚点 | commit | git 来源：commit；本地来源：dirDigest（内容快照） |
| 不变量 | 可证明同一性 | **不变**：声明 → 锚点 → 内容 digest → 可离线重放 → 可导出审计 |

"通用"的边界（三条硬约束，不得借机扩张）：**不做 registry 协议**、**不做 semver 求解**、
**不接管 `node_modules`**。vendor 仍是唯一落地形态。

### 2.2 两类来源与两种证明模型（本 ADR 的核心）

| 维度 | git 来源（保持） | local-git（`refType: local-git`） | local-dir（`refType: local-dir`） |
|---|---|---|---|
| 声明 | `name: host:org/repo`, `ref`, `refType: tag\|branch\|commit` | `name: local:kernel`, `ref: v1.0.0`, `refType: local-git`, `path: ./packages/kernel`, `repo: <git 仓根路径>` | `name: local:kernel`, `ref: <dirDigest>`, `refType: local-dir`, `path: ./packages/kernel` |
| 身份键 | `canonical(name)[#subPath]` | `local:<name>[#subPath]`（独立命名空间） | `local:<name>[#subPath]` |
| 不可变锚点 | commit（40-hex） | commit（从本地 git 仓解析，**复用**） | dirDigest（目录清单 sha256，**新建**） |
| 内容证明 | archiveDigest / moduleDigest（git tree） | 同 git 来源（**完全复用**） | dirDigest = `sha256(BuildManifestWithVersion("ngm-local-digest/v1", records))` |
| 离线重放 | mirror + content store | 本地 git 仓 + content store | 源目录 + content store（不依赖网络也不依赖 git） |
| 漂移检测 | `checkRef` + `checkDigest` + `checkLanding` | 同 git 来源 | `checkLocalSnapshot`（重算 dirDigest）+ `checkLanding` |

**为什么两种都要**（用户拍板"两者都支持"）：

- **local-git** 工作量极小——git 层已支持本地路径（`internal/core/git/remoteurl.go:19-72, 99-132`），
  ADR-031 已有 `repo + subPath` 身份，整条 commit → tree → digest 链不用动。
  它服务"本地目录本身在一个 git 仓里"的场景（pivoise 的子包正是如此）。
- **local-dir** 服务"任意本地目录"——没有 git 仓、或不想把本地改动提交成 commit 才能验证的场景。
  它必须新建内容锚点，是本 ADR 的主要工作量。

### 2.3 dirDigest 规范（local-dir 的信任根）

复用现有清单机制，**不新造算法**：

- 记录类型用 `digest.Record`（`internal/core/digest/manifest.go:47-65`）：`{Path, Mode, BlobSHA256}`。
- 清单格式用 `digest.BuildManifestWithVersion`（同文件 `:93-`），只新增版本常量
  `LocalManifestVersion = "ngm-local-digest/v1"`，与 `ManifestVersion` / `ModuleManifestVersion` 域分离，
  避免三类 digest 混用。
- Mode 映射：本地普通文件 → `100644`；可执行文件（Unix 执行位 mode&0111，
  或 Windows 可执行扩展名 `.sh/.bash/.cmd/.bat/.ps1/.exe`——NTFS 不保留 Unix 执行位，
  这是跨平台一致所必需）→ `100755`；符号链接 → `120000` 且 BlobSHA256 = 链接目标字符串的 sha256（与 git blob 语义一致）。
- 路径：相对源目录根、`/` 分隔、无前导 `./`；UTF-8 字节序升序（复用既有排序）。
- **排除集**（必须显式、可审计）：`.git/`、`node_modules/`、`ngm.vendor/`、`.tmp*/`、
  以及 `ngm.json` 自身（本地来源清单由消费方 lock 记录，不把清单文件纳入自身摘要）。
  排除规则写入 `docs/architecture/locking.md`，并有一个机械判据守护（同集合 ⇒ 同字节）。
- 锚点表示：`ref: "sha256:<hex64>"`（与现有 digest 前缀一致），lock 新字段 `dirDigest` 存同一值。

### 2.4 版本策略：升版本 + 拒绝旧格式

| 文件 | 旧 | 新 | 兼容策略 |
|---|---|---|---|
| `ngm.json#schemaVersion` | 写入 1，读取 1..2 | 写入 **3**，读取 **3** | 1..2 明确拒绝（exit 3），提示"重新 `ngm init` 或手工升级声明" |
| `ngm.lock#lockfileVersion` | 2.0.0 | **3.0.0** | MAJOR 不匹配即拒（现有 `majorVersion` 判据已具备），提示"删除 lock 后重新 install" |
| 迁移命令 | 无 | **仍不提供**（沿用 ADR-031 §决策 9"无用户不写迁移"） | 在 `configuration.md` / `locking.md` 写明这是新的显式例外 |

理由：`refType` 是必填枚举，local 值会被旧读取器在 `config/validate.go:148-153` 以 exit 3 拒绝；
lock 的 `commit` 是必填 40-hex（`lock/schema.go:272-276`），local-dir 根本没有 commit。
这是字段**形状**改变，不是可选字段增量，按项目自己的版本规则必须升 MAJOR。

### 2.5 lock 条目新形状

新增字段（均 `omitempty`，git 来源的字节形状不变）：

```jsonc
{
  "name": "local:kernel",
  "ref": "sha256:ab12...",     // local-dir 的 dirDigest；local-git 仍是人类 ref
  "refType": "local-dir",      // 或 local-git
  "sourcePath": "./packages/kernel",   // 新：声明时的本地路径（可含 .. 需校验）
  "dirDigest": "sha256:ab12...",       // 新：仅 local-dir，local-git 为空
  "commit": "",                // local-dir 为空（validate 按 refType 分支放宽 40-hex 要求）
  "vendorPath": "local/kernel",       // 新命名空间，避免与 host/org/repo 冲突
  "requiredBy": ["(root)"]
}
```

`ContentDigest()`（`internal/core/lock/schema.go:527-532`）扩展为按 refType 分支：
local-dir 返回 `dirDigest`，其余沿用 `IsModule()` 分支。`validateUnique` 的键把 source 纳入，
避免"git 的 `host:org/x` 与 local 的 `local:x`"互相冒充。

## 3. 代价与边界

- **代价 1｜信任锚点分裂**：从"一切锚在 git commit"变成"两类锚点"，verify 增加一条完全独立的检查路径
  （`checkLocalSnapshot`）。好处是覆盖本地来源，代价是证明逻辑不再是单一实现。
- **代价 2｜旧项目必须重装**：升 MAJOR 后所有存量 `ngm.json` / `ngm.lock` 被拒，需手工改声明 + 删 lock 重装。
  pivoise 本身要同步改 `ngm.json` 的 schemaVersion（3）并重新 install 生成 3.0.0 lock。
- **代价 3｜ADR 面扩大**：ADR-043/040/044/031/032/002/004 都要写补记（本轮同批完成）。
- **边界 1｜local 不是 workspace**：本地来源仍是一个独立、内容寻址的依赖单元；不共享 lock、不 hoisting、
  不"一次 install 所有本地模块"（守住 ADR-031 §决策 4）。
- **边界 2｜local-git 不等于绕过 git 网关**：本地 git 仓的读取仍走 `internal/core/git`（ADR-038 不变量）；
  local-dir 不读 git，但由新模块 `internal/core/localfs` 承担路径安全与清单生成，二者边界清晰。
- **边界 3｜不做 registry**：本轮任何"通用"都不包括 registry 协议、semver 求解、publish。
- **边界 4｜dirDigest 排除集是契约**：排除规则变化会改变所有 local-dir 的 digest，必须升
  `LocalManifestVersion` 并走下一次 lock MAJOR，不允许静默改规则。

## 4. 判据（可机械验证）

1. `TestLocalDirDigestDeterministic`：同一目录不同顺序遍历 ⇒ 相同 dirDigest；改变一个文件字节 ⇒ 不同。
2. `TestLocalGitReusesCommitProof`：local-git 条目的 lock 形状与 git 来源逐字段一致（除 name/vendorPath）。
3. `TestLocalDriftIsCritical`：install 后修改本地目录内容 → `ngm verify` exit 2（`DriftCritical`）。
4. `TestSchemaV3RejectsV12`：schemaVersion 1/2 的 ngm.json 被拒 exit 3；lockfileVersion 2.x 被拒 exit 3。
5. `TestOldGitProjectReinstall`：存量 git 项目升 3 后重新 install，依赖字节与升级前一致。
6. `TestLocalNotWorkspace`：两个 local 依赖不产生共享 lock、不 hoisting。
7. `TestLocalVendorPathNamespace`：`local/...` 与 `host/org/repo` 不冲突、无路径逃逸。
8. `TestAdrIndexAndCounts`（计划名；实际为 `TestV71AdrIndexCoversEveryAdrFile` / `TestV142AdrCountsAgreeWithFiles`）：新增 ADR-052 后索引连续、三处计数同步。

## 5. 相关文档

- [ADR-031 模块集合](./adr-031-module-collections.md)（身份 = repo + subPath；本 ADR 增加 local 命名空间）
- [ADR-032 模块级内容证明](./adr-032-scoped-content-proof.md)（moduleDigest/subtreeOid；本 ADR 增加 dirDigest）
- [ADR-038 不自研 Git](./adr-038-no-own-git.md)（local-git 仍走 git 网关）
- [ADR-044 独立生态](./adr-044-independent-ecosystem.md)（本 ADR 触发其重启条件③）
- [ADR-043 vendored bin shim](./adr-043-vendored-bin-shim.md)（同上，必须一起改）
- [改造规格](../specs/local-sources/spec.md)（逐文件改造点、测试矩阵、分批计划）
