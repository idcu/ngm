# Tasks：2026-11-18 评估后的开发批次

来源：[spec.md](./spec.md) · 依据 `docs/analysis/ngm-architecture-and-npm-gap-2026-11-18.md`。
状态：A/B/C/D1/D3 已完成并逐项在 worktree 上通过 build/vet/定向测试；D2 按 ADR-043 暂缓；E 需凭据；F 部分完成（见文末）。每提交在独立干净 worktree（`git worktree add --detach`）上跑
`go build ./...` + `go vet ./...` + `go test -count=1 ./internal/...` 后才合并。

---

## Batch A（P0，安全）—— 止血

- [x] **A1** `cmd/ngm/engine.go:57-73`：把 `cat.WithProbeGate(...)` 的注册**移出**
  `if env.Policy != nil` 块。
  - 理由：无全局配置 → `newPermissionPolicy(nil)` 返回 nil（`security.go:13-15`）→
    闸门不注册 → `catalog.probeAllowed`（`catalog.go:369-370`）放行 →
    `engines list/validate` 的 `Version()` 真实 spawn 仓库声明的程序。
  - 已确认 nil-safe：`Policy.CheckRun` → `Check` → `Allows`/`isDenied`/`Source`
    全部处理 nil receiver（`permissions.go:193-198,211-232,275-292`），nil Policy
    按 `DefaultTier(run:*)=Configurable` 拒绝。**不要**改成"构造一个空 Policy"，
    那会改变 `config show` 的语义。
  - `runner.WithDeniedEnv(deniedEnv)` 可留在块内（nil → 空列表，语义相同）。

- [x] **A2** 新判据 `v131_probe_gate_no_config_test.go`（禁止 `t.Skip`）：
  - `TestNoGlobalConfigDoesNotSpawnProbe`：临时 `NGM_HOME` 目录**不放 config.json**，
    项目 `ngm.engines.json` 声明一个写 marker 的探针；跑 `engines list` 与
    `engines validate`，断言 marker **零命中**，输出标 `not probed`，
    `validate` 的退出码为预期值（fail-closed）。
  - 本轮已手工黑盒复现该缺陷（无配置时 marker 被写），修复后必须反向成立。

- [x] **A3** 新判据 `TestNoDirectEngineVersionCalls`：
  - 除 `internal/adapter/catalog.go` 的 `Probe` 与它调用的 `subprocess.go` 实现外，
    生产代码不得出现 `.Version()` 的直接调用点（`engines_extra.go:212-214` 的
    `probeVersion` 是已知的一处，需确认它在 `preflight` 之后被闸门间接覆盖，
    或一并收进 `Catalog.Probe`）。
  - 形态仿 `TestSpawn_OnlyOneFileBuildsProcesses`（读源码机械断言），避免只靠调用顺序。

- [x] **A4** 文档：`docs/guides/cli.md` 的 `engines` 段补一句"无全局配置时探测同样被
  `run:<engine>` 拒绝"；`CHANGELOG.md` 的 Unreleased → Fixed 记一条。

## Batch B（P1，信任根与进程边界）

- [x] **B1** `--deep` 反向集合校验：
  - 位置：`internal/core/verify/verify.go:1002-1051` 的 `replayDigestFromEntries`
    及其调用点 `checkLanding`（`:836-875`），模块分支
    `replayModuleDigestFromEntries`（`:968-998`）按同样口径检查。
  - 改动：`replayDigestFromEntries` 现在只按 `git.ListTree` 的条目去 `byPath` 取值，
    **不检查 store 清单里多出的条目**。补一条反向比对：store 清单中不在 Git tree
    （模块：不在 `subtreeOid` 对应子树）的条目 → 报 critical，附
    `ContentMismatch{Path, Kind:"extra-store-entry"}`。
  - 分类必须是 **critical(2)**，不能返回 error 落入 `checkLanding` 的
    `operational`(4) 分支；也不能只靠 `metaMismatches`（`:1069-1087`，只比 digest
    与 manifestVersion）——攻击者往清单塞一个指向合法 blob 的额外条目时，
    `meta` 的自述 digest 仍不变。
  - 模块分支的 `ValidateRecord`（`:994`）防 NUL 伪造已存在，保留；本项是集合差。
  - 判据：表驱动 `TestDeepDetectsExtraStoreEntry`——正常 / 缺条目 / 多条目 /
    等长改写四形状，断言退出码 2/4/2/2 与报告定位；模块子路径同样跑一遍。

- [x] **B2** git 环境残留：
  - `internal/core/git/exec.go:147-183`：在 hijack 表里补 `GIT_EXEC_PATH`
    （可使 git 从任意目录加载 `git-remote-https`/`git-upload-pack` 等 helper）
    与 `LD_PRELOAD` / `DYLD_INSERT_LIBRARIES`。
  - 同文件 `:230` 的 `opts.ExtraEnv` 在过滤循环之后 append，绕过
    repoLocating/hijack/deniedEnv 三张表；把它并进同一过滤流程，或明确限制
    调用方（modproof 路径 `verify.go:653-661` 会透传）。
  - 判据：`TestBuildEnvDropsGitExecPathAndPreload`、
    `TestExtraEnvCannotResurrectDeniedVar`。
  - 注：`envutil.Without`（`envutil.go:22-42`）声称是 git/Deno/引擎三处共享语义，
    但 `git.buildEnv` 自己另写一份；本批不强制合并，但要在提交说明里记一笔债。

## Batch C（P1，对外契约）

- [x] **C1** `cmd/ngm/doctor.go:59`：`c.parse(..., nil)` 改成传 `jsonOut` 并在
  parse 之后调用 `c.markJSON(*jsonOut)`（对齐 `verify.go:213`）。
  - 判据：`TestDoctorJSONConfigFailureEnvelope`——构造 parse 后失败（如 `c.env`
    报错），`--json` 时 stdout 一份合法信封、exit 3；成功路径的 JSON 形状不变。

- [x] **C2** `contracts/exit-codes.json` 的 `description` 把
  `TestV119ExitCodeContractMatchesCode` 改成实际测试名
  `TestV119ExitCodesJsonContract`（`exit_codes_json_test.go:22`）。
  - 判据：加一条测试从 JSON 文本里抽出反引号测试名，断言该 `func` 在仓库里存在
    （防止再次登记一个不存在的守门人）。

- [x] **C3** semver 双实现统一：
  - `internal/observability/outdated.go:17` 的 `reSemver`（要求三段）与
    `internal/adapter/engines_extra.go:217` 的 `reVersionNumber` + `versionAtLeast`
    （任意段数）行为不同（`v2.4` 在一处不匹配、在另一处可比较）。
  - 抽一个共享的宽松版本解析（`internal/version` 或新 `internal/semver`），
    两处都用它；不要改变 `outdated` 对外报告的措辞。
  - 判据：表驱动覆盖 `v2.4` / `v2.4.1-rc1` / `2.4.1+meta` / 非版本串。

## Batch D（P2，npm 迁移的最后几厘米）

- [x] **D1** `adopt` 读 pnpm/yarn lock：
  - `internal/adopt/adopt.go:496-551` 当前只解析 `package-lock.json` v2/v3，
    pnpm/yarn lock 只识别并报 unknown。
  - 按 spec §3 的决策点：优先窄形状解析，只提取 Git 来源字段进 provenance，
    其余写进 `notes` 与 `coverage.unknown`；不引入 YAML 依赖，除非实测无法覆盖
    主流 lock 形态（那时先停下来拍板）。
  - 判据：三类夹具（npm / pnpm / yarn）各产一份 `ngm.proof.json`，断言 provenance
    的 Git commit 与 lock 一致、digest 稳定；unknown 仍如实上报。

- [~] **D2**（按 ADR-043 暂缓，非功能债） vendored Git 依赖的 `.bin` shim：
  - 在 `ngm.vendor/.bin/` 为声明了 `bin` 的 vendored 依赖生成 shim
    （POSIX 无扩展名 + Windows `.cmd`；与平台 shell 语义一致，不自研引号）；
    `runscript.BinDirs`（`runscript.go:213-231`）除 `node_modules/.bin` 外也收集
    `ngm.vendor/.bin`（近者在前）。
  - **不写 `node_modules`**，不改 ADR-042 的"脚本在沙箱外、单一 run:script 闸门"。
  - 先补 ADR-043（或在 ADR-042 加段）说明 shim 的归属与边界，再动代码。
  - 判据：`ngm run <tool>` 在未装 npm 的 PATH 下能找到 vendored 工具；
    `ngm build` 的引擎解析（`subprocess.go:116-144 resolveProgram`）同样能找到。

- [x] **D3** `ngm gc` 覆盖 module cache：
  - 当前 `cmd/ngm/gc.go:181-282` 扫 store/mirror/vendor 三层，
    `~/.ngm/module-cache/`（partial clone）不在其中。
  - 按同一"先算计划、再删、逐对象失败收集、默认 dry-run"的形状加一层；
    global 条目无法判定引用时保留并 warning（沿用 ADR-041）。
  - 判据：`--apply` 只回收当前项目 lock 不可达的 module 条目；dry-run 零删除。

## Batch E（需外部凭据，不占工程排期）

- [ ] **E1** release 资产与 tag 对齐：至少让一个 `0.102.x` 有可下载、可校验的
  release；`installation.md` 的"npm 分发（规划中）"要么落实、要么删掉。
  - 判据：`scripts/check-release-status.sh --published` 与
    `check-gitee-release-status.sh` 的读数一致；无 token 时保持暂缓，不伪装成功。

## Batch F（技术债，D 之后）

- [x] **F1**（2026-11-19 收尾：先定契约，再收敛）收敛 JSON encoder。

  实测盘点（`cmd/ngm` 非测试代码）：stdout 报告写出点 9 处，全部在写失败时退 **6**
  （ADR-026 语义已经一致），差别只在**编码**：

  | 写法 | 位置 | 转义 |
  |---|---|---|
  | `json.MarshalIndent(v,"","  ")` + 手动补 `\n` | `engines.writeJSON`、`init`、`install`、`integrations`、`modules`×3、`run --list` | HTML 转义（默认） |
  | `json.NewEncoder` + `SetEscapeHTML(false)` | `gc --json` | **不转义** |

  两处实际差异只有 `&` `<` `>` 写成 `\u0026` 与否——**解码后完全相同**，
  因此这不是对外契约问题，而是一致性问题。`sbom` / `attest` 的 `SetEscapeHTML(false)`
  是**另一类**：它们的字节要被人拿去比对/签名，注释已写明理由，不该被统一掉。

  决策（2026-11-19 拍板，见 [ADR-045](../../adr/adr-045-json-output-contract.md)）：
  **`--json` 的契约是解码语义，字节不是**。于是可以收敛：
  - `writeJSON` / `writeModulesJSON` / `writeIntegrationsJSON` / `gc` 内联 → 一个
    `writeJSONDocument`（`cmd/ngm/json_document.go`），统一 `write report: …` + 退 6；
  - `gc` 的 `SetEscapeHTML(false)` 去掉，与其余报告一致（**解码后不变**）；
  - 三档分类写进 ADR：**产物**（sbom/attest，字节是交付物，不转义）/ **报告**（转义）/
    **错误信封**（main.go，装的是人话，不转义）；
  - 守卫：`json.NewEncoder(` 在 `cmd/ngm` 只准出现在 `main.go`（`checkEncoderIsSingle`）。
  按字节 diff `--json` 从此是**未定义行为**——需要稳定字节用产物，不用报告。
- [x] **F2**（2026-11-19 完成）14 条命令 / 24 处手写骨架迁到 `cmdContext`：
  - 已迁移：`add` `attest` `mappings` `remove` `sbom`（有 env 的一批）、
    `build` `cache` `config` `init` `transform` `typedecl`（无 env 的一批）、
    `modules`（5 个子命令）`store`（3 个）`typecheck`（2 个：`typecheck` / `css`）。
  - **新增一条分叉判据**（`cmd_context.go` 的 `jsonAware`）：迁移立刻撞上 v0.16 规则④——
    `add --json` 必须 stdout 为空。于是拆出 `newCmdContextNoJSON`：只有
    stdout 本身即文档的命令（`sbom` / `attest`）与真正不接受 `--json` 的命令
    才走它，解析失败不写信封。`nil` 这个参数**不能**复用为这个语义：
    `verify` 就是用 `nil` 调 parse（把 `--format` 校验放在 `--json` 生效之前）
    却需要信封。
  - **机械守卫** `TestCmdContextIsTheOnlySkeleton`（`cmd_context_guard_test.go`）：
    `fs.Parse(normalizeArgs(...))` 只准出现在 `cmd_context.go` 与 `run.go`（例外登记了理由：
    `--` 拆分是 run 自己的语义）；`fs.Usage = func()` 只准出现在 `cmd_context.go`；
    每个文件的 `newFlagSet` 数必须等于 `newCmdContext*` 数。已做变异检验：
    把 `cache` 改回手写，立刻红。
  - 唯一保留的手写骨架是 `run`（`--` 之后的参数不进 flagset，且它有 `runArgError`
    这条更细的错误通道）。
  - 用户可观察的变化只有一处：`modules` 的五个子命令在 `--json` 下**失败**时现在
    给错误信封（此前解析失败与运行期错误都不给，stdout 为空）。
    这条是**顺带的修正**：那五个子命令早就有 `--json`，但 ctx 从未被标记成 JSON 模式
    （它们只把 `*jsonOut` 用来选输出形态），于是所有 `runErr` 都走人读通道。
- [x] **F3** 清理死代码 `cmd/ngm/tree.go:231-238` 的 `countVulns`（实际用的是
  `observability.VulnCount`）。

## Batch G（2026-11-19，S3 残项与最后一条未验收门槛）

- [x] **G1** `ngm run` 的子进程环境遵守 `permissions.deny: ["env:X"]`：
  - `runscript.ExecSpec` 新增 `DeniedEnv`，`ChildEnv` 走 `envutil.Without`；
    `run.go` 传 `env.Policy.DeniedEnvVars()`。治理范围是**透传**那一段——
    lifecycle 契约变量（`PATH` / `INIT_CWD` / `npm_lifecycle_*` / `npm_package_*`）
    照旧注入，与 `git.buildEnv` 对 `GIT_TERMINAL_PROMPT` / `LC_ALL` 的处理同源。
  - 判据：`TestChildEnvStripsDeniedVariables`（单元，含"契约变量必须存活"的反面）、
    `TestRunScriptDoesNotSeeDeniedEnv`（端到端：脚本打印两个变量，被拒绝的那个不出现、
    未被拒绝的那个出现——正向对照防止"什么都没传过去"也能变绿）。已做变异检验：
    把 `DeniedEnv` 接成 nil，后者立刻红。
  - 施加点表与文档同步：`internal/security/enforcement_test.go` 的 `env:` 条目、
    `docs/architecture/security-model.md` 的表、`ngm run --help`、`configuration.md`。

- [x] **G2** 模块条目"多一条 store 记录"的端到端判据：
  - 现状：B1 的反向集合只覆盖根条目，模块分支靠"会被 `moduleDigest` 重放抓住"
    这句注释支撑（`verify.go` 的 2c 段）。注释不是证据。
  - 判据：`TestV135ModuleExtraStoreEntryIsCritical`——真实 monorepo 子包
    （`#path=packages/a`）+ `install --offline`，先跑一次干净的 `verify --content`
    作基线，再往模块的 store 清单注入一条复用合法 blob 的额外条目：
    `verify --content` 必须 exit 2，且 mismatch 的 `Kind` 是 `module-digest`。
  - 这条判据同时是一条**结构守卫**：若哪天模块改成不重放 store 清单，它会红，
    提醒把 `extraStoreEntries` 扩展到模块分支。

- [x] **G3** 跨包覆盖率按 CI 口径实测：
  - 命令：`go test -count=1 -timeout 40m -coverpkg=./internal/core/verify -coverprofile=... ./...`
    （与 `ci.yml` 的 Linux 分片等价）。
  - 读数（2026-11-19，本机 Windows，整套 18 分钟）：**82.6%**（历史读数 82.1%）。
    注意这是**下限**：同一次整套跑里有若干**环境性**失败（见下），覆盖率只会被低估。
  - **CI（Linux）的真实读数：80.5%**（run 288，门槛那一行现在会把平台名一起发出来）。
    两个平台不同不是怪事：平台专属判据只在自己的平台上跑，覆盖到的 verify 分支因此
    不同。真正的教训是**门槛必须用它实际测量的那个平台来校准**——此前 80% 取自
    Windows 的 82.1%，而跑门槛的是 Linux，余量只剩 0.5pp，见下一条。

- [x] **G4**（顺带，纪律缺口清零）`README.md` 598 行：
  - `check-discipline.mjs` 第 11 问此前是唯一 ❌（README 606 行 > 600）。
  - 处置：把"发布读数"那段**压缩成 3 行散文**——四个数字（101 个正式 tag / **63** 个正式版 /
    `v0.2.0`–`v0.4.0`（3 个）/ 缺口 **38 个**）一个不少，因为
    `TestV126LiveDocsAgreeWithReleasePipeline` 正在读它们。**第一版把整段删掉是错的**：
    那份"重复"是被判据守着的同源读数，删掉等于让同源检查静默变松（该判据当场就红了）。
  - 读数：13/13 全绿（此前 12/13）。

---

## G5–G8：完整 `go test ./...` 暴露的漏项（全部是 A–F 的尾巴，已修）

定向测试全绿 ≠ 没问题。首次完整跑全量抓出四处"改了代码/文档，但没同步改判据或索引"：

- [x] **G5** `docs/adr/README.md` 漏登 ADR-043（`TestV71AdrIndexCoversEveryAdrFile`）。
  新建 ADR 必须同时登记索引——这条判据就是为"读者按索引找不到已定的决策"存在的。
- [x] **G6** `ngm gc --json` 新增的 `moduleCache` 字段没进 `docs/guides/cli.md` 的形状表
  （`TestV26EveryFieldOfTheReportIsDocumented`：用户看得见、文档不点名的字段是半个契约）。
  同时把该行的"三层"改成"四层"。
- [x] **G7** `TestDeclaredEngineVersionIsComparedWithTheLocalOne` 依赖旧行为（无配置时探测不设防）。
  回退 A1 是错的方向；改法是让它先 `testutils.AllowEngines(t, "fake-engine")` 再 `isolateUserEnv`
  ——权限必须在隔离 HOME **之前**声明，否则写不进那份配置。
- [x] **G8** 参数维度判据（`TestV39`）的 `doctor/junk-arg`：`matrixArgs["doctor"]` 带 `--json`，
  信封写 stdout、用法写 stderr，拼接后首行是信封。判据改判"stdout 是 JSON 信封时看 stderr 首行"，
  它守的命题（打出来的是**这个命令**的用法）不变。

---

## 本机跑全量时**必然红**的判据（环境，不是代码）

2026-11-19 首次在本机跑完整 `go test ./...`：**18 个失败 = 6 个真回归（已修）+ 12 个环境**。
修完复跑（`go test -count=1 ./cmd/ngm`，18 分钟）：**19 个失败全部落在下面三组，0 回归**。
逐条 `-run` 单跑**全绿**，因此它们是环境或并行干扰：

| 组 | 判据 | 原因 |
|---|---|---|
| PowerShell | `TestV90PowerShellWrappersRunTheirImplementation`、`TestV91GiteeReadingRunsOffline` 的 Windows 入口 | 本机 `pwsh` 未安装，`powershell.exe` 的 ExecutionPolicy 为 Restricted → `.ps1` 一律 `UnauthorizedAccess` |
| 缺工具 | `TestV95HTTPSFetchEndToEnd` | 便携 Git 里没有 `git-http-backend` |
| 并行干扰 | `TestV79VendoredScan*`、`TestV86*`、`TestV87*`、`TestV88*`、`TestV102GiteeRelease*`、`TestV102ReleaseDriftGuard*` | 整套并行跑时沙箱的批量删除守卫把 `[safe-delete]…` 插进脚本输出，搅乱退出码；单跑全绿 |

**处置**：不改代码、不加 `t.Skip`（那会让 CI 上也少跑这些判据）。CI 的三平台 runner
不受这三类影响；本机读数只用于"有没有引入回归"，口径写清楚即可。

---

## 验收（每批）

- [x] `go build ./... && go vet ./...`（2026-11-19 复跑：全绿）
- [x] `go test -count=1 ./internal/...`（本机 107s，23 包全绿）+ 受影响命令定向测试
- [x] 新增判据在默认 `go test` 中执行、无 `t.Skip`
- [x] 权限改动有"子进程零命中"断言（V131 探针 marker；V135 的 deny-env 端到端）；
      JSON 改动有单文档字节断言
- [x] `verify` 改动跑 `-coverpkg=./internal/core/verify ./...`：Windows **82.6%** /
      Linux CI **80.5%**；门槛按 Linux 校准为 **78%**（余量 2.5pp，见 ci.yml 的注释）
- [x] gofmt 在干净 LF 导出上判定（0 个文件）
- [x] `check-docs-links.sh` → all docs links OK；`check-discipline.mjs` → **13/13**
- [x] 完整 `go test ./...`（本机首次，18 分钟）：18 个失败中 6 个是**真回归**（已修）；
      修完复跑 19 个失败**全为本机环境**（V79/V86/V87/V88/V90/V91/V95/V102*，0 回归）

---

## Batch I（2026-11-19，进行中）：主 CI 自 2026-11-07 起持续变红

现场记录：[docs/development/ci-red-2026-11-07.md](../../development/ci-red-2026-11-07.md)（证据链、
已排除项、下一步该看什么）。

- [x] **I1** 判定"断的不是代码"。首个红色的 run（268）对应的提交 `cb31978` **只改了一份
  markdown**（2 增 3 删），而它同时打红了 3 个平台的 `go test ./...`、`-race` 与真引擎验收。
  另外三条读数支持同一结论：失败集在 run 275（2026-11-08，我动手之前）与 278（本轮）
  完全相同；本机 `go build` / `go vet` / `go test -run '^$' ./...` 全绿（Go 版本与 CI 相同）；
  失败时上传的日志 artifact 只有约 **900 B**，说明 `go test` 没跑到用例。
- [x] **I2** 让失败**可读**：`.github/workflows/ci.yml` 新增三个 `if: failure()` 的
  annotation 步骤——退出码 + 失败包名 + 失败用例名 + 真引擎 job 的 `FAIL`/`SKIP`。
  依据：`check-runs/{id}/annotations` **免认证**可读，而 job summary 与日志都要 token；
  这 12 天公开侧只有 "exit code 1"，诊断只能靠猜。名字过字符白名单，防止 `::` 伪造
  工作流命令（与 `ghEscape` 同一顾虑）。
- [x] **I3** 按 annotation 给出的名字修完，**run 286（`54d0fb8`）24/24 全绿**——
  主 CI 自 2026-11-07 起第一次绿。三条原因（外加一条夹具自伤）互不相关：
  1. `readCache` 只把 `ENOENT` 当未命中 → POSIX 上缓存不可读时 `ngm audit` 直接失败；
  2. `engines validate` 的退出码被当成"探测成功"的判据（开发机 0 / CI 5）；
  3. `npm_config_*` 只"不设"仍透传 → 从 npm 启动与从 shell 启动行为不同；
  4. `TestV03SandboxRealDeno/…not a pass`：子用例名含逗号 → `t.TempDir()` 把逗号
     写进路径 → 沙箱按设计拒绝含逗号的读路径。夹具改用无标点临时目录，并给那条
     限制补了判据（`internal/security` 的 comma 子用例）。
  第 4 条只有"0.43s 就失败"这一个线索，真正原因来自 I2 的**细节 annotation**。

**提交纪律**：一主题一提交；每个提交在独立 worktree 上三读数全绿才合并；
旧/新二进制的 `--help`、`--bogus`、`--json` 错误面对照（若改了命令骨架或输出）。

---

## Batch M（2026-11-19）：产物原子写从"注释里的规则"变成源码判据

- [x] **M1** v0.82 把 `atomicfile` 定为"写用户可见产物"的唯一出口，但 `ngm init`
      （`ngm.json` + 入口文件）、`ngm transform --outfile`、`internal/adapter/wasm.go`
      的 `--outfile` 仍是裸 `os.WriteFile`。三处改为 `atomicfile.Write`。
      用户可观察的变化：中断/磁盘满不再留下截断产物；`--force` 覆盖一个 0600 的
      `ngm.json` 后是 0644；目标是符号链接时替换链接本身（产物的语义）。
- [x] **M2** 新增 `cmd/ngm/atomic_write_guard_test.go`：用 AST 扫 `cmd/ngm` + `internal`
      的产品文件（按 import 别名认 `os.WriteFile`，注释里的字眼不算），四个**非产物**
      站点登记理由（temp 暂存 / 布局标记 / 0600 探针 / OSV 缓存），并要求账本条目
      仍能在源码里找到（防止账本腐烂）。反向断言三个曾经裸写的出口仍调用
      `atomicfile.Write`。临时把 `init` 改回裸写实测过判据会红，随后还原。
- [x] **M3** 新增 POSIX 行为断言（0644 + 无 `.ngm-*.tmp` 残骸），Windows 权限位
      是能力边界：登记进 skip 账本（**57 处 / 39 文件**，V130 由此归绿）。
- [x] **M4** 本机复核：`go build ./...` + `go vet ./...` + docs links + discipline 13/13
      全绿；`go test ./cmd/ngm -timeout 20m` 的失败集仍是已知的**本机环境**组
      （V79/V86/V87/V88/V90/V91/V95/V102*），0 回归。默认 10m 超时会杀掉本机
      18 分钟的全包运行——本机复跑必须显式 `-timeout 20m`。

---

## Batch N：`go test` 卡住时，公开侧看不到卡在哪

M4 那次本机全量先被**默认 10m 超时**杀掉：panic 说 "test timed out"，但
`running tests:` 只给出一条陈旧采样，公开侧没有 `--- FAIL`，annotation 只剩
退出码和包名——与 CI 红 12 天时"看不到名字，只能靠猜"是同一种失效。

- [x] **N1** `runChecker` 从 `exec.Command` 换成 `exec.CommandContext`，单次自检器
      上界 **2 分钟**（实测每次约 30s）；错误里点名脚本与参数。
      同时设 `cmd.WaitDelay = 2s`：自检器起的子进程在直接进程被杀后仍握着
      stdout 管道，不设这一行，所谓"超时"还会继续挂在 `CombinedOutput` 上。
- [x] **N2** 拆出 `runCheckerRaw`（返回错误而不是 `t.Fatal`），并用
      `exec sleep 30` 夹具证明超时在秒级返回、退出码不为正常判据、错误点名脚本。
      "超时有没有牙齿"从此有判据，不再等下一次真卡住才发现超时只是注释。
- [x] **N3** `.github/workflows/ci.yml` 新增 `failure annotations — test timeout`：
      从 panic 头与 `running tests:` 发布两条 annotation（用例名过字符白名单，
      剩余细节仍只进脱敏 artifact）。用 M4 留下的真实超时日志验证过提取结果
      能拿到 `TestV80NestedMonorepoIsReportedAsIncomplete`。
- [x] **N4** 所有 `runChecker` 调用方（V76/V77/V78/V79/V80/V87/V88 + 新自测）
      跑过：同进程批量运行里只有 V79 / V87 红，原因仍是已登记的 `[safe-delete]`
      输出污染；两者**各自单跑全绿**（13.7s / 58.7s），0 回归。
      skip 账本未变（57 处 / 39 文件）。

---

## Batch O：报错让人去用一个不存在的 flag

`ngm transform` 建输出目录失败时说 "point --outdir at a directory that exists"，
而 transform 的选项叫 `--outfile`。这类错位不会让任何判据变红——脚本照样拿到
exit 3，人却照提示去试一个会被拒的选项。

- [x] **O1** 修正提示为 `--outfile`，并在 `TestV05TransformAcceptance` 加一条
      **平台无关**的端到端子用例：父路径是普通文件 ⇒ `MkdirAll` 必失败，
      断言提示点名 `--outfile` 且绝不出现 `--outdir`。
- [x] **O2** 新增 `cmd/ngm/flag_hint_guard_test.go`：AST 解析每份命令源码，
      flag 从 `fs.String/Bool/Int/...` 派生，`--xxx` 只从**字符串字面量**取
      （注释不算），大小写混写的外部工具参数（tsc 的 `--noEmit`）整词排除。
      跨文件引用逐条登记理由（全局 flag、给 esbuild/git 的参数、别的命令的 flag），
      并要求登记项仍被引用。变异检验：把提示改回 `--outdir` ⇒ 判据红，已还原。
- [x] **O3** 新增判据同时带空跑保护（文件数 / 提及数下界），防止扫描面塌掉后
      永远绿。**口径边界**：按文件而非按单个子命令派生，因此同一文件里
      A 子命令引用 B 子命令的 flag 仍需人工判断——这一点写在测试注释里。
- [x] **O4** CI 对最新提交 24/24 全绿（N 批次之后复核过），本批定向测试、
      build/vet、docs links、discipline 全绿。

---

## Batch P：vendor copy 的权限只在"文件不存在"时生效

`internal/core/vendor/links.go` 的 `copyFile` 用 `os.OpenFile(dst, O_CREATE|O_TRUNC, perm)`
落盘。`perm` **只在创建时生效**：上游把文件从 0644 改成 0755，重新 install 到既有
vendor 目录时内容更新了、可执行位仍是旧的。而 `verify --content` 只比字节，
所以这条错位不会被判红。同一行代码还会在中断时把旧文件截断。

- [x] **P1** `atomicfile` 增加 `WriteFrom(path, io.Reader, perm)`：与 `Write` 共用
      临时文件 → Chmod(perm) → Sync → rename → fsyncDir 的顺序；`Write` 变成
      `WriteFrom(bytes.NewReader(data), Perm)`，原子写仍只有一份实现。
- [x] **P2** `copyFile` 改为 `atomicfile.WriteFrom(dst, src, perm)`：权限来自清单，
      且覆盖是原子的。`permForMode` 仍是唯一映射（0644 / 0755）。
- [x] **P3** 判据用**只读位**而不是 0755 断言（Windows 上 0755/0644 不可表达，
      只读位三平台都有语义）：既有目标先 0644，`copyFile(..., 0444)` 后必须
      失去写位；反向再证明可写清单能清掉只读位。另有"无 `.ngm-*` 残骸"断言。
      变异检验：把 `tmp.Chmod(perm)` 改成 `tmp.Chmod(Perm)` ⇒ 两条判据同时红。
- [x] **P4** 复核：`internal/core/{atomicfile,vendor}` 全包绿（0.2s / 79.8s）；
      11 条 install 相关 CLI 判据绿（33.3s）；build/vet、docs links、discipline 全绿。
      **没有新增 t.Skip**，skip 账本仍是 57 处 / 39 文件。

---

## Batch Q：钩子失败回滚了 vendor，却没回滚 mappings

`install` 的写入顺序是：vendor 落地 → 写 `ngm.mappings.json` → 跑 postinstall 钩子
→ 写 `ngm.lock`。钩子失败时 `rollback` 只删除本次新建的 vendor 目录；
mappings 若**调用前就存在**，则原样留下——于是磁盘上是"新 mappings + 旧 lock"，
mappings 指向的目录已被删掉。下一次 `ngm build` 从"能跑"变成"找不到文件"。

- [x] **Q1** 回滚前先读一份 mappings 快照；失败时用 `atomicfile.Write` 恢复，
      原本不存在则删除，读不出来则按不可回滚处理（不猜）。**global vendor** 模式
      仍然不删共享目录，但本项目自己的 mappings 照样恢复。
- [x] **Q2** 把 `TestV124HookFailureRollsBackNewVendor` 的夹具加一个无钩子依赖：
      失败这次会写出与调用前不同的 mappings（两条而不是一条），只回滚 vendor 的
      旧实现因此留下第二条记录。断言失败后 mappings 与调用前**逐字节相同**。
- [x] **Q3** 变异检验：把恢复分支改成 `hadMappings && false` ⇒ 判据红并打印出
      多出来的 `github:v3/second` 条目，随后还原。
- [x] **Q4** 复核：V124（3.9s）、V02 安装模式（11.5s）、V66 JSON 两条（各 1.8s）、
      V73（1.8s）全绿；本机 Deno 1.45 低于门槛，V124 的第二个子用例按既有边界
      skip（账本未变）。build/vet、docs links、discipline 全绿。

---

## Batch R：CI 在跑测试之前就断了（Gitee 模块的 TLS 握手）

`a2081f5` 的 CI 有 4 个 Ubuntu job 红：`build/vet/test`、`-race`、
`lock 字节可复现`、`engine integration`。annotation 给出的原因不是断言失败：

```
unrecognized import path "gitee.com/idcu-go/modproof":
https fetch: net/http: TLS handshake timeout
```

也就是说 **`go test` 根本没跑到用例**，四个 job 是在拉依赖时一起断的。

- [x] **R1** 根因：三个 workflow 都设了 `GOPRIVATE: gitee.com/idcu-go/*`，
      它**强制直连** Gitee（绕过代理）。冷缓存时每个 job 都要赌一次到 Gitee 的
      TLS 握手；2026-11-20 这次同时赌输四次。
- [x] **R2** 改为代理优先：`GOPROXY=https://goproxy.cn,https://goproxy.io,
      https://proxy.golang.org,direct`，保留 `GONOSUMDB`（该模块未被
      sum.golang.org 收录），去掉 `GOPRIVATE`。go.sum 仍逐字节校验，代理改不了内容。
      本机用**全新的 GOMODCACHE** 分别经 goproxy.cn / goproxy.io 下载成功；
      proxy.golang.org 对该模块返回 502（所以排在其后）。
- [x] **R3** 新增 `scripts/go-mod-download.sh`：3 次重试（间隔 10s / 20s），
      ci.yml 与 release.yml 的 `go mod download` 改用它（显式 `shell: bash`，
      Windows 上也走同一段实现）。脚本的成功与失败路径都实测过（失败返回 1）。
- [x] **R4** 这次红**不是代码回归**：同一提交的本机 build/vet 与定向测试全绿。
      修完后 CI #296 证明网络那一段已经解决——测试真的跑起来了，`go test` 的
      失败变成了**判据失败**而不是依赖拉取失败。
- [x] **R5** 但新脚本被既有判据抓到：`TestV90WrapperOptionsParity` 要求每个
      `scripts/*.sh` 有同名 `.ps1` 入口，否则必须登记理由。`go-mod-download.sh`
      是 CI 专用包装（工作流显式 `shell: bash`，无选项可转发），已登记进
      `noWindowsEntry` 并写明原因；单跑该判据绿。

---

## Batch S：两条 schema 写回路径不问"这份内容合法吗"

`lock.Write` 会先 `Validate()` 再落盘，而 `config.WriteProjectFile` 与
`mappings.Write` 不会。后两者由 `ngm add` / `ngm remove` / `ngm install` 调用，
模式是"读入已校验的文件 → 改一处 → 写回"；写回这一步不再校验，于是内部构造
出错时，一份"名字对、内容坏"的文件会落盘，失败推迟到下一条命令，且表现成
另一个字段的问题（错误离原因隔了一次进程）。

- [x] **S1** `mappings.Write` 写前 `Validate()`，错误里点名"生成 mappings 的那一步"
      而不是让 `ngm build` 去报"找不到模块"。
- [x] **S2** `config.WriteProjectFile` 写前 `Validate()`，与 `lock.Write` 同一条纪律。
      副作用是"完整 40 位 commit 省略 refType"的窄默认会在写回时被补上（此前
      要等下一次读取才补）；add 的 golden 判据仍绿。
- [x] **S3** 两条新判据：非法内容（mappings 缺 `to` / ngm.json 缺 `name`）必须
      报错且**不创建文件**；合法内容照常写出。去掉校验即红。
- [x] **S4** 复核：`internal/config` 0.37s、`internal/mappings` 0.64s 全绿；
      add / mappings / config 相关 CLI 判据 19.3s 全绿；build/vet 全绿。
