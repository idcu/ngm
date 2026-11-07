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
- [x] G2 覆盖率门槛（CI，verify ≥ 55%，实测下界）+ 3 个 fuzz 目标 + [`t.Skip` 审计](../../internals/test-skip-audit.md)（59 → 57）
- [x] G3 cat-file 流式（digest 重放不保留 blob；判据 `TestV130StreamedDigestMatchesRetainedMaterial`）。`Has` 缓存**评估后继续拒绝**：已有基准注释记录的失效信号（同进程内被外部改写）无法消除，缓存会把"检查"变成"猜"
- [x] G4 构建可离线性：[评估](../../internals/build-offline-assessment.md)（实测无凭据冷构建 14.3s 通过；决定维持现状 + CI 探测）

## 验收（每批）

- [ ] `go build ./... && go vet ./...`
- [ ] `go test ./...`
- [ ] 新增判据在默认 `go test` 中执行
- [ ] JSON 改动有字节级单文档断言
- [ ] 权限改动有"零命中"断言
