# ngm 全量逐行代码审计与竞品全维度评估（2026-11-23）

> 基线：HEAD `ae310a8`（Gitee `main`，与 `origin/main` 同步），工作树干净。
> 上一份逐行审计基线为 `b6cfad8`；本报告在其后新增的 138 个文件 / +5603 −387 行基础上做**全量重审**，不是增量摘要。
> 事实标注：`[实测]`=本机跑命令所得；`[源码]`=逐行读码+行号复核；`[官方]`=标准/一手文档；`[媒体]`=二手报道；`[推断]`=静态推演，未实测触发。

## 0. 结论摘要

1. **当前 HEAD 处于红灯状态，且红灯原因是新代码而非环境问题。** `ae310a8` 把 `local-dir` 落地从 `localfs.CopyTree(src,dst)` 改为 `localfs.Materialize(src,dst,pf.Vendor.LinkMode)`，但 `pf` 来自 `config.ReadProjectFile`（原始清单，不合并默认值），清单省略 `vendor` 段时 `pf.Vendor == nil` → **空指针 panic**。仓库自带的 `TestLocalDirInstallVerifyE2E` 在 HEAD 直接 panic `[实测]`；GitHub 上该提交 61 条 check-run 中 **15 条 failure**（三平台 build/vet/test、go test -race、三平台 pre-release tests、docs link check 等）`[实测]`。
2. **本次 pull 新增的 local sources 存在系统性集成缺口：`local:` 命名空间只接进了 install/resolve/verify 主路径，没有接进 lock 的消费者。** 实测同一份合法 local-git 项目：首次 install 成功，第二次 install / `--frozen-lockfile` / `gc --dry-run` / `sbom` / `verify local:gitmod` **全部 exit 3**（`unknown host "local"`）`[实测]`。这是"幽灵功能"——写进 lock 后自己读不回来。
3. **旧审计的 35 项发现确实全部落地，没有回退**；本次新缺陷集中在最后两个 commit（7325893 的 local sources、ae310a8 的 symlink 落地），属于"新功能合入时未按既有契约网全量回归"的回归，而非历史债复活 `[源码]`。
4. **竞品坐标已再次移动**：2026 年主流包管理器把"脚本默认拒绝 + 发布晾晒期 + 安装期恶意软件检查"做成了标配；ngm 的"默认拒绝脚本 + P1D 晾晒期"不再是独有卖点。ngm 真正仍然领先的是**把 refType、内容 digest、漂移三态、离线重放收束进同一台可重放状态机**；真正落后的是签名背书、采用度/发布覆盖、构建系统与 workspace 语义，以及本次暴露的工程完整性（HEAD 红灯）。
5. **优先级建议**：P0 先修 panic 与文档断链让 CI 回绿；P1 把 `local:` 身份判定统一到单一入口并覆盖全部 lock 消费点；P2 补 symlink 逃逸与 JSON 报告完整性；P3 才是判据空转与标准形状微调。**在这些修完前，v0.103.0-dev.2 不应作为可用版本对外推荐。**

## 1. 方法、覆盖与读数

### 1.1 审计方法

- 10 组并行审查覆盖全部 Go 源码与测试、契约、CI、脚本、文档；要求逐文件通读并给出 `file:line`。
- 主审对每条候选**回源码复核**，剔除 3 类误报/夸大：① `report.go:274-278` 明确写明"critical 与 operational 允许同依赖双计"，因此 agent 报的"双计 bug"降级为呈现层一致性问题；② CycloneDX 1.7 schema 实际仍接受 legacy `tools` 数组（只是 deprecated），降级为未来兼容提示；③ `mappings` 清单不可读时仍执行逃逸校验，属 fail-closed，误伤 global 模式是可用性问题而非放行漏洞。
- 对最高危项写**临时探针测试**实测（跑完即删，工作树已恢复干净）：直接复现二次 install、gc、sbom、verify 目标选择、panic。
- 竞品事实重新联网核对（2026-11-23），不沿用 11-21 报告的快照；对二手来源标注 `[媒体]`。

### 1.2 覆盖与规模读数 `[实测]`

| 项 | 当前值 |
| --- | --- |
| Go 文件 / 总行数 | 509 / 99,302 |
| 测试文件 / `func Test*` / `func Fuzz*` | 357 / 1,045 / 3 |
| 非测试 Go 文件 | 152 |
| 顶层命令 | 28（`cmd/ngm/root.go:49-78`，均有真实 Run） |
| ADR / README 行数 | 52 / 599（README 已贴 discipline 上限；本轮收口时合并一行） |
| 外部 Go 依赖 | 仅 2：`wazero v1.9.0`、`modproof v0.2.0` |
| 最新标签 | `v0.103.0-dev.2`（正式版仍停在 `v0.101.0`） |
| 本次 pull | 89 文件，+3625 −208，3 个新 dev tag |

分组覆盖：CLI 核心命令两组（约 12,500 行）、CLI 新增/契约测试组（4,795 行）、resolve/lock/digest 组（8,902 行）、git/localfs/vendor 组（16,281 行，其中 6,000 行逐字精读）、verify/attest/sbom/supplychain 组（7,644 行）、security/run 组（3,519 行）、adapter/adopt/integrations/mappings/observability 组（14,526 行）、config + CLI 测试扫描组、文档/契约/CI 组。少量存量 CLI 测试采用"元测试全覆盖 + 重点精读 + 全量模式扫描"，未逐字读完 170 个文件——这一点如实记录，不声称 100% 逐字覆盖。

## 2. 架构速写与代码质量评价

```
ngm.json ──config──▶ resolve.Graph ──▶ lock v3 ──▶ vendor/content store
                         │                 │              │
                  refType 三路分流      dirDigest/archive   mappings
                         │                 │              │
                    supplychain 门禁   verify 三态漂移   integrations/build hooks
                         └──────── attest / SBOM / SARIF / --json 通道 ────────┘
```

**优点（有证据）**：依赖极少；`atomicfile.Write` 的临时文件→Chmod→Sync→Rename→syncDir 完整；`lock.Parse` 二次 Decode 要求 EOF；digest 清单按复合键排序并有 `ValidateUniquePaths`；git 命令单一构造点 + 三张环境过滤表 + 门禁前置；vendor symlink 逃逸判据在落地前执行；verify 漂移三态与 exit 2/4 优先级正确；K-02（伪造清单归 critical）、P-01（逗号 target）、C-02/C-03（LSRemote URL 与 fail-closed）、A-01~A-06（JSON 信封）等旧发现均有判据且读码确认仍在位。

**结构性问题**：契约网极密（1,045 个 Test 函数、大量源码派生判据），但**判据与产品代码共同漂移**——本次 panic 就发生在"新增 `pf.Vendor.LinkMode` 而所有 local-dir 测试 fixture 恰好省略 vendor 段"的组合上，现有判据没有 `vendor` 缺失场景，CI 因而没能在合入前拦住。密网不等于全覆盖网。

## 3. 发现清单

### 3.1 P0（阻断发布）

**P0-1 · 缺少 `vendor` 段时 local-dir 安装 panic**
- 位置：`cmd/ngm/install.go:423`、`cmd/ngm/install.go:601`、`cmd/ngm/update.go:210`。
- 机制：`pf` 来自 `config.ReadProjectFile`（`internal/config/io.go:21`），只做单文件解码，不给 `ProjectFile.Vendor` 填默认值；默认值只存在于 `Resolved.Effective`（`config.go:502-515`）。`ae310a8` 直接解引用 `pf.Vendor.LinkMode` → nil panic。
- 证据 `[实测]`：`go test ./cmd/ngm -run '^TestLocalDir'` → `panic: invalid memory address` at `install.go:423`；该测试是仓库自带、非临时探针。CI 61 条 check-run 中 15 条 failure。
- 修法：统一用 `env.LinkTree(pf)` 或 `linkModeOf(pf)`（缺省 auto）取 linkMode，三处一起改；补一条"最小清单无 vendor 段 + local-dir install/update"的回归判据，并在 CI 要求 `go test ./...` 绿。

**P0-2 · 新 spec 文档断链导致 docs-links 判据红**
- 位置：`docs/specs/local-sources/spec.md:4`，`[ADR-052](../adr/adr-052-local-sources.md)`；从 `docs/specs/local-sources/` 出发应为 `../../adr/...`。
- 证据 `[实测]`：`bash scripts/check-docs-links.sh` → `found 1 broken links`。
- 修法：改相对路径并重跑链接判据（锚点判据 `TestDocsAnchorsResolve` 当前是绿的，不能替代链接判据）。

### 3.2 P1（功能不可用 / 契约破坏）

| 编号 | 位置 | 机制与实测 | 建议 |
| --- | --- | --- | --- |
| P1-1 | `cmd/ngm/install.go:608` | `installFromLock` 只对 `local-dir` 分流；`local-git` 落进 `resolve.ParseSlug("local:x")` → `unknown host "local"`。`[实测]` 首次 install exit 0，第二次 install 与 `--frozen-lockfile` 均 exit 3 | 仿 `verify.go:305-312` 构造伪 Canonical；lock 路径必须完全离线，从 `localRepo` 取 commit 而非 mirror |
| P1-2 | `cmd/ngm/gc.go:360` | 每条 lock 依赖无条件 `ParseSlug`；任一 local 条目使 gc 整体 exit 3（连 `--dry-run` 也不行）。`[实测]` | 按 `RefType.IsLocal()` 分流；local-dir 无 mirror，直接跳过 mirror 可达性但保留 vendor 路径可达 |
| P1-3 | `internal/sbom/entries.go:26` | `FromLock` 无条件 `ParseSlug` → `ngm sbom` / `ngm attest` exit 3。`[实测]` sbom；attest 同源码路径 `[源码]` | local 条目需独立 purl（`pkg:generic/local/<name>@<digest>`），不能只放行 ParseSlug——否则 `VCSURL()` 会造 `https://local/...` |
| P1-4 | `cmd/ngm/verify.go:538,546`；`why.go:75`；`remove.go:54` | 目标解析与 lock 匹配都用 ParseSlug，按名筛选 local 依赖必然 0 命中。`[实测]` `verify local:gitmod` exit 3 | 抽一个 `DepIdentity(name, refType)` 单一入口，所有按名筛选复用 |
| P1-5 | `cmd/ngm/update.go:219` | `--json` 时 `humanOut` 已改指 stderr，但 local-dir 进度行直写原始 `stdout`，破坏"stdout 恰好一份 JSON"契约 `[源码]` | 改 `fmt.Fprintf(humanOut, ...)`；install.go:604/442 的同类写法因为传入的 `stdout` 形参本就是 `humanOut` 而正确，不要误改 |
| P1-6 | `cmd/ngm/update.go:348` | `autoVerifyAfterLock` 的 `errOut` 传原始 `stdout` 而非 `envelope`，verifyOnLock 硬错误时绕过 sink，`emitWriteReport` 再写第二份 JSON `[推断，需 verifyOnLock 硬错误触发]` | 末参改 `envelope`，与 `install.go:550/755` 对齐 |
| P1-7 | `cmd/ngm/install.go:577-606`、`update.go:200-221` | local-dir 分支未向 `rep.Dependencies` 追加条目，而 `emitWriteReport` 用 `len(rep.Dependencies)` 当 `count`（`install.go:910`）→ local-only 项目报告 `count=0` 但 lock 有依赖 `[源码]` | 按 `installFresh:436-441` 形状补写；加"报告 count == lock 依赖数"的行为判据 |

**共同根因**：`local:` 身份判定分散在至少 9 处，风格混杂（`IsLocal()`、`== RefTypeLocalDir`、`HasPrefix("local:")`、裸 `ParseSlug`）。建议新增 `internal/core/resolve/localidentity.go` 作为唯一判定+伪 Canonical 构造入口，并加一条"全仓 lock 消费点不得裸调 ParseSlug"的源码扫描判据。

### 3.3 P2（安全边界 / 确定性 / 配置）

1. **local-dir symlink 逃逸面不对称** — `internal/core/localfs/path.go:35-84` 只做词法校验（`filepath.Clean`，无 `EvalSymlinks`），而 spec §10 要求"解析后仍在项目根内"；`copy.go:52-58` 原样重建 symlink 目标，git 来源则有 `vendor/links.go:414-441` 的 `EscapesPackageTree`。源目录内一条指向 `../../etc` 的链接会被复制进 vendor 且 verify 仍判 OK `[源码]`。让 localfs 复用同一判据，必要时拒绝逃逸链接。
2. **local-git 的 branch ref 不可解析** — `resolve/reftype.go:79-87` 把非 hex ref 一律按 tag 解析，而 `cmd/ngm/add.go:26` 与 ADR-004 承诺 tag/branch/commit 三选一。`@main` 指向 branch 时报 tag not found `[源码]`。
3. **配置有效视图浅拷贝回写** — `internal/config/config.go:476-479` 只拷贝 `SupplyChain` 结构体，内嵌三个 `[]string` 仍与来源共享，违反 v0.66"改 Effective 不回写 Project"；`TestV151` 只测 string 字段 `[源码]`。逐字段深拷贝切片并加变异判据。
4. **本地依赖重复根声明漏校验** — `internal/config/validate.go:103-110` 遇 `ParseSlug` 失败即 `continue`，两条相同 `local:x` + 相同 path 不被拒绝；`add` 路径有字符串去重兜底，手写清单无 `[源码]`。
5. **verify 呈现层自相矛盾** — `internal/core/verify/verify.go:264` 在已确证 dirDigest 篡改时仍写 `res.Err`，`report.go:291-293` 因此把同一依赖同时计入 critical 与 operational（**这是注释明确的设计，不是计数 bug**），但 SARIF `sarif.go:166-175` 会追加 "check incomplete" warning，且 `verifyLocalDir` 未调 `remediationFor`（`verify.go:477/556` 的其他路径有），CI 读 JSON 会看到"没查完"的建议而 actual 是"已确证篡改" `[源码]`。
6. **boundbuf 限流误伤数据输出** — `git/exec.go:109` 把通用 `Run` 的 stdout/stderr 一律限到 1 MiB；`tree.go:41` 的 `ls-tree -r -z`（大树）会被截断成 "trailing record without NUL"，`catfile.go:203` 的 `GetBlob` 会返回混入截断标记的污染字节 `[源码]`。诊断输出与数据输出需分流，后者应走 pipe 或可配置上限。
7. **integrations/mappings 的 vendor 根 fallback** — 清单不可读时 `VendorRoot` 留空，`mappings.Validate` 回退到 `./ngm.vendor`（`validate.go:129-155`）；逃逸检查仍执行（fail-closed，这点旧发现已修好），但 global vendor 模式下可能误拒绝对路径 `to`，也可能接受一个"相对默认根合法但相对真实 global 根无意义"的别名 `[推断]`。fallback 应优先从 global 配置或 lock 推断真实根。
8. **晾晒期测试口径分裂** — 远端 fixture 时间固定在 2025-01-01，本地 `testutils.NewGitRepo` 用当前时间；若干 drift 测试之所以绿，依赖"lock 命中路径不做 P1D 检查"这一实现细节。若将来给 from-lock 补门禁，测试会以错误理由变红 `[源码]`。统一固定 committer date 或显式加 `--allow-fresh`。
9. **文档读数漂移** — `COMPETITIVE-ANALYSIS.md:250` 结论仍称"不是通用包管理器替代品"（与 ADR-052 冲突）；`docs/specs/assessment-2026-11-21-line-by-line-followup/tasks.md` 声称 V159–V169 判据存在且"验证会红"，源码实际只有 V143–V158；`local-sources/spec.md:68` 承诺 local-git 允许绝对路径，与 §10 及 `ValidateSourcePath` 冲突；`:97` 承诺 `--refresh-digest` flag 未实现；README 行数/ADR 行号读数过期（审计时实际 600 行、593 行处）`[源码+实测]`。
10. **`verify --json` 的 exit 2 不可解释** — 签名/沙箱附加层把 `summary.exitCode` 抬到 2，但 dependencies 全 ok、critical=0，失败原因只在 stderr `[源码]`。报告应带机器可读的 gate 结果。

### 3.4 P3（边缘 / 未来兼容 / 判据质量）

- `internal/sbom/cyclonedx.go:33,37,99-103`：CDX 1.7 用 legacy `tools` 数组。**已核对官方 1.7 schema：数组形式仍在 `oneOf` 中，只是标 deprecated** `[官方]`，因此 schema 合法；建议迁到 `tools.components[]` 以防未来版本移除。
- `internal/security/permissions.go:353-365` 与 `internal/runid/runid.go:34-53`：`isPathGrant("C:foo")` 与 `PathForm("C:foo")` 判定不对称，授权静默失效（方向 fail-closed）。
- `cmd/ngm/engine.go:158-160`：`execSuffixes` 含 `.ps1`，`tool.ps1` 与 `tool` 共用 `run:tool` 身份；wasm 条目不解析 Path，项目内 wasm 可被名字授权放行，弱于 subprocess `[源码]`。
- `internal/security/sandbox.go:136-204`：逗号 fail-closed 只在 `PlanSandbox`，导出的 `DenoArgs` 不自检；当前调用点均经 PlanSandbox，不可达。
- `internal/core/git/lsremote.go:55-63`：git warning 输出到 stdout 时整条 ls-remote 失败，属于 C-03 fail-closed 的可用性回归。
- `internal/adopt/lockformats.go:24-26`：`git://` 被第二次 `TrimPrefix("git:")` 剥成 `//host/repo.git`，与 `adopt.go:595-599` 的 I-04 修复口径不一致（只影响 provenance URL）。
- **判据空转/过弱**（绿灯≠会红）：`v128_engine_identity_test.go:279-283` 循环无 found 守卫；`v143_json_contracts_test.go:74` 恒假分支 + `checked` 恒真；`internal/config/v105_test.go:78` 用本身非法的清单"证明" BOM 行为，恒真；`add_local_e2e_test.go:29` 忽略 `ReadFile` 错误；`v130_skip_ledger_test.go:14-17` 正则把注释里的 `t.Skip(` 计入；`update_local_e2e_test.go:44-47` 只断言 lock 字节变化，时间戳变化即可满足。
- **零覆盖高价值场景**：local-git 二次 install / tag 漂移 / vendor 篡改、local 源路径逃逸（`../`、绝对、盘符、UNC、`file://`、软链）、local 定点 verify/update/gc/sbom、`update` 的 rc 1/2/5 行为路径。

## 4. 上次审计 35 项的落地复核

逐项抽查 11 项均命中、无回退：A-01（`install.go` 独立 `errOut`）、A-03（config OK 打印后置）、C-02（LSRemote URL 校验）、K-01（SARIF level 随 strict/allow-drift）、K-02（伪造清单 typed 为 integrity failure）、K-05（lock EOF）、P-01（逗号 target 拒绝）、V-01（首次 clone 校验 commit）、C-05（git 输出 boundbuf）、E 批三项（复合键排序、`Parse` EOF、`addDigit` 饱和加法）均有对应判据仍在位。tasks.md 复选框全勾。**结论：历史欠账已闭环；当前红灯完全由新增 local sources 引入。**

## 5. 竞品全维度对比（2026-11-23 刷新）

### 5.1 竞品当前状态（相对 11-21 快照的变化）

- 安全默认已商品化：npm 12 默认阻断 lifecycle scripts（`allowScripts` 白名单）、`allow-git`/`allow-remote` 默认 `none`；pnpm 默认 `minimumReleaseAge` 1 天、`blockExoticSubdeps`、内置 `pnpm sbom`；Yarn 4.x `npmMinimalAgeGate` + 默认禁用第三方 postinstall；Bun 1.3+ `minimumReleaseAge`、1.4 对 git/tarball 依赖记 SHA-512；Deno 2.8/2.9 `min-release-age` + `trust-policy: no-downgrade`；uv 0.12 `--require-hashes` 且拒 MD5-only `[媒体，多篇 2026 报道一致]`。
- 发布新鲜度已从"特性"变"基线"：Bundler、npm、Yarn、mise、Hex、Mamba、Cargo（nightly `-Zmin-publish-age`）均有对应门禁；Dependabot 自 2026-08 起默认 3 天冷却；安全公告可解锁冷却成为共同设计模式 `[媒体]`。
- 安装期恶意软件检查成为新战场：Composer 2.10 默认过滤、uv `UV_MALWARE_CHECK`、npm registry 发布期扫描并附 contentPolicy verdict `[媒体]`。
- 构建/缓存轴：pnpm 12 Rust 重写 stable；Nix/Bazel 仍把"内容寻址 + 沙箱 + 远程缓存"做到最强；语言包管理器普遍只有下载缓存，无共享任务输出缓存 `[媒体]`。
- 标准侧：SLSA v1.1 为当前稳定版，SLSA Build L2（托管构建 + 签名 provenance）通过 npm trusted publishing 已成事实门槛；Sigstore keyless + cosign 是主流验证路径 `[媒体]`。CycloneDX 1.7 schema 仍接受 legacy `tools` 数组 `[官方，已直接核对 schema]`。

### 5.2 全维度矩阵

| 维度 | ngm 当前 | JS 主流（npm/pnpm/yarn/bun/deno） | 系统/多语言（Cargo/Go/uv/Conan/vcpkg） | 构建系统（Nix/Bazel） | 判定 |
| --- | --- | --- | --- | --- | --- |
| 依赖身份 | commit + refType 自描述 + 内容 digest | 版本/范围为主，git 依赖为地址 | 版本 + lock hash；Cargo/Go 偏 commit/校验和 | 输入闭包哈希 | **ngm 领先**（身份表达力） |
| 锁文件与锁定 | v3、`--frozen-lockfile`、拒绝尾部内容、复合键排序 | 各有 lock，pnpm 拒缺 integrity；Go 用 go.mod+go.sum 分离 | Cargo.lock / uv.lock / go.sum 成熟且 merge-friendly | flake.lock / MODULE.bazel.lock | 持平（格式工程 ngm 不弱，生态兼容弱） |
| 离线可复现 | content store 命中即零网络落地；verify 可离线重放 | 缓存+lock，能离线但证明链依赖 registry 元数据 | 本地缓存齐全，离线语义类似 | Nix 内容寻址最强 | **ngm 领先**（离线证明） |
| 本地源 / monorepo | 新增 local-git/local-dir + symlink 落地，**但九个消费点未接通，实测多处 exit 3** | `workspace:` / catalog / 单一 root lock 成熟 | Cargo/uv workspace 统一 root lock | 强隔离但需重写构建描述 | **落后**（本次重度缺陷） |
| 脚本默认拒绝 | 默认 deny，路径级 `run:` 授权 + 解析后真实文件判定 | 2026 已全部跟进，多为包级白名单 | 较少执行 install 脚本 | 沙箱天然隔离 | 持平（ngm 粒度更细，对手生态更大） |
| 发布晾晒期 | 默认 P1D（ADR-050）+ `--allow-fresh` | 已是标配且更细（公告解锁、严格模式） | Cargo nightly / uv `--exclude-newer` / Hex | 依赖锁本身 | 持平（不再独有） |
| 恶意软件检测 | 无安装期扫描；靠 digest + 门禁 | Composer/uv/npm registry 已上线 | 部分有 | 不适用 | **落后**（定位选择：ngm 证内容，不判意图） |
| 签名与溯源 | attest 未签名；cosign 仅 CI 配方 | npm trusted publishing → SLSA L2 + registry 签名 | PyPI/GitHub 原生 provenance | 工件签名普遍 | **落后**（真实差距） |
| SBOM | CDX 1.7 输出、确定性排序、digest 覆盖；**local 依赖导出失败** | pnpm 内置 `pnpm sbom`（CDX 1.7）、生态工具最全 | cyclonedx-gomod/uv 等 | SBOM 作为构建产物 | 落后（local 缺口 + 工具链覆盖） |
| 漂移/验证语义 | 三态 + critical/operational 分层 + 退出码 2/4 | 多为"lock 一致/不一致"二态 | 校验和不一致即失败 | 哈希不匹配即失效 | **ngm 领先** |
| 机器接口与 CI action | 28 命令、14 个 `--json`、统一错误信封、退出码契约、action.yml | CLI JSON 输出多不统一；CI 靠生态插件 | 多数无统一 JSON 契约 | 结构化输出较弱 | **ngm 领先** |
| 构建链路 / adapter | adapter 中立（subprocess/wasm/Deno），不自研 bundler | 自带 bundler/transpile（Vite 等） | 编译器自带 | 强 hermetic 构建 | 正交（ADR-044 明确不做） |
| 工程纪律 | 52 ADR、契约网、判据变异要求、13/13 discipline | 大厂工程规范强但无等价"判据必变异"纪律 | 各有发布门禁 | 形式化最强 | **ngm 领先但在 HEAD 失守**（CI 红） |
| 采用度 / 发布覆盖 | 小众；release 63/101，38 个明确不回填；Gitee 为主 | 安装量以十亿计 | Cargo/Go/uv 大众化 | Nix/Bazel 细分但成熟 | **显著落后** |

判定按 ADR-044 校准："正交/明确不做"（registry、semver、自研 bundler、peer deps）不算落后；真正落后仅 5 格：本地源集成、恶意软件检查、签名背书、SBOM 的 local 覆盖、采用度/发布覆盖——其中本地源集成是本次新引入且可立即修复的。

## 6. 建议路线（按"先让绿灯可信，再改功能"）

- **P0（立即）**：修 `pf.Vendor` 三处 nil 解引用；修 spec.md:4 断链；CI 全绿后再打任何 tag。判据：最小清单（无 vendor 段）+ local-dir 的 install/update 往返。
- **P1（本轮核心）**：引入 `DepIdentity` 单一入口，统一 installFromLock / gc / sbom+attest / verify-target / why / remove / tree / outdated / verify_signatures / config 去重 / lock 校验；补 local-git 二次 install、tag 漂移、vendor 篡改三条 e2e；修 update 的 stdout 与 envelope 接线、报告 `count`。
- **P2**：localfs symlink 逃逸判据与 git 来源对齐；local-git branch 解析；config 深拷贝；SARIF/remediation 一致性；boundbuf 数据路径分流晾晒期 fixture 时间统一。
- **P3**：修 6 条空转判据（各配变异验证）；CDX tools 形状迁移；清理文档读数漂移与 V159–V169 虚构判据编号；`git://` adopt 口径统一。
- **明确不做**（ADR-044）：registry/semver/peer deps/node_modules、自研 bundler、自造签名格式；签名走 cosign/SLSA 外部背书，不自签。

## 7. 证据边界

- 本报告是**逐行静态审计 + 关键路径实测**，不是全量变异测试；未逐字读完 170 余个存量 CLI 测试文件，已注明。
- 已实测：local-git 二次 install/frozen/gc/sbom/verify-target 的 exit 3、`TestLocalDirInstallVerifyE2E` 的 panic、docs-links 断链、CI 15 条 failure。未实测：symlink 逃逸、`update --json` 污染、envelope 双写、CDX 工具链消费兼容性——均标 `[推断]` 或 `[源码]`。
- 竞品事实为 2026-11-23 联网所得，多为二手汇总；关键标准结论（CycloneDX 1.7 `tools` 形状）直接核对官方 schema。竞品条目未逐一下载安装实测版本行为，不应译为"已复现竞品缺陷/能力"。

## 8. 落地状态（2026-11-23 同日全量推进）

用户授权后按 P0→P3 全量执行，状态如下（判据编号见各测试文件头注释）：

- **P0 已完成**：`pf.Vendor` 三处解引用改为 `linkModeOf(pf)` 缺省 auto（V200 变异红/修复绿）；`spec.md:4` 断链修复。CI 红灯的两项直接原因均已消除。
- **P1 已完成**：新增 `resolve.DepIdentity` / `LocalCanonical` / `ParseIdentityArg` 单一身份入口；接通 `installFromLock`、`gc`、`sbom.FromLock`（含 local purl / hashKind / N/A 的 VCS URL）、`attest`（local 材料用 `local:<name>@<digest>` + sha256，不再造 `git+@`）、`filterLock`、`why`、`remove`、`config` 去重与移除、`tree`、`outdated`、`verify_signatures`（local 记 N/A）、`core/verify`；`update` 的 stdout/envelope/报告 count 三处接线修复。V201/V202 变异验证均红→绿。
- **P2 已完成**：逃逸判据下沉 `pathutil.EscapesPackageTree` 并由 git/localfs 共用（digest 与落地双门禁，V203 两层同时变异才红）；`local-git` branch 回退查找（V204）；`SupplyChain` 切片深拷贝与 local 重复根判重（V205）；verify 呈现层一致性与 SARIF 操作性别升级；`git.Options.OutputLimit` 数据路径分流（V207）；`verify --json` 的 `gates` 段（V206）；晾晒期与漂移 fixture 解耦。
- **P3 已完成**：6 条空转/过弱判据复健（`found` 守卫、删恒假分支、合法 BOM 清单、ReadFile 错误检查、dirDigest 真值断言、skip 账本）；CDX 1.7 tools 迁移到 components + `$schema`（V138 强化）；`git://` adopt 口径统一（V208）；`isPathGrant` 复用 `runid.PathForm`、`DenoArgs` 自防御（V209）、ls-remote warning 与 `-` pattern 边界（V210）、wasm 身份例外写入安全模型；文档读数/判据编号漂移纠偏（新判据从 V200 起，避免与历史计划 V159–V169 撞号）。
- **CI 契约债清理**：在 HEAD 基线上复核确认 5 项判据红不是本轮回归：core 边界扫描范围已扩到 `pathutil`（9→10）、local-dir 裸写需在 atomicfile 例外表登记、全仓库空 hint 棘轮卡在 118 而 HEAD 实为 126（冻结为 126 并写明来源）、`graph.go` 的 3 处 `CodeInternal` 未登记、用户层 3 处空 hint。全部补齐后与 `gates` 字段文档形状表一同复绿。
- **附带修复**：README 收回 599 行（第 11 问此前因 HEAD 的 600 行而行红）；skip 台账补齐两处 HEAD 既有/新增的平台边界（63 处 / 44 文件）。
- **有意未做**：`add.go` 的写声明尾段重复属纯代码风格债，改动面与回归风险不匹配当前收益，登记不修；`integrations/mappings` 在清单不可读时回退默认 vendor 根是 fail-closed 的设计选择，global 模式误拒属于可用性权衡，不改判据口径。
- **校验边界**：`go build ./...`、`go vet ./...`、`go test ./internal/...` 全绿；discipline 13/13；docs 链接/锚点/gofmt/diff-check 均绿。本机 `go test ./cmd/ngm` 全量跑会被环境级 `[safe-delete][SAFE_DELETE_BULK_CONFIRM_REQUIRED]` 提示污染 stdout（每轮 50 次临时目录删除后注入），叠加 PowerShell 执行策略与缺少 `git-http-backend`；受影响的测试组已逐组单独复跑全绿（V86/V87/V88/V90、V102/V79 均通过），V95 属缺工具的环境门槛而非产品失败。

## 9. 相关文档

- 上轮逐行审计：`docs/analysis/ngm-line-by-line-audit-2026-11-21.md`
- 上轮竞品评估：`docs/analysis/ngm-competitive-gap-full-2026-11-21.md`
- 功能梳理：`docs/analysis/ngm-feature-inventory-2026-11-21.md`
- 本地来源 spec / ADR：`docs/specs/local-sources/spec.md`、`docs/adr/adr-052-local-sources.md`
- 边界坐标：`docs/adr/adr-044-independent-ecosystem.md`
