# Tasks：2026-11-07 评估后的修复批次

来源：`docs/specs/assessment-2026-11-07-followup/spec.md`。
状态：A–G 已合入 main；F2（P2-3 命令骨架抽取）已完成，逐提交在干净 worktree 验证。

## Batch A（P0，安全）

- [x] A1 `internal/adapter/runner.go` — `Plans` 增加 probe 开关；dry-run 不 spawn
- [x] A2 `internal/adapter/catalog.go` — `Validate`/`probeEngine` 接 policy + deniedEnv
- [x] A3 `internal/adapter/subprocess.go` — `Available()` 按项目目录解析；`Version()` 用 DeniedEnv
- [x] A4 `cmd/ngm/engines.go` — 探测前判定；缓存键加绝对路径
- [x] A5 `cmd/ngm/build.go` — usage 改 "render only, never spawn"
- [x] A6 测试：`TestDryRunSpawnsNothing`、`TestEnginesListDeniedDoesNotSpawn`（`v109_probe_gate_test.go`）

## Batch B（P0，契约）

- [x] B1 `cmd/ngm/audit_hook.go` — hook 失败不写 stdout 第二份 JSON
- [x] B2 `cmd/ngm/verify.go` — `finishVerify` 最高严重度；摘要用合成码
- [x] B3 `cmd/ngm/gc.go` — 配置/lock 失败出 JSON 信封
- [x] B4 `internal/core/errs` — `MergeCodes`
- [x] B5 测试：单文档、critical 胜 5、gc 信封（`v110_contract_test.go`）

## Batch C（P1，边界）

- [x] C1 `resolve` 增加 `Offline`；`tree`/`verify` 传入；冷 mirror → 4（`v111_offline_test.go`）
- [x] C2 lock `vendorPath`/`subPath` 白名单；读取端越界检查；`env` `EvalSymlinks`（`schema_test.go` 表驱动）
- [x] C3 git `buildEnv` 净化 `GIT_CONFIG_*` 等；clone scheme 白名单（`v113_env_test.go`）
- [x] C4 adopt 识别 pnpm 布局；修正零包提示（`TestV122AdoptPnpmLinkedLayout`）

## Batch D（P1，存储与 install）

- [x] D1 `Put` 自修损坏清单；`writeBlob` 拒绝截断复用（`v123_repair_test.go`）
- [x] D1-重做 `ContentStore.Has` 的 blob 校验：必须区分"条目缺失 → exit 4"与"条目存在但内容不符/已截断 → critical exit 2"，并配套 verify 语义调整与表驱动用例（不能仅把大小不符当作 `Has` 为假——默认 hardlink 布局下就地篡改会同时改变 blob 大小，那样会把已确证的篡改误报成资源缺失）。实现：`CheckEntry` 分开返回"条目存在"与逐条目问题，`Has` 仅在字节齐全且长度一致时为真；verify 对 missing 记 operational、对 size 记 critical。判据 `TestV125StoreEntryStates`（正常 / 缺失 / 截断 / 等长改写 四形状，断言退出码与报告定位）
- [x] D2 `materializeVendor` 失败回滚本次新建 vendor 目录；lock 不写（`v124_install_rollback_test.go`）

## Batch E–G（P1-7 / P2）

- [x] E1 三份文档读数对齐 `release-pipeline.md`
- [x] E2 CI 加 `timeout-minutes`；npm 钉版本；tag 正则
- [x] E3 SARIF 前缀 / `ghEscape`
- [x] F1 合并四处 probe → 单一函数（`Catalog.Probe` + 子进程计数判据 `TestV127*`）
- [x] F2 `cmdContext` 抽取（P2-3）：12 条命令的 parse/usage/`--json`/env/报告收尾合并为 `cmd_context.go`；`run` 因 `--` 拆分与自有错误通道保留原骨架（见该文件注释）
- [x] F3 modules semver 统一（`parseModuleVersion`）+ origin URL 脱敏（`resolve.RedactURL`）
- [x] F4 沙箱补 deny（`--deny-sys/ffi/hrtime`；`--deny-node` 在 Deno 1.45.2 不存在）+ 最小环境 + 逗号路径 fail-closed
- [x] G1 OSV 硬化（同源重定向、8 MiB 上限、缓存写失败降级 warning、`--offline --no-cache` 明确失败）
- [x] G2 覆盖率门槛（CI，verify 跨包 ≥ 80%，本机实测 82.1%）+ 3 个 fuzz 目标 + [`t.Skip` 审计](../../internals/test-skip-audit.md)（59 → 57）
- [x] G3 cat-file 流式（digest 重放不保留 blob；判据 `TestV130StreamedDigestMatchesRetainedMaterial`）。`Has` 缓存**评估后继续拒绝**：已有基准注释记录的失效信号（同进程内被外部改写）无法消除，缓存会把"检查"变成"猜"
- [x] G4 构建可离线性：[评估](../../internals/build-offline-assessment.md)（实测无凭据冷构建 14.3s 通过；决定维持现状 + CI 探测）

## 取舍记录（为什么这样定）

- **覆盖率门槛取 verify ≥ 80%（跨包口径）；旧的 "≥ 55%" 与 35% / 54.4% 读数作废。**
  口径修正（2026-11-08）：35.0% 是 `internal/core/verify` **包内自测**（只有包自己的测试），
  54.4% 是只跑 M5+M7 两条验收的**子集**读数——两者都不是 CI 指标。CI 用
  `go test -coverpkg=./internal/core/verify ./...` 让全部测试（主要是 `cmd/ngm` 的验收）
  给 verify 记数，合并后本机（Windows，CI 等价命令）实测 **82.1%**。门槛取 80%：略低于
  实测值、留 2.1pp 给平台差异，且远高于 35.0%，所以"退化回单包读数"会同时撞上门槛和
  CI 的日志断言（`cmd/ngm … in ./internal/core/verify` 行必须存在）。评估表里的 60% 是
  早期建议值，已被本次实测取代。Linux CI 上的实际读数尚未在本机验证（本机没有 CI runner），
  80% 是"本机实测可达"的下界，不是"CI 已验证可达"。**2.1pp 余量很薄**：首次
  GitHub/Linux CI 运行若低于 80%，先看日志断言是否通过（确认口径没有退回单包），
  再按 Linux 实测值校准门槛——那是校准，不是回退；不要把首次运行的红当成代码退步。

- **`Has` 结果缓存：评估后拒绝。** 缓存要正确，必须有可靠的"条目变了"失效信号；而本项目的
  真实威胁恰恰是**同一进程内内容被外部改写**（`Has` 的基准注释已记录这个信号无法消除）。
  若按"路径 + 大小/mtime"缓存，就地等长改写（或 mtime 粒度相同）会命中旧的 `true`，于是
  `verify`/`install` 会把已确证的篡改读成"字节齐全"——这正好绕过 D1-重做用 `CheckEntry`
  区分"条目缺失 vs 内容不符"的全部意义。缓存把一次"检查"变成一次"猜"，收益（基准里
  0.93ms 的全命中页缓存）与错误代价不对等，故继续不缓存。

- **P2-10 选 A（维持现状 + CI 冷构建探测），不 vendor、不引镜像。** 实测支持 A：在空
  `GOMODCACHE`、无 Gitee 凭据（`GIT_TERMINAL_PROMPT=0`、空 `GIT_CONFIG_GLOBAL`、
  `GIT_CONFIG_NOSYSTEM=1`）的条件下，`go build ./...` 14.2535414 秒通过，两个模块都能匿名
  取到。vendoring 的代价（`vendor/` 体积、每次升级重跑 `go mod vendor`、LICENSE 汇总，以及
  与本项目 `ngm.vendor/` 运行时证明存储的概念混淆）在"需要"出现之前就先付了。升级触发条件：
  ① CI 的 `cold-build-no-credentials` 连续失败（Gitee 不可达或模块转私有）→ 立即评估 B；
  ② 发布或供应链审计要求"离线可构建" → B；③ 若选 B，必须先把 `vendor/`（Go 构建输入）与
  `ngm.vendor/`（运行时证明存储）的区别写进 `docs/architecture/`；④ 若 B 的体积不可接受且
  Gitee 长期不可用 → C（模块镜像）。

## 已知 flaky（记录在案，不隐藏）

- ~~`TestV106WhyBoundsTheSearchEvenWhenNothingMatches`~~ **已修复（v0.131）**：
  原判据用 `elapsed > 500ms` 作为"工作量预算生效"的代理，并发高负载下 h7 验证时
  实测 645.97ms 判红（同一 worktree 单独复跑 20.4183ms PASS）。现在改为**结构性断言**：
  `findPaths` 额外返回访问计数，判据钉住"预算恰好在 `MaxWhyWork+1` 次访问处停手"，
  再注入小预算（5 次）证明参数真的控制停点；生产入口 `FindPaths` 另行钉住
  `truncated=true`。预算值未调大、未加 retry——只把墙钟代理换成了它代理的那个量。
  复跑读数：3 路并发 × `-count=20`（共 60 次）叠加全量套件负载，全部 PASS。

- 其它同类**墙钟上界**用例（本轮只登记，不改；阈值相对被测耗时均留 8–10 倍余量，
  暂未观察到抖动，若出现按 V106 同一思路改成计数/结构断言）：
  - `TestV17WhyPathsAreBoundedAndSaySo`（`internal/observability/limits_test.go`，100ms 上界）
  - `internal/adapter/waitdelay_test.go`（20s 上界）
  - `internal/core/git/waitdelay_test.go`（20s 上界）
  - `internal/core/vendor/store_scale_test.go`（`Usage()` 30s 上界）

## 验收（每批）

- [x] `go build ./... && go vet ./...`
- [x] `go test ./...`
- [x] 新增判据在默认 `go test` 中执行
- [x] JSON 改动有字节级单文档断言
- [x] 权限改动有"零命中"断言

依据：A–G 按"一主题一提交"，每个提交都在**独立干净 worktree**（`git worktree add --detach`
到该提交）上跑 `go build ./...`、`go vet ./...`、`go test -count=1 -timeout 40m ./...`，
三读数全 0 才提交/推送；P2-3 另有旧/新二进制的 `--help`、`--bogus`、`--json` 错误面逐字节
对照（ALL-IDENTICAL）。
