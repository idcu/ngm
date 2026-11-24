# ngm

ngm 是一个 Go 编写的单二进制 CLI：**把 Git 仓库或本地目录里的依赖，变成可核验、可离线重放、可导出审计产物的对象**。

它用 `ngm.json` 声明依赖（名字 + ref + refType），用 `ngm.lock` 钉住锚点（commit 或 dirDigest）与内容摘要，用 `ngm verify` 重放证明，并可直接从 lock 导出 SBOM 与 in-toto / SLSA 语句。构建、测试、压缩、发布都交给外部工具。

它**不是** npm / pnpm / Yarn / Bun 的替代品或兼容层，而是[独立生态](docs/adr/README.md)：不解析 registry 包、不接管 `node_modules`、不做 semver 求解、不替你选运行时或签名体系。

## 它解决什么问题

| 没有 ngm | 有 ngm |
|---|---|
| `@main` 分不清是 tag 还是 branch | `refType` 显式声明 `commit` / `tag` / `branch` / `local-git` / `local-dir` |
| 只知道 commit，不知道落地的是哪些字节 | `archiveDigest` / `moduleDigest` 对规范化内容清单做 SHA-256 |
| 上游 tag 被重打、分支被改写，CI 无法区分 | `ngm verify` 把漂移分成 `expected` / `unexpected` / `critical` |
| 离线时只能相信缓存 | digest 可从本地 mirror 的 commit tree 重放，不依赖 archive API |
| SBOM 只有版本号，没有内容哈希 | `ngm sbom` / `ngm attest` 直接从 lock 导出带摘要的产物 |

## 边界：不做什么

- 不解析 registry 包、不解析 semver 区间、不做 publish：registry 包继续交给 pnpm / npm / yarn。
- 不做 workspace / hoisting，不接管 `node_modules`；monorepo 只支持同一仓库 + 子路径（`path`）。
- 不执行 `git add` / `git commit`：`ngm.vendor/` 是否提交由你的 Git 流程决定。
- 不自研构建引擎，也不自研 minifier：esbuild / tsc / postcss 等只是被适配的外部工具。
- 不编排任务：编排交给 npm scripts / Just / Taskfile，`ngm run` 只执行被调度的单条脚本。
- 不签名、不管密钥：`ngm attest` 产出未签名语句，签名用你自己的标准工具。

能力归属的唯一事实源是[能力矩阵](docs/internals/capability-matrix.md)。

## 安装

需要 **Git**（拉取与重放依赖）和 **Go ≥ 1.25**（仅构建时需要）。Node.js / Deno 是你的项目运行时，不是 ngm 的运行依赖。

```bash
git clone https://gitee.com/idcu/ngm.git
cd ngm
go build -o ngm ./cmd/ngm
ngm --version
```

Windows 用 `go build -o bin\ngm.exe .\cmd\ngm`。`ngm --help` 列出全部 29 个顶层命令。

也可下载预编译二进制：产物名为 `ngm-<os>-<arch>[.exe]`（`GOOS` / `GOARCH`），随 release 附带 `SHA256SUMS`，下载后用 `sha256sum -c --ignore-missing SHA256SUMS` 校验。发布产物可能落后于源码，需要当前能力就从源码构建。详见[安装指南](docs/guides/installation.md)。

## 仓库与发布

- **主仓**：<https://gitee.com/idcu/ngm> —— 源码、issue、PR 与 tag 的唯一权威来源，开发改动合入这里。
- **镜像**：<https://github.com/idcu/ngm> —— 由主仓单向同步，只作 GitHub 侧的只读入口；不在镜像上开发，也不从镜像提 PR。
- **版本发布**：在 [GitHub Releases](https://github.com/idcu/ngm/releases) 发布。tag 从主仓同步过去后，发布工作流创建 release 并上传 `ngm-<os>-<arch>[.exe]` 与 `SHA256SUMS`；Gitee 发行版只在同步开启时提供同一份产物，不是第二个发布源。

## 5 分钟跑通

前置：机器上有 Git；构建步骤还需外部引擎（下面用 esbuild，`npm i -g esbuild` 或装在项目里）。

```bash
# 1. 初始化（生成 ngm.json 与 src/index.ts）
ngm init github.com/my-org/my-app --runtime=node

# 2. 添加依赖：refType 通常必填，完整 40 位 commit 可省略
ngm add github:my-org/utils@v1.2.3 --ref-type=tag
ngm add github:my-org/logger@main --ref-type=branch

# 3. 安装：首次访问远端需放行一个主机
ngm install

# 4. 验证：检查 ref 漂移并（可选）逐字节重放 digest
ngm verify
ngm verify --content

# 5. 构建：ngm 只把 mappings 变成引擎别名，实际打包由 esbuild 做
ngm build --engine=esbuild --outfile=dist/app.js

# 6. 导出审计产物：只读 lock，不联网
ngm sbom --format=cyclonedx --out=sbom.cdx.json
ngm attest --out=provenance.intoto.json
```

第 3 步第一次会失败并给出提示，这是[默认拒绝 `net:` 权限](docs/guides/configuration.md)的设计：把提示里的 `{"permissions":{"allow":["net:github.com"]}}` 写进 `~/.ngm/config.json` 再重跑。调用外部引擎还需要 `run:esbuild`。

提交 `ngm.json`、`ngm.lock` 与 `ngm.mappings.json`；CI 里用 `ngm install --frozen-lockfile --offline` + `ngm verify --json` 做门禁。完整可复制流程见[快速上手](docs/guides/quickstart.md)。

## 核心对象

| 对象 | 作用 |
|---|---|
| `ngm.json` | 声明依赖、引擎选择、供应链策略、vendor 模式（schemaVersion 3） |
| `ngm.lock` | 锚点 + 内容 digest + `requiredBy` 图；必须提交 |
| `ngm.mappings.json` | 把裸导入映射到 `ngm.vendor/`，供 Vite / esbuild / Deno / TypeScript 读取；必须提交 |
| `ngm.engines.json` | 可选，声明或覆盖引擎条目 |
| `~/.ngm/config.json` | Git 协议与凭证变量、默认引擎、权限 allow / deny |

`archiveDigest` 与 `moduleDigest` 都是规范化文件清单的 SHA-256，不是 tarball 字节的哈希；指令清单包含 path、mode 与 blob hash，Git 子模块与 LFS 指针会被拒绝。根依赖写 `archiveDigest`，monorepo 子路径写 `moduleDigest` + `subtreeOid`。定义见 [ADR-008](docs/adr/README.md) 与 [ADR-032](docs/adr/README.md)。

磁盘布局：`~/.ngm/` 下的 mirror（裸仓库）、module-cache、content store 与 cache，加上项目内的 `ngm.vendor/`（链接到内容树）。`NGM_HOME` 可覆盖 `~/.ngm`。`ngm store usage` 看占用，`ngm store prune` 清残骸，`ngm gc --apply` 按当前项目 lock 回收不可达对象（默认 dry-run）。

## 命令速览

| 类别 | 命令 |
|---|---|
| 项目 | `init` `adopt` `config` |
| 依赖 | `add` `install` `update` `remove` `verify` `why` `tree` `outdated` |
| 模块集合 | `modules` |
| 供应链 | `audit` `sbom` `attest` |
| 引擎与构建 | `typecheck` `typedecl` `build` `transform` `css` `engines` |
| 集成与缓存 | `integrations` `cache` `store` `gc` `mappings` |
| 任务与迁移 | `run` `migrate` |
| 诊断 | `doctor` |

规则：命令自己的 flag 写在命令之后（`ngm --json verify` 会被拒绝）；`ngm <command> --help` 打印该命令自己的用法。支持 `--json` 的命令把机器输出写到 stdout、人类文本写到 stderr。逐条用法与 JSON 形状见 [CLI 参考](docs/guides/cli.md)。

## 退出码

| 码 | 含义 | 可否重试 |
|---:|---|---|
| 0 | 成功 | — |
| 1 | 策略失败（非预期漂移、漏洞超阈值、引擎运行失败、钩子否决） | 否 |
| 2 | 完整性失败（digest 不匹配、字节被篡改） | 否 |
| 3 | 配置 / 用法 / lock 错误 | 否 |
| 4 | Git 或网络失败，含 `--offline` 缺资源 | 是 |
| 5 | 引擎不可用 | 装好后重试 |
| 6 | 内部失败（panic 或结论送不到消费方） | 是 |

两条容易记错的规则：**2 压过 4**（已确证篡改优先于"没查成"）；`--out` 写不进是 **3** 而不是 6。`ngm run` 透传项目脚本自己的退出码，不映射到 0–6。机器可读契约见 [contracts/exit-codes.json](contracts/exit-codes.json)。

## 文档入口

- 上手：[快速上手](docs/guides/quickstart.md) · [安装指南](docs/guides/installation.md) · [示例工程](docs/guides/examples.md)
- 日常：[CLI 参考](docs/guides/cli.md) · [配置详解](docs/guides/configuration.md) · [依赖管理](docs/guides/dependency-management.md) · [构建](docs/guides/build.md) · [测试](docs/guides/test.md) · [迁移](docs/guides/migration.md) · [发布](docs/guides/publishing.md) · [签名配方](docs/guides/signing.md)
- 理解边界：[生态定位](docs/guides/positioning.md) · [术语表](docs/guides/glossary.md) · [能力矩阵](docs/internals/capability-matrix.md)
- 架构：[总览](docs/architecture/overview.md) · [依赖解析](docs/architecture/dependency-resolution.md) · [锁定](docs/architecture/locking.md) · [引擎 adapter](docs/architecture/engine-adapter.md) · [供应链](docs/architecture/supply-chain.md) · [安全模型](docs/architecture/security-model.md) · [可观测性](docs/architecture/observability.md)
- 决策：[ADR 索引](docs/adr/README.md)；开发与贡献见 [docs/README.md](docs/README.md)。

MIT License，见 [LICENSE](LICENSE)。
