# ngm 全维度代码评估（2026-11-20）

> 本文是对当前工作树（分支 `main`，HEAD `5961cbc`）的**一次性全量快照评估**：146 个非测试 Go 源文件（约 12,000 行）、324 个测试文件、329 个 Markdown 文档、4 个工作流与 33 个脚本。
>
> **修复进度（2026-11-20 当日）**：P0-1、P1-1~P1-7、文档一致性、CI 超时/重复检查（批次 A–D）
> 以及**追加批次 E/F/G**（子路径归一化统一、P2 正确性与安全加固、注释/调用点一致性、
> `--` 分隔符 / materialCache / fsyncDir 去重）已全部落地，逐项带判据 + 变异检验。
> M11/M9/M14 经复核属"已记录的决定"或"描述与代码不符"，明确不改并附理由。逐项状态见
> [assessment-2026-11-20-followup/tasks.md](../specs/assessment-2026-11-20-followup/tasks.md)。
>
> **第二批（2026-11-20 当日，[followup-2](../specs/assessment-2026-11-20-followup-2/tasks.md)）**：
> 把第一批刻意留下的剩余项收口——H 批修 M1（auto link 混合树 → 整树复制）与 scp 地址误报；
> I 批让各产品包覆盖率可见（仍只守 verify 门槛）并定下 v089 判据"不改"；J 批为三项结构性债
> （package main 规模、usage/flag 双源、契约网冻结）各写一条带触发条件的 ADR-047~049。
> 下文保留**评估当时的原始结论**作为历史快照，不因修复而改写。
>
> 它是**评估，不是事实源**。命令行为的权威仍是代码与 `docs/guides/cli.md`；退出码契约的权威是 `contracts/exit-codes.json`；发布读数的权威是 `docs/guides/release-pipeline.md`。本文给结论与出处，不复制这些表。
>
> 方法：按包分组逐文件审查（cmd/ngm、internal/core/*、internal/* 上层包），交叉核对文档与测试，全部“已确认”项都在源码中定位到具体行并复核过上下文。`go build ./...` 与 `go vet ./...` 均通过（exit 0）。

---

## 一、结论摘要

**整体评价：成熟度显著高于一般早期 CLI 项目，主线正确性可靠，没有发现可远程利用的 RCE / token 泄漏 / 沙箱逃逸。**

| 维度 | 评级 | 一句话 |
|---|---|---|
| 功能正确性 | 良好 | 核心闭环（refType → commit → digest → vendor → verify）算法扎实；有一处安装期校验缺失与两处 fail-open 值得修 |
| 安全边界 | 良好偏上 | Deno 沙箱、`run:git` 单一构造点、default-deny、`WaitDelay` 资源上限都到位；**一处 net 门禁旁路已确认** |
| 错误处理 / 契约 | 良好但分裂 | 错误模型与退出码优先级设计优秀；`--json` 信封在若干早期失败路径与 WASM 输出上不一致 |
| 确定性 | 优秀 | 清单排序、序列化格式、SBOM/attest 稳定字节都有纪律；一处 `verify --json` 详情顺序未排序 |
| 可维护性 | 中等 | 注释即回归档案是稀缺资产；但 `package main` 过大、契约网过剩、双实现漂移 |
| 测试 | 良好 | 分层清晰、覆盖率有门禁（仅 verify 包）、跳过有账本；覆盖率口径与超时设置自相矛盾 |
| 文档 | 中等偏上 | 结构完整、诚实边界写得好；**发布读数与版本号在多份文档中漂移**，是本次重写的主要对象 |

**最值得优先处理的三项**（详见 §三）：

1. **P0** `ModuleCache.Ensure` 首次 clone 绕过 `net:` 门禁 —— `internal/core/vendor/module_cache.go:79-93`。
2. **P1** 安装期不做 `ValidateRecord`，可能造出“装得出、验不过”的依赖 —— `internal/core/git/tree.go:233,300,337-349`。
3. **P1** `verify --json` 的 `Details` 顺序不确定，破坏确定性契约 —— `internal/core/vendor/integrity.go:132-149,282-290`。

---

## 二、架构评估

### 2.1 分层与依赖方向

```
cmd/ngm (package main, 44 个非测试文件)
   │  命令实现 / 报告渲染 / 环境装配
   ▼
internal/{adapter,supplychain,security,config,mappings,sbom,attest,
          adopt,observability,runscript,integrations,...}
   │  策略与编排
   ▼
internal/core/{git,resolve,vendor,verify,digest,lock,atomicfile,errs}
   │  语言无关的证明内核，无 JS 运行时
   ▼
外部：git 子进程 / Deno 沙箱 / esbuild·tsc·postcss / wazero(WASM)
```

- `internal/core` 物理抽离已完成（ADR-033），依赖方向单向、无反向引用，边界测试 `core_boundary_test.go` 守护。
- 命令统一骨架 `cmdContext`（parse → markJSON → env → fail → writeReport）在 14 个命令 / 24 处落地，`TestCmdContextIsTheOnlySkeleton` 扫描源码守护。
- 单一事实源纪律普遍：git 进程只有 `newGitCommand` 一个构造点；原子写只有 `atomicfile.WriteFrom`；JSON 报告由 `writeJSONDocument` 收敛（但见 §三 M5）。

### 2.2 设计优点（值得保留，不要在重写中破坏）

1. **流是可注入接口**：所有 `Run(ctx, args, stdout, stderr) int`；`newFlagSet` 显式 `SetOutput(stderr)`；`writeAccounting` 在唯一入口把“结论算出来了却送不出去”折算为 exit 6，并跨平台识别 broken pipe。
2. **错误模型**：`errs.NgmError` + `MergeCodes` 的 rank 表（2>3>5>4>6>1）与 ADR-039 一致，“2 恒胜于 4/5”不被后写覆盖。
3. **安全纵深**：git 一律参数数组（`exec.CommandContext`，无 shell）；`run:git` 门禁在唯一 spawn 点；沙箱用 `--deny-run/--no-npm/--no-config/--no-lock/--no-remote` 封住实测的 npm 模块加载逃逸；最小环境 + `deny` 净化在 git / 沙箱 / 引擎 / run 脚本四个施力点共用 `envutil.Without`。
4. **digest 算法与 ADR-008 逐字一致**：单 NUL 分隔、头部区分 archive/module scope、UTF-8 字节序排序、不转换换行、末尾 NUL。这是全项目最扎实的部分。
5. **blob 身份掺入 mode**（`content_v2.go:191-197`）：hardlink 共享 inode 时保留可执行位，真正解决了跨平台去重冲突。
6. **注释即回归档案**：大量注释记录“踩过的坑 + 实测数据 + 判据出处”，对维护者价值极高——这是重写文档时最应保留的信息密度。

### 2.3 结构性技术债

- **`package main` 过大**：44 个文件、约 2.2 万行非测试代码混在一起；跨文件耦合靠约定而非接口。
- **usage 常量与 flag 声明双源**：`verifyUsage` 单条 105 行；flag 改了忘改 usage 只能靠 golden 测试兜底。
- **契约网过剩**：约 39 个测试文件用 `go/ast` / 源码扫描做“文档↔代码”对账。`docs/internals/roadmap.md:334` 自己记录外部评估的停止清单——“停止新建契约网，它抓的最后一个产品缺陷是 v0.55”，但此后仍持续新增 V102~V135 网。用判据维持冗余，而不是消除冗余。
- **过度抽象**：`materialCache`（`cmd/ngm/material_cache.go:29-49`）用 mutex + done channel 为从未发生的并发准备，调用点全是顺序循环。

---

## 三、缺陷清单

> 严重度定义：**P0** 违反已声明的安全承诺；**P1** 可导致错误结论或契约破坏；**P2** 一致性 / 可维护性；**P3** 注释、措辞、边界。

### P0 — 安全承诺被违反

**P0-1 `ModuleCache.Ensure` 首次 clone 绕过 `net:` 门禁**（`internal/core/vendor/module_cache.go:79-93`，已复核）
第 79 行注释声称“顺序与 Mirror 一致：先 run:git，再 net”，但 `!c.Exists(repo)` 分支直接调 `git.ClonePartial`（第 90 行），`CheckNetAccess` 只在缓存已存在、commit 缺失的 fetch 路径（第 102 行）出现。对比 `Mirror.Ensure`（`mirror.go:227-244`）在 clone/fetch 前都做门禁。
- **影响**：`deny net:github.com`（或默认未放行 net）下，首次模块缓存 clone 仍真实触网，直接违反 `security-model.md` 的“未显式放行的 net 按拒绝处理”。
- **修复**：在 `git.ClonePartial` 前插入 `git.CheckNetAccess(c.opts.Policy, remoteURL)`，或复用 `Mirror.Ensure` 的门禁 + URL 选取结构；补一条“零配置 clone 被拒”的端到端判据。

### P1 — 可导致错误结论或契约破坏

**P1-1 安装期不执行 `ValidateRecord`**（`internal/core/git/tree.go:233,300,337-349`；`digest/validate.go:28-60`）
`materialFromEntries` / `finishMaterial` / `streamedMaterial` 直接构造 `digest.Record` 并 `BuildManifestWithVersion`，全仓库 `ValidateRecord` 的唯一生产调用点是 `verify/verify.go:1026`。Git 允许文件名含 `\` 与换行（`ls-tree -z` 原样返回），这类条目能被写入 lock，但 `verify --content` 读清单时会被 `ValidateRecord` 拒绝而报 critical(2)——**工具造出一个自己拒绝的世界**。
- **修复**：在 `materialFromEntries` 构造记录时统一 `ValidateRecord`，把不支持的文件名变成 install 期 exit 3；或为 NUL 分隔格式提供专门边界校验并在两条路径都调用。

**P1-2 `parseListTreeZ` 静默丢弃畸形记录**（`internal/core/git/tree.go:50-92`）
缺 TAB、空路径、空 meta、`SplitN(meta," ",3)` 不等于 3 的记录全部 `continue` 跳过，无日志。digest 输入来自这段解析；被改写的 mirror 产生畸形条目时会被静默排除后算出“看起来合理”的错误 digest——最危险的“结论正确、证据错误”形态。
- **修复**：改为返回 error（带脱敏的原始记录预览），fail-closed。

**P1-3 `verify --json` 的 `Details` 顺序不确定**（`internal/core/vendor/integrity.go:132-149,282-290`，已复核）
两类路径集合差异在遍历 Go map 时 append 到 `res.Details`，末尾只对 `res.Mismatches` 做了 `sort.Strings`（289 行），`Details` 未排序。同一棵树两次运行会产生字段顺序不同的 JSON，破坏项目自称的确定性纪律与 golden 契约。
- **修复**：按 `(Path, Kind)` 排序 `res.Details`，或从排序后的 key 列表构建切片。

**P1-4 `--json` 错误信封在若干早期失败路径缺失**（`cmd/ngm/install.go:88-91`、`update.go:83-86`、`outdated.go:58-60`、`engines.go:102-121`、`verify.go:162-211`）
这些命令声明了 `JSON: true`，但位置参数校验 / flag 校验失败时只写 stderr、stdout 为 0 字节。`root.go` 的 `jsonAskedEarly`（105-107 行，连“值写坏”都算提出）与 `README.md:432` 的承诺（“失败会写完整信封到 stdout”）都指向应有信封。注意这与 v0.16 规则④（不接受 `--json` 的命令 stdout 必须为空）并不冲突——这里都是**接受** `--json` 的命令。
- **修复**：在这些分支补 `failEarly`；或把位置参数判据并入 `cmdContext`，从结构上杜绝遗漏。`verify.go` 需在 flag 校验前先 `markJSON`。

**P1-5 WASM 引擎 stdout/stderr 无上界**（`internal/adapter/wasm.go:299-303`，已复核）
`var stdout, stderr bytes.Buffer` 无界；对比 subprocess（`subprocess.go:502-505`）与 Deno 沙箱（`sandbox.go:319-322`）都用了 `boundbuf`。`WithMemoryLimitPages(512)` 只约束模块线性内存，不约束宿主侧缓冲——模块循环 `fd_write` 即可 OOM。正常执行路径的 ctx 也无 deadline。
- **修复**：改用 `boundbuf.New(...)`（实现 `io.Writer`）；产物通道用 artifactLimit 并检查 `Truncated()`；给执行路径设超时。

**P1-6 `gcExitCode` 漏算 module cache 删除失败**（`cmd/ngm/gc.go:377-383`，本次新发现）
`gcExitCode` 只看 `Store/Mirror/Vendor.Errors`，但报告结构有 `ModuleCache gcClass`（109 行），且在 277 行会 append `rep.ModuleCache.Errors`、人类渲染器（417 行）也打印它。结果：**层 1.5 的逐对象删除失败会显示在报告里，却不把退出码折成 4**，与 `CHANGELOG.md` 对 gc“删除失败汇总为 exit 4”的承诺矛盾。
- **修复**：把 `len(rep.ModuleCache.Errors)` 纳入条件；补一条 module cache 删除失败 → exit 4 的判据。

**P1-7 `attest` / `sbom` 在 lock 过期时仍产出待签名产物**（`cmd/ngm/attest.go:143-149`、`sbom.go:109-114`）
`lockManifestMismatch` 只向 stderr 打 warning，随后照常生成。产物会被拿去签名（`attest.go` usage:40-41），签名后这份“描述过期状态但结构合法”的证据再也难以发现；README 对 attest“同一 lock 产出相同字节”的强调反而放大了风险。
- **修复**：为 attest 增加显式 stale 门槛（默认拒绝或要求 `--allow-stale`），至少让退出码反映该事实，而非把判断托付给易被 CI 忽略的 stderr。

### P2 — 一致性与可维护性

| # | 问题 | 位置 | 修复方向 |
|---|---|---|---|
| M1 | `auto` link 模式是**逐文件**降级（hardlink 失败后改 copy），但 `vendor-layers.md:141` 承诺“整树复制”；返回 `Mode=LinkCopy` 掩盖混合树，就地编辑仍会污染 content store | `links.go:436-489` | 预探测 hardlink 可行性，或让 `LinkResult` 报每文件实际模式 |
| M2 | 子路径归一化跨平台不一致：`resolve.ValidateSubPath` 把 `\`→`/`，但 `config.normalizeSubPath` 用 `filepath.ToSlash` 只在 Windows 转换 → `a/b` 与 `a\b` 在 Linux 不判重，绕过 v0.102 的“重复根声明”修复 | `config/io.go:247-249`、`config/validate.go:111`、`resolve/subpath.go:43` | 统一用 `strings.ReplaceAll(p,"\\","/")` + Trim，与 resolve 同源 |
| M3 | `BuildModuleMaterial`（单数）不校验 subPath 的 `..`，而复数版与 `TreeOIDAtPath` 都做 `normalizeModulePath` | `git/tree.go:185-196` | 统一调用 `normalizeModulePath` |
| M4 | `--store` flag 名不副实：`Store.Put` 无条件执行，flag 只影响打印；usage 写“populate the content store” | `update.go:31,70,195-224` | 要么让 flag 真正控制，要么改写 help 为“report content-store status” |
| M5 | `json_document.go:11-15` 注释称是“cmd/ngm 唯一 stdout JSON 写出点”，但至少 9 处直接写：`adopt.go:163,234`、`attest.go:188`、`sbom.go:168`、`verify.go:286,293`、`audit.go:139`、`install.go:802`、`cmd_context.go:130` | `json_document.go` 及上述 | 扩展统一出口接受已编码字节，或修正注释如实描述 |
| M6 | `collectReleases` 对每个 tag 起一次 `git.RevParse`（O(tags×modules)），与同文件 `writeIndexBranch` 用一次 `hash-object --stdin-paths` 的优化相矛盾 | `modules.go:789-826,941-948` | 改用一次 `git for-each-ref --format='%(refname:short) %(objectname)' refs/tags` |
| M7 | `runWithRecovery` 写死 `os.Stderr`，绕过注入的 stderr writer——与同文件 `newFlagSet` 的长注释批评的缺陷同型 | `root.go:496-504` | 让 `runWithRecovery` 接受 stderr writer，由 `runMain` 传入 |
| M8 | 沙箱脚本输出（含 ANSI 转义）原样转发终端/日志，可伪造报告行；`indent()` 用字符串累加对上界内大输出是 O(n²) | `verify_sandbox.go:102-108,119-135`、`audit_hook.go:96-101` | 过滤控制字符；用 `strings.Builder` |
| M9 | 空值 flag 语义分裂：`--out=` 在 attest/sbom 等价 stdout、在 adopt 报错；`--hook=` 静默忽略；`--format=` 报错 | `attest.go:166`、`sbom.go:141`、`adopt.go:124-128`、`audit_hook.go:39-41` | 在 `cmdContext` 层提供统一的 `explicitEmpty` 判定 |
| M10 | `NGM_OSV_URL` 环境变量在生产路径可重定向 OSV 端点，无 scheme/白名单校验；能污染 CI 环境者可返回“无漏洞”绕过门禁 | `audit.go:118`、`tree.go:204` | 仅允许 localhost 的 http(s)，或改为构建期注入 / 显式 flag |
| M11 | OSV `CheckNet` 默认 nil 放行（fail-open）：库级默认语义与 README 的“未放行即拒绝”相反；当前生产调用点都传了真实策略，暂不可利用 | `supplychain/osv.go:91-93,200` | 构造时强制带策略，或 nil 返回内部错误 |
| M12 | `time.Duration(n)*unit` 无上界检查，极大值回绕成小正数且末尾 `total<=0` 检不出 → 很长的 `minimumReleaseAge` 实际变很短 | `config/duration.go:71-79,98` | 逐字段校验 `n > math.MaxInt64/unitNanos` |
| M13 | 子路径校验把 `\` 当分隔符，下游 git pathspec 按字面量用：`#path=packages\core` 通过校验却 install 找不到模块 | `resolve/subpath.go:43`、`git/tree.go:186,366-376` | 校验函数返回规范化路径并在所有消费点使用 |
| M14 | `modproof` 离线校验失败时把错误当 warning：`go.sum` 仍逐字节校验，但离线信任根被静默降级 | `internal/core/verify/verify.go`（modproof 分支） | 明确“离线不可证明”的退出码语义并写进报告 |

### P3 — 注释、措辞、边界

- 陈旧注释：`module_proof.go:68-69` 引用不存在的 `modules validate --release`（实际是 `--strict`）；`git/clone.go:147` 称先试 `--depth=1` 但 `FetchPartial` 没有；`testutils.go:35` 称 `-update` 默认 true 但实际 false；`root.go:30-32` 仍在讲早已修好的 `--help` 占位符历史。
- `hostFromRemoteURL("foo:bar")` 落入 scp 分支返回 host `foo`，触发 `net:foo`——fail-closed 但提示误导（`git/remoteurl.go:82-90`）。
- `parseComparableVersion` 用 `Atoi`，接近 `MaxInt` 时 `major+1` 回绕为负数（`modules.go:532-549`）。
- `mergeIntoExisting` 的 `incoming.RootDeclared` 分支在当前调用路径不可达，且若真触发会用空 `Commit/Repo` 覆盖（`resolve/graph.go:539-549`）。
- `fsyncDir` 在 `atomicfile` 与 `vendor/content_v2.go:279-286` 逐行重复。
- yarn v1 scoped 包 + `#ref` 名字解析错误，provenance 降级为 unknown（`adopt/lockformats.go:132-142`）。
- 中英混杂：根帮助全中文、子命令 usage 全英文；`store.go:384-385` 是全 CLI 唯一中文错误文案；`content_maintain.go:465-476` 中文错误。

---

## 四、测试与 CI 评估

**优点**：约 973 个顶层测试；M1–M7 里程碑验收 + 83 个 `vNNN_*` 版本回归 + 真实引擎集成（esbuild/tsc/postcss/Deno/真实 git server）+ 3 个 fuzz + golden。`test-skip-audit.md` + `v130_skip_ledger_test.go` 把 57 处跳过按文件计数入账；CI 检查“装了工具却 SKIP 即失败”。`go.mod` 只有 2 个外部依赖（wazero、modproof）且都实际使用。

**问题**：

1. **超时自相矛盾**：`ci.yml:32` build-test job `timeout-minutes: 30` 小于 `go test -timeout 40m`（`ci.yml:105`）；注释与 `README.md:574` 都说“放宽到 40m”，但 job 30 分钟就杀掉。acceptance / engine job 同为 30m。本机实测全包约 18 分钟，暂未触发，但这是“文档说的”与“实际做的”分裂。
2. **覆盖率口径**：只对 `internal/core/verify` 设跨包门槛 78%（`ci.yml:110-173`），其余产品代码无门禁。门槛本身是按 Linux 读数校准的 `80.5 − 2.5pp`，平台名已写进 annotation——这部分纪律是对的。
3. **重复执行**：`check-docs-links.sh` 在 `release-state`（`ci.yml:391`）与独立 `docs-links` job（`ci.yml:740`）跑两次；后者还有 `if [ -d docs ]` 守卫（恒真）与 M0 注释“docs 是空仓”。`check-release-status.sh` 同样在两个 job 各跑一次。
4. **空转判据**：`v089_artifacts_test.go:24-27` 自承“按引用记账，一个名字写进说明文字也算被提到”；`scripts/build-release.ps1` 是 `.sh` 的完整第二实现，无任何 CI 调用，只在注释里出现，因此“run-or-explain”门禁放行——判据绿但脚本从未执行。这也证伪了 `docs/README.md:20-21` 的“同名 .ps1 只转调，唯一实现始终是 .sh”。
5. **写死的版本号**：`release-dryrun.sh:29` 默认 `0.102.0-dev.1`；`ci.yml:823/828/834` 写死 dev.1 / v0.101.0。

---

## 五、文档一致性评估

本次按约定重写“活文档 + 内部文档”，保留 ADR 与逐版本归档。发现的一致性缺陷（本次重写已修正或应在后续修正）：

| 位置 | 问题 | 事实 / 权威 |
|---|---|---|
| `README.md:593` | “41 个决策记录” | 实际 **45** 个 ADR |
| `README.md:550` | ngm-repo pin `0.102.0-dev.1` 且“没有可下载产物” | pin 已是 `v0.102.0-dev.2` 且已发布 13 附件（`release-pipeline.md:41`、`CHANGELOG.md:177-179`）；`ci.yml:367-377` 门禁要求 pin 有产物 |
| `docs/README.md:295` | “ADR-001 ~ 023” | 实际 45；同页 `:10` 又引用 ADR-031~037，自相矛盾 |
| `docs/README.md:299` | “v0.1 ~ v0.12 实施计划与复盘” | 实际到 `v0.101-*` |
| `docs/guides/installation.md:4-6` | 头部称 v0.1~v0.4 “两个源都可取到” | Gitee 只有 v0.2~v0.4、v0.1 没有（同文件 `:36-49` 与 `release-pipeline.md:22` 已说对） |
| `docs/internals/project-state.md:172` | “最新可取 GitHub v0.64.0” | `v0.101.0`（`release-pipeline.md:17`） |
| `docs/internals/project-state.md:99,137` | Gitee 发行版“97 个” vs “18 个” | 实际 3 个；同一事实三份读数 |
| `docs/internals/roadmap.md:5` | 缺口“v0.66.0 起” | `v0.65.0` 起（`release-pipeline.md:19`） |
| `docs/internals/capability-matrix.md` | 自称能力 SSOT 但缺 `run` / `gc` / `adopt` / `sbom` / `attest` / `verify --content` | 这些都已在 CLI 实现 |
| `docs/specs/*/spec.md` | “待批准” | 对应 `tasks.md` 全 `[x]`，`CHANGELOG` 已列为实现 |
| 多份文档 | “v0.120 / v0.135 / v0.136 新增/修” | git tag 只到 `v0.101.0` + 两个 dev；内部 `vNNN` 任务号被当成已发布版本号 |

**发布读数 63 / 3 / 38 / 101 硬编码在 8 份文档中**（README、docs/README、installation、release-pipeline、roadmap、project-state、milestones-b、progress-c）。这是本次重写重点：只在 `release-pipeline.md` 保留权威表，其余引用。注意——`TestV126LiveDocsAgreeWithReleasePipeline` 会核对其中几处读数，重写时必须让这些文件与权威表同源，不能简单删数字。

**文档链接与锚点**：`check-docs-links.sh` 基线通过（exit 0）；16 处机器本地绝对路径为告警不阻断。

---

## 六、清理评估

见本次同时产出的删除清单（需人工确认后执行）。摘要：

| 类别 | 体积 | 可清理性 |
|---|---|---|
| `ngm.exe`、`bin/ngm.exe`、`dist/`（13 个二进制 + SHA256SUMS）、`.screenshots/` | 约 213 MB | 全部 gitignored、可再生，**可直接删除** |
| `docs/internals/p1-fixes-2026-11-01.diff` | 小 | 是一次性补丁快照，无任何代码/测试/文档引用，建议删除 |
| ADR（46 个）、逐版本 plan/retrospective（211 个） | — | **不建议删除**：17+ 个测试文件引用它们（`TestV71AdrIndexCoversEveryAdrFile`、`TestV63MilestoneTablesListEveryVersionExactlyOnce`、`TestV118DeletionLogIsComplete` 等），删除会让 CI 变红；且文档范围选择明确要保留它们 |

---

## 七、建议的推进顺序

1. 修 P0-1（net 门禁旁路）+ 判据；这是唯一违反已声明安全承诺的项。
2. 修 P1-1 / P1-2（安装期校验与 fail-closed）——防止“装得出验不过”。
3. 修 P1-3 / P1-6（确定性 + gc 退出码）——低成本、高契约价值。
4. 在 `cmdContext` 层结构性解决 P1-4（信封一致性），而不是逐个分支补。
5. 文档：以 `release-pipeline.md` 为唯一发布读数源，收敛其余 7 份；把内部 `vNNN` 任务号与已发布 tag 区分开。
6. CI：修正 job 超时 ≥ go test 超时；合并重复的 docs-links / release-status 检查。
7. 暂缓：新建契约网（roadmap 的停止清单仍然有效）、清理 ADR / 归档（测试强耦合）。

---

*本文是 2026-11-20 的一次性快照；结论随代码演进而失效。复核入口：`go build ./... && go vet ./...`、CI 的 annotation、以及各判据测试名。*
