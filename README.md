# ngm

> **Git 依赖的证明层**：把 `github:org/repo@v1.2.3` 这样的声明，变成「声明 + commit + 内容 digest + 可离线重放 + 可导出审计产物」的可核验对象。

ngm 是一个 Go 编写的单二进制 CLI，用 `ngm.json` 声明依赖、用 `ngm.lock` 锁定 commit 与内容摘要、用 `ngm verify` 重放证明，并可从 lock 直接导出 SBOM 与 in-toto / SLSA 语句。

它**不是** npm / pnpm / Yarn / Bun 的替代品：它只管理直接来自 Git 仓库的依赖，不解析 registry 包，不接管 `node_modules`，也不替你决定运行时或签名体系。

---

## 目录

- [当前状态](#当前状态)
- [它解决什么问题](#它解决什么问题)
- [边界：它不做什么](#边界它不做什么)
- [核心概念](#核心概念)
- [安装](#安装)
- [快速上手](#快速上手)
- [命令参考](#命令参考)
- [退出码](#退出码)
- [配置文件](#配置文件)
- [CI 与自动化](#ci-与自动化)
- [已知问题与限制](#已知问题与限制)
- [开发与测试](#开发与测试)
- [相关文档](#相关文档)

---

## 当前状态

| 项目 | 事实 |
|---|---|
| 最新 tag | 以 `git tag` 的读数为准（本文不锁定数字，避免再次漂移） |
| 当前分支 | `main`，最新 tag 之后另有未打 tag 的提交（开发线包含模块集合、lock v2、`internal/core` 抽离等） |
| 构建方式 | `go build ./cmd/ngm`；无 cgo、无运行时依赖（仅需 Git） |
| 版本号 | 未注入 ldflags 的本地构建输出 `ngm 0.0.0-dev (git:dev, built: unknown)`；发布构建通过 `-ldflags -X` 注入版本、git 短哈希与构建时间 |
| 测试与 CI | 三平台（Linux / macOS / Windows）build + vet + test，另有 `-race`、验收测试、真实引擎集成与文档链接检查 |
| 许可 | MIT |

发布读数（2026-10-30 文档记录，本轮未联网复核）：

| 源 | 有 release 的版本 | 说明 |
|---|---|---|
| GitHub | 只有较早的一部分 tag | release 覆盖到 `v0.64.0` 一带；镜像转发的 tag 不会触发 release workflow |
| Gitee | 仅 `v0.2.0`–`v0.4.0` | 只有这几个版本有附件 |

两个主机上的 tag 集合一致（以 `git tag` 与远端 ref 的读数为准），差的就是 release 附件覆盖：
差的 0 个是未推送的。

> **预编译产物不是最新能力的可靠来源。** 两个源的 release 覆盖都远少于 tag。要使用当前源码的行为，请按[从源码构建](#方式一从源码构建推荐)。

---

## 它解决什么问题

| 没有 ngm | 有 ngm |
|---|---|
| 依赖写成 `@main`，无法区分 tag 还是 branch | `refType` 显式声明 `commit` / `tag` / `branch` |
| 只知道 commit，不知道落地的是哪些字节 | `archiveDigest` / `moduleDigest` 对规范化内容清单做 SHA-256 |
| 上游 tag 被重打、分支被改写，CI 无法区分 | `ngm verify` 把漂移分成 expected / unexpected / critical 三态 |
| 离线时只能相信缓存 | digest 可从本地 mirror 的 commit tree 重放，不依赖 Git host 的 archive API |
| SBOM 只有版本号，没有内容哈希 | `ngm sbom` / `ngm attest` 直接从 lock 导出带内容摘要的产物 |

---

## 边界：它不做什么

- **不解析 npm 配置**：依赖里的 `.npmrc` 只是普通文件，会进入 digest、会被 vendor，但**永远不会被解释**。
- **不安装 registry 包**：只接受 Git 地址；没有 registry 版本解析、没有 tarball 完整性体系。
- **不做 workspace 依赖解析**：monorepo 只支持「同一仓库 + 子路径」(`path`)，不是 pnpm workspace。
- **不接管 `node_modules`**：`ngm adopt` 只读扫描并出具内容证明，不安装、不修改。
- **不自造签名体系**：`ngm attest` 产出未签名语句，请用 `cosign attest-blob` 等标准工具签名。
- **不执行 `git add` / `git commit`**：`ngm.vendor/` 是否提交由你的 Git 流程决定。

---

## 核心概念

### refType：声明的意图

依赖的三元组是 `(name, ref, refType)`：

| refType | 含义 | 解析结果 |
|---|---|---|
| `commit` | 固定 commit | 完整 40 位 hash 可省略 `refType`；7–39 位短 hash 必须显式声明 |
| `tag` | 标签 | 解析为 tag 当前指向的 commit |
| `branch` | 分支 | 解析为分支当前 tip |

`@main` 无法区分「叫 main 的 tag」与「叫 main 的分支」，因此 `refType` 通常必填。`ngm add --infer-ref-type` 会用一次 `ls-remote` 消歧；两者都存在时直接报错，不会猜。

### archiveDigest 与 moduleDigest

两者都是「规范化内容清单」的 SHA-256，不计算 tar/zip 字节流：

- 清单头部分别为 `ngm-archive-digest/v1`（整仓）与 `ngm-module-digest/v1`（子路径模块）。
- 每条记录为 `<path> NUL <mode> NUL <blob-sha256> NUL`，按 UTF-8 字节序排序。
- 只记录文件：`100644` / `100755` / `120000`（符号链接，内容为目标字符串）。
- 目录不单独成条；**gitlink（子模块）与 LFS 指针会被拒绝**，不会静默哈希一个指针。

根依赖写 `archiveDigest`；带 `path` 的模块依赖写 `moduleDigest` + `subtreeOid`（`<commit>:<subPath>` 的 Git tree 对象 ID，支持 sha1 / sha256）。两者互斥，见 [ADR-032](docs/adr/adr-032-scoped-content-proof.md)。

### ngm.json 的 schemaVersion

`ngm init` / `ngm add` 写出的 `schemaVersion` 是 **1**；读取器（`config validate`、`install`、
`modules validate` 等全链路）接受 **1..2**。v2 目前只增加可选语义，不改变已有字段形状，
因此旧项目无需迁移，ngm-repo 这类生产方也可以继续使用 v2 清单。超出范围的版本（如 3）
会以 exit 3 拒绝并写出支持区间。

### ngm.lock（lock v2）

`ngm.lock` 是唯一事实源，必须提交。顶层结构：

```json
{
  "version": 1,
  "lockfileVersion": "2.0.0",
  "dependencies": []
}
```

每个依赖条目包含 `name` / `ref` / `refType` / `commit` / `archiveDigest` 或 `moduleDigest` + `subtreeOid` / `resolvedAt` / `vendorPath`，以及（自 v0.73 起）`requiredBy` 直接引用者列表。`requiredBy` 让 `ngm why` 无需重新解析即可回答来源，并让 SBOM 有依赖关系。

**确定性纪律**：除 `resolvedAt` 外，同一 `ngm.json` + 同一上游状态应产生字节级一致的 lock。字段顺序、2 空格缩进、LF、末尾换行都是格式契约的一部分。

### 漂移三态

`ngm verify` 对每个依赖给出一个 `driftKind`：

| driftKind | 触发条件 | 默认退出码 | `--allow-drift` | `--strict` |
|---|---|---|---|---|
| `expected` | 分支正常前进 | 0 | — | 1 |
| `unexpected` | tag 被移动、分支历史被改写 | 1 | 0 | 1 |
| `critical` | digest 重放不匹配、落地字节被篡改 | 2 | **仍为 2** | 2 |

资源缺失（未安装、冷 mirror、content store 缺条目）**不产生 `driftKind`**：该依赖被标为
`operational`，报告里的 `summary.operational` 加一，退出码为 **4**。只有「比对得出不同」
才是 `critical`。`verify --json` 的 `summary` 会同时统计这两类，因此
`critical + operational` 可能大于 `total`。

退出码优先级：**2（已确证篡改）压过 4（没查成）**。这是 ADR-039 的结论：把「已证明被篡改」当成「资源缺失、可重试」会让 CI 重试而不阻断。

### 离线重放

digest 的输入是本地 mirror 里的 commit tree 与内容树字节，因此可以完全离线重算：

```bash
ngm install --frozen-lockfile --offline   # 只落地 lock 已钉住的内容
ngm verify --offline                      # 只比对本地快照，结果标记为 stale
```

`--offline` 禁止一切网络访问；缺 lock、缺 mirror、缺对象都会失败（exit 4）。`--content`
（或 `--level=content`）还会重放 lock 的 digest 并逐文件比对字节；默认的 structure 只比结构与
大小，**等长字节篡改检不出来**（hardlink 下 vendor 与 blob 可能共享 inode）。

### 四层磁盘布局

```
~/.ngm/
├── mirror/        # 层 1：Git 裸仓库，完整历史与全部 refs
├── module-cache/  # 层 1.5：partial clone（blob:none），服务模块条目
├── content/       # 层 2：内容寻址 blob 池 + 树清单（v2 布局）
└── cache/         # 层 4：可丢弃缓存（OSV / 元数据 / 临时文件）

<project>/ngm.vendor/   # 层 3：逐文件链接到层 2
```

`NGM_HOME` 可覆盖 `~/.ngm`。层 3 的 `linkMode`：

| 值 | 行为 |
|---|---|
| `auto`（默认） | 优先 hardlink，跨卷或不支持时降级复制 |
| `hardlink` | 强制 hardlink，失败即报错 |
| `copy` | 普通复制（需要就地修改依赖时用这个） |
| `symlink` | v2 布局下退化为逐文件 hardlink，CLI 会如实打印实际模式 |

> 就地编辑 `ngm.vendor/` 里的文件会同时改写 content store（hardlink 语义），且后续 `ngm install` 不会察觉。检测路径是 `ngm verify --content`。

`ngm store usage` 报告各层占用，`ngm store prune` 清理中断克隆/解包留下的残骸，`ngm store gc` 对 mirror 执行 `git gc`。`ngm gc` 则按**当前项目的 lock** 回收 store / mirror / vendor 里不再被引用的对象：默认 dry-run，必须显式 `--apply` 才删；global vendor 条目无法判定是否被其它项目引用，一律保留并 warning（[ADR-041](docs/adr/adr-041-project-scoped-gc.md)）。

### SBOM 与 attestation

| 命令 | 产物 | 关键事实 |
|---|---|---|
| `ngm sbom` | CycloneDX 1.5 或 SPDX 2.3 JSON | 只读 `ngm.lock`；每个依赖带 commit、内容 digest 与 purl；SPDX 的许可证写 `NOASSERTION`（不猜） |
| `ngm attest` | in-toto Statement v1，谓词为 SLSA provenance v1 | subject 是依赖的 git purl + 内容摘要；inputs 是 `ngm.json` / `ngm.lock` 的字节 sha256；materials 是 commit + 声明的 ref；**无时间戳、无随机 ID**，同一 lock 产出相同字节 |

两者的 purl 按 host 分流：`pkg:github/<owner>/<repo>`、`pkg:gitlab/...`、`pkg:gitee/...`；
自建 host 使用 `pkg:generic/<host>/<path>?vcs_url=...`。CycloneDX 的 `bom-ref` 仍保留
稳定的旧形态 `pkg:git/<canonical>@<commit>`，与可对外解析的 purl 分开。
attestation **不签名**，也**不覆盖依赖图**（图看 SBOM）。

### 权限与沙箱

`~/.ngm/config.json` 的 `permissions` 段控制 `net:` / `run:` / `env:` 等能力。**未显式放行的 `net:` 与 `run:` 按拒绝处理**，因此第一次访问远端或第一次调用引擎会失败，并给出可直接粘贴的 JSON 提示。

- `run:git` 是 Git 子进程的唯一放行点；没有任何配置项能替换 git 二进制。
- postinstall 钩子默认禁止；`postInstallPolicy=allow` 时也只在 Deno 沙箱里运行，无网络、无 `run`、无写权限。
- `ngm verify --sandbox` 与 `ngm audit --hook` 同样在沙箱中执行依赖自带脚本；缺 Deno 时返回 exit 5，**绝不在沙箱外运行**。
- ngm 会读取 `GITHUB_TOKEN` / `GITEE_TOKEN` 的值用于**输出脱敏**，并把环境变量透传给 git；token 不会进入命令行参数、lock、日志或 mirror 配置。

---

## 安装

### 方式一：从源码构建（推荐）

```bash
git clone https://gitee.com/idcu/ngm.git
cd ngm
go build -o ngm ./cmd/ngm
```

Windows：

```powershell
git clone https://gitee.com/idcu/ngm.git
cd ngm
go build -o bin\ngm.exe .\cmd\ngm
```

前置要求（以仓库实际声明为准）：

| 工具 | 要求 | 用途 |
|---|---|---|
| Go | **≥ 1.25**（`go.mod` 的 `go 1.25.0`；CI 使用 1.27） | 仅构建时需要，运行 ngm 不需要 |
| Git | ≥ 2.30（文档声明） | 拉取依赖、重放对象 |
| Node.js | ≥ 22（可选） | 用户项目运行时 |
| Deno | ≥ 2.0（可选；`deno bundle` 需 ≥ 2.4） | Deno 项目、沙箱执行 |

验证：

```bash
ngm --version
# ngm 0.0.0-dev (git:dev, built: unknown)   ← 本地未注入版本信息时的输出

ngm --help
# 28 个顶层命令与全局退出码约定
```

### 方式二：预编译二进制（有前提）

发布产物的命名是 `ngm-<os>-<arch>[.exe]`（`GOOS`/`GOARCH`），随 release 附带 `SHA256SUMS`。
从 v0.102 起同一份字节还会有一个版本化别名 `ngm-<version>-<os>-<arch>[.exe]`。
完整发布范围、回填策略与凭据要求见[发布流水线](docs/guides/release-pipeline.md)。

```bash
sha256sum -c --ignore-missing SHA256SUMS      # Linux
shasum -a 256 -c --ignore-missing SHA256SUMS  # macOS
```

**前提与限制**：项目文档记录的 release 覆盖不完整，Gitee 侧只有 `v0.2.0`–`v0.4.0` 的附件，GitHub 侧也远少于 tag 总数；下载前请以对应 release 页面为准。若需要当前源码的能力，请使用方法一。

---

## 快速上手

以下流程除特别标注外，均已在本机用源码构建的二进制实际执行过。涉及远端 Git、esbuild 等外部条件的步骤标注了前提。

### 1. 初始化

```bash
ngm init github.com/my-org/my-app --runtime=node
```

生成：

```
my-app/
├── ngm.json
└── src/index.ts
```

`ngm.json` 的实际内容（字段顺序固定）：

```json
{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [],
  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild"
  },
  "vendor": {
    "mode": "local",
    "linkMode": "auto"
  }
}
```

`src/index.ts` 已存在时不会被覆盖，`--force` 也不会覆盖入口文件。`--runtime` 只接受 `node` / `deno`；重复初始化（目录里已有 `ngm.json`）返回 exit 3。

### 2. 添加依赖

```bash
# tag
ngm add github:my-org/utils@v1.2.3 --ref-type=tag

# 分支（ref 可以带 `/`，例如 feature/x）
ngm add github:my-org/logger@feature/x --ref-type=branch

# 完整 40 位 commit 可以省略 refType
ngm add github:my-org/legacy@5b28ce4d1a2b3c4d5e6f708192a3b4c5d6e7f809

# monorepo 子路径
ngm add github:my-org/monorepo#path=packages/core@v2.0.0 --ref-type=tag

# 只打印结果，不写文件
ngm add github:my-org/utils@v1.2.3 --ref-type=tag --dry-run
```

`ngm add` 本身不联网（`--infer-ref-type` 例外）。地址支持 `https://`、`git@host:org/repo`、`git://` 与 `host:org/repo` 简写。

### 3. 校验配置

```bash
ngm config validate
# ngm.json OK
```

`--dir=<path>` 可校验另一个目录；目录里没有 `ngm.json` 时返回 exit 3（不会假装通过）。

### 4. 安装

```bash
ngm install
```

首次访问远端时，`net:` 默认未放行，会失败并给出提示：

```
ConfigInvalid: permission denied: net:github.com
  hint:  add "net:github.com" to `permissions.allow` in ~/.ngm/config.json;
         paste {"permissions":{"allow":["net:github.com"]}}
```

把提示里的 JSON 写入 `~/.ngm/config.json`（文件不存在就新建），再重跑。漏洞查询还需要 `net:api.osv.dev`。

流程是：解析 refType → commit → 计算 digest → 落地 vendor → 写 `ngm.lock` 与 `ngm.mappings.json`。

**无依赖项目的边界行为（已实测）**：直接 `ngm install` 会成功并写出空 lock；但 `ngm install --offline` 在没有 lock 时返回 exit 4（offline 只能重放已钉住的内容，不能解析）。

### 5. 验证

```bash
ngm verify
ngm verify --content       # 重放 digest + 逐文件字节比对（旧的 --deep 仍可用）
ngm verify --level=content # 与 --content 等价；默认 structure
ngm verify --json          # CI 用这个
ngm verify --format=github # PR 注解
ngm verify --format=sarif > ngm.sarif
```

空 lock 的实际输出：

```
verified 0 dependency(ies): nothing to verify (exit 0)
note: --offline — refs were compared against the local mirror snapshot (stale)
note: structure-only (no byte hashes): same-length byte edits are detectable only with `ngm verify --content`
```

### 6. 构建（需要外部引擎与权限）

```bash
ngm build --engine=esbuild --outfile=dist/app.js
ngm build --dry-run     # 只打印 ngm 会怎么调引擎
```

前提：安装 `esbuild`，并在 `~/.ngm/config.json` 中放行 `run:esbuild`。未声明引擎时 `engines.bundle` 默认是 `esbuild`；引擎缺失返回 exit 5，不会产出空文件。

`ngm build` 会把 `ngm.mappings.json` 的每条映射变成引擎别名，因此裸导入 `github:org/repo` 能解析到 `ngm.vendor/` 中那份可证明的代码。

### 7. 审计产物（只读 lock）

```bash
ngm sbom --format=cyclonedx --out=sbom.cdx.json
ngm sbom --format=spdx
ngm attest --out=provenance.intoto.json
```

两条命令都不联网、不重解析、不写项目；没有 lock 时返回 exit 3。

---

## 命令参考

共 **28 个顶层命令**。逐条的用法、参数与退出码见 [cli.md](docs/guides/cli.md)（唯一权威）。
`ngm <command> --help` 打印该命令自己的用法，与参数错误时看到的是同一份文本。

| 类别 | 命令 |
|---|---|
| 项目 | `init` `adopt` `config` |
| 依赖 | `add` `install` `update` `remove` `verify` `why` `tree` `outdated` |
| 模块集合 | `modules`（`list` / `validate` / `index` / `changed` / `plan`） |
| 供应链 | `audit` `sbom` `attest` |
| 引擎与构建 | `typecheck` `typedecl` `build` `transform` `css` `engines` |
| 集成与缓存 | `integrations` `cache` `store` `gc` `mappings` |
| 任务执行 | `run` |
| 诊断 | `doctor` |

四条容易踩的：

- `--json` 在 14 个命令可用（`run` 仅 `--list` 形态），stdout 只放**恰好一份** JSON，人类文本与诊断走 stderr；
- 命令自己的 flag 必须写在命令之后（`ngm --json verify` 会被拒绝，exit 3）；
- 内置引擎 `esbuild`（bundle/transform）、`typescript`（optional）、`postcss`（optional）、`self`（stub，别用于生产）；
- `remote` adapter 按 ADR-013 / ADR-020 **决定不发布**，声明它得到的是「按决定排除」而非「待实现」。

---

## 退出码

| 码 | 一句话 | 重试？ |
|---:|---|---|
| 0 | 成功 | — |
| 1 | 策略失败（漂移 / 漏洞超阈值 / 引擎跑了但失败 / 钩子否决） | 否，要人看 |
| 2 | 完整性失败（digest 不匹配、字节被篡改） | 否 |
| 3 | 配置 / 策略 / lock / 用法错误 | 否 |
| 4 | Git 或网络失败 | 是 |
| 5 | 引擎不可用 | 是（装好引擎后） |
| 6 | 内部失败（panic，或结论送不出去） | 是 |

**透传族（`ngm run` 例外）**：项目脚本自己的退出码原样透传（信号终止见 `128 + signal`，
SIGINT→130、SIGTERM→143），**不映射**到上表的 0–6。那些码由脚本产生，不属于 ngm 的错误分类。

**机器可读版本**：[contracts/exit-codes.json](contracts/exit-codes.json) —— 含 `retryable`。
脚本与 CI 判断"该不该重试"读它，**不要 grep 人类可读输出**（措辞一改就断）。
逐码的典型来源见 [cli.md](docs/guides/cli.md)。

三条容易记错的规则：

1. **2 压过 4**：已确证篡改优先于「没查成」。
2. **`--out` 写不进是 3，不是 6**：文件通道的路径与权限由调用方负责。
3. **管道被对端关闭不算失败**：`ngm verify | head` 会安静退出，不报 6。

`--json` 模式下失败会写一份完整错误信封到 stdout（不是报告，但也不是半份 JSON）：

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

`code` 与人读文本使用同一个错误名，`hint` 与人读那侧的 `hint:` 行逐字相同，`exitCode` 等于进程退出码。

---

## 配置文件

| 文件 | 作用 | 位置 |
|---|---|---|
| `ngm.json` | 依赖声明、引擎选择、供应链策略、vendor 模式 | 项目根，必须提交 |
| `ngm.lock` | commit + 内容摘要 + `requiredBy` 图 | 项目根，必须提交 |
| `ngm.mappings.json` | 构建工具的裸导入映射 | 项目根，由 install 生成 |
| `ngm.engines.json` | 自定义引擎清单（覆盖内置） | 项目根或 `~/.ngm/` |
| `~/.ngm/config.json` | Git 协议/凭证变量、默认引擎、权限 | 用户 home（`NGM_HOME` 可改） |

### ngm.json

```json
{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [
    { "name": "github:my-org/utils", "ref": "v1.2.3", "refType": "tag" },
    { "name": "github:my-org/core", "ref": "v2.0.0", "refType": "tag", "path": "packages/core" }
  ],
  "engines": { "bundle": "esbuild", "typeCheck": { "primary": "typescript", "fallbacks": ["deno"] } },
  "vendor": { "mode": "local", "linkMode": "auto" },
  "supplyChain": { "allowedGitHosts": ["github.com", "gitee.com"], "minimumReleaseAge": "P3D", "verifyOnLock": true }
}
```

字段边界（完整说明见 [configuration.md](docs/guides/configuration.md)）：

- `name` / `version` / `runtime` / `dependencies` 必填；`version` 被回显但不参与解析。
- `vendor.mode=global` 时 `vendor.globalPath` **只能**指向 `~/.ngm/global-vendor` 或其子目录。
- `vendor.commit` 已移除，写入会被 `DisallowUnknownFields` 拒绝（exit 3）。

### 全局配置

```json
{ "git": { "defaultProtocol": "ssh", "tokenEnvVars": { "github.com": "GITHUB_TOKEN" } },
  "engines": { "defaultBundle": "esbuild" },
  "permissions": { "allow": ["run:git", "run:esbuild"], "deny": ["run:npm", "env:GITHUB_TOKEN"] } }
```

默认协议是 **ssh**；需要 HTTPS 时在 `~/.ngm/config.json` 显式写 `"defaultProtocol": "https"`。

---

## CI 与自动化

### 可复现安装

```bash
ngm install --frozen-lockfile --offline
ngm verify --json
```

- `--frozen-lockfile`：lock 缺失或与 `ngm.json` 不一致时 exit 3；禁止解析新 commit，但不禁止为冷 content store 下载已钉住的内容。
- `--offline`：完全禁止网络；冷 content store / 冷 mirror 失败（exit 4）。
- 二者组合是 CI 默认：需要 `ngm.lock` 已提交且 content store 已预热。

### 机器输出

```bash
ngm verify --json         # 单个 JSON 文档，summary.exitCode 等于进程退出码
ngm verify --format=github # ::error / ::notice 注解，不需要额外权限
ngm verify --format=sarif > ngm.sarif
```

`--format` 与 `--json` 是同一个通道的两种拼法，互斥。SARIF 2.1.0 不产出行号：ngm 不知道行号，伪造 `line: 1` 会把所有注解钉在第一行。

### GitHub Action

仓库根的 `action.yml` 是 **composite action**，刻意不下载 ngm（版本与校验和应由使用者决定）。三种模式：

```yaml
- uses: actions/checkout@v4
- run: echo ok   # 先把 ngm 装到 PATH（发布产物或源码构建）
- uses: idcu/ngm@<tag>
  with:
    mode: verify            # verify | adopt | adopt-check
    verify-level: structure # 默认；发布门槛可改成 content
    args: "--strict"
```

`verify-level` 会被展开成 `--level=<值>`；若 `args` 里已经写了 `--content` / `--deep` /
`--level=`，action 不再注入默认值（两种写法同时给出会被 ngm 拒绝）。

| mode | 前提 | 作用 |
|---|---|---|
| `verify` | 需要 `ngm.lock` | 漂移 + digest 重放，失败项变成注解或 SARIF |
| `adopt` | 只需 `node_modules` | 生成内容证明（默认 `ngm.proof.json`） |
| `adopt-check` | 需要一份早先的证明 | 只读复核，有变化即失败 |

---

## 已知问题与限制

### 仍然存在

1. **发布链路不完整。**
   现有 tag 中，GitHub release 与 Gitee 附件都远未覆盖；文档中的安装地址停留在早期版本。
   下游 `ngm-repo` 的 `.ngm-version` 固定为 `0.102.0-dev.1`，但没有任何可下载的对应产物，
   因此其 CI 的 `ngm-validate` job 默认需要在本地构建二进制（见 ngm-repo 的 workflow）。
   依据：ngm-repo 的 `.github/workflows/ci.yml` 与其仓库根的 `.ngm-version`、
   [installation.md](docs/guides/installation.md)。

### 设计性限制（不是缺陷）

- 只解析 npm `package-lock.json`（v2/v3）；`adopt` 不解析 pnpm-lock.yaml / yarn.lock，
  会在 `notes` 与 `coverage.unknown` 里明说。
- 不支持 registry 包、workspace 依赖、子模块（gitlink）与 LFS 实体内容。
- `verify` 默认只比结构与大小，字节级保证必须加 `--content`（或 `--level=content`；`--deep` 是旧别名）。
- `ngm gc` 的可达性只看**当前项目的 lock**：层 1/2 是全机共享的，另一个 checkout 仍可能需要被回收的 mirror 或 digest（可重新拉取/安装，但离线 CI 会失败）。global vendor 条目无法判定，一律保留并 warning。

> v0.102 已修复的缺陷（选择性 verify 的幽灵边、冷 mirror 的退出码、`summary` 与
> `exitCode` 的矛盾、重复根声明、`tree` 的图来源、`typecheck` 帮助、默认协议、
> purl 分流，以及 9 项 IO/路径硬化）由 `TestV102*` 端到端回归守护。

---

## 开发与测试

```bash
go build ./...
go vet ./...
go test ./...          # cmd/ngm 串行在 Windows 上可能超过默认 10m，CI 显式放宽到 40m
go test -race ./...    # 可选；CI 在 ubuntu 上单独跑，同样放宽到 40m
```

仓库常规：

- 格式门禁：`gofmt -l .` 必须为空；所有文本文件必须是 LF；含非 ASCII 的 `.ps1` 必须带 UTF-8 BOM。
- CI 的 `build-test` 覆盖三平台；另有验收测试（M1–M7 与各版本端到端）、真实引擎集成（esbuild / tsc / postcss / Deno）、lock 字节可复现、release 状态与文档链接检查。
- 新增命令时必须同时更新根帮助字符串、命令的 usage 常量与 `docs/guides/cli.md`；帮助文本由 golden 测试守护。

---

## 相关文档

- [CLI 参考](docs/guides/cli.md) — 每个命令的完整用法、`--json` 形状与退出码矩阵
- [配置详解](docs/guides/configuration.md) — 字段语义、vendor 模式、supplyChain
- [快速上手](docs/guides/quickstart.md)、[安装指南](docs/guides/installation.md)
- [术语表](docs/guides/glossary.md)
- [架构总览](docs/architecture/overview.md) — 分层、依赖解析、锁定、供应链、安全模型
- [ADR 索引](docs/adr/README.md) — 41 个决策记录（refType、archiveDigest、锁、沙箱、attestation、项目范围的 gc 等）
- [项目评估与路线图](docs/internals/assessment-2026-11-01-full.md) — 独立评估、缺陷清单与优先级

---

MIT License，见 [LICENSE](LICENSE)。
