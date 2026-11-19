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

- [~] **F1**（本轮未做：收敛 JSON encoder 触及多命令 golden，风险/收益不匹配，留下一轮） 收敛多套 JSON encoder（engines `writeJSON`、integrations、
  `gcReport`、`runList`、各 modules writer）到 `cmdContext.writeReport` 或
  一个共享 helper；保持字节级输出不变（golden 测试守住）。
- [~] **F2**（本轮未做：15 条命令迁 cmdContext 同上） 剩余约 15 条手写 flag 模板命令迁到 `cmdContext`；`run` 因 `--` 拆分
  与自有错误通道保留（见 `cmd_context.go:23-25`）。
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
  - 读数（2026-11-19，本机 Windows，整套 18 分钟）：**82.6%**（门槛 80%，历史读数 82.1%）。
    注意这是**下限**：同一次整套跑里有若干**环境性**失败（见下），覆盖率只会被低估。

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
- [x] `verify` 改动跑 `-coverpkg=./internal/core/verify ./...`：**82.6%**（≥ 80%）
- [x] gofmt 在干净 LF 导出上判定（0 个文件）
- [x] `check-docs-links.sh` → all docs links OK；`check-discipline.mjs` → **13/13**
- [x] 完整 `go test ./...`（本机首次，18 分钟）：18 个失败中 6 个是**真回归**（已修）；
      修完复跑 19 个失败**全为本机环境**（V79/V86/V87/V88/V90/V91/V95/V102*，0 回归）

**提交纪律**：一主题一提交；每个提交在独立 worktree 上三读数全绿才合并；
旧/新二进制的 `--help`、`--bogus`、`--json` 错误面对照（若改了命令骨架或输出）。
