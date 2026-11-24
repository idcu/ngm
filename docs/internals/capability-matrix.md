# 能力矩阵

> ngm 的能力现状。成熟度取值：`done`（已实现，标注版本）/ `planned`（设计承诺，标注目标版本）/ `n/a`（不由 ngm 提供）。
>
> 本文是**能力归属的唯一事实源（SSOT）**：其他文档引用本表，不再复制"不做的能力 → 交给谁"的清单。
> v0.1 的实测结论见 [v0.1 复盘](../development/v0.1-retrospective.md)。

---

## 核心能力

| 能力 | ngm 做法 | 成熟度 | 竞品已有 | 备注 |
|------|---------|--------|---------|------|
| Git 拉取 | 4 种协议归一化 | done (v0.1) | npm/pnpm/Yarn 都支持 | ngm 的 refType 是增量 |
| 依赖锁定 | refType + commit + digest | done (v0.1)；**模块 lock v2 done（当前开发线）** | npm 记录 commit | 根用 `archiveDigest`；模块用 `moduleDigest + subtreeOid` |
| 一仓多模块 | `repo + subPath`；模块清单优先 | done（当前开发线，未打 tag） | Go multi-module / Cargo workspace | 不做共享 lock 的 workspace |
| 模块级内容证明 | `ngm-module-digest/v1` + `subtreeOid` | done（当前开发线） | 无直接等价 | 只证明模块子树；兄弟 LFS/gitlink 不毒害 |
| 集合索引 | `ngm modules list/validate/index` | done（当前开发线） | 无直接等价 | 可内联或分片；本地 `ngm-index` 分支 |
| 部分克隆模块缓存 | `blob:none` + lock 钉住的 commit | done（当前开发线） | pnpm store | filter 不支持明确回退完整 mirror |
| vendor 落地 | 4 层模型 | done (v0.1)；**层 2 按文件寻址 done (v0.8)** | pnpm content store | ngm 强调可审计/可提交。**一处曾对 ngm 不利的差别（v0.6 实测，v0.8 已消除）**：层 2 过去按**整棵树**寻址，跨 commit **不去重**（实测每 commit 复制一整棵树 = 源码真实增量的 **20×**），而 pnpm 的 store 按**文件**寻址。v0.8 换成 blob 池 + 树清单后**同一场景约 0.6×**。仍未变的是：层 2 **没有任何人回收**（层 1 有 `git gc`）——去重改的是斜率，不是终点。见 [metrics · 磁盘增长](../internals/metrics.md#磁盘增长内容寻址-storev06) |
| ref 漂移检测 | verify | done (v0.1) | Yarn immutable | ngm 区分预期/非预期 |
| 供应链门禁 | JSON-first 策略 | done (v0.2)；**postinstall 入口 done (v0.3)** | pnpm/Deno/npm | 白名单与 minimumReleaseAge 生效；postinstall 只在沙箱内、只跑 JS 钩子（[ADR-009](../adr/adr-009-supply-chain-policy.md)） |
| OSV 集成 | audit | done (v0.2) | npm/yarn audit | **按 commit 查询**，而非按版本 |
| 可观测性 | why/tree/outdated | done (v0.2) | pnpm | why 给出**全部**来源路径；outdated 查不到时报 unknown 而非"最新" |
| 脚本执行 | `ngm run <script>` / `--list` | done（当前开发线） | npm scripts | 读 `package.json.scripts`（`ngm.json.scripts` 回退）；`run:script` 默认拒绝；退出码原样透传，不映射 ngm 的 0–6（[ADR-042](../adr/adr-042-run-outside-sandbox.md)） |
| 内容级复核 | `ngm verify --content` / `--level=content` | done（当前开发线） | 少见的 archive 级 | 重放 digest + 逐文件字节比对；`--deep` 为旧别名。默认 structure 只比结构与大小 |
| 审计产物 | `ngm sbom` / `ngm attest` | done（当前开发线） | CycloneDX / SPDX 工具 | 只读 lock 导出 CycloneDX 1.7 / SPDX 2.3 与 in-toto/SLSA v1 未签名语句；确定性字节 |
| 采用扫描 | `ngm adopt` | done（当前开发线） | 少见 | 只读扫 `node_modules` 出内容证明；是迁移坡道不是兼容层（[ADR-040](../adr/adr-040-adopt-proof-layer.md)） |
| 项目范围回收 | `ngm gc [--apply]` | done（当前开发线） | pnpm store prune | 按当前项目 lock 回收 store / mirror / vendor / module cache 的不可达对象；默认 dry-run（[ADR-041](../adr/adr-041-project-scoped-gc.md)） |
| Git 签名报告 | `ngm verify --signatures` | done (v0.4) | npm provenance | 报告或强制锁定 commit 的 Git 签名状态；未签名不是失败（[ADR-014](../adr/adr-014-self-report-signatures.md)） |

---

## 构建能力（通过 adapter）

| 能力 | ngm adapter | 外部引擎 | 成熟度 | 备注 |
|------|------------|---------|--------|------|
| Transform | subprocess | esbuild | done (v0.1)；**命令入口 v0.5** | `ngm transform`：单文件、走 stdin/stdout、**不解析导入**（那是一 bundle 的事） |
| Bundle | subprocess | esbuild | done (v0.1) | `ngm build` |
| Tree-Shaking | 不内置 | esbuild 原生 | n/a | 依赖引擎 |
| Type Check | subprocess | tsc / deno | done (v0.2) | `tsc` 在**内置清单**里（`optional`，未装不算 catalog 问题），但**需要声明** `engines.typeCheck` 或 `--engine`：内置的**默认选择**只覆盖 bundle / transform（v0.14 实测并改正，此前写成"内置、装了就能用"）；deno 需自行声明 |
| .d.ts 生成 | subprocess | tsc | done (v0.2) | 同上 |
| CSS/SCSS | subprocess | postcss | done (v0.2) | **此处此前写 `postcss / esbuild` 是错的**：实测 `ngm css --engine=esbuild` 报"no css engine named esbuild"，可用的是 `postcss`、`self`。postcss 无内建压缩，`--minify` 会**明确告知**被忽略 |

---

## 明确不做的能力

| 能力 | 不做的原因 | 交给谁 |
|------|-----------|-------|
| 自研生产级 bundler | esbuild/Rollup 已覆盖 | esbuild / Rollup / Vite |
| HMR | Vite 已是标杆 | Vite |
| 代码分割优化 | Rollup/Vite 已成熟 | Rollup / Vite |
| Source Map 完整链路 | esbuild/Vite 已成熟 | esbuild / Vite |
| 自研 test runner | Vitest/Deno test 已成熟 | Vitest / Deno test |
| 自研 docs generator | TypeDoc/Deno doc 已成熟 | TypeDoc / Deno doc |
| 自研 LSP | tsserver/Deno LSP 已成熟 | tsserver / Deno LSP |
| 自研 Dev Server | Vite/Deno 已成熟 | Vite / Deno |
| 自研 CSS 编译器 | postcss/lightningcss 已成熟 | postcss / lightningcss |
| registry 包管理 | pnpm/npm/yarn 已成熟 | pnpm / npm / yarn |
| 自研 type checker | tsc 经过十年打磨 | tsc / deno check / tsgo |
| 自研 transformer | esbuild/SWC/Biome 已成熟 | esbuild / SWC / Biome |

---

## 机器可读索引（V222 门禁）

> 本节是上面各表与「明确不做的能力」一节的**解析视图**，供 `TestV222CapabilityMatrixMatchesCode` 读取：
> 它只陈述正文已有的事实，不新增能力。改动正文时同步本节；解析边界严格限于
> `<!-- v222:begin -->` 与 `<!-- v222:end -->` 之间的三列表格（类别 / 名称 / 状态），
> 表格外的说明文字不是门禁输入。

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

---

## 诚实版能力评估

### ngm 真正独有的

1. **refType 自描述（仅完整 commit 窄默认）** + 内容 digest + verify 闭环
2. **vendor 4 层**为可审计目标服务（不只是省磁盘）
3. **供应链策略绑定到 Git commit** 而不是 registry version
4. **mappings 协议**桥接 ngm 与外部构建工具

### ngm 不独有的

1. Git 依赖支持（npm/pnpm/Yarn 都有）
2. content-addressable store（pnpm 已有）
3. OSV 集成（npm/yarn 都有）
4. minimumReleaseAge（pnpm/Deno/npm 都有）
5. 白名单（Yarn approvedGitRepositories）
6. ref 漂移检测（Yarn immutable）

**结论**：ngm 的差异化不在单项能力，而在**流程闭环**和**共享状态模型**。若这些策略只是分别调用外部命令，ngm 就只是配置与编排层。

---

### 2026-11-24 生态路线新增（E1–E14）

- **已实现**：构建回执（`build --provenance` → `ngm.build.json`）、回执复验（`verify --receipt`）、本地构建缓存（`build --cache`）、`run --explain` 只读计划、引擎版本/摘要绑定、CSS 压缩 fail-closed、CI action `receipt` mode、四个示例工程。
- **仍非目标**：任务图 / 并行 / 增量 / watch / 安装型 `run` / vendored `.bin` shim（见 ADR-042 补记、ADR-043）；registry / semver / publish / workspace / 多版本共存；自研 bundler / minifier / test runner / remote adapter。
- **边界**：回执只记录事实，不承诺跨工具可复现构建；provenance 不证明内容无害。

## 相关文档

- [架构总览](../architecture/overview.md)
- [健康度指标](./metrics.md)
- [路线图](./roadmap.md)
