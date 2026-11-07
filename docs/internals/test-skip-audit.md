# `t.Skip` 审计（2026-11-07）

> 状态：`done (P2-8)`。账本是 [TestV130EverySkipSiteIsAccountedFor](../../cmd/ngm/v130_skip_ledger_test.go)，
> 数字变了它就会红——改动请同步这一页。

## 1. 为什么要有这份账

`go test ./...` 的绿色有两种含义："断言都通过了"，或者"该跑的都没跑"。仓库里
59 处 `t.Skip` 里绝大多数是**真边界**（这台机器没有 deno/tsc/node，或没有开启
`NGM_REAL_UPSTREAM`），它们跳过是正确的；但同一句 `t.Skip` 也可以用来把
**漂移**藏起来：函数体只有一句 skip、错误面样例现在退 0、fixture 不再共享树。

审计的目的是把这两类分开，并让"新增一处 skip"成为一个**需要解释的动作**，
而不是一次静默的覆盖率下降：

- 门禁 `TestV130EverySkipSiteIsAccountedFor` 按文件比对 skip 调用点数；
- 真实引擎 job 里已有的"出现 `--- SKIP` 即失败"检查继续保留（那条针对
  "装了引擎却跳过"的失效形态）。

账本用的是**每个文件的个数**，不是行号：行号会因任何无关编辑漂移，会误报的
门禁最终会被忽略；个数能抓住"多了/少了一处 skip"，这正是需要人解释的动作。

基线：59 处 → 审计后 **57 处 / 39 个文件**（减掉的 2 处见 §4）。

## 2. 分类规则

| 类别 | 判定 | 允许 skip？ |
|------|------|------------|
| 外部工具缺失 | `deno` / `tsc` / `esbuild` / `node` / `ssh-keygen` / `sh` 不在 PATH | 是（换成断言只能在"装了工具的机器"上跑） |
| 平台能力缺失 | symlink、exec bit、大小写不敏感卷、Windows 权限位 | 是（能力本身不存在） |
| 环境条件 | 端口被占用、远端不可达、临时目录在同卷 | 是（前置条件不成立） |
| opt-in 测量 | `NGM_BENCH`、`NGM_REAL_UPSTREAM`、`NGM_STORE_SCALE_BLOBS`、`NGM_FIXTURES_OUT` | 是（默认 CI 不该触网/做长跑） |
| fixture 假设 | 两个 commit 没有共享子树等 | 是，但必须在 skip 里写清"为什么这个夹具不再成立" |
| 已登记排除 | 扫描器里显式列出的例外项（**先断言失败，再跳过附加契约**） | 是（例外本身被断言覆盖） |
| 漂移遮蔽 | 函数体只有 skip；或"预期失败"的样例成功后被跳过 | **否**——改成断言 |

## 3. 清单（57 处）

| 文件 | 处数 | 类别 | 说明 |
|------|-----|------|------|
| `cmd/ngm/acceptance_m3_test.go` | 2 | 工具 / fixture | git 缺失；同一 commit 不可得 |
| `cmd/ngm/acceptance_m4_test.go` | 1 | 平台 | symlink 不可用 |
| `cmd/ngm/acceptance_m5_test.go` | 1 | fixture | 两个夹具没有共享树 |
| `cmd/ngm/acceptance_m6_test.go` | 1 | 工具 | esbuild 不在 PATH |
| `cmd/ngm/acceptance_m7_test.go` | 2 | 工具 | esbuild / node |
| `cmd/ngm/adapter_real_engines_test.go` | 2 | 工具 | tsc / postcss |
| `cmd/ngm/bench_test.go` | 1 | opt-in | `NGM_BENCH` |
| `cmd/ngm/engine_floor_test.go` | 3 | 工具 | deno 缺失或版本过旧 |
| `cmd/ngm/fixtures_gen_test.go` | 1 | opt-in | `NGM_FIXTURES_OUT` |
| `cmd/ngm/fixtures_remote_test.go` | 2 | opt-in | 网络夹具 |
| `cmd/ngm/integrations_acceptance_test.go` | 3 | 工具 | node / esbuild / tsc |
| `cmd/ngm/library_standard_checker_test.go` | 2 | 工具 | node / bash |
| `cmd/ngm/p1_regressions_test.go` | 1 | 平台 | symlink |
| `cmd/ngm/real_upstream_test.go` | 2 | opt-in / 环境 | `NGM_REAL_UPSTREAM`；远端不可达 |
| `cmd/ngm/report_sweep_test.go` | 1 | 已登记排除 | 失败已断言，仅跳过 remediation 行契约 |
| `cmd/ngm/sandbox_acceptance_test.go` | 1 | opt-in | 30s 超时不属于 `-short` |
| `cmd/ngm/transform_real_engine_test.go` | 1 | 工具 | 真实引擎 |
| `cmd/ngm/typedecl_real_tsc_test.go` | 1 | 工具 | tsc |
| `cmd/ngm/v088_powershell_scripts_test.go` | 1 | 工具 | powershell |
| `cmd/ngm/v090_ps1_wrappers_run_test.go` | 1 | 工具 | powershell |
| `cmd/ngm/v091_gitee_offline_test.go` | 2 | 工具 | curl / python3；powershell |
| `cmd/ngm/v095_https_fetch_e2e_test.go` | 1 | 环境 | 443 已被占用 |
| `cmd/ngm/v102_diagnostic_test.go` | 1 | 工具 | python3 |
| `cmd/ngm/verify_signatures_test.go` | 1 | 工具 | ssh-keygen / git |
| `internal/adopt/adopt_test.go` | 1 | 平台 | symlink |
| `internal/core/atomicfile/atomicfile_test.go` | 1 | 平台 | Windows chmod |
| `internal/core/git/ancestry_test.go` | 1 | fixture | amend 后同一 commit id 不可得 |
| `internal/core/git/auth_test.go` | 2 | 环境 | 端口不可达；git 缺失 |
| `internal/core/git/signature_test.go` | 2 | 工具 | ssh-keygen / git |
| `internal/core/git/waitdelay_test.go` | 1 | 工具 | `sh` |
| `internal/core/lock/v066_write_test.go` | 1 | 平台 | Windows 权限位 |
| `internal/core/vendor/content_legacy_test.go` | 2 | 平台 | symlink |
| `internal/core/vendor/integrity_test.go` | 2 | 平台 | symlink |
| `internal/core/vendor/links_test.go` | 1 | 平台 | Windows exec bit |
| `internal/core/vendor/store_incomplete_test.go` | 1 | 平台 | symlink |
| `internal/core/vendor/store_scale_test.go` | 1 | opt-in | `NGM_STORE_SCALE_BLOBS` |
| `internal/core/vendor/v105_test.go` | 1 | 平台 | 大小写不敏感卷 |
| `internal/security/sandbox_test.go` | 2 | 工具 | deno 缺失 / 旧版本缺 flag |
| `internal/testutils/gitrepo_test.go` | 3 | 工具 / 平台 | symlink、exec bit、annotated tag |

## 4. 本轮修正的两处"漂移遮蔽"

| 位置 | 原来 | 现在 |
|------|------|------|
| `internal/core/resolve/reftype_test.go` | `TestResolveRef_NoGitInPath` 的函数体只有一句 `t.Skip`，"git 未安装 → exit 4 + 安装提示"从未被断言 | 把 `PATH` 指向空目录后调用 `ResolveRef`，断言 `CodeGitFetch` 且 hint 提到 git |
| `cmd/ngm/json_error_envelope_test.go` | 遍历只挑预期失败的样例，某条现在退 0 时 `t.Skipf` 静默跳过 | `t.Fatalf`：要么在 `surfaceCases` 里声明 `expectZero`，要么把失败修回来 |

## 5. 复核

```bash
go test -count=1 -run TestV130EverySkipSiteIsAccountedFor ./cmd/ngm
```

它失败时给出逐文件的 `ledger N, file has M`，并要求你决定：是真边界（在本页
登记并把账本加上），还是掩盖了缺口（改成断言）。
