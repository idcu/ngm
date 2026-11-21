# ngm 功能现状梳理（2026-11-21）

> 依据：[README](../README.md) · [CLI 参考](../guides/cli.md) · [能力矩阵](../internals/capability-matrix.md) ·
> [架构总览](../architecture/overview.md) · `cmd/ngm/root.go` 命令表。本文只做**现状索引**，不替代上述文档；
> 逐条用法的唯一事实源是 [cli.md](../guides/cli.md)。
>
> 基线：`5d383bb`，`git describe --tags` = `v0.102.0-dev.2-165-g5d383bb`；本地无 ldflags 构建显示 `0.0.0-dev`。
> 已纳入 2026-11-21 逐行审计五批修复后的行为（见[落地记录](../specs/assessment-2026-11-21-line-by-line-followup/tasks.md)）。

## 0. 一句话定位

ngm 是**以 Git 为唯一依赖源、以 lock + 内容摘要为事实源、以可审计 vendor 为交付形态**的 JS/TS 依赖与构建编排工具。
差异化不在单项能力，而在**闭环**：显式 `refType` → commit 锁定 → 内容 digest → 漂移三态 → 离线重放 → SBOM/attest。
能力归属 SSOT 见[能力矩阵](../internals/capability-matrix.md)。

**不做**（[README §60](../README.md#边界它不做什么) / [能力矩阵 §47](../internals/capability-matrix.md#明确不做的能力)）：
registry 包管理、semver/peer deps 解析、workspace 依赖、接管 `node_modules`、自造签名体系、
自研 bundler/type checker/transformer/test runner/dev server、自动 git add/commit。
npm/pnpm/Yarn 只是迁移来源与共存对象，不是兼容目标（ADR-044）。

## 1. 命令地图（28 个顶层命令）

| 类别 | 命令 | 核心职责 |
|---|---|---|
| 项目 | `init` · `adopt` · `config` | 初始化清单；只读扫描已有 `node_modules` 出内容证明（`--check` 复核基线）；配置校验/查看 |
| 依赖生命周期 | `add` · `install` · `update` · `remove` | 声明依赖（`refType` 显式、`--infer-ref-type` 消歧）；按 lock 安装；更新 ref 与锁定；移除声明 |
| 验证与解释 | `verify` · `why` · `tree` · `outdated` | 漂移 + digest 重放 + 签名/GitHub/SARIF 输出；来源路径；依赖树；新版本发现 |
| 模块集合 | `modules list\|validate\|index\|changed\|plan` | 嵌套模块发现、一致性校验、确定性索引（可分片）、变更列表、只读版本/tag 建议 |
| 供应链 | `audit` · `sbom` · `attest` | OSV 按 commit 查询 + 沙箱钩子；CycloneDX 1.7 / SPDX 2.3；in-toto Statement v1 + SLSA provenance v1（未签名） |
| 引擎与构建 | `build` · `transform` · `typecheck` · `typedecl` · `css` · `engines` | adapter 调外部引擎；引擎探测/校验（fail-closed） |
| 任务与集成 | `run` · `integrations add` · `mappings validate` | 项目脚本执行（`run:script` 闸门）；生成 vite/esbuild/deno/webpack 集成；映射一致性 |
| 存储维护 | `cache` · `store usage\|prune\|gc` · `gc` | 缓存清理；分层占用可见与残骸/孤儿回收；按当前项目 lock 的显式可达性回收 |
| 诊断 | `doctor` | 本地环境 + 项目 + 四层布局 + vendor Git 可见性诊断 |

`--json` 在 **14 个命令**可用（`run` 仅 `--list` 形态）：`verify` · `audit` · `tree` · `why` ·
`outdated` · `engines` · `integrations` · `install` · `update` · `doctor` · `modules`（五个子命令各自）·
`adopt` · `gc` · `run --list`。形状表见 [cli.md §190](../guides/cli.md#`--json`-的形状机器接口)。

## 2. 数据与状态模型

| 对象 | 作用 | 关键契约 |
|---|---|---|
| `ngm.json` | 声明：依赖三元组、引擎选择、供应链策略、vendor 模式 | 写 `schemaVersion:1`，读 1..2；超出区间 exit 3（[README §96](../README.md#ngmjson-的-schemaversion)） |
| `ngm.lock` | 唯一事实源，必须提交 | lock v2：`commit` + `archiveDigest`/`moduleDigest` + `subtreeOid` + `resolvedAt` + `requiredBy` 图；确定性字节；尾部多余 JSON 现被拒绝 |
| `ngm.mappings.json` | 外部构建工具的裸导入映射 | install 生成；`mappings validate` 与 lock/vendor 对账 |
| `ngm.engines.json` | 自定义引擎清单（覆盖内置） | 项目根或 `~/.ngm/` |
| `~/.ngm/config.json` | Git 协议、凭证变量、默认引擎、权限 | `NGM_HOME` 可覆盖；`net:` / `run:` 未显式放行即拒绝 |

**摘要**：根依赖记 `archiveDigest`（整仓），带 `path` 的模块记 `moduleDigest` + `subtreeOid`；
都是规范化清单（`<path>\0<mode>\0<blob-sha256>\0`，UTF-8 字节序排序）的 SHA-256，
不是 tarball 字节流；gitlink 与 LFS 指针被拒绝（[README §85](../README.md#archiveDigest-与-moduleDigest)）。

**漂移三态**（`verify`）：`expected`（分支前进，默认 0，`--strict` 为 1）、
`unexpected`（tag 移动/改写，默认 1，`--allow-drift` 为 0）、
`critical`（digest/落地字节不符，恒为 2）；资源缺失不产生漂移，标 `operational` 并退 4。
**2 压过 4**（ADR-039）；SARIF/GitHub 注解级别与最终退出码同源。

**离线重放**：`--offline` 禁止一切网络，只用本地 mirror / content store；
`verify --content`（别名 `--level=content`，`--deep` 为旧名）逐文件重放字节。
默认 structure 只比结构与大小，**等长字节篡改检不出来**；GitHub Action 默认已是 content（CLI 仍默认 structure，ADR-051）。

**四层磁盘**（[README §149](../README.md#四层磁盘布局) / [vendor-layers.md](../architecture/vendor-layers.md)）：

```
~/.ngm/mirror/        层 1    Git 裸仓库，全历史 + 全 refs
~/.ngm/module-cache/  层 1.5  partial clone（blob:none），服务模块条目
~/.ngm/content/       层 2    内容寻址 blob 池 + v2 树清单
<project>/ngm.vendor/ 层 3    逐文件硬链接到层 2（linkMode: auto/hardlink/copy/symlink）
~/.ngm/cache/         层 4    可丢弃缓存
```

## 3. 横切能力

- **权限与沙箱**：`permissions.allow/deny` 管 `run:git` / `net:<host>` / `run:<engine>` / `run:script` / `env:`；
  未放行即拒绝并给出可粘贴的 JSON 提示。postinstall 默认禁止，`allow` 时也只在 Deno 沙箱内运行；
  `verify --sandbox` / `audit --hook` 同理，缺 Deno 且确有脚本则 exit 5，**绝不在沙箱外降级执行**。
  含逗号的 net/env target 现在直接拒绝（避免 ngm 判一条、Deno 授两个）。token 只用于输出脱敏，不进参数/lock/日志。
- **供应链门禁**：`allowedGitHosts` / `allowlistRepos` / `minimumReleaseAge`（写 `supplyChain` 段未写冷却时**默认 P1D**，ADR-050）/
  `postInstallPolicy` / `verifyOnLock`；完全不配 `supplyChain` 段则不门禁（ADR-009）；单命令逃生口 `--allow-fresh`。
- **机器接口**：退出码 0 成功 / 1 策略 / 2 完整性 / 3 配置用法 / 4 Git 网络 / 5 引擎不可用 / 6 内部失败；
  `run` 透传脚本自身退出码。机器可读契约在 [contracts/exit-codes.json](../../contracts/exit-codes.json)；
  `--json` 失败时 stdout 恰好一份完整报告或错误信封，不再混入另一份报告；不接受 `--json` 的命令 stdout 保持为空。
- **CI 集成**：根目录 `action.yml` 是 composite action（不下载 ngm），三种 mode：`verify` / `adopt` / `adopt-check`；
  `verify-level` 默认 `content`（ADR-051）。典型 CI：`install --frozen-lockfile --offline` + `verify --json`。
- **引擎 adapter**：内置 `esbuild`（bundle/transform）、`typescript`（typeCheck/typeDecl，`optional`）、
  `postcss`（css，`optional`）、`self` stub；wasm adapter 可用；`remote` 按 ADR-013/020 **决定不发布**。
  声明引擎是前置条件——未声明 exit 3，声明了但没装才 exit 5。
- **可观测性纪律**：`why` 默认最多 64 条来源路径、`tree` 最多 4096 条条目，`--all` 才解除（ADR-024）；
  `doctor` 报告环境而非只报成功；`outdated` 查不到写 `unknown` 并说明原因。
- **回收纪律**：`store prune` 只清残骸，默认不动对象；`--orphans` 带年龄门槛删无人引用 blob；
  `store gc` 整理 mirror；`gc --apply` 按当前 lock 回收且默认 dry-run，global vendor 一律保留并 warning（ADR-041）。

## 4. 成熟度快照

| 能力 | 状态 | 备注 |
|---|---|---|
| Git 拉取 / refType / lock v2 / 根与模块内容证明 | done | ngm 的核心闭环 |
| 一仓多模块 + 集合索引 + partial clone 缓存 | done（开发线） | 不是 pnpm workspace |
| 漂移检测 / 内容级复核 / OSV / 供应链门禁 | done | 内容级是 `--content`，默认 structure |
| SBOM 1.7 / SPDX 2.3 / SLSA provenance 语句 | done（开发线） | 语句未签名、确定性字节；时间戳需显式开启 |
| `adopt` 证明层 / `ngm run` / `modules changed|plan` / 项目范围 GC | done（开发线） | 详见[能力矩阵 §10](../internals/capability-matrix.md#核心能力) |
| 发布完整性 | **受限** | 101 个正式 tag 中 GitHub 63 个 release、Gitee 3 个，缺口 38 个；预编译产物不是最新能力的可靠来源 |
| registry / workspace / gitlink / LFS / 自研构建核心 | n/a | 明确不做，见 §0 |

## 5. 当前已知限制（不是待修清单）

1. 预编译 release 覆盖远少于 tag → 要用当前行为请从源码构建（[README §544](../README.md#已知问题与限制)）。
2. `adopt` 只解析 npm `package-lock.json`（v2/v3）；pnpm/yarn lock 里的 Git 来源条目窄解析，registry 来源标 `unknown` 并写进 `notes`。
3. `verify` 默认 structure；字节级保证必须显式 `--content`。
4. `gc` 可达性只看当前项目 lock；层 1/2 是全机共享，另一个 checkout 可能需要被回收的对象（可重新拉取，但离线 CI 会失败）。
5. 层 2 老布局条目不自动迁移；删 `sha256/` 后重装即可重建。

## 6. 证据索引

- 命令与 flag：[`cmd/ngm/root.go`](../../cmd/ngm/root.go) · [cli.md](../guides/cli.md)
- 配置与锁：[configuration.md](../guides/configuration.md) · [locking.md](../architecture/locking.md)
- 摘要与信任：[trust-model.md](../architecture/trust-model.md) · [ADR-032](../../docs/adr/adr-032-scoped-content-proof.md)
- 供应链与沙箱：[supply-chain.md](../architecture/supply-chain.md) · [security-model.md](../architecture/security-model.md)
- 引擎与模块：[engine-adapter.md](../architecture/engine-adapter.md) · [module-collections.md](../architecture/module-collections.md)
- 可观测与回收：[observability.md](../architecture/observability.md) · [ADR-041](../adr/adr-041-project-scoped-gc.md)
- 决策全集：[ADR 索引](../adr/README.md)（51 条）
