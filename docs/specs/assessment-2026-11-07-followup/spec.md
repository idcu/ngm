# Spec：2026-11-07 评估后的修复批次

依据：`docs/internals/assessment-2026-11-07-full.md`（问题 ID、证据、批次）。
状态：A、B 已合入 main；C（P1-1…P1-4）已完成，四个代码提交各自在干净 worktree
通过 `go build ./...` + `go vet ./...` + 全量测试；D–G 待批准。
禁止强推、改历史、改 git config。

## 0. 结论

本轮只做三件事，按顺序：

1. **止血**：`--dry-run` / `engines list` / `engines validate` 不得 spawn 未授权程序（P0-1）。
2. **修契约**：`--json` 单一文档、`verify` 单一退出码、`gc` 错误信封（P0-2）。
3. **补边界**：离线、lock 路径、git 环境、adopt pnpm（P1-1…P1-4）。

其余（存储命中校验、install 顺序、文档、CI、性能）按 §8.4 的 C/D/E/F/G 批次跟进。
**不新增功能**；本轮所有改动都是"实现追上已有承诺"。

## 1. 不变量（任何批次都不得破坏）

- 退出码 0–6 的单码语义不变；`ngm run` 仍透传子进程码。
- `run:<engine>`、`run:git`、`net:<host>`、`run:script`、`postInstallPolicy`
  五套闸门不合并、不互相放行。
- git 只有一个子进程构造点（`internal/core/git/spawn.go`）；HTTP 出口只在
  `queryRemote` 登记。
- 沙箱脚本"只能否决"，不得改变 ngm 自身结论。
- 人类输出与 `--json` 同源；`--json` 时 stdout 恰好一份 JSON 文档。

## 2. 逐文件改动与判据

### Batch A — P0-1（安全闸门）

| 文件 | 改动 |
|---|---|
| `internal/adapter/runner.go:191-227` | `Plans` 拆出 `probe` 开关；`--dry-run` 传 false；所有 `Available()`/`Version()` 收进单一函数，函数内先判定策略 |
| `internal/adapter/catalog.go:412-432,515-554` | `Validate` / `probeEngine` 接收 policy 与 deniedEnv；拒绝 → `CodePermission`(3) |
| `internal/adapter/subprocess.go` | `Available()` 用 `<dir>/.bin` + 进程 PATH 合并解析；`Version()` 使用 `DeniedEnv` |
| `cmd/ngm/engines.go:275-315` | 探测前判定策略；缓存键加解析后的绝对路径 |
| `cmd/ngm/build.go:25` | usage 改为 "render only, never spawn" |

判据（必须真跑，禁止 `t.Skip`）：

- `TestPlansDryRunSpawnsNothing`：子进程计数 = 0。
- `TestEnginesListHonorsRunDeny`：`deny:["run:probe"]` → exit 3，探针零命中。
- `TestSpawn_OnlyOneFileBuildsProcesses` 仍绿。
- `TestV109EnginesValidateDeniedIsFailClosed`：探针被 `run:<engine>` 拒绝时
  `engines validate` 显式产出 `probe-denied` 条目、计入摘要、exit 3；
  人与 `--json` 两路都必须点名要授予的 `run:<engine>`。

#### A 的语义取舍：`engines list` 如实呈现，`engines validate` fail-closed

两者都受 `run:` 闸门约束，探针被拒绝时**都不 spawn**；区别在职责：

| 命令 | 职责 | 探针被拒时 | 退出码 |
|---|---|---|---|
| `engines list` | **如实呈现**事实 | 逐行标 `probeDenied` / `runTarget`，版本留空 | 0 |
| `engines validate` | **给结论** | 显式 `probe-denied` 条目，计入 `issues` 与摘要，点名 `run:<engine>` | 3（`CodeConfigInvalid`，与 `CodePermission` 同值） |

理由：`list` 回答"现在看到什么"，"没问"是一个事实而不是失败；`validate`
回答"清单是否可用"，**拿不到结论就不能报成功**——跳过核对而不产出问题，
等于把权限缺失伪装成校验通过。`ExitCode` 的优先级里 `probe-denied`(3)
高于可用性(5)：先补授权，才谈得上"没装"。

该差异同时写入 `docs/guides/cli.md`（命令表 + 独立段落）。

### Batch B — P0-2（输出与退出码契约）

| 文件 | 改动 |
|---|---|
| `cmd/ngm/audit_hook.go:48-104` | hook 失败不得向 stdout 写第二份信封 |
| `cmd/ngm/verify.go:326-377,490` | `finishVerify` 取"最高严重度"（2 不可被 5 覆盖）；摘要打印合成码 |
| `cmd/ngm/gc.go:129-195` | 配置/lock 失败同样出 JSON 错误信封 |
| `internal/core/errs` | 新增 `MergeCodes`，优先级单一裁决点 |

判据：

- `TestAuditJSONHookIsSingleDocument`：stdout 只有一个顶层 JSON 对象。
- `TestVerifyCriticalBeatsMissingDeno`：返回 2，`summary.exitCode == 2`。
- `TestGCJSONConfigFailureEnvelope`：stdout 一份信封、exit 3。

### Batch C — P1-1…P1-4（边界）

| 项 | 文件 | 判据 |
|---|---|---|
| P1-1 离线 | `resolve/reftype.go:16-33,230-247`、`cmd/ngm/tree.go:127-132`、`cmd/ngm/verify.go` 的 ref 解析 | 冷 mirror + `--offline` → 4；`CloneURL` 零调用；`resolve/graph.go` 的嵌套解析已走 `EnsureMirror`（离线语义在 mirror 层），无需额外字段 |
| P1-2 lock 路径 | `lock/schema.go:277-315`、`verify/verify.go:847`、`verify_sandbox.go:30`、`cmd/ngm/env.go:285-320` | 表驱动逃逸用例全拒；`EvalSymlinks` 越界拒绝 |
| P1-3 git 环境 | `git/exec.go:158-201`、`clone.go`、`remoteurl.go` | `GIT_CONFIG_*`/`GIT_SSH_COMMAND`/`GIT_ASKPASS` 被剔除；非 git scheme 拒绝 |
| P1-4 adopt pnpm | `adopt/adopt.go:140-160,255-288` | pnpm 夹具包数 > 0；零包提示不再指向"重装" |

### Batch D — P1-5 / P1-6（存储与 install）

| 项 | 文件 | 判据 |
|---|---|---|
| P1-5 Has 可证明 | `vendor/content.go:125-157`、`content_v2.go:313-318`、`cmd/ngm/install.go:409-414` | 删除 blob / 截断 manifest 后必须重修，不得硬链 |
| P1-6 install 顺序 | `cmd/ngm/install.go:186-192,361-369,528` | hook 失败后 vendor 与 lock 均无半写状态 |

## 3. 批次与提交

| 批次 | 内容 | 前置 | 提交粒度 |
|---|---|---|---|
| A | P0-1 | — | 1 提交（adapter + engines + build + 3 个测试） |
| B | P0-2 | — | 1 提交（audit/verify/gc/errs + 3 个测试） |
| C | P1-1…P1-4 | B | 4 提交，每项独立 |
| D | P1-5 / P1-6 | C（P1-2） | 2 提交 |
| E | P1-7 / P2-1 / P2-7 | — | 按项 |
| F | P2-2 / P2-3 / P2-4 / P2-6 | A | 按项 |
| G | P2-5 / P2-8 / P2-9 / P2-10 | 独立 | 按项 |

耦合：P1-2 → P1-5 → P1-6 是同一信任根链，必须按序；P1-1 与 P2-3 有文件冲突，
P1-1 先合。

## 4. 回归门槛

每个批次合并前必须：

1. `go build ./... && go vet ./...`
2. `go test ./...`（`cmd/ngm` 允许 10 分钟）
3. 新增判据必须在默认 `go test` 里执行，不得依赖 `-run` 手工触发。
4. 涉及 JSON 的改动补一条"字节级单文档"断言。
5. 涉及权限的改动补一条"拒绝时子进程零命中"断言。

## 5. 批准点

- 批准 Batch A + B 即开始实现；C 之后每个批次开始前再确认一次。
- 若发现 S0-1 的修复会改变 `ngm engines list` 的既有输出（例如未授权引擎从
  "未安装"变成 exit 3），**先停下来确认**：这会影响 CI 对 engines 的读数。
