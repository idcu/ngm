# ngm 文档

> **ngm 只解决一个问题**：当依赖直接来自 Git 仓库时，证明"我正在运行的代码"就是"我审过的那份代码"。
> 它不是 npm / pnpm / Yarn / Bun / Vite 的通用替代品。

**当前状态：v0.1 ~ v0.12 均已交付**，**`v0.1.0` ~ `v0.12.0` 十二个 tag 均已打**。
v0.12 的主题是**把读数修准**——包括本页自己的读数：它此前把 `v0.1.0` 算作"Gitee 也可取到"。
许可证也已从"保留所有权利"改为 **MIT**（见[许可证](#许可证)），逐版见[发布状态](#发布状态)。
**"交付"与"发布"是两件事，这一页把它们分开写**：GitHub 上有 12 个里的 **8 个** release，
Gitee 上只有 **3 个**，而 `v0.1.0` 在那里**没有发行版**。逐版状态与出处见[发布状态](#发布状态)
——那张表的数字是 2026-10-04 **直接问两个源的 API** 得到的，不是本页的记忆
（Gitee 侧随时可用 `scripts/check-gitee-release-status.ps1` 复读）。

各版计划与复盘见[开发总览](./development/README.md)；
其中 v0.6 的[复盘](./development/v0.6-retrospective.md)含**跨机器可复现性的实测判定**
（三平台 + 本机同值 `treeA=124f03b1de58091a`），v0.8 的[复盘](./development/v0.8-retrospective.md)
记录了层 2 换布局：把"每 commit 复制一整棵树"的 **20.0×** 放大降到 **0.6×**。
已发布版本的 `SHA256SUMS` 在两源上逐个比对为**逐字节相同**，见[安装指南](./guides/installation.md)
与[发布清单](./development/README.md#补发记录2026-10-02gitee-侧)。

本目录每篇文档都用**成熟度**标注可用范围，不会让规划中的能力看起来像已经能用：

| 标记 | 含义 |
|------|------|
| `done (v0.1)` | 现在就能用 |
| `done (v0.2)` / `done (v0.3)` / `done (v0.4)` | 该版本交付的能力，现已可用 |
| `planned (vX.Y)` | 规划中，**尚未实现**；用到时会明确报错（通常 `exit 3`），不会静默成功 |

> v0.1 ~ v0.4 的能力**全部已交付**；下表与各页保留版本号是为了说明"从哪一版起可用"。

能力归属的唯一事实源是[能力矩阵](./internals/capability-matrix.md)；路线图是 [roadmap](./internals/roadmap.md)。
其余文档引用它们，不复制——发现两处说法冲突时，以事实源为准。

---

## 从这里开始

**第一次接触 ngm**，按这个顺序约 20 分钟走完：

1. [安装指南](./guides/installation.md) —— 装好 `ngm` 命令
2. [快速上手](./guides/quickstart.md) —— 5 分钟跑通 `init → add → install → verify → build`
3. [术语表](./guides/glossary.md) —— 遇到不认识的词先查这里（每个词都标了出处）
4. [配置详解](./guides/configuration.md) —— `ngm.json` / `ngm.lock` / `ngm.vendor`

**带着具体目的来**：

| 我想… | 看这篇 | 读者 |
|-------|--------|------|
| 查某个命令、某个 flag | [CLI 参考](./guides/cli.md) | 使用者 |
| 增删改依赖、处理冲突 | [依赖管理](./guides/dependency-management.md) | 使用者 |
| 用 ngm 构建 / 类型检查 / 编译 CSS | [构建](./guides/build.md) | 使用者 |
| 让 CI 用上 ngm、发布产物 | [CI 与发布](./guides/publishing.md) | 使用者 |
| 在 Node 和 Deno 之间选 | [Node vs Deno](./guides/node-vs-deno.md) | 使用者 |
| 从 npm / pnpm 迁过来 | [迁移指南](./guides/migration.md) | 使用者 |
| 给**你的项目**配测试（Vitest / Deno test） | [测试](./guides/test.md) | 使用者 |
| 跑 ngm 自己的测试与里程碑验收 | [开发总览](./development/README.md) | 贡献者 |
| 搞懂它为什么这样设计 | [架构总览](./architecture/overview.md) | 贡献者 |
| 查某个设计决策的理由 | [ADR 索引](./adr/README.md) | 贡献者 |
| 看 ngm 与竞品的差异 | [竞品分析](./COMPETITIVE-ANALYSIS.md) | 评估者 |
| **一眼看清"现在能用到什么程度、卡在哪、下一步"** | **[项目状态评估](./internals/project-state.md)** | **评估者 · 接手者** |
| 参与开发 | [开发总览](./development/README.md) | 维护者 |

---

## 它是什么

一条闭环，把原本分散的 Git 依赖控制点收束成同一套默认流程：

```
refType 声明 → commit 解析 → archiveDigest 锁定 → vendor 落地
  → verify 漂移检测 → 〔v0.2：OSV / 白名单门禁〕 → 可复现构建
```

**它不做什么**（完整清单见[能力矩阵 · 明确不做的能力](./internals/capability-matrix.md)）：

- 不管 npm registry 的 tarball 生态（交给 pnpm）
- 不重写 Vite / Rollup / esbuild（走 engine adapter）
- 不做"构建 + 测试 + 文档 + LSP + Dev Server"大一统
- 不自研 bundler / HMR / test runner / LSP 去追赶竞品

---

## 运行时模型（核心决策）

| 层 | 技术 | 职责 |
|---|---|---|
| **ngm core** | **Go 单二进制** | 依赖图 / lock / vendor / cache / hardlink / verify |
| **Host runtime** | **Node.js 或 Deno** | 用户项目实际运行的环境 |
| **Engine adapters** | subprocess（v0.1）/ **wasm（v0.3）**；`remote` 已被 [ADR-013](./adr/adr-013-remote-adapter.md) 排除 | 调用外部引擎；内置只有 esbuild（`bundle` / `transform`）与 `self` stub |
| **Security sandbox** | Deno-style capability model（v0.3） | verify / audit / postinstall 都在同一套 default-deny 权限下 |

> **实现语言用 Go；宿主运行时 Node/Deno 都支持；安全模型学 Deno。**

详见 [ADR-007：Node 还是 Deno？](./adr/adr-007-runtime-node-deno.md) 与[架构总览](./architecture/overview.md)。

---

## 为什么不是"又一个包管理器"

| 工具 | 已有能力 | ngm 不重复造 |
|------|---------|-------------|
| npm | Git commit 记录、signature、provenance、audit | npm 已是 registry 包的标准答案 |
| pnpm | content-addressable store、minimumReleaseAge、blockExoticSubdeps | pnpm 已是高效磁盘管理的标准答案 |
| Yarn | approvedGitRepositories、checksum、immutable | Yarn 的 Git 审批已成熟 |
| Bun | 一体化（install/build/test/docs/HMR/runtime） | Bun 已是速度/一体化标杆 |

**ngm 只填一个窄缺口**：把上述分散的控制点——refType + commit + archiveDigest + vendor + verify +〔v0.2：OSV〕——收束为同一默认流程。

若这些策略只是分别调用外部命令，ngm 就只是配置与编排层。所以 ngm 的差异化必须在**流程闭环**与**共享状态模型**上成立，而不是在单项性能上。逐项对比见[竞品分析](./COMPETITIVE-ANALYSIS.md)。

---

## 最小闭环（v0.1 可逐字执行）

```bash
# 1. 初始化项目（指定宿主运行时）
ngm init github.com/my-org/my-app --runtime=node
# 也可 --runtime=deno

# 2. 添加依赖（refType 必填：commit / tag / branch）
ngm add github:my-org/utils@v1.2.3 --ref-type tag
ngm add github:my-org/logger@main --ref-type branch

# 3. 解析 ref → commit，计算 archiveDigest，落地 vendor，写入 ngm.lock
ngm install

# 4. 复核：ref 是否漂移、digest 是否可本地重放
ngm verify

# 5. 构建（通过 adapter 调引擎；内置 esbuild / typescript / postcss）
ngm build --outfile=dist/app.js
```

> 完整的 5 分钟流程（含 `ngm build` 的实际输出）见[快速上手](./guides/quickstart.md)，
> 并由 CI 的端到端验收守护。CI 上用 `ngm install --frozen-lockfile --offline`：
> frozen 锁死"装什么"（不许重新解析 ref），offline 锁死"怎么拿"（不许联网）。

### 项目结构

```
my-app/
├── ngm.json              # 依赖声明（refType 必填）+ 引擎选择 + 运行时
├── ngm.lock              # 锁定：refType + commit + archiveDigest + resolvedAt（必须提交）
├── ngm.mappings.json     # 构建工具映射（供 esbuild 等读取裸导入）
├── ngm.engines.json      # 引擎清单（可选，覆盖内置）
├── src/
│   └── index.ts
└── ngm.vendor/           # 依赖源码落地（默认 hardlink 到 content store）
    └── github.com/
        └── my-org/
            ├── utils/
            └── logger/
```

---

## 核心能力状态

v0.1 起就有的核心闭环（**现在就能用**）：

| 能力 | 入口命令 | 说明 |
|------|---------|------|
| 依赖声明与解析 | `ngm init` / `add` / `remove` | refType 必填；广度优先解析传递依赖、检测冲突 |
| 解析安装与锁定 | `ngm install` | 有 lock 则尊重 lock，不重新解析 ref |
| ref 更新 | `ngm update` | 只有它会把锁定 commit 移到新位置 |
| 漂移与完整性复核 | `ngm verify` | 三级检查 + `--deep` 全量字节校验 + `--json` |
| 构建（adapter） | `ngm build` | 内置 esbuild（`bundle`）；`typecheck` / `css` / `typedecl` 在缺引擎时明确 `exit 5` |
| mappings | `ngm mappings` | 生成 / 校验 `ngm.mappings.json` |
| 缓存维护 | `ngm cache clean` | 清缓存层，不影响可证明性 |
| 配置与引擎 | `ngm config` / `ngm engines` | 校验与查看 |

**v0.2 已补上这两个引擎**：`ngm typecheck` 内置 `typescript`（tsc，装了就能用）；
`ngm css` 内置 `esbuild` 与 `postcss`。三者未安装时是 `exit 5` 并给出安装命令——
**那是"没有可用引擎"，不是"检查通过"**。

仍要分清两类失败（很容易误读）：

- **未声明对应引擎** → `exit 3`（缺配置）
- **声明了但引擎干不了这事** → `exit 5`：例如拿 esbuild 顶类型检查——它只剥掉类型标注、
  不做校验，ngm 明确拒绝而不是静默当作通过

`deno` 已适配但**需自行声明**（它的 bundle 是 Deno ≥ 2.4 的实验特性，且内置会抢掉
typeCheck 的默认顺序），见[引擎 adapter](./architecture/engine-adapter.md)。

**v0.2 已落地**：`ngm audit` —— 按 lock 中的 commit 查 OSV.dev，结果缓存 24 小时，支持
`--offline`（只读缓存）、`--no-cache`、`--json`。注意"审计通过"的含义是**库里没有关于这个
commit 的记录**，不等于安全——覆盖局限会写进每一份报告。

**v0.3 已落地**：`ngm integrations add vite|esbuild|deno|webpack` —— 生成各构建工具消费
mappings 所需的配置（含 tsconfig `paths`）。它**不覆盖**用户已有的配置文件：内容不同时
报出差异并以 exit 3 结束，由用户决定怎么合。

**v0.4 已落地**：`ngm typedecl --outdir=<dir>` 给 `typeDecl` 能力一个入口（此前该能力只有
adapter 与单测、没有命令驱动）；`ngm verify --signatures` / `--require-signed` 报告或强制
锁定 commit 的 Git 签名状态——**未签名不是失败**，判定交给你自己的密钥配置，ngm 不管理密钥
（[ADR-014](./adr/adr-014-self-report-signatures.md)）。

**v0.5 已落地**：`ngm transform` —— 单文件转换（输入默认 stdin、产物默认 stdout、
**不解析导入**）。它补上了最后一项"能力在 adapter 层有实现、却没有命令驱动"的缺口：
此前 `engines.transform` 与 `engines.defaultTransform` 对用户没有任何可观察的效果。
补入口时顺带修掉一处协议缺陷：自定义引擎的 transform 分支**静默丢弃** `--format` /
`--minify` / `--sourcemap`。

至此 CLI 参考里列出的命令**全部已实现**，命令表中不再有占位项。没有列出的命令不存在，
用到会明确报错，不会静默成功。逐项成熟度以[能力矩阵](./internals/capability-matrix.md)为准。

---

## 诚实版：劣势与边界

### 为什么你可能**不应该**用 ngm

| 你的场景 | 建议方案 | 原因 |
|---------|---------|------|
| 只用 npm registry 包 | pnpm / npm | ngm 不管理 registry 包 |
| 需要生产级前端构建 + 插件生态 | Vite | ngm 只管依赖，不管构建 |
| 需要成熟测试框架 | Vitest / Deno test | ngm 不做 test runner |
| 需要 Dev Server / HMR | Vite / Deno | ngm 不做 dev server |
| 追求最小学习成本 | 任何主流工具 | ngm 是窄场景工具 |

### 你可能想用 ngm 的理由

- **Git 依赖需要可证明**：私有 fork、上游 commit 追溯、跨团队可信组件协作
- **vendor 需要可审计**：离线交付、审计或镜像场景
- **供应链策略需要统一**（v0.2）：不想在 Git URL、lockfile、校验脚本、SBOM、OSV 之间来回拼接
- **愿意接受早期工具链的接口变化与性能未验证**

### 四类自嗨风险（本项目持续自检）

1. 声称"无 registry"代表安全或去中心化——却忽略 Git host 与凭证依赖
2. 声称 vendor 一定节省磁盘——却忽略跨项目重复与仓库膨胀（pnpm content store 更优）
3. 声称 Go 单二进制天然更快——却忽略底层仍需调用外部引擎
4. 声称一体化覆盖 test / docs / LSP / Dev Server——却忽略这些能力需要大量生态适配

> 已实测的边界（含 2 条**未达标**的性能目标）见 [v0.1 复盘](./development/v0.1-retrospective.md)。
> 我们不把未达标项读作"接近达标"。

### 四个必须能回答的问题

| # | 问题 | 当前答案 | 出处 |
|---|------|---------|------|
| 1 | 同一 commit 是否能在断网、Git host 故障和 token 失效时继续**构建**？ | 可以：vendor 落地后构建不依赖网络。完全离线的*安装*用 `install --frozen-lockfile --offline`（v0.2 已实现） | [vendor 4 层](./architecture/vendor-layers.md) |
| 2 | branch 前进时，`verify` 是否区分"预期更新"与"非预期漂移"？ | 区分：`driftKind`（`expected` / `unexpected` / `critical`），预期更新默认不阻断 | [信任模型](./architecture/trust-model.md) |
| 3 | 传递性 Git 依赖是否也进入 lock、verify 与 OSV（v0.2 起）范围？ | 进入；只识别上游 `ngm.json` 声明，上游 `package.json` 里的 Git 依赖不自动递归 | [依赖解析](./architecture/dependency-resolution.md) |
| 4 | `archiveDigest` 的 origin、normalization 和算法是否明确定义？ | 已定义：本地生成的规范化内容清单（path / mode / blob-sha256）+ sha256 | [ADR-008](./adr/adr-008-archive-digest.md) |

---

## 里程碑

以[路线图](./internals/roadmap.md)为唯一事实源，此处只给一页速览：

| 版本 | 目标 | 状态 |
|------|------|------|
| v0.1 | Git 声明/锁定 + vendor 4 层 + verify + 最小 esbuild adapter | **已完成**（四条退出标准全部达成） |
| v0.2 | 供应链策略最小字段集 + OSV / audit + `why`·`tree`·`outdated` + **多引擎 adapter（tsc / deno / postcss）** + `install` 的 CI 模式 + verify 性能优化 | **已完成**（离线 verify 达标；在线 verify 差 1.1×，见[复盘](./development/v0.2-retrospective.md)） |
| v0.3 | wasm adapter + 集成脚手架（Vite / esbuild / Deno / Webpack）+ Deno 沙箱 + 权限与凭证 + mappings 子路径扩展 | **已交付**（`remote` adapter 经 [ADR-013](./adr/adr-013-remote-adapter.md) 决定不发布） |
| v0.4 | `ngm typedecl` + `verify --signatures` / `--require-signed` + 配置字段接线的机械检查 | **已交付**（沙箱自述文件签名检查经 [ADR-014](./adr/adr-014-self-report-signatures.md) 以"决定不做"结项） |
| v0.5 | 在线 verify 的成本与方差 + ADR-013 翻案条件判定 + 权限施加点的机械核对 + **v0.2~v0.4 补发布** + 挂账项收尾 | **已交付**（详见[复盘](./development/v0.5-retrospective.md)；残项已于 2026-10-02 补齐：Gitee 侧 21 个附件） |
| v0.6 | 让结论**可判别**：spawn 预算门禁 + 真实形态的可复现证据 + store 增长数据 + Gitee 补传 | **已交付**（[复盘](./development/v0.6-retrospective.md)） |
| v0.7 | content store 的**占用可见**（`ngm store usage`，只读）与**残骸可回收**（`ngm store prune`） | **已交付**（[计划](./development/v0.7-plan.md) / [复盘](./development/v0.7-retrospective.md)） |
| v0.8 | 层 2 换布局（blob 池 + 树清单，[ADR-019](./adr/adr-019-content-addressed-blobs.md)）：先让消费方与布局解耦，再换布局 | **已交付**（[计划](./development/v0.8-plan.md) / [复盘](./development/v0.8-retrospective.md)：20.0× → 0.6×；一处用户可见的变化：`symlink` 落地模式退化） |
| v0.9 | 让 store 的读数**说真话**：blob 池切成共享/独占/孤儿 + 锚点检查 | **已交付**（[计划](./development/v0.9-plan.md) / [复盘](./development/v0.9-retrospective.md)：blob 池切成共享/独占/孤儿 + 锚点检查 + 10 万级规模实测） |
| v0.10 | 把检查推到**最外圈**：根 `README.md` 的链接与锚点 + "store 不完整"时的两条承诺 | **已交付**（[计划](./development/v0.10-plan.md) / [复盘](./development/v0.10-retrospective.md)：实现 **0 行**改动，钉的是既有承诺） |
| v0.11 | 把挂着的四项决策**结掉**（[ADR-020](./adr/adr-020-remote-adapter-shelved.md)~[023](./adr/adr-023-orphan-reclaim.md)）+ 层 2 第一次真回收 + workflow YAML 门禁 | **已交付**（[计划](./development/v0.11-plan.md) / [复盘](./development/v0.11-retrospective.md)：三件决策的结论是"不做"，但都写明了重开条件） |
| v0.12 | **可信读数**：文档三层纠偏（9 处）+ MIT 许可证 + 子命令帮助真实现 + **六处**"静默失效"缺陷（`outdated --offline` 触网、`--json --hook` 破坏 JSON、`css` 丢位置参数、`typedecl` 吞诊断、脚手架半套落盘、端口被静默丢弃）+ 6 条机械网 | **已交付**（[计划](./development/v0.12-plan.md) / [复盘](./development/v0.12-retrospective.md)：唯一的功能增量是子命令帮助，其余全是把读数修准） |

### 发布状态

**"交付"与"发布"是两件事**，这一页把它们分开写——因为本项目已经两次把后者漏掉
（v0.2~v0.4，然后是 v0.5~v0.8）。

| 版本 | 源码 | tag | GitHub release | Gitee 发行版 |
|------|------|-----|----------------|-------------|
| `v0.1.0` | ✅ | ✅ | ✅ 7 个资产 | ❌ **不存在**（复核：`/releases/tags/v0.1.0` 返回 `null`） |
| `v0.2.0` / `v0.3.0` / `v0.4.0` | ✅ | ✅（2026-10-01 补） | ✅ 各 7 个 | ✅ 各 7 个附件（2026-10-02 补，另附 2 个源码包） |
| `v0.5.0` ~ `v0.8.0` | ✅ | ✅（2026-10-02 补打，附注 tag，已在两个源上） | ❌ **未生成**（镜像转发 tag 未触发 `release.yml`）——2026-10-03 暂缓已取消，恢复时走 **Actions → Release → Run workflow** | ❌ 未上传（`scripts/upload-gitee-assets.ps1 -Tag v0.X.0`，需 `GITEE_TOKEN`） |
| `v0.9.0` / `v0.10.0` / `v0.11.0` | ✅ | ✅ | ✅ 各 7 个（tag 推送触发，已复核） | ❌ 未上传（同上） |
| **`v0.12.0`** | ✅ | ✅ | ✅ 7 个（tag 推送触发，2026-10-04 00:57 UTC 已发布——**这条路径第四次连续成功**） | ❌ 未上传（同上，需 `GITEE_TOKEN`） |

> **`v0.1.0` 这一格是本页此前写错的地方**：它写着 `v0.1.0` ~ `v0.4.0` "两个源都可取到"，
> 而补发记录里从来只有 `v0.2.0`/`v0.3.0`/`v0.4.0` 三个——两处说法不一致时，**是 API 说了算**。
> 同一条纪律的另一次体现：`v0.8` 复盘记的"tag 全在、release 一个没有"，
> 而当时**没有任何检查会红**。

> **"tag" 这一栏不再靠人记**：`scripts/check-release-status.sh`（CI job `release-status`）机械地
> 要求**有复盘 ⇔ 有 tag**（按 `vX.Y` 配对），缺一边即红。它判据用的是复盘文件而不是计划里的
> 状态行——因为状态行是散文、格式历来不统一，**会漏的判据等于没有判据**。
> 为什么必须有这条：清单第 0 步正是为 v0.2~v0.4 的同类事故写的，却仍被跨过四次
> （记在 [v0.8 复盘 §5.6](./development/v0.8-retrospective.md)）。
>
> **"GitHub release" 这一栏有它自己的读数**：`bash scripts/check-release-status.sh --published`
> ——它问 GitHub 每个 tag 有没有 release、资产齐不齐（CI 里作为 `release status` 的第二个步骤
> 跑，**告警不阻塞**）。这次正是"tag 全在、release 一个没有"，而当时没有任何检查会红。
>
> **"Gitee 发行版"这一栏从 v0.12 起也有读数**：`powershell -File scripts/check-gitee-release-status.ps1`
> ——它对每个 `v*` tag 报"有没有发行版、7 个附件齐不齐"，**只读公开 API、不需要 token**，
> 查不动时 exit 3 而**不报成功**。它是**本地读数**（CI 是 ubuntu，跑不了 PowerShell），
> 用途就是让上面那句错话无法再出现：写"已发布"之前先让它签个字。

**v0.1 起就必须保留引擎接口、lock schema 与可复现性**：若先实现功能、再补策略与接口，后续很可能被迫破坏早期设计。

---

## 文档地图

| 目录 | 内容 | 读者 |
|------|------|------|
| [guides/](./guides/) | 安装 / 快速上手 / 配置 / 依赖管理 / 构建 / CLI 参考 / 术语表 / Node vs Deno / 测试 / 发布 / 迁移 | 使用者 |
| [architecture/](./architecture/) | 总览 / 运行时模型 / 信任模型 / vendor 4 层 / 引擎 adapter / 供应链 / 依赖解析 / 锁定 / 安全 / 可观测性 | 贡献者 |
| [adr/](./adr/README.md) | 架构决策记录（ADR-001 ~ 023）与 ADR 流程 | 贡献者 |
| [modules/](./modules/) | 模块分解（P0 ~ P8） | 维护者 |
| [internals/](./internals/) | **项目状态评估** / 能力矩阵 / 健康度指标 / 路线图 | 维护者 · **接手者** |
| [development/](./development/) | 开发总览 / v0.1 ~ v0.12 实施计划与复盘 / v0.5 复核 | 维护者 |
| [COMPETITIVE-ANALYSIS.md](./COMPETITIVE-ANALYSIS.md) | 竞品逐项对比（**能力矩阵与竞品表的引用方**） | 评估者 |

### 单一事实源（SSOT）约定

同一件事只在**一个地方**维护，其余文档引用它——避免"两处说法不一致时读者不知道该信谁"：

| 内容 | 唯一维护位置 |
|------|-------------|
| 能力归属与成熟度 | [internals/capability-matrix.md](./internals/capability-matrix.md) |
| 里程碑与范围 | [internals/roadmap.md](./internals/roadmap.md) |
| 模块结构 | [modules/p0-core.md](./modules/p0-core.md) |
| 引擎 adapter 模型与内置清单 | [architecture/engine-adapter.md](./architecture/engine-adapter.md) |
| subprocess 协议 | [modules/p4-ecosystem.md](./modules/p4-ecosystem.md) |
| 退出码 | [architecture/observability.md](./architecture/observability.md) |
| 术语定义 | [guides/glossary.md](./guides/glossary.md) |

---

## 许可证

**MIT** —— 见仓库根目录的 [`LICENSE`](../LICENSE)。

此前这里写的是"保留所有权利、未授予任何许可"。那让**评估之后想用的人也无法合法使用**，
而本项目的下一步恰恰是"找一个真实使用场景"：一个没人有权运行的工具，
收集不到它唯一需要的那样东西。改动的记录见 [v0.12 计划](./development/v0.12-plan.md)。
