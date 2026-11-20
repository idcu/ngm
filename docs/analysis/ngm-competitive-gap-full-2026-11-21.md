# ngm 与市面主流竞品的全维度差距评估（2026-11-21）

> **基线**：HEAD `9474d6c`，工作树干净。方法是"己方回到源码与当前文档核实 + 竞品一手检索交叉验证"，
> 不照抄 `ngm-architecture-and-npm-gap-2026-11-18.md` 或 `COMPETITIVE-ANALYSIS.md` 的旧快照。
> **本轮不修改产品代码**；文末 §5 识别的过期事实与读数漂移已就地修订（加时间限定与当前状态，不重写历史快照）。
> **事实源分级**：`[官方]` = 官方文档 / changelog / 官方博客；`[实测]` = 第三方可复现测量；
> `[媒体]` = 行业媒体二手报道（方向可信、细节待核）；`[推断]` = 本报告基于证据的判断，非事实陈述。
> **评测坐标**：按 [ADR-044](../adr/adr-044-independent-ecosystem.md)，ngm 是独立生态，
> 正确的问题是"**依赖的内容身份能否被独立复核**"，而不是"npm 的哪一格还空着"。
> 因此本报告把"落后"与"正交/明确不做"严格分开——把正交项算成落后，是评测 coordinate error。

---

## 0. 结论摘要

1. **最大的变化不是 ngm 退步，而是对手把 ngm 的差异化论据商品化了。** 2026-07 的 npm v12 把
   依赖生命周期脚本、`allow-git`、`allow-remote` 全部改成默认关闭 `[官方]`；pnpm 11/12 把
   `minimumReleaseAge`（默认 1 天）、`blockExoticSubdeps`（默认 true）、`allowBuilds`（默认全禁）
   变成开箱默认，并自带 `pnpm sbom` `[官方]`；Deno 2.9 默认强制 24 小时最低依赖年龄、
   支持 `trust-policy: no-downgrade` `[官方]`。**COMPETITIVE-ANALYSIS.md 里"ngm 三项无对应物"
   的措辞已不再成立**（见 §5.1）。

2. **但 ngm 的主轴仍未被覆盖**：把"refType 显式声明 → commit → 内容 digest → 离线从 Git 对象重放 →
   verify 漂移三态 → 审计产物"收束到**同一状态模型**并默认执行，主流工具仍没有等价物 `[推断]`。
   Cargo 的 `cargo vendor` 有逐文件 checksum，但官方注释明确"仅防误改，非安全机制" `[官方]`；
   `git-tag-guardian` 之类工具能做到 tag 重写检测 + SHA 基线，但它是**外挂工具**，不是安装/锁定/
   验证共用的状态机 `[官方（npm 包文档）]`。这是"闭环"与"单点"的差别，不是"有/无"的差别。

3. **真实落后集中在三处，且都不在主轴上**：① 签名溯源——npm 已到 SLSA L3（Sigstore + 透明日志 +
   trusted publishing 默认带 provenance），ngm 的 `attest` 明确只产**未签名**语句 `[官方]`；
   ② SBOM 格式版本——pnpm 产 CycloneDX **1.7**，ngm 产 **1.5** `[官方]`；
   ③ 采用度与分发——npm 周下载 6500 万级、Vite 6500 万级，ngm 的 101 个正式 tag 里 GitHub
   release 仅覆盖 63 个、Gitee 仅 3 个，且无外部安全审计 `[官方 + 本地实测]`。

4. **有一条 SSOT 事实必须纠偏**：`COMPETITIVE-ANALYSIS.md` 写"npm 以 works as expected 关闭
   PackageGate"，那是 2026-01 的历史状态。npm v12 已经关闭这条路径——官方 changelog 明说
   `--allow-git` 默认 none 是"关闭 Git 依赖的 `.npmrc` 覆盖 git 可执行文件的代码执行路径，
   那条路径即使 `--ignore-scripts` 也会运行" `[官方]`。**历史叙述保留，但必须加当前状态与时间限定**，
   否则就是本项目最反感的"读数说谎"（见 §5.1 与 §7）。

5. **按 ADR-044 准入规则筛选后，值得做的只有 4 项**（SBOM 升版、attest 谓词升级、发布覆盖、纠偏
   过期事实），其余都是"唯一理由是更像 npm"或"不在轴上"，不进路线图（见 §6）。

---

## 1. 范围、方法与事实基线

### 1.1 竞品集合与维度

| 组 | 对象 | 为什么在对比集里 |
|---|---|---|
| 依赖管理 | npm 12、pnpm 11/12、Yarn Berry 4.x、Bun 1.4、Deno 2.8/2.9 | 覆盖 JS 生态 5 大主流 + vlt（PackageGate 当事人） |
| 构建链路 | Vite 8/Rolldown、Turbopack、Rspack、esbuild、Biome/Oxc、tsgo | ngm 通过 adapter 调用/对标"明确不做"的边界 |
| 跨语言参照 | Cargo（workspace + vendor）、git-tag-guardian（npm 生态外挂） | 检验"ngm 独有"断言是否真的独有 |
| 供应链标准 | SLSA v1.x、Sigstore/cosign/Rekor、SPDX 3.0 / CycloneDX 1.6–1.7、OSV | 判断 ngm 在标准坐标系里的位置 |

维度共 14 个，见 §4 的逐维矩阵。**"明确不做"是结论，不是欠账**（ADR-044 + capability-matrix）。

### 1.2 己方读数（本地实测，非文档照抄）

| 项 | 读数 | 来源 |
|---|---|---|
| HEAD / 工作树 | `9474d6c` / 干净 | `git log -1`、`git status --porcelain` |
| 顶层命令 | **28** | `grep -cE '^\s*\{Name:' cmd/ngm/root.go` |
| Test 函数 | **991**（严格签名 `func TestXxx(t *testing.T)`）；含 `TestMain` 992；加 3 Benchmark + 3 Fuzz = **998** 个测试入口 | `grep -rhE '^func Test[A-Za-z0-9_]*\(t \*testing\.T\)' --include='*_test.go' . \| wc -l` |
| Go 文件 / 行数 | **481 / 94,905**（实现 147 文件 / 非测试行约 36.4k，测试 334 文件） | `find . -name '*.go' … \| xargs cat \| wc -l` |
| Markdown 文件 | **339** | `find . -name '*.md' -not -path './.git/*' \| wc -l` |
| ADR | **49**（README 第 592 行仍写"45 个决策记录"——**这是本次发现的第二处读数漂移**，见 §5.4） | `find docs/adr -name 'adr-*.md' \| wc -l` |
| tag | **103 个 `v*`**（101 正式 + `v0.102.0-dev.1/.2`） | `git tag \| grep -c '^v'` |
| 最新正式 tag | **v0.101.0**；距最新 tag（`v0.102.0-dev.2`）另有 **151 个提交**（含 lock v2 / core 抽离） | `git tag --sort=-version:refname`、`git rev-list --count v0.102.0-dev.2..HEAD` |
| CI job 数 | **15**（含 race / fuzz / bench / acceptance / engine / docs-links / discipline 等） | `.github/workflows/ci.yml` |
| 覆盖门槛 | `verify` 78%（CI 用 `go tool cover` 合并，2026-11-20 修正了 awk 求和错误） | `ci.yml` + `v137_coverage_visibility_test.go` |
| 核心闸门 | `refType` 必填且仅完整 40-hex 走窄默认；`postInstallPolicy` 枚举 deny/prompt/allow；OSV 按 40-hex commit 查询 | `internal/config/config.go:66-77,214-220`、`internal/supplychain/osv.go:105-158` |

### 1.3 证据边界（诚实声明）

- 竞品事实来自公开文档/官方 changelog 与第三方实测报道，**未逐个本地安装复跑**。
  `[媒体]` 级数字（provenance 采用率、下载量）口径各异，报告只取"方向一致"的结论，不把单一数字当精确事实。
- ngm 侧不变量回到源码核对了 4 项（见 §1.2），其余沿用当前 SSOT（`capability-matrix.md` /
  `README.md` / `metrics.md`），这些 SSOT 本身有 13 条 discipline 判据守护。
- `verify` 覆盖率 78% 是 **CI 口径**；本机 `-race` 因无 gcc/CGO 无法复跑，靠 CI 上次全绿读数（24/24）。

---

## 2. 竞品全景（更新到 2026-11，取代旧快照）

### 2.1 依赖管理：安全默认成为标配

| 能力 | npm 12.2.1 | pnpm 12.x / 11 | Yarn Berry 4.x | Bun 1.4 | Deno 2.9 | ngm（当前） |
|---|---|---|---|---|---|---|
| 信任根 | registry version + tarball integrity | 同左 + CAS checksum | 同左 + checksum | 同左 + lockfile integrity | npm integrity / JSR | **Git commit + 内容 digest** |
| Git 依赖 | **默认拒绝**（`allow-git=none`）`[官方]` | 直接可；**传递被 `blockExoticSubdeps` 拦** `[官方]` | `approvedGitRepositories` | 可；**记录 SHA-512** `[媒体]` | 可 | **唯一对象，Git-first** |
| lockfile | `package-lock.json` | `pnpm-lock.yaml` | `yarn.lock`（+ PnP / zero-install） | `bun.lock`（JSONC） | `deno.lock` | `ngm.lock` v2 |
| 内容摘要 | registry tarball integrity | CAS checksum（hex 索引，SQLite） | checksum | **SHA-512 for git/tarball** | integrity | **规范化清单 SHA-256，可从 Git 对象离线重放** |
| 漂移检测 | lock + audit signatures | `trustPolicy: no-downgrade` `[官方]` | `immutable`（lock 一致性） | lock integrity | `trust-policy: no-downgrade` `[官方]` | **ref 漂移三态 + 内容级 `--content`** |
| 安装脚本 | **默认 off**（`allowScripts`）`[官方]` | **默认全禁**（`allowBuilds`）`[官方]` | 默认受限 | 默认受限 | **默认 off**（`--allow-scripts`） | **默认 deny；开启仅在 Deno 沙箱** |
| 版本冷却 | 无默认（cooldown 由生态侧） | **默认 1 天**（1440 分钟）`[官方]` | 支持 | 支持 | **默认 24 小时**（2.9 起）`[官方]` | **有 `minimumReleaseAge`，但非默认强制** |
| 漏洞审计 | `npm audit` / `audit signatures` | `pnpm audit`（GHSA） | 外部 | `bun audit fix` | `deno audit fix` | `ngm audit`（**按 commit**，覆盖有限） |
| SBOM | `npm sbom`（CycloneDX） | **`pnpm sbom`：CDX 1.7 / SPDX 2.3** `[官方]` | 外部 | 无原生 | 外部 | `ngm sbom`：**CDX 1.5 / SPDX 2.3** |
| 溯源签名 | **SLSA L3**：Sigstore + Rekor + trusted publishing 默认 provenance `[官方]` | publish 可附 provenance | 无 | 无 | 无 | **`attest` 未签名**；签名方用 cosign 外部完成 |

**读法**：`allowScripts` / `allowBuilds` / `no-downgrade` / `minimumReleaseAge` / `sbom` 在 2026 年已是
"5 家里 3–5 家都有"的标配。ngm 在这五行里不再有"独有"，只有"更细的三态 + 沙箱执行"这一细分差别。

### 2.2 构建链路：Rust 化完成，adapter 模型反而更中立

- **Vite 8**（2026-03-12 稳定）用单一 Rust bundler **Rolldown** 取代 esbuild+Rollup 双管线，
  官方称构建快 10–30×；Rolldown 1.0 于 2026-05 稳定 `[官方]`。
- **Turbopack** 成为 Next.js 16 的 dev + build 默认；**Rspack 2.2.7** 是 webpack 兼容迁移路径 `[官方/媒体]`。
- **Biome v2** 引入 type-aware lint；**Oxc** 的 Oxlint/Oxfmt 进入生产 `[媒体]`。
- **云整合**：Cloudflare 2026-06 收购 VoidZero（Vite/Vitest/Rolldown/Oxc），Anthropic 收购 Bun 公司 `[媒体]`。

对 ngm 的意义：adapter 模型（把 transform/bundle/typecheck/css 交给外部引擎）在"工具链快速换代"时
是**优势**——Vite 换内核不影响 ngm 的信任链；但它也意味着 ngm 的"构建能力"永远等于所接引擎的能力，
不能自称一体化开发体验（见 §4.10）。

### 2.3 供应链标准：SLSA L2 成为事实门槛，验证侧仍是短板

- SLSA v1.x 四级仍在用；top500 GitHub 项目 L1+ 占比 38%（2025 Q4）→ 46%（2026 Q1），L2+ 20%→26% `[实测]`。
- **provenance 采用率仍低**：top50 npm 包仅 6 个有 provenance（12%）`[实测]`；另一组 771 版本中
  299 个（38.8%）有 attestation `[实测]`；880 个安装包中 293 个有 attestation `[实测]`。
  口径不同但方向一致：**多数包没有 provenance，且 `npm audit signatures` 不区分"有签名"与"有溯源"**。
- SBOM 标准已收敛到 SPDX 3.0 / CycloneDX 1.6–1.7；CRA、EO 14028 推动"要产物不要叙述" `[官方/媒体]`。
- 生态共识：**"生产 attestation 不等于消费侧验证"是 2026 的待解问题** `[媒体]`——ngm 的 `verify`
  正好落在这一格，但 ngm 验的是内容身份，不是签名。

---

## 3. 己方能力基线（回到源码核实后的当前状态）

**主轴（Git 依赖可证明性闭环）** — 已实现并默认执行：

```
ngm.json(refType 必填)
  → 图解析 → ref/commit 解析
  → archiveDigest / moduleDigest+subtreeOid（内容清单 SHA-256）
  → 四层 vendor（mirror / module-cache / content-store / ngm.vendor）
  → verify 四查 + 漂移三态（expected/unexpected/critical）
  → OSV 按 commit → SBOM / in-toto attest → gc
```

**18 个顶层命令**（`root.go` 全表，28 项含别名/入口）：`init adopt add install update remove gc verify
sbom attest audit build transform typecheck typedecl css run modules mappings config engines doctor store
why tree outdated integrations cache`。其中构建类 5 个走 adapter，审计类 4 个直接读 lock。

**明确不做**（`capability-matrix.md` + ADR-044）：registry 包管理、semver 求解、peer deps、
`node_modules` 布局、lifecycle 全族、publish、自研 bundler/HMR/test runner/LSP/dev server/CSS 编译器。

**设计性限制**（README 自述）：`adopt` 只解析 `package-lock.json` v2/v3，不读 pnpm-lock/yarn.lock；
不支持 registry / workspace / gitlink / LFS 内容；`verify` 默认只比结构与大小，字节级要 `--content`；
`gc` 可达性只看当前项目的 lock。

---

## 4. 全维度逐项差距矩阵

**判定图例**：🟢 领先（ngm 有、主流无） · ⚪ 持平 · 🔴 真实落后 · 🟡 部分/有限 ·
🔵 正交（不同轴，不可直接比） · ⬛ 明确不做（ADR 已定）

### 4.1 依赖模型与信任根

| # | 维度 | ngm | 竞品最好水平 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 1 | Git-first 声明 | 唯一对象模型，refType 必填（仅完整 40-hex 窄默认） | 5 家都把 Git 依赖当"例外源"；npm 12 默认拒绝 | 🟢 领先 | 无差距；这是产品定义 |
| 2 | 内容摘要绑定 commit | 规范化清单 SHA-256，可从 mirror 的 commit tree **离线重放** | Bun 1.4 记录 SHA-512 for git/tarball；Cargo 逐文件 checksum（官方声明非安全机制） | 🟢 领先（细粒度） | "记录哈希"≠"可离线重放 + 验证闭环"，见 git-tag-guardian 对照 |
| 3 | 传递依赖模型 | 只读上游 `ngm.json`；package.json 的 Git 依赖仅 warning | 主流解析整条 registry 传递链 | 🟡 有限 | 不是缺陷：上游多为 npm 项目时信息天然缺失；成本=换生态，不在 ADR-044 准入 |
| 4 | 一仓多模块 | `repo + subPath`、模块清单、`modules list/validate/index` | Cargo workspace：单一 lock + workspace.dependencies 继承 | ⚪ 持平（不同形） | Cargo 是同源内建，ngm 跨仓子路径；各自合理 |
| 5 | workspace / monorepo | **不做共享 lock 的 workspace** | pnpm workspace 是 best-in-class | ⬛ 明确不做 | ADR-044；唯一理由"更像 npm"→ 不入路线图 |
| 6 | registry 包 / semver / peer deps | 不解析 | 5 家全部内建 | ⬛ 明确不做 | 六座大山，ADR-044 边界；补齐=摧毁定位，60–120 人日且永远追不上 `[推断]` |

### 4.2 锁定、可复现与漂移

| # | 维度 | ngm | 竞品最好水平 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 7 | lock 确定性 | v2 支持模块 digest；字节级可复现判据 | 各 lock 均确定性；pnpm 用 SQLite 索引加速 | ⚪ 持平 | 无实质差距 |
| 8 | verify 四查 | ref / binding / digest 重放 / landing，并发 8 | 主流 lock 一致性检查更浅 | 🟢 领先 | 无差距 |
| 9 | 漂移三态 | expected / unexpected / critical，critical 不被 `--allow-drift` 降级 | `git-tag-guardian` 做 tag rewrite 检测 + SHA 基线；Yarn/pnpm/Deno 只查 lock 一致 | 🟢 领先（细分） | 三态 + critical 语义是 ngm 独有细分，但"tag 重写"单项已有外挂工具覆盖 |
| 10 | 离线重放 | digest 从本地 mirror 的 commit tree 重算，不依赖 archive API | 主流依赖 registry tarball / 缓存 | 🟢 领先 | 无差距；这是离线 CI 的真实优势 |
| 11 | 签名溯源 | `attest` 产**未签名** in-toto Statement v1（SLSA provenance 谓词）；签名交 cosign | npm L3：Sigstore 签名 + Rekor + trusted publishing 默认带 provenance | 🔴 真实落后 | **在 SLSA 轴上低一档**。ADR-014 已决定不自签；补法=给 manifest 加谓词并让 CI 现场签（v0.70 已做一半），非自造签名体系 |

### 4.3 供应链安全策略

| # | 维度 | ngm | 竞品最好水平 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 12 | 脚本默认拒绝 | `postInstallPolicy=deny`（默认）+ Deno 沙箱（无网/无 run/写入不授予） | npm 12 `allowScripts=off`、pnpm `allowBuilds` 全禁、Deno 默认 off | ⚪ 持平 + 🟢 细分 | "默认拒绝"已不独有；**"开了也只在沙箱跑"仍领先**（pnpm/npm 放行后是裸执行） |
| 13 | 版本冷却 | 支持 `minimumReleaseAge`（ISODuration，已修溢出） | pnpm **默认 1 天**、Deno **默认 24h** | 🔴 落后（默认强度） | ngm 有开关但非默认。改成默认会破坏现有 CI；按 ADR-044 需独立理由，暂不动 `[推断]` |
| 14 | 来源门禁 | `CheckNetAccess` + git 单一构造点 + OSV 端点 http(s) 校验 | pnpm `blockExoticSubdeps` 默认拦 git/tarball 传递；npm `allow-git/allow-remote=none` | ⚪ 持平（机制不同） | ngm 靠 net: 权限 + 单一构造点；pnpm 靠配置默认。效果相当 |
| 15 | 白名单 / 策略 | JSON-first 供应链策略 + `run:script` / `run:git` 权限闸门 | pnpm `trustPolicy: no-downgrade`、Yarn `approvedGitRepositories` | ⚪ 持平 | ngm 的策略绑 commit，竞品绑 registry 版本——正交复杂度，见 §4.6 |
| 16 | 漏洞审计 | OSV **按 commit** 查询；报告明说"查不到≠安全" | npm/pnpm/Deno audit 按版本，覆盖率更高 | 🟡 领先（轴）/ 落后（覆盖） | commit 查询是独有，但 OSV 对 Git commit 覆盖有限；补法=同时按 commit + 版本双查（需 registry 输入，不在轴上） |
| 17 | SBOM | CDX **1.5** / SPDX 2.3，确定性字节 | pnpm CDX **1.7** / SPDX 2.3，校验官方 schema | 🔴 真实落后（版本） | **可低成本补齐**：升 CDX 1.7 生成器 + 加 schema 校验判据；不触碰信任链 |
| 18 | 回收 / 磁盘 | 四层 vendor（v0.8 后放大比 20×→0.6×）；`gc --apply` 按当前 lock；层2 无可达性回收 | pnpm `store prune` 完整 | 🟡 部分 | "去重斜率"已解决；"终点无人回收"仍在；可达性删除需项目注册表，ADR-023 部分解决，不强行补 |

### 4.4 审计产物与标准位置

| # | 维度 | ngm | 竞品/标准 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 19 | 审计产物从 lock 导出 | `sbom` / `attest` 只读 lock，确定性字节 | npm/pnpm 原生 sbom；CycloneDX 1.7 校验 schema | ⚪ 持平 + 🔴 版本 | 见 #17 |
| 20 | SLSA 等级 | 内容侧 L2 的一半（有内容↔commit 绑定、无签名） | npm L3（签名 + 透明日志 + 默认 provenance） | 🔴 真实落后 | 正交与落后并存：npm 的 provenance 不回答 ngm 的"tag 重打/内容漂移"问题，但"谁发布的"ngm 无对应物 |
| 21 | 内容身份可独立复核 | 主轴，四查 + `--content` 逐文件 | Cargo checksum 声明非安全机制；外挂工具只做单点 | 🟢 领先 | **这是 ngm 唯一不可替代的能力**，应作为全部对外叙事的中心 |

### 4.5 运行时与脚本

| # | 维度 | ngm | 竞品 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 22 | 脚本执行 | `ngm run` 读 package.json.scripts；`run:script` 默认拒绝；退出码原样透传 | npm scripts（v12 后脚本仍可显式允许）；Deno tasks | 🟡 有限 | ADR-044：读 scripts 是成本考量，不模拟 npm_config_* / npm_execpath |
| 23 | 运行时 | 引擎经 adapter；支持 Node/Deno，WASM 沙箱（32 MiB、只读挂载、无 socket） | Bun/Deno 自带 runtime + test + docs + LSP | 🔵 正交 | ngm 不做 runtime；不算落后，见 ADR-007 |

### 4.6 构建链路（adapter 边界）

| # | 维度 | ngm | 竞品 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 24 | transform / bundle / typecheck / .d.ts / css | 5 命令走 adapter，外部引擎 | Vite 8 / Rolldown / Turbopack / Rspack 原生一体化 | 🔵 正交 + 🟡 有限 | ngm 的能力上限=所接引擎；不自研是 ADR 决定 |
| 25 | HMR / dev server / test / LSP / docs | **明确不做** | Vite/Vitest/tsserver/TypeDoc 成熟 | ⬛ 明确不做 | capability-matrix；补齐无差异化收益 |
| 26 | 工具链换代韧性 | adapter 解耦，Vite 换内核不影响信任链 | 一体化工具换代要改自己 | 🟢 领先（架构） | 2026 的 Rust 化浪潮反而验证了 adapter 模型的中立性 |

### 4.7 工程纪律、CLI 与采用度

| # | 维度 | ngm | 竞品 | 判定 | 差距量化 / 补齐成本 |
|---|---|---|---|---|---|
| 27 | 测试与判据 | 998 测试函数、3 Fuzz；退出码/JSON 信封/help golden/命令骨架/覆盖率门槛/claims 账本全有机械判据 | 主流工具测试量大，但少有"文档判据" | 🟢 领先 | 这是项目最扎实资产 |
| 28 | CI 纪律 | 15 job，三平台 + race + fuzz + docs-links + discipline 13/13 | 主流 CI 成熟 | ⚪ 持平 | 无差距 |
| 29 | 错误提示与 JSON 契约 | 统一 hint + `--json` 信封（2026-11-20 补齐早期失败路径） | 主流 CLI 质量参差 | 🟢 领先 | 无差距 |
| 30 | 文档规模 | 339 md，远超代码量 | 主流工具文档由大团队维护 | 🟡 双刃 | 可发现性好，但维护成本与漂移风险高（本次过期事实即一例） |
| 31 | 发布覆盖 | 101 正式 tag：GitHub 63 / Gitee 3，缺口 38 | 主流工具 tag ↔ release 一一对应 | 🔴 真实落后 | 凭据阻塞（无 GITHUB_TOKEN/GITEE_TOKEN），非工程能力问题 |
| 32 | 采用度 / 社区 | 无公开下载量；无外部安全审计 / 渗透测试 | npm 6500 万下载/周，Vite 6500 万/周；大量第三方审计 | 🔴 真实落后 | 这是产品阶段差距，不是代码差距；靠内容身份叙事 + demo 逐步建立 |
| 33 | 安装分发 | 单二进制 Go CLI，仅需 Git；源码构建是可靠来源 | 主流为 npm 包 / 单二进制 | ⚪ 持平 | ngm 的"无运行时依赖"是优势 |

---

## 5. 关键发现：过期事实与"独有"断言的失效

### 5.1 必须纠偏：`works as expected` 已是历史状态

`COMPETITIVE-ANALYSIS.md` §PackageGate（第 101–140 行）与 `README.md`（第 249–255 行）写：

> "npm 以 `works as expected` 关闭——用户有责任审查自己安装的包"

**这是 2026-01 的准确历史记录**，但当前状态已经变了：

- 2026-02-18 npm 宣布 `--allow-git` 默认 none（npm 11.10+ 可用）`[官方（nodejs/Release #1161）]`；
- 2026-07-08 npm v12 GA，三个默认全部生效，官方 changelog 原文：
  "`--allow-git` defaults to none: ... This closes a code-execution path where a git dependency's
  `.npmrc` could override the git executable, which ran even with `--ignore-scripts`." `[官方]`；
- 同一来源还说明这是 RFC 54，使 npm "与 pnpm、Yarn、Bun、Deno 对齐——它们都已默认阻止安装脚本"。

**结论要改写为**："npm 在 2026-01 以 works as expected 关闭；2026-07 的 v12 用默认拒绝 Git 依赖
的方式关闭了同一条路径。ngm 的免疫来自**设计时就不解释 `.npmrc`、git 单构造点、脚本默认 deny**，
是对手用配置默认补齐的同类保护在 ngm 这边的**构造性等价物**，而不是'对手拒绝修、只有 ngm 修了'。"

历史事实不必删除（它解释了产品假设的由来），但**必须加时间限定与当前状态**，否则读者会得到错误图景。
`scripts/packagegate-demo.sh` / `.ps1` 里的背景注释同样需要同步。

### 5.2 三项"无对应物"断言的当前有效性

`COMPETITIVE-ANALYSIS.md` 第 88–99 行的控制点矩阵里，ngm 标 ✓ 而竞品全 ✗ 的三项是：

| 断言 | 2026-11 是否仍成立 | 证据 |
|---|---|---|
| "refType 声明（必填）" 独有 | **基本仍成立** `[推断]` | 5 家没有等价字段；但 npm 用"默认拒绝 Git 依赖"达到类似效果，语义不同 |
| "内容摘要（规范化清单）" 独有 | **不再成立** | Bun 1.4 为 git/tarball 依赖记录 SHA-512 `[媒体]`；Cargo 逐文件 checksum `[官方]`；差别在"可重放 + 验证闭环"，不在"有哈希" |
| "ref 漂移三态（预期/非预期）" 独有 | **部分成立** | `git-tag-guardian` 做 tag rewrite检测 + SHA 基线 `[官方（包文档）]`；pnpm/Deno 的 no-downgrade 做信任降级；ngm 的"三态 + critical 不可降级 + 内容级重放"仍是更细的模型 |

**正确的对外措辞**：不声称"独有控制点"，而声称"**唯一把这些控制点收束为默认、可离线重放的同一状态机**"。
这与 2026-09 版"未形成默认闭环"的自我约束一致，且与 §5.1 的修正方向相同。

### 5.3 SLSA 段落需要一处小修

`COMPETITIVE-ANALYSIS.md` §6 说 npm 达 L3、PyPI 采用率约 17%——方向仍对；但应补一句 2026-10/11
的实测：provenance **实际覆盖率仍低**（top50 仅 12%；另一口径 38.8%），且 `npm audit signatures`
不区分"有签名"与"有 provenance"。这反而**加强**了原报告"provenance 采用率远未普及"的论断，
属于补充证据而非推翻。

### 5.4 第二处读数漂移：ADR 数与测试数

| 位置 | 写的 | 实际读数 | 处置 |
|---|---|---|---|
| 根 `README.md:592` | "45 个决策记录" | **49** 个 `adr-*.md`（索引已列到 ADR-049，`TestV71` 判据绿） | 改为 49 |
| 旧报告 `ngm-architecture-and-npm-gap-2026-11-18.md:33` | 932 Test + 1 TestMain / 3 Fuzz | 现 **991 Test（严格签名）/ 992 含 TestMain / 3 Fuzz + 3 Benchmark** | 旧报告是**历史快照**，不改；本报告 §1.2 记当前读数 |

> 这两处都印证了"数字写死在文档里就会漂移"这一项目反复遇到的失效模式；`TestV71` 那种
> "索引必须覆盖每个 ADR 文件"的机械判据才是防漂移的正确形状。

---

## 6. 建议（按 ADR-044 准入规则筛选）

### 6.1 值得做（理由不依赖"更像 npm"）

| 优先 | 项 | 理由（非迁移类） | 成本 |
|---|---|---|---|
| P0 | **纠偏过期事实**（§5.1–5.3）：`COMPETITIVE-ANALYSIS.md` + `README.md` + demo 脚本注释 + §6 补证据 | 读数说谎是当前最大的认知风险；不改代码 | 低（文档） |
| P1 | **`ngm sbom` 升 CycloneDX 1.7 + 官方 schema 校验判据** | 合规互操作性（CRA/EO 14028 要 schema 合法产物），与信任轴无关但可独立成立 | 低–中 |
| P1 | **`attest` 谓词升级**：在现有未签名 in-toto Statement 上补齐 SLSA v1 谓词字段与 SPDX 3.0 兼容提示 | 让 CI 现场 cosign 签名时不必手工补字段；不引入自签名（ADR-014 不变） | 中 |
| P2 | **发布覆盖回填**（38 个 tag），需先解决 GITHUB_TOKEN/GITEE_TOKEN 凭据 | 分发可信度，非工程能力 | 中（凭据阻塞） |

### 6.2 明确不做（唯一理由是"更像 npm"）

registry 协议 / semver 求解 / peer deps / `node_modules` 布局 / lifecycle 全族 / publish /
自研 bundler / HMR / test runner / LSP / dev server / **默认强制版本冷却**（#13，改造 CI 语义但无独立理由）。

### 6.3 观察，不承诺

- **外部安全审计 / 渗透测试**：能把"免疫 PackageGate"从自证变成第三方背书，但需预算与排期；
  当前以 `scripts/packagegate-demo.sh` 的自证断言作为过渡证据。
- **默认强制 `minimumReleaseAge`**：等出现"因新版本太快进入而被攻击"的真实案例再重开（ADR 式触发条件）。

---

## 7. 数据限制说明

- 竞品版本与采用率以检索日（2026-11-20/21）公开信息为准；`[媒体]` 级数字仅供方向参考，
  不用于对外宣传。
- 性能数字（Vite 10–30×、Bun 12ms、pnpm vs npm）来自各官方 benchmark 或第三方测量，环境不同不可直接横比；
  ngm 的对应读数在 `metrics.md`（cold install 1.88–1.94s、warm 12ms、verify 100 依赖约 2s），**口径不同，未做同机对比**。
- 本报告是架构与差距决策输入，**不是对外宣传材料**；对外措辞须经 §5.1–5.2 的修正后再引用。

---

## 8. 相关文档

- [竞品对比分析（SSOT，待按 §5 修订）](../COMPETITIVE-ANALYSIS.md)
- [能力矩阵（SSOT）](../internals/capability-matrix.md)
- [2026-11-20 全量代码评估](./ngm-full-assessment-2026-11-20.md)
- [架构与 npm 替代缺口（2026-11-18，历史快照）](./ngm-architecture-and-npm-gap-2026-11-18.md)
- [ADR-044：独立生态定位](../adr/adr-044-independent-ecosystem.md)
- [ADR-014：自证签名不做](../adr/adr-014-self-report-signatures.md)
- [ADR-027：attestation（未签名 in-toto）](../adr/adr-027-attestation.md)
