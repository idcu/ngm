# CLI 参考

ngm 是单二进制 CLI：`ngm <command> [options]`。本文给出命令速览、全局约定与 `--json` 的机器接口形状。每条命令的完整参数以 `ngm <command> --help` 为准——它与该命令参数错误时打印的是同一份文本。

## 命令一览

| 命令 | 作用 | 主要文档 |
|------|------|---------|
| `ngm init <name> [--runtime=node\|deno] [--dir=<path>] [--force]` | 初始化项目，生成 `ngm.json` 与入口文件（已有入口不覆盖） | [快速上手](./quickstart.md) |
| `ngm adopt [--dir] [--out] [--json] [--check]` | 为已装好的依赖只读出具内容证明，**不接管安装**；`--check` 与一份早先的证明比较，差异 exit 2 | [定位](./positioning.md) |
| `ngm add <git-url>[@<ref>] [--ref-type] [--infer-ref-type] [--path] [--dir]` / `ngm add local:<name> --ref-type=<local-git\|local-dir> --source-path` | 添加 Git 或本地来源依赖；refType 通常必填 | [依赖管理](./dependency-management.md) |
| `ngm install [--dir] [--digest] [--frozen-lockfile] [--offline] [--allow-fresh] [--json]` | 解析并安装；有 lock 则尊重 lock，不重新解析 ref | [依赖管理](./dependency-management.md) |
| `ngm update [<dep>] [--all] [--offline] [--digest] [--store] [--json]` | 更新 ref 与锁定；`--json` 与 install 同形，覆盖整张图 | [依赖管理](./dependency-management.md) |
| `ngm remove <dep>` | 只改声明，移除依赖 | [依赖管理](./dependency-management.md) |
| `ngm verify [<dep>...] [--offline] [--content \| --level] [--strict] [--allow-drift] [--json] [--format] [--sandbox] [--signatures] [--require-signed]` | ref 漂移 + digest 重放；`--content` 逐文件哈希，`--receipt` 只读复验构建回执 | [依赖管理](./dependency-management.md) |
| `ngm audit [<dep>...] [--json] [--offline] [--no-cache] [--hook]` | OSV 漏洞扫描（按 commit 查询 + 缓存）；`--hook` 在沙箱里跑团队策略 | [配置详解](./configuration.md) |
| `ngm why <dep> [--json] [--all]` / `ngm why --digest=<sha256>` | 依赖的来源路径；`--digest` 只读反查引用该摘要的条目 | [依赖管理](./dependency-management.md) |
| `ngm tree [--osv] [--offline] [--json] [--all]` | 依赖树 + 漂移；优先读 lock 的 `requiredBy` 图 | [依赖管理](./dependency-management.md) |
| `ngm outdated [--offline] [--json]` | 有哪些新版本；查不到报 `unknown` 并说明原因 | [依赖管理](./dependency-management.md) |
| `ngm typecheck [<entry>] [--engine] [--tsconfig] [--dry-run]` / `ngm typedecl [<entry>] --outdir=<dir> [--engine] [--dry-run]` | 类型检查 / 生成 `.d.ts`，由外部引擎执行 | [构建](./build.md) |
| `ngm build [<entry>] [--engine] [--outfile] [--production] [--dry-run] [--dir]` | 构建（adapter）；`--dry-run` 只打印怎么调引擎 | [构建](./build.md) |
| `ngm transform [<file>] [--engine] [--outfile] [--loader] [--target] [--format] [--minify] [--sourcemap] [--dry-run]` | 单文件转换，默认 stdin → stdout，**不解析导入** | [构建](./build.md) |
| `ngm css <input.css> [--engine] [--outfile] [--minify] [--dry-run]` | CSS 编译；压缩需引擎显式支持，否则 exit 3 | [构建](./build.md) |
| `ngm mappings validate` | 校验 mappings 与 lock / vendor 三方一致 | [配置详解](./configuration.md) |
| `ngm integrations add <tool> [--dry-run] [--json]` | 生成 Vite / esbuild / Deno / webpack 集成配置，不覆盖已有文件 | [构建](./build.md) |
| `ngm cache clean` | 清空可丢弃缓存，不影响可证明性 | [配置详解](./configuration.md) |
| `ngm store usage` / `ngm store prune [--dry-run] [--orphans] [--older-than]` / `ngm store gc [--dry-run]` | 报告磁盘占用、清理残骸、整理 mirror；默认只读或 dry-run | [配置详解](./configuration.md) |
| `ngm gc [--dir] [--apply] [--dry-run] [--json]` | 按当前项目 lock 回收不可达对象；默认 dry-run，global vendor 一律保留 | [配置详解](./configuration.md) |
| `ngm config validate\|show [--dir]` | 校验 / 查看配置；目录里没有 `ngm.json` 时 validate 不是通过 | [配置详解](./configuration.md) |
| `ngm engines list\|info\|validate [--json]` | 引擎目录探针；`list` 如实呈现，被权限拒绝时退出 0，`validate` 给结论，不可用时退出 3 | [配置详解](./configuration.md) |
| `ngm modules list\|validate\|index\|changed\|plan [--dir] [--json] [--limit] [--strict] [--index] [--out] [--dry-run]` | 模块集合的发现、校验、索引与发布计划；不做 semver 求解 | [包开发指南](./package-authoring.md) |
| `ngm sbom [--format=cyclonedx\|spdx] [--out] [--dir] [--allow-stale]` | 从 lock 导出 SBOM；只读、不联网，lock 过期默认 exit 3 | [快速上手](./quickstart.md) |
| `ngm attest [--out] [--dir] [--allow-stale] [--with-timestamps]` | 导出未签名的 in-toto / SLSA v1 语句；默认逐字节确定 | [签名配方](./signing.md) |
| `ngm run [--list] [--if-present] [--explain] [-- <args>]` | 运行项目脚本（package.json 权威、ngm.json 回退），沙箱外执行，需 `run:script` 授权；退出码原样透传 | [生态定位](./positioning.md) |
| `ngm migrate [--dir] [--dry-run] [--json]` | 把 v1/v2 清单与 2.x lock 迁到 v3；`--dry-run` 预演并备份 | [迁移指南](./migration.md) |
| `ngm doctor [--dir] [--json]` | 诊断 Git、配置、lock、`~/.ngm` 布局与 vendor 可见性 | [安装指南](./installation.md) |

## 全局约定

- **退出码**：0 成功 / 1 策略失败 / 2 完整性失败 / 3 配置错误 / 4 Git 网络失败 / 5 引擎不可用 / 6 内部失败
  - 2 压过 4：已确证的篡改优先于"没查成"；`--out` 写不进属于 3，不属于 6。
  - 管道被对端关闭（如 `ngm verify | head`）不算失败，安静退出。
  - `ngm run` 透传脚本自己的退出码（信号见 `128 + signal`），不映射到 0–6。
- **配置文件**：`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json` / `~/.ngm/config.json`，见[配置详解](./configuration.md)。
- **`--offline`**：禁止网络，只用本地 mirror / content store；资源未命中 exit 4。适用于 `verify` / `tree` / `update` / `install` / `audit` / `outdated`。
- **`--json`**：机器可读报告走 stdout，人类文本与诊断走 stderr；CI 不应解析人类文本。命令的 flag 必须写在命令之后，`ngm --json verify` 这类根级写法会被拒绝（exit 3）。

## `--json` 的形状（机器接口）

写脚本时依赖的是**解码语义**（字段名、类型、`version`、错误信封、退出码），不是逐字节稳定。下表记录各命令的顶层形状；带退出码字段的命令，该字段等于进程退出码。

| 命令 | 顶层形状 | 带退出码字段 |
|------|---------|------------|
| `ngm verify --json` | 对象：`version` · `level`（取值 structure / content） · `strict` · `deep` · `offline` · `allowDrift` · `dependencies` · `summary` · `notes`（选择性 verify 裁剪了 requiredBy 等来源说明） · `gates`（启用签名/沙箱附加层时，逐层给出 `name` · `enabled` · `exitCode` · `status`）。`--content` 时每个依赖另有 `content`：`level` · `expectedDigest` · `computedDigest` · `expectedSource` · `files` · `symlinks` · `mismatches`（元素键 `path` · `kind` · `expected` · `actual`，哈希为完整 sha256）；`--receipt` 复用本形状：`level` 取 receipt，`gates` 逐层给出 receipt-output 与 receipt-engine 的 `exitCode`，`summary.exitCode` 是合成结论（产物不符 2，缺引擎 5，2 压过 5） | ✅ `summary.exitCode` |
| `ngm audit --json` | 对象：`generatedAt` · `coverageNote` · `dependencies` · `findings` · `vulnerabilities` · `ignoredByPolicy` · `exitCode`（有发现时另有 `bySeverity`） | ✅ `exitCode` |
| `ngm integrations add <tool> --json` | 对象：`tool` · `dryRun` · `artifacts` · `warnings` · `exitCode` | ✅ `exitCode` |
| `ngm tree --json` | 对象：`project` · `source`（图来源：锁内的 requiredBy 图 / 回退解析，取值就是锁文件名或清单文件名） · `entries` · `drifted` · `dependencies` · `osvChecked` · `notes`（图来源之外没查成的项，例如 mirror 不完整导致漂移未比对）；（截断时另有 `entriesTruncated` · `entriesLimit`，停止处条目带 `truncated`；查到漏洞时另有 `remediation`） | ✗ |
| `ngm why <dep> --json` | 对象：`name` · `ref` · `refType` · `commit` · `subPath`（monorepo 子路径，无则省略） · `locked` · `paths` · `rootDeclared` · `source`（图从哪来：读 ngm.lock 时为 lock（未启动 git 子进程）；lock 里没有图、退回重新解析时为 resolved）（截断时另有 `pathsTruncated` · `pathsLimit`） | ✗ |
| `ngm outdated --json` | 对象：`offline` · `entries` · `dependencies` · `updates` · `stale` | ✗ |
| `ngm install --json` | 对象：`version` · `command` · `offline` · `dependencies` · `count` · `fromStore`（verifyOnLock 开启时另有 `verification`：`exitCode` · `drifts`——元素键 `name` · `driftKind` · `remediation` · `error`）（`dependencies` 元素键：`name` · `ref` · `refType` · `subPath` · `commit` · `archiveDigest` · `moduleDigest` · `subtreeOid` · `version` · `vendorPath` · `source`——`source` 是 `resolved` / `store` / `fetched` / `module-cache`；模块条目的 `archiveDigest` 为空） | ✗ |
| `ngm update --json` | 对象：`version` · `command` · `offline` · `dependencies` · `count` · `fromStore` · `verification`（与 install 命令同一形状；`dependencies` 覆盖**整张图**，而不只是被点名的条目） | ✗ |
| `ngm engines list --json` | **数组**：每个元素是 `name` · `kind` · `adapter` · `command` · `declaredVersion` · `version` · `available` · `stub` · `probeDenied` · `runTarget` · `builtin` · `supportedInput` · `defaultOptions` | ✗ |
| `ngm engines info <name> --json` | **数组**：同一个元素形状（`name` · `kind` · `adapter` · `command` · `declaredVersion` · `version` · `available` · `stub` · `probeDenied` · `runTarget` · `builtin` · `supportedInput` · `defaultOptions`）；同名不同 kind 会各占一行（`esbuild` 同时是 bundler 与 transformer） | ✗ |
| `ngm engines validate --json` | 对象：`version` · `ok` · `issues` | ✗ |
| `ngm doctor --json` | 对象：`version` · `ok` · `checks`；`checks` 元素键：`name` · `status` · `detail`（name 可取值含 supplyChain）；另有 `scripts`：`status`（none/declared/pending） · `policy` · `entries` · `allow` · `deny`，只预演不执行 | ✗ |
| `ngm run --explain --json` | 对象：`version` · `project` · `scripts`（`--list` 时返回的脚本清单） · `script` · `auth`（`permission` · `allowed` · `decision` · `source`） · `pathDirs` · `steps`（元素键：`phase` · `event` · `source` · `command`） · `note`；只读计划，不执行脚本 | ✗ |
| `ngm modules list --json` | 对象：`version` · `modules` · `truncated`（截断时） · `limit`；`modules` 元素键：`name` · `path` · `version` · `error` | ✗ |
| `ngm adopt --json` | 对象：`version` · `tool` · `source` · `coverage` · `packages` · `notes`（`source` 键：`manager` · `lockfile` · `lockfileDigest`；`coverage` 键：`packages` · `withCommit` · `withIntegrity` · `unknown` · `skipped`；`packages` 元素键：`name` · `version` · `path` · `digest` · `files` · `bytes` · `nested` · `provenance`——`provenance` 键：`kind` · `url` · `commit` · `ref` · `resolved` · `integrity`） | ✗ |
| `ngm modules validate --json` | 对象：`version` · `ok` · `modules` · `errors` | ✗ |
| `ngm modules changed --json` | 对象：`version` · `since` · `modules`；`modules` 元素键：`name` · `path` · `version` · `error` | ✗ |
| `ngm modules plan --json` | 对象：`version` · `since` · `bump` · `modules`；`modules` 元素键：`name` · `path` · `version` · `nextVersion` · `tag` | ✗ |
| `ngm modules index --json` | 对象：`version` · `out` · `dryRun`（dry-run 时） · `branch` · `branchCommit` · `releaseFiles`（分片时） · `index`（`schemaVersion` · `source` · `releaseDir` · `modules` · `releases` · `constraints`） | ✗ |
| `ngm gc --json` | 对象：`version` · `dryRun` · `applied` · `project` · `lock` · `store` · `mirror` · `moduleCache`（层 1.5，partial clone） · `vendor` · `total`（`objects` · `bytes`） · `warnings`；四层的键：`root` · `exists` · `mode`（vendor） · `candidates`（元素键 `id` · `bytes`，store 另有 `layout`） · `objects` · `bytes` · `removed` · `bytesFreed` · `kept`（global vendor） · `keptYoungBlobs` / `keptYoungBytes`（store） · `errors` | ✗ |
| `ngm run --list --json` | 对象：`version` · `project` · `scripts`；`scripts` 元素键：`name` · `command` · `source`（定义来自 package.json 还是 ngm.json） · `pre` · `post`（按 npm 约定存在的 `pre<name>` / `post<name>`，无则省略） | ✗ |

两处**数组**不是笔误：`engines` 的两条命令返回的是"行"，而一个名字可以对应多种 kind；其余命令返回的是"一份报告"，所以是对象。统一形状是破坏性变更，不在这里顺手改。

### 构建回执旁车 `ngm.build.json`

`ngm build --provenance` 在项目根写旁车文件：不进 lock、不含时间戳与绝对路径。顶层键：`receiptVersion`、`inputs`（`path` + `digest`）、`engine`（`name` / `kind` / `version` / `digest`，摘要是**工具文件字节**而非路径）、`argv`（排序后）、`mappingsDigest`、`output`（`path` / `digest`）、`source`（`built` 或 `cache`）。`ngm verify --receipt` 只读复验它，不重跑构建，也不对跨工具/跨平台的字节一致性背书。

### 四条每次调用都成立的规矩

1. stdout 要么为空、要么是**恰好一份**合法 JSON 文档，不会出现半份。
2. `--json` 不改变退出码：同一状态、同一夹具下，带与不带退同一个码。
3. 报告里若带 `exitCode` / `summary.exitCode`，它等于进程退出码。
4. 失败时 stdout 是完整报告，或一份完整的错误信封：

```json
{
  "version": 1,
  "error": {
    "code": "ConfigInvalid",
    "exitCode": 3,
    "message": "no ngm.lock found at /path/to/ngm.lock",
    "hint": "run `ngm install` to resolve dependencies and create the lock"
  }
}
```

`code` 与人读文本用同一个错误名，`hint` 与人读那侧的 `hint:` 行逐字相同。解析阶段的失败也按原始参数判断：用户提了 `--json` 就给信封，例如 `ngm verify --json --bogus` 会得到 `code: Usage`、exit 3；命令本身不接受 `--json` 时 stdout 始终为空。

## 常用组合

```bash
# CI 门禁
ngm install --frozen-lockfile --offline
ngm verify --json

# PR 注解 / Code Scanning（与 --json 互斥）
ngm verify --format=github
ngm verify --format=sarif > ngm.sarif

# 开发循环
ngm add github:my-org/utils@main --ref-type=branch
ngm install
ngm verify
ngm build --dry-run
```

仓库根的 `action.yml` 是 composite action，刻意不下载 ngm——版本与校验和由使用者决定。它提供 `verify` / `adopt` / `adopt-check` 三种模式；`verify` 需要 `ngm.lock`，`adopt` 只需 `node_modules`。

## 相关文档

- [快速上手](./quickstart.md) · [依赖管理](./dependency-management.md) · [构建](./build.md) · [配置详解](./configuration.md)
- [可观测性](../architecture/observability.md) · [退出码契约](../../contracts/exit-codes.json)
