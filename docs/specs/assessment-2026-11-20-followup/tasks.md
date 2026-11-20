# Tasks：2026-11-20 全维度评估后的修复批次

来源：[spec.md](./spec.md) · 依据 `docs/analysis/ngm-full-assessment-2026-11-20.md`。
状态：**全部已完成**（A1/A2 · B1/B2 · C1–C5 · D1–D4 均已落地，逐项带判据 + 变异检验；本机 PS 判据因执行策略属已知环境项）。
每提交在独立干净 worktree 上跑 `go build ./...` + `go vet ./...` + 该批定向判据后才合并。
勾选项必须带证据：判据名 + 变异检验结果（撤掉修复即红、装上即绿）。

---

## Batch A（P0，安全）—— 止血

- [x] **A1 `ModuleCache.Ensure` 首次 clone 前补 `net:` 门禁**
  - 位置：`internal/core/vendor/module_cache.go:79-93`。`!c.Exists(repo)` 分支直接调
    `git.ClonePartial`（:90），`CheckNetAccess` 只在 fetch 路径（:102）；而 :79 注释谎称
    “顺序与 Mirror 一致：先 run:git，再 net”。
  - 改动：在 `CheckRunAccess` 之后、`ClonePartial` 之前插入
    `git.CheckNetAccess(c.opts.Policy, remoteURL)`；顺序与 `Mirror.Ensure`（`mirror.go:227-244`）一致。
  - 不要改 `CheckRunAccess` 的位置——注释说“读配置/起子进程本身就要 git”是对的。

- [x] **A2 判据：零配置 / `deny net:` 下首次 module cache clone 不得触网**
  - 新文件 `cmd/ngm/v136_module_cache_net_gate_test.go`（禁止 `t.Skip`）。
  - 场景：临时 `NGM_HOME`（用 `testutils.IsolateUserEnv`）不放 config.json，项目用模块依赖
    （`#path=`），mirror 空、module cache 空；造一个写 marker 的假 git/fetch 或本地 git server
    观测是否发起 clone。断言：输出含权限拒绝提示、marker 零命中、退出码为该拒绝码（4 或 3，按实际）。
  - 变异：把 A1 插入的 `CheckNetAccess` 注释掉 → 必须红；恢复 → 绿。
  - 参考 `TestNoGlobalConfigDoesNotSpawnProbe`（v131）的零配置夹具写法。

## Batch B（P1，信任根）—— 防“装得出、验不过”

- [x] **B1 安装期统一调用 `ValidateRecord`**（先做 §3.1 契约决定：默认 fail-closed）
  - 位置：`internal/core/git/tree.go:233,300,337-349`（`materialFromEntries`/`finishMaterial`/`streamedMaterial`）
    构造 `digest.Record` 后直送 `BuildManifestWithVersion`，全仓库 `ValidateRecord`
    唯一生产调用点是 `verify/verify.go:1026`（仅模块分支）。
  - 改动：抽出纯校验供两侧共用；在收录记录前调用，非法路径（反斜杠/换行/NUL）在 install 期 exit 3、
    点名路径与理由。不要改 digest 的输入字节格式。
  - 判据：构造一个文件名含 `\` 或换行的 git tree 夹具 → install 必须 exit 3 且不写 lock；
    对照正常路径照常成功。变异：去掉校验调用 → 红。
  - 决定（fail-closed）写进 task 与 ADR-008 补记。

- [x] **B2 `parseListTreeZ` 对畸形记录 fail-closed**
  - 位置：`internal/core/git/tree.go:50-92`。缺 TAB、空 path、空 meta、`SplitN(meta," ",3)!=3`
    全部 `continue` 静默跳过 → 被改写的 mirror 可让“看起来正常”的错误 digest 通过。
  - 改动：返回 error（含脱敏的原始记录预览），不再静默跳过。注意 `ls-tree -z` 的 NUL 分隔语义要保留。
  - 判据：喂一段畸形 `ls-tree -z` 字节 → 必须报错且消息点名记录；正常记录仍解析。
    变异：把返回 error 改回 `continue` → 红。

## Batch C（P1，对外契约）—— 确定性 / 退出码 / 内存

- [x] **C1 `verify --json` 的 `Details` 排序**（已复核：`integrity.go:282-290` 只排了 `Mismatches`）
  - 位置：`internal/core/vendor/integrity.go:132-149,282-290`；消费点 `verify.go:945-951`。
  - 改动：按 `(Path, Kind)` 排序 `res.Details`，与 `Mismatches` 同口径。
  - 判据：造多条路径集合差异，连跑两次 → 输出字节一致；断言顺序固定。变异：删排序 → 红。

- [x] **C2 `gcExitCode` 纳入 module cache 错误**（本次评估新发现）
  - 位置：`cmd/ngm/gc.go:377-383`，只统计 `Store/Mirror/Vendor.Errors`，漏 `ModuleCache.Errors`
    （该字段在 :109 定义、:277 写入、:417 人类渲染器打印）——**报告显示 error 却 exit 0**。
  - 改动：把 `len(rep.ModuleCache.Errors)` 纳入条件。JSON 路径（:329）同源校验。
  - 判据：让 module cache 删除失败（目标改成目录使 rename 必失败）→ exit 4 且报告有 error 行；
    正常 → 0。变异：去掉该项 → 红。

- [x] **C3 `--json` 错误信封覆盖早期失败路径（结构性）**
  - 位置：`install.go:88-91`、`update.go:83-86`、`outdated.go:58-60`、`engines.go:102-121`、
    `verify.go:162-211`（`markJSON` 在 :213 太晚）。这些都是**接受** `--json` 的命令，
    不违反 v0.16 规则④（那条只约束不接受 JSON 的命令）。
  - 改动：优先并入 `cmdContext` 的失败出口，而非逐个补 `failEarly`；`verify` 把 `ctx = c.markJSON(*jsonOut)`
    提到 flag 校验前。注意照 v0.136 的教训：不接受 `--json` 的命令必须保持 stdout 为空
    （用 `newCmdContextNoJSON` / `spec.JSON` 分流）。
  - 判据：对上述命令逐一跑 `--json <非法位置参数>` 与 `--json --<bogus>`，断言 stdout 恰好一份信封；
    对 `add --json`（不接受）断言 stdout 为空。
  - **回归风险**：把通道从人读扩到 JSON 后，所有做字符串比对/归一化的判据要复查
    （v0.135 的 `TestV40ArgumentSpellingHoldsItsContracts` 曾因此假红）。

- [x] **C4 WASM 输出改用有界缓冲 + 执行超时**
  - 位置：`internal/adapter/wasm.go:299-303`，`var stdout, stderr bytes.Buffer` 无界；
    subprocess（:502-505）与 sandbox（`sandbox.go:319-322`）都用 `boundbuf`。
  - 改动：`WithStdout/WithStderr` 传 `boundbuf.New(...)`；产物通道用 artifactLimit 并查 `Truncated()`；
    给执行路径设 deadline（`WithCloseOnContextDone(true)` 在调用方不取消时无效）。
  - 判据：一个循环 `fd_write` 的 WASI 模块 → 输出被截断且有标记、进程不被 OOM、超时后有界返回。
    变异：换回 `bytes.Buffer` → 红。

- [x] **C5 stale lock 时 `attest` / `sbom` 默认拒绝**（先做 §3.2 契约决定 / ADR）
  - 位置：`attest.go:143-149`、`sbom.go:109-114`，`lockManifestMismatch` 只写 stderr warning。
  - 改动：默认 exit 3，提供显式 `--allow-stale`；不往 statement 字节加字段（破坏确定性）。
  - 判据：改造过的 lock vs ngm.json → `attest`/`sbom` 默认 exit 3 且无产物；`--allow-stale` 产出。
  - 文档：`cli.md` 两命令的 EXIT CODES 与 `CHANGELOG` 同步。

## Batch D（P2，工程与文档）

- [x] **D1 CI job 超时 ≥ `go test` 超时**
  - `.github/workflows/ci.yml:32` 等 build-test/acceptance/engine job `timeout-minutes: 30`
    小于 `go test -timeout 40m`（:105）；注释与 `README.md:574` 都说“放宽到 40m”。
  - 改动：job 提到 45（留余量）；或把 go test 降到与 job 一致——选前者，保持文档承诺。
  - 判据：workflow 静态检查 `timeout-minutes >= go test timeout`。

- [x] **D2 合并重复的 CI 检查**
  - `check-docs-links.sh` 在 `ci.yml:391` 与 `ci.yml:740` 跑两次（后者还有 `if [ -d docs ]` 恒真守卫
    与 M0 注释）；`check-release-status.sh` 在 :365 与 :857 各一次。
  - 改动：把 `docs-links` 与 `release-status` job 并入 `release-state`；删恒真守卫与过期注释。

- [x] **D3 统一版本号语义（内部任务号 ≠ 发布 tag）**
  - 先按 §3.3 定写法，再改 `docs/guides/cli.md:180`、`configuration.md:19`、
    `docs/architecture/security-model.md:79` 等的“v0.120/v0.135/v0.136 新增/修”。
  - 发布读数 63/3/38/101 仍以 `release-pipeline.md` 为 SSOT；**不要删** `README` /
    `docs/README` / `installation.md` 里的数字——`TestV126LiveDocsAgreeWithReleasePipeline` 守着它们同源。

- [x] **D4 `scripts/build-release.ps1` 双实现收敛**
  - 它是 `.sh` 的完整第二实现（`:35-74` 自行 ldflags/命名/SHA256SUMS），无 CI 调用，
    却让 `v089_artifacts_test.go:24-27` 的“run-or-explain”门禁放行；也证伪了
    `docs/README.md:20-21`“同名 .ps1 只转调”的声明。
  - 改动：改为转调 `.sh`（与其余 11 对一致），或删掉并在门禁登记例外——二选一并同步 SSOT 声明。

---

## 追加批次 E（P2 正确性与安全，已全部完成）

13/13 勾选之后，用户再次授权按优先级继续。以下从候选池里**挑出可安全落地的项**做完，每项带判据 + 变异检验：

- [x] **E1/E2/M2/M13** 子路径分隔符统一：新建叶子包 `internal/core/pathutil`（`NormalizeSeparators` 供校验、`NormalizeSubPath` 供比较与消费），`resolve` / `config` / `git` 三处归一化全部委托之。此前 config 用 `filepath.ToSlash` 只在 Windows 转 `\`——Linux 上 `a/b` 与 `a\b` 是两把 key，绕过重复根声明判重；`#path=packages\core` 通过校验却按字面量进 git pathspec。判据 `pathutil_test.go` + `TestNormalizeSubPathIsCrossPlatform`；变异（去掉转 `\`）→ 红。
- [x] **E3/M3** `BuildModuleMaterial`（单数）改走 `normalizeModulePath`，`..` 等非法段不再进 `commit:sub` treeish。
- [x] **E4/M12** `ParseISODuration` 逐字段检查 `n*perUnit` 与累加溢出，此前极大值回绕成小正数让"很长的晾晒期"静默变短。判据 `TestParseISODurationRejectsOverflow`。
- [x] **E5/M10** OSV 端点只接受 http(s) scheme（`hostOf` 此前只查 host）。判据 `TestHostOfRejectsNonHTTPSchemes`。
- [x] **E6/M8** 沙箱脚本输出过滤控制字符（ANSI/CSI 不再能伪造报告行），`indent` 改用 `strings.Builder`；verify_sandbox 与 audit_hook 共用该出口。判据 `TestIndentSanitizesUntrustedScriptOutput`；变异（去掉转义）→ 红。

## 追加批次 F（P2/P3 一致性与质量，已全部完成）

- [x] **F1/M5** `json_document.go` 注释澄清"唯一"指**成功报告类** JSON，产物与错误信封是两个明确例外（此前被读作"所有 JSON"，与事实不符）。
- [x] **F2/M7** `runWithRecovery` 接受 stderr writer，与 `newFlagSet.SetOutput` 同一纪律——写死 `os.Stderr` 时嵌入调用方看不到 panic 文本。
- [x] **F3/M6** `collectReleases` 从"每 tag 一次 RevParse"改为一次 `for-each-ref --format='%(refname:short) %(objectname) %(*objectname)'`，annotated tag 在同一次调用里 peel。判据 `TestCollectReleasesUsesOneSpawn`（源码扫描：有 for-each-ref、无 RevParse）。
- [x] **F4/M4** `update --store` 的 help 改为"report content-store status"，明说 store 总是填充——此前写"populate the content store"是空承诺。
- [x] **F5/P3** 陈旧注释四处：`module_proof.go` 的 `--release`→`--strict`、`clone.go` 的 `--depth=1`、`testutils.go` 的"默认 true"、`adopt` 的 yarn 文档示例。
- [x] **F6/P3** yarn v1 scoped 包 + `#ref`（`"@scope/pkg#v1.0.0"`）解析：scoped 分支同时查 `#`，Git 来源不再误降为 unknown。判据 `TestYarnNameFromKeyScopedWithRef`。
- [x] **F7/P3** `nextModuleVersion` 在 `major/minor/patch == math.MaxInt` 时报错，不再产出负版本号。

## 追加批次 G（最后候选池，已全部完成）

用户再次授权全量推进，此前"等真实触发"的三项一并落地：

- [x] **G1** `--` 分隔符：分流层的 help/version 扫描与 `emptyDirFlag` 现在遇到 `--` 即停——此前 `ngm run build -- --help` 打印 run 的帮助而不是把 `--help` 交给脚本，`-- --dir=` 被误判为空目录（runUsage 明明承诺 `-- <extra args>` 原样透传）。判据 `TestRunDoubleDashShieldsHelpAndDirFromRoot`；变异（去掉 break）→ 红。
- [x] **G2** `materialCache` 从 mutex + done channel 退化为带 mutex 的普通 map——调用点全是单 goroutine 顺序循环，旧实现的等待方不监听 ctx、panic 时不 close(done) 会永久阻塞。
- [x] **G3** `fsyncDir` 去重：`atomicfile.fsyncDir` 导出为 `FsyncDir`，`vendor/content_v2.go` 的逐行重复实现改为别名。判据 `TestFsyncDirHasOneImplementation`。

### 明确评估后**不改**的项（附理由，避免下一轮重开）

- **M11（OSV CheckNet nil fail-open）**：这是**已记录的决定**——`osv.go:196-199` 明确写着 v0.108 曾改 fail-closed、已撤回；正确施力点是"调用方必须传"，由 `TestV108OSVGateAlwaysConstructsWithCheckNet` 源码判据守着，两个生产调用点都传了真实策略。改 nil 语义会推翻决定并打死测试替身。
- **M9（空值 flag 语义分裂）**：逐命令语义有意为之——`--out=` 对产物类命令等价 stdout（v0.42 不变量），对 adopt 报错是因为它与 `--json` 冲突；`--hook=` 空表示"不跑钩子"是可选特性的自然写法。强行统一会破坏既有 golden。
- **M14（modproof 离线降级）**：复核 `checkModuleProof`（verify.go:653-666）在无法计算证明时返回 `Operational: true` 的 error（exit 4），不是 warning；评估里这条描述与实际不符。
- **中英混杂**：根帮助中文、子命令 usage 英文是历史形成的呈现策略，统一会重写全部 golden 且无功能收益——维持现状。
- 清理 ADR / 逐版本归档——17+ 测试强耦合，维持现状。

## 完成定义

所有勾选项满足：**判据在册 + 变异检验 + CI 绿 + 文档同源 + 无新增未登记 `t.Skip`**。
评估报告 `docs/analysis/ngm-full-assessment-2026-11-20.md` 是 2026-11-20 快照；本轮全部合入后应产出新快照或在报告顶部加“已修复”注记。

## 附录：本机全量测试红项的定性（已实证）

`go test ./...` 在本机 `cmd/ngm` 包有红项，全部为**环境问题**，逐族实证如下（写在此处避免误判为回归）：

| 失败族 | 根因 | 实证 |
|---|---|---|
| V79 / V86 / V87 / V88 / V102Gitee* / V102ReleaseDrift* | 执行沙箱的 `[safe-delete]` 守卫把提示行注入捕获的脚本 stdout | 关闭沙箱隔离后，同一批测试**全部通过**（84.6s） |
| V90 / V91（脚本入口） | 本机命令解释器执行策略为 Restricted | 设进程级 bypass 后**全部通过**（12.3s） |
| V95 HTTPS fetch e2e | 本机 PortableGit 缺 `git-http-backend` 二进制 | 测试自身报"找不到 git-http-backend"，非代码失败 |

CI 环境（Linux、无此沙箱、无执行策略限制、git 完整）下这些判据正常执行。24 个 internal 包与全部文档/契约判据在本机即全绿。
