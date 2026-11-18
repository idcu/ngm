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

---

## 验收（每批）

- [x/待] `go build ./... && go vet ./...`
- [x/待] `go test -count=1 ./internal/...`（本机 87s）；受影响命令定向测试
- [x/待] 新增判据在默认 `go test` 中执行、无 `t.Skip`
- [x/待] 权限改动有"子进程零命中"断言；JSON 改动有单文档字节断言
- [x/待] `verify` 改动跑 `-coverpkg=./internal/core/verify ./...`，跨包 ≥ 80%
- [x/待] gofmt 在干净 LF 导出上判定

**提交纪律**：一主题一提交；每个提交在独立 worktree 上三读数全绿才合并；
旧/新二进制的 `--help`、`--bogus`、`--json` 错误面对照（若改了命令骨架或输出）。
