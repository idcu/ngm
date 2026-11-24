# ngm 与主流包管理器的全维度对比（2026-11-24）

> **基线**：ngm HEAD `39e60c6`，`0.103.0-dev.3` 发布准备完成；工作树干净。
> **方法**：己方回到当前源码 / ADR / guides 核实，不沿用 2026-11-21 及更早评估的结论；
> 竞品以官方文档 / release notes 为主，第三方综述只作旁证。
> **事实标记**：`[源码]` = 当前仓库可核验；`[官方]` = 官方站点 / release notes；
> `[第三方]` = 可交叉检索但不是一手；`[推断]` = 本报告判断，非事实陈述。
> **坐标**：按 [ADR-044](../adr/adr-044-independent-ecosystem.md)，ngm 是独立生态，
> 正确问题不是"npm 的哪一格还空着"，而是"**依赖的身份与内容能否被独立复核，且执行边界是否可控**"。
> 因此"刻意不做"与"落后"严格分开；把正交项算成落后是坐标错误。

---

## 1. 一页结论

1. **ngm 的强项不在"装得快"，而在"证明得住"**：commit / dirDigest / 内容摘要三层锚点、
   可离线重放、`verify --content`、确定性 SBOM/attest，整体比 npm/pnpm/Yarn/Bun 的
   "版本 + integrity 哈希"更细；接近 Cargo/Go/Nix 的证明思路，但多了本地来源这条独立链。`[源码]`
2. **执行边界是 ngm 最被低估的优势**：依赖 `postinstall` 默认禁止，脚本只在 Deno 沙箱里跑，
   且 `run:` 从不授予；npm 12（2026-07）才把依赖脚本默认关掉，pnpm 11 / Yarn 4.14 / Bun 1.3
   也只是在同一时期跟进。ngm 的沙箱比"默认不跑脚本"更严一层。`[源码][官方]`
3. **真正的短板是生态面，不是证明面**：无 registry / semver 求解 / publish / peer deps /
   workspace / 迁移命令 / 多版本共存。其中前四类是 [ADR-044/052] 的决策边界，
   不应算缺陷；但**迁移命令缺失、冷却期条件默认、无签名闭环、adopt 只吃 npm lock**
   是实打实的工程债。`[源码][推断]`
4. **2026 年竞品最强的养分是"默认安全 + 迁移体验"**：pnpm 11 把冷却期默认 24h、
   `blockExoticSubdeps` 与 `strictDepBuilds` 默认打开；Deno 2.9 默认 24h 冷却期并提供
   `no-downgrade` 信任策略、还能从 npm/pnpm/yarn/Bun 锁文件迁移；npm 12 默认拒绝依赖脚本
   与 git/URL 依赖。ngm 已有多数能力，但**默认值和迁移入口仍可借鉴**。`[官方]`
5. **优先建议（详见 §6）**：P0 = 冷却期默认化、adopt 扩展、签名配方产品化；
   P1 = lock 迁移命令、catalogs/constraints 的轻量版、依赖图审计 API；
   P2 = universal lock、发布年龄元数据、性能剖析与结构化日志。
   **不建议学** registry/semver/workspace 全量实现——那会改写 ngm 的定位。`[推断]`

---

## 2. 全维度对照

> 表中 JS 组 = npm / pnpm / Yarn Berry / Bun / Deno；系统组 = Cargo / Go modules / uv；
> hermetic 组 = Nix / Bazel。"—"表示不适用或未核到可靠一手证据。

| 维度 | ngm 当前状态 | JS 组（2026-11） | 系统组 | hermetic 组 | 判读 |
|---|---|---|---|---|---|
| 依赖标识 | `host:org/repo@ref` + 五类 refType；`local:` 独立命名空间；无裸名/semver `[源码]` | registry 包名 + semver range；Git/URL 依赖在 npm 12 默认被拒 `[官方]` | Cargo/Go 用版本 + registry；Go 无锁文件而靠 MVS `[官方]` | Nix 用 flake input rev；Bazel 用 module + BCR `[官方]` | ngm 与竞品**不同物种**：身份来自仓库/目录，而非包索引 |
| 解析算法 | 无版本求解：ref 必须显式；tag/branch 解析后钉 commit `[源码]` | npm/pnpm/Yarn/Bun 走 semver；Deno 兼容 npm + JSR `[官方]` | Cargo 回溯；Go/Bazel MVS；uv PubGrub `[官方][第三方]` | Nix 由 nixpkgs 提交与求值决定 `[官方]` | ngm 放弃"求解能力"换"声明即锚点"；代价是无自动升级/冲突解决 |
| 锁文件 | `lockfileVersion 3.0.0`，确定性字节；`resolvedAt` 唯一非确定字段；无迁移命令 `[源码]` | 各家都有 lock，均有 frozen/immutable 严格安装 `[官方]` | Cargo.lock / go.sum / universal uv.lock `[官方]` | flake.lock（rev+narHash）/ MODULE.bazel.lock `[官方]` | ngm 锁的"不可变性"对齐主流；**迁移**落后 |
| 内容证明 | archiveDigest / moduleDigest+subtreeOid / dirDigest；content store 可离线重放 `[源码]` | integrity sha512 为主；pnpm 内容寻址 store，但 lock 锁的是图非字节 `[官方]` | Cargo checksum；Go sumdb；uv wheel/sdist SHA-256 `[官方]` | Nix narHash；Bazel sha256 `[官方]` | ngm 处于上游；尤其 subPath 绑定位置+字节是稀缺能力 |
| 验证默认级别 | CLI 默认 structure，字节级需 `--content`；`verify --deep` 已是废弃别名 `[源码]` | 各家默认安装时校验 integrity，但安装后不重放内容 `[官方]` | cargo build 校验 checksum；Go 构建校验 go.sum `[官方]` | Nix 构建沙箱 + 内容哈希 `[官方]` | ngm 的"安装后再验"是独立维度；但**默认值偏弱** |
| 供应链冷却期 | `supplyChain.minimumReleaseAge` 支持 ISO-8601；**写了该段才默认 24h**，未写则不门禁 `[源码]` | npm `min-release-age`（天，11.10+）；pnpm 11 默认 1440 分钟；Yarn 4.10+；Bun 1.3（秒）；Deno 2.9 默认 24h `[官方]` | uv `exclude-newer`；pip `--uploaded-prior-to` `[第三方]` | 不适用（源由 lock/rev 固定） | 功能有，**默认姿态**落后于 pnpm/Deno |
| 信任降级防护 | 无 `no-downgrade` 类策略；不做 provenance 信任分级 `[源码]` | pnpm `trustPolicy: no-downgrade`；Deno 2.9 `trust-policy=no-downgrade` `[官方]` | Cargo Sigstore trusted publishing 推进中；Go sumdb 2026 有绕过漏洞并修复 `[第三方]` | Bazel BCR provenance 仍"即将推出" `[官方]` | 可借鉴，且与 ngm 的"锚点不降级"哲学一致 |
| 安装/运行脚本 | `postinstall` 默认禁止；脚本仅在 Deno 沙箱，`run:` 从不授予，30s 超时 `[源码]` | npm 12 默认 `allowScripts` off + git/remote 默认拒绝；pnpm 11 `allowBuilds`/`strictDepBuilds`；Yarn 4.14+ 默认禁；Bun 默认不跑 `[官方]` | Cargo `build.rs` 是信任边界；uv `lock`/`uvx` 会执行代码 `[第三方]` | Nix 沙箱禁网；Bazel Starlark 沙箱求值 `[官方]` | ngm 仍是最严一档，但**审批 UX**可学 |
| SBOM / 证明导出 | CycloneDX 1.7 + SPDX 2.3（均从 lock 导出）；in-toto v1 / SLSA v1；确定性字节 `[源码]` | 多为插件/第三方（npm sbom、pnpm sbom 较新）；Deno/Cargo 靠工具链外挂 `[官方][第三方]` | cargo-cyclonedx / cargo-sbom 等外挂 `[第三方]` | Nix 天然闭包级 SBOM；Bazel 靠 action graph `[第三方]` | ngm 明显领先；但**未内置签名** |
| workspace / monorepo | `modules` 集合 + subPath + 索引，**明确不做 workspace** `[源码]` | pnpm/Yarn/npm/Bun/Deno workspace 成熟；Cargo/uv 共享单锁；Bazel 模块化 `[官方]` | Cargo/uv workspace 共享 lock `[官方]` | Nix flake inputs / Bazel module `[官方]` | 这是**定位差**；但 module collections 可继续补强 |
| 覆盖/补丁机制 | 无 overrides/patch；同依赖多版本共存刻意不做 `[源码]` | pnpm overrides/patch、Yarn `patch:`、Bun catalog `[官方]` | Cargo `[patch]` 最干净；Go `replace` `[官方]` | Bazel `patch/replace` `[官方]` | 真实能力缺口，需在不破坏锚点的前提下设计 |
| 迁移入口 | `adopt` 仅 npm `package-lock.json` v2/v3 `[源码]` | Deno 2.9 可从 npm/pnpm/yarn/Bun lock 直接生成 `deno.lock` `[官方]` | uv 提供 `uv pip` 兼容接口 `[官方]` | — | ngm 明显落后；这是最高性价比的可学项 |
| 引擎与扩展 | embed/subprocess/wasm 四类 adapter；deno 刻意不入内置；无 remote adapter `[源码]` | 各 JS PM 直接跑 npm scripts；Yarn 插件体系；Bun 自带 test/build `[官方]` | Cargo/Go 由编译器生态承接 `[官方]` | Bazel 规则生态广 `[官方]` | ngm 的 adapter 纪律强，但生态宽度小 |
| 性能 | 并行 verify + 四层缓存（mirror/module-cache/content store/vendor）；无墙钟性能目标 `[源码]` | Bun 安装速度领先；pnpm SQLite store；Deno 2.9 迁移/缓存体验好 `[官方]` | uv 解析快；Go 构建缓存 `[第三方]` | Nix/Bazel 远程缓存命中是核心卖点 `[第三方]` | ngm 有正确结构，缺可对外承诺的性能读数 |
| DX / 文档 | 28 命令、`--json`、退出码契约、golden help；首次触网/引擎因 default-deny 以 exit 3 起步 `[源码]` | npm 零门槛；Deno 2.9 迁移体验最佳；pnpm catalog 降 monorepo 心智 `[官方]` | Go 极低配置；uv 一个二进制替代多工具 `[官方]` | Nix/Bazel 学习成本高 `[官方][第三方]` | ngm 的机器可读面强，**首次使用门槛**高 |
| 发布信任 | 标签发布 + 三平台 Release；attest 不签名，签名交由外部 cosign `[源码]` | npm/PyPI trusted publishing（OIDC）成标准；provenance 不等于安全 `[官方]` | Cargo Sigstore 推进；Go sumdb `[官方][第三方]` | Nix 缓存签名；BCR provenance 未完成 `[官方]` | 可借鉴流程，不应把 provenance 当"内容更可信" |

---

## 3. ngm 的优点

1. **身份链比主流更细**：Git 来源以 commit + archiveDigest/moduleDigest/subtreeOid 绑定位置与字节；
   local-dir 以 `dirDigest`（`ngm-local-digest/v1`）绑定目录内容，并排除 `.git/node_modules/ngm.vendor`
   等非证据项。`[源码: internal/core/localfs/manifest.go, internal/core/lock/schema.go]`
2. **可离线重放**：mirror → module cache → content store → vendor 四层；verify 可从内容摘要重算，
   不依赖上游仍在线。这是 npm/pnpm 的 integrity 校验（只保证下载字节没变）没有覆盖的一层。`[源码]`
3. **默认不执行他人代码**：`postinstall` 默认 deny；唯一执行面是 Deno 沙箱，且 `run:` 永不授予。
   npm 12 才在 2026-07 把依赖脚本默认关掉；pnpm/Yarn/Bun 是同期跟进。`[源码][官方]`
4. **导出产物确定性**：CycloneDX 1.7 `tools.components` + `$schema`、SPDX 2.3、in-toto/SLSA v1，
   无时间戳/随机 ID；同 lock 同字节。主流多为构建后插件生成，确定性不一。`[源码]`
5. **subPath 与模块集合**：`name#subPath` 进 lock key，`moduleDigest+subtreeOid` 同时绑定"取哪一个子树"
   和"子树字节是什么"；`modules validate/index/changed/plan` 把集合一致性做成门禁。`[源码]
6. **机器可阅读面完整**：28 顶层命令，`--json` 错误信封，退出码 0–6 独立契约文件与 errs 对账，
   文档与 golden help 判据齐备。`[源码]`

## 4. ngm 的缺点（真正的工程债，不是定位差）

1. **迁移能力弱**：`lockfileVersion 2.x → 3.0.0` 与 schema v1/v2 → v3 都没有迁移命令，只能手改+重装；
   `adopt` 只解析 npm lock，pnpm-lock/yarn.lock 完全没接。Deno 2.9 已能从四家 lock 直接迁移。`[源码][官方]`
2. **冷却期是"条件默认"**：`supplyChain` 段存在时默认 24h，段不存在则无门禁；
   pnpm 11 与 Deno 2.9 已经是全局默认 24h。`[源码: internal/supplychain/policy.go:48-58][官方]`
3. **无信任等级/降级防护**：竞品的 `no-downgrade` 比较同包历史最高信任级别，
   ngm 没有对应策略；在"锚点固定但发布凭证被劫"的攻击面里少一道闸。`[源码][官方]`
4. **无签名闭环**：attest 只产出可签名语句，签名必须外部 cosign；没有 trusted publishing/OIDC 流程。
   需明确：provenance 只证明"谁在什么流程下发布"，2026 年 TanStack 事件证明它也可能为真却有害。`[源码][官方][第三方]`
5. **验证默认值偏弱**：CLI 默认 structure（只比结构/大小），字节级需要显式 `--content`；
   竞品在安装期就做完整性校验。ngm 的"安装后再验"更强，但默认值没把它用满。`[源码]`
6. **无覆盖/补丁/多版本共存**：遇到传递依赖冲突没有 `overrides`/`patch` 这类逃生口；
   这既是设计选择，也是现实项目迁移时的硬摩擦。`[源码]
7. **DX 首触门槛**：default-deny 让首次 install / 首次引擎调用以 exit 3 教育用户；
   预编译 release 覆盖远少于 tag，Windows 上还要 Git Bash。`[源码]`
8. **性能缺乏对外读数**：并行 verify 与缓存结构是对的，但没有像 Bun/pnpm/Nix 那样可复述的
   安装/校验基准；ADR-022 也明确门禁是 git spawn 预算而非墙钟目标。`[源码]`

## 5. 明确未做 / 刻意不做（不要误判为落后）

| 未做项 | 决策来源 | 为什么不是缺陷 |
|---|---|---|
| registry / semver 求解 / publish / peer deps | [ADR-044](../adr/adr-044-independent-ecosystem.md)、[ADR-052](../adr/adr-052-local-sources.md) | ngm 管的是"来源可证明"，不是"从索引取-range 内最新版本"；补上等于换产品 |
| 不接管 `node_modules` / `.bin` shim | ADR-043、README | 避免把 Node 布局当成自己的契约 |
| workspace / 共享 lock / hoisting | [ADR-031](../adr/adr-031-module-collections.md) | module collections 是"集合+索引+subPath"，不是 workspace |
| 同依赖多版本共存、模块级签名、跨仓搜索 | ADR-031、module-collections 架构文档 | 与"一份 lock 一份图"的读法冲突 |
| remote adapter、内置默认引擎 | ADR-013/020/025 | 远程执行与隐式默认会破坏显式授权与确定性 |
| lock/schema 迁移命令 | ADR-052 | 当前选择是"拒绝旧格式并手工升级"，代价已记录在报告里 |
| 沙箱内 `run:`、内存/CPU 上限 | security-model.md | 允许派生进程会一次绕开文件系统与网络边界；资源上限仍未做，需单独立项 |

## 6. 可从竞品吸取的养分（按优先级）

> 原则：**学机制与默认姿态，不学生态坐标**。每条都给出 ngm 的可落点，避免"抄一个 workspace 出来"。

### P0 · 值得立刻做

| 养分 | 竞品证据 | ngm 落点 | 不学什么 |
|---|---|---|---|
| 冷却期全局默认 | pnpm 11 `minimumReleaseAge: 1440` 默认；Deno 2.9 默认 24h `[官方]` | 让未配置 `supplyChain` 也有安全默认，或至少在 `init`/doctor 显式提示；保留 `--allow-fresh` 作为逃生口 | 不照搬四家三种单位；ngm 继续用 ISO-8601 |
| 信任降级防护 | pnpm `trustPolicy: no-downgrade`；Deno 2.9 `trust-policy=no-downgrade` `[官方]` | 在 `supplyChain` 增加"锚点/证明等级不得弱于历史"的策略；与 dirDigest/archiveDigest 等级联动 | 不把 provenance 当作内容可信证明 |
| 多锁文件迁移 | Deno 2.9 从 npm/pnpm/yarn/Bun lock 生成 `deno.lock` `[官方]` | 把 `adopt` 扩展到 `pnpm-lock.yaml` / `yarn.lock` / `bun.lock` / `deno.lock`；沿用现有"不猜 provenance"原则 | 不把无法解析的条目编造成 Git 来源 |
| 脚本审批 UX | npm 12 `npm approve-scripts --allow-scripts-pending`；pnpm `allowBuilds` `[官方]` | ngm 已是默认 deny，但缺"预演哪些脚本会被拒"的命令；可在 `doctor`/`run --dry-run` 给出可粘贴白名单 | 不默认放行任何脚本 |

### P1 · 值得排期

| 养分 | 竞品证据 | ngm 落点 |
|---|---|---|
| lock/schema 迁移命令 | 主流都有自动或半自动升级路径；ngm 没有 `[源码]` | `ngm migrate --from=2.0.0` 只搬可安全映射的字段，digest 无法迁移的条目强制重新安装并给差异报告 |
| catalog / constraints 的轻量版 | pnpm catalogs、Yarn constraints、Cargo `workspace.dependencies` `[官方]` | 在 `ngm.collection.json` 上做"集合内共享 ref 必须一致"的策略校验，已有 `modules validate` 可扩展 |
| 安全覆盖/补丁机制 | Cargo `[patch]`；pnpm overrides/patch；Yarn `patch:` `[官方]` | 允许显式替换来源锚点，但必须写入 lock 的新锚点与差异理由，禁止隐式 range 覆盖 |
| 依赖图审计 API | Cargo `cargo tree -e features`；pnpm `why`；Bazel `bazel mod` `[官方]` | ngm 已有 `tree/why`，可增加"按 digest 反查哪些 lock 条目/模块集合引用了它"的图查询 |
| 默认验证级别 | 竞品安装期即校验完整性 `[官方]` | 评估把开发者本地 `install` 后的轻量 content 校验做成默认，CI 继续保持 `--content`；注意性能预算 |

### P2 · 观察即可

| 养分 | 竞品证据 | ngm 的判断 |
|---|---|---|
| universal lock | uv 一份 lock 覆盖所有 OS/架构/Python 区间 `[官方]` | ngm 的 lock 本就与平台无关；可把"跨平台一致性"写成显式判据与文档，而不是新增字段 |
| 结构化日志 / 性能读数 | Cargo `cargo check --timings`/structured logging 是推进方向；Bun/pnpm 有可复述速度读数 `[第三方]` | 给 verify/install 增加可选 timing JSON，先有读数再谈优化 |
| trusted publishing / OIDC | npm/PyPI 已成标准，但 provenance 被真签名利用攻击绕过 `[官方][第三方]` | 可为 ngm 发布流水线加 OIDC，但签名配方必须明确"只证明发布流程，不证明内容无害" |
| 零安装 / 预填缓存 | Yarn zero-installs、Bazel `vendor`/repository_cache、Nix 缓存 `[官方]` | ngm 的 content store + vendor 已有雏形；可产品化"离线包"导出与校验 |

### 明确不建议学

- registry + semver 求解 + publish：会直接违背 ADR-044。
- workspace / hoisting：module collections 与 workspace 是两种模型，混做会出现第二份真相。
- 把 provenance 当作"更可信"：2026 年的真实事件已经证明它只能证明流程，不能证明意图。`[第三方]`
- 把 Cargo PubGrub 当成已稳定默认：截至本次核对，PubGrub 更像已接受的推进方向 / 跟踪项，
  不是可写进对比表的"现状"。`[第三方]`

---

## 7. 证据与未验证项

- **一手核对**：ngm 当前源码 / ADR / guides（本报告的 `[源码]` 结论均有 §2–§4 路径或前文 Explore 证据）；
  pnpm 11 release notes 与 settings、Deno 2.9 release notes、npm 12 GitHub changelog、
  npm v12 文档、Bazel 8.3 lockfile 文档、Bazel 外部依赖概览。`[官方]`
- **交叉检索但非一手**：npm/pnpm/Yarn/Bun/Deno 的 cooldown 单位表、Cargo/uv/Nix/Bazel 的差异综述、
  Nix 可复现性研究读数、Go sumdb 2026 CVE 编号。均按 `[第三方]` 标注，不用于单点结论。
- **本次主动纠偏**：二手资料称"Cargo 已迁 PubGrub"，但官方/社区一手材料显示它仍是
  Cargo vision 与 tracking issue；报告中未采纳"已稳定"说法。
- **未验证**：未逐一下载安装竞品做基准实测；未复核部分第三方性能百分比；
  未验证 GitHub 镜像侧 dev.3 Release 的产物字节（用户确认已生成）。`[推断]`

---

## 8. 相关文档

- 上一轮竞品评估：[ngm-competitive-gap-full-2026-11-21.md](./ngm-competitive-gap-full-2026-11-21.md)
- 早期定位分析：[ngm-architecture-and-npm-gap-2026-11-18.md](./ngm-architecture-and-npm-gap-2026-11-18.md)
- 本轮代码审计：[ngm-full-assessment-2026-11-23.md](./ngm-full-assessment-2026-11-23.md)
- 定位边界：[ADR-044](../adr/adr-044-independent-ecosystem.md)、[ADR-052](../adr/adr-052-local-sources.md)
