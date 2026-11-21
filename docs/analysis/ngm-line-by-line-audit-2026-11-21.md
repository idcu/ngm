# ngm 全量逐文件代码审计（2026-11-21）

> **审计口径**：实现文件逐行读完，测试文件逐文件读完；每条候选发现都由审计者给出精确行号，
> 再由主审逐条回到源码行复核。基线的 HEAD 为 `7457693`（工作树干净，CI 24/24）。
> 这与 [竞品全维度评估](./ngm-competitive-gap-full-2026-11-21.md)（维度覆盖 + 关键读数实测）和
> [2026-11-20 全量代码评估](./ngm-full-assessment-2026-11-20.md)（146 个非测试实现文件的按包审查）
> 是**不同粒度**的三次评估；本文补的是"测试文件与全部实现文件都逐文件读过"这一层。
>
> **本轮只读，不改产品代码**。本文是审计输入，不是事实源；命令行为的权威仍是代码与
> `docs/guides/cli.md`，退出码契约的权威是 `contracts/exit-codes.json`。

---

## 0. 结论摘要

- 覆盖 **479 个 Go 文件 / 约 94,047 行**（全仓 Go 共 95,454 行，覆盖率约 98.5%），
  10 个分组，无整文件跳过。
- 共产出 **36 条候选发现**；逐条复核后 **35 条确认**（P1 9 条、P2 24 条、P3 2 条），
  **1 条误报剔除**（详见 §4.1）。
- **没有发现 P0**，也没有发现可远程利用的 RCE、token 泄漏或沙箱逃逸。项目的核心证明链
  （refType → commit → digest → 离线重放 → verify）依然扎实；本轮发现集中在三类：
  1. **错误通道绑定**：`--json` 的错误信封被写进人读 writer，或早期失败绕过信封（A-01/02/05）；
  2. **跨路径校验缺口**：同一条校验在一条路径上有、在另一条同型路径上漏掉（A-04、C-01、C-02、K-02）；
  3. **判据空转**：断言恒真、glob 永不匹配、源码扫描允许注释蒙混——"看家但咬不动"（T-02/06/08、I-01/02/03、C-04）。

---

## 1. 审计口径、分组与覆盖

| 组 | 范围 | 文件 / 行数 | 方式 |
|---|---|---|---|
| 1 | `cmd/ngm` 核心命令实现（15 文件） | 15 / 4,108 | 逐行通读 |
| 2 | `cmd/ngm` verify/sbom/attest/audit 实现（8 文件） | 8 / 1,729 | 逐行通读 |
| 3 | `cmd/ngm` 其余命令实现（20 文件） | 20 / 5,354 | 逐行通读 |
| 4 | `cmd/ngm` 测试文件 a–m | 93 / 17,928 | 逐文件通读 |
| 5 | `cmd/ngm` 测试文件 n–z | 93 / 15,493 | 逐文件通读 |
| 6 | `internal/core/git` + `resolve` | 69 / 11,041 | 实现逐行 + 测试逐文件 |
| 7 | `internal/core/vendor` | 39 / 9,517 | 同上 |
| 8 | `internal/core` 其余六个小包 | 35 / 6,433 | 同上 |
| 9 | `internal/{supplychain,security,config,mappings,observability}` | 49 / 10,615 | 同上 |
| 10 | `internal` 适配/集成/工具包 | 58 / 11,829 | 同上 |

**复核流程**：审查者给出行号 + 证据原文 + 复现场景 → 主审对每条发现重新打开该文件相应行段，
并沿调用链核对 writer 绑定、函数别名、契约注释与既有 ADR → 只有源码支持的才进入矩阵。
凡 2026-11-20 评估已修复项、已有 ADR 明确决定项，一律不作为新发现。

**证据边界**：复现场景是根据源码与控制流推导的，本轮**没有逐条执行变异测试来证明它会红**；
报告中的"建议"是修复方向，不是已验证的补丁。判定"已确认"指**代码事实与行号已复核**，
不指已实测触发。需要把某条发现升级为行动项时，应先写一个会红的判据。

---

## 2. P1 发现（9 条）

### A-01 · `install --json` 的错误信封写进人读 writer
- **位置**：`cmd/ngm/install.go:138-141,148,156,356-381`；`cmd/ngm/main.go:83-95`；`cmd/ngm/install.go:801-815`
- **证据**：`humanOut := stdout; if *jsonOut { humanOut = stderr }`，随后 `installFresh(..., rep, humanOut, stderr)`；
  `installFresh` 内部失败路径全部是 `runErr(ctx, stdout, ...)`，而 `runErr` 在 `wantsJSON(ctx)` 时把信封写到这个 `stdout` 形参。
- **影响**：`install --json` 中途失败（策略拒绝、ResolveGraph 失败、晾晒期门禁、Store.Put 失败）时，
  错误信封进 stderr 混在人读文本里；stdout 仍由 `emitWriteReport` 写出一份 `writeReport`，
  形成"机器可读通道里没有错误信封"的契约违背（`main.go:81-82`、`ADR-045`）。
- **修法**：给 `installFresh` / `installFromLock` 增加独立的 `errOut`（真正的 stdout），
  进度文本继续走 `humanOut`；同型检查 `update.go:310` 的 `autoVerifyAfterLock`。
- **复核**：✅ `runErr`、`emitWriteReport` 与调用链均逐行核过。原候选称"stdout 为 0 字节"不准确，已修正为上述表述。

### A-02 · `modules` 的 7 处位置参数失败绕过 JSON 信封
- **位置**：`cmd/ngm/modules.go:208-211,223-226,262-265,315-318,442-445,487-490,588-591`
- **证据**：`if fs.NArg() != 0 { fmt.Fprint(stderr, modulesUsage); return 3 }`，顶层无子命令/未知子命令同型。
- **影响**：`modules` 声明了 `JSON: true`，五个子命令都接受 `--json`，但位置参数失败既不走
  `c.rejectExtraArgs`（`cmd_context.go:122-128`）也不走 `failEarly`，stdout 空；
  与 `install --json extra` 给信封的承诺相反。已修复的 P1-4 只迁移了 5 个命令，`TestV136` 的 case 表也不含 `modules`。
- **修法**：五处改 `return c.rejectExtraArgs(modulesUsage, msg)`；顶层两处在 `runModules` 内按同一出口处理。

### A-03 · `config validate` 在校验完成前打印 OK
- **位置**：`cmd/ngm/config.go:109-117`
- **证据**：`fmt.Fprintln(stdout, "ngm.json OK")` 在 `newPermissionPolicy(r.GlobalEffective.Permissions)` 之前。
- **影响**：全局配置写 `permissions.allow=["bogus:foo"]` 时，stdout 已有 `ngm.json OK`，
  而进程 exit 3——与本文件 `:47-48` 刚修过的"假通过"同型；按 stdout 文本判定的 CI 会误绿。
- **修法**：把成功打印移到 `pol.Conflicts()` 检查之后。

### A-04 · `integrations add` 读不到 manifest 时静默跳过 vendor 逃逸校验
- **位置**：`cmd/ngm/integrations.go:92-99`；对照 `cmd/ngm/mappings.go:105-114`
- **证据**：`if pf, perr := env.ReadManifest(); perr == nil && pf != nil { findings, verr := mappings.Validate(...) }`。
- **影响**：读 manifest 失败时整块 `EscapesVendor` 校验被跳过，而 `mappings validate` 在同样情况下
  会留出 `VendorRoot=""` 继续校验（回退协议默认根）。于是"被 mappings validate 拒绝的别名"能被
  `integrations add` 写进 `tsconfig.paths` / `vite.resolve.alias`，构建消费 vendor 之外的代码。
- **修法**：与 `mappings.go` 同口径回退默认根继续校验，或显式 fail-closed。

### C-01 · 批量模块材料构造漏掉路径校验
- **位置**：`internal/core/git/tree.go:592-598`（对照 `:255-266`、`:648-663`）
- **证据**：批量路径直接 `metas = append(metas, digest.Record{Path: e.Path, Mode: e.Mode})` 后 `finishMaterial`；
  单模块路径 `materialFromEntries` 先调 `digest.ValidatePathAndMode`，而 `validateTreeEntry` 只校验 mode。
- **影响**：`:530` 注释声称"模块作用域与根作用域使用同一套条目校验"，但路径校验缺失。
  子树含反斜杠/换行的文件时，安装路径拒绝、模块证明批量路径却算出清单 digest，verify 再拒绝
  → "装得出、验不过"或 digest 分叉（v0.136 修复的同型遗漏）。
- **修法**：`BuildModuleMaterials` 对每条 `e.Path` 调 `ValidatePathAndMode`，或复用 `materialFromEntries`。

### C-02 · `LSRemote` 无 URL 形状校验，`ext::` helper 执行面未封
- **位置**：`internal/core/git/lsremote.go:31-40`；`internal/core/git/remoteurl.go:10-35`；
  调用方 `internal/core/resolve/infer.go:21-27`、`internal/core/resolve/reftype.go:203-213`
- **证据**：`args := []string{"ls-remote", url}` 直接交给 `Run`；`ValidateRemoteURL` 只在 `clone.go:34,99` 调用。
- **影响**：两个调用方只做 `CheckNetAccess`（策略判定），没有形状判定。`ext::sh -c ...` 会被 git 作为
  远程 helper 执行，与 `ValidateRemoteURL` 注释要阻断的威胁同源。mirror config 的 `remote.origin.url`
  或 `GitURL` 一旦是这类形状，在线 verify / `--infer-ref-type` 就有 helper 执行面（默认 net-deny 部分缓解）。
- **修法**：`LSRemote` 入口先 `ValidateRemoteURL(url)`，与 clone 单构造点同口径。

### K-01 · SARIF / GitHub 注释级别不随 `--strict` / `--allow-drift` 变化
- **位置**：`internal/core/verify/sarif.go:199-216`；`internal/core/verify/github.go:49-58`
- **证据**：`sarifRuleFor` 只看 `DriftKind` 与 `Check`；`writeAnnotation` 只看 `c.Operational` 与 `DriftKind`。
- **影响**：`--allow-drift` 让 unexpected 的进程码降为 0，SARIF 仍 `level=error`；
  `--strict` 让 expected 升为 1，注释仍 `notice`。SARIF 自称"级别跟着阻断语义走，与退出码表同源"，
  实际与进程退出码相反；按 SARIF 阻断的 CI 和按退出码判定的 CI 会给出相反结论。
- **修法**：把 `rep.Strict` / `rep.AllowDrift` 传入级别映射，或明确文档化"级别只看漂移种类"。

### K-02 · 被篡改的模块 store 清单被分类为"未查成"(exit 4)
- **位置**：`internal/core/verify/verify.go:878-895,1000-1028`
- **证据**：`replayModuleDigestFromEntries` 对不可信清单做 `digest.ValidateRecord` 并返回 error
  （`"content store manifest is not a valid manifest"`），`checkLanding` 把 `derr` 一律 append 到 `operational`。
- **影响**：清单被塞入含 NUL 的路径时，这是**已确证的完整性失败**，却被报成操作性失败（exit 4）
  与"run `ngm install` 补齐资源"，与 v0.105 修过的"critical 被 operational 压过"同型。
- **修法**：`ValidateRecord` 类错误单独归入 critical（exit 2）；只有文件读取失败才归 operational。

### P-01 · 沙箱把含逗号的 `net:` / `env:` 权限拆成多条，造成权限放大
- **位置**：`internal/security/sandbox.go:97-108,130-131,157-158`；`internal/security/permissions.go:75-96`
- **证据**：`ReadDirs` 有逗号 fail-closed（`:86-91`），`NetHosts` / `EnvVars` 只做 `CheckExplicit`；
  `DenoArgs` 用 `strings.Join(g.Net, ",")` / `strings.Join(g.Env, ",")` 拼 flag；`Parse` 不禁止 target 含逗号。
- **影响**：`permissions.allow=["net:a.example,b.example"]` 在 ngm 侧是一条永远匹配不到真实主机的 target
  （用户视为无效），到 Deno 侧变成两个真实授权。与"判定主机 = 实际访问主机"的承诺相反，
  且发生在 `CheckExplicit` 通过之后的参数生成阶段（非 M11 的 nil 策略问题）。
- **修法**：对 `NetHosts` / `EnvVars` 复用 `ReadDirs` 的逗号拒绝逻辑。

---

## 3. P2 / P3 发现（26 条）

### 命令与 JSON 契约

| ID | 级别 | 位置 | 事实与影响 | 建议 |
|---|---|---|---|---|
| A-05 | P2 | `cmd/ngm/integrations.go:55-58` | `NArg()!=2` 直接写 usage + `return 3`，无信封；同机 `engines --json` 有完整信封（P1-4 遗漏） | 改用 `c.rejectExtraArgs` |
| A-06 | P2 | `cmd/ngm/modules.go:662-667,676-685,735-739` | `index --dry-run --json` 输出裸 `collectionIndex` 字节，非 dry-run 输出 `modulesIndexReport`（含 `version`/`index`），同一 flag 两种 schema | dry-run 也走 `writeJSONDocument` 包装报告 |
| T-01 | P2 | `cmd/ngm/doc_field_tables_test.go:58-62`；`cmd/ngm/doc_prose_fields_test.go:144-150` | `supplyChain` 映射到整目录 `internal/config/*.go` 的 tag 集合，反向网只登记 `ProjectFile`/`Dependency`，无 `SupplyChainConfig` → 正向过宽、反向缺席 | 映射到具名类型并补反向 section；补两个变异场景 |
| T-02 | P2 | `cmd/ngm/argument_spelling_test.go:177-181` | `dupGoodLast+dupSkipped` 与 `dupBadLast+dupSkipped` 恒等于 19，只守命令表缩水；所有 `--dir` 被忽略时仍绿 | 给 skipped 设上限、passed 设下限 |
| T-03 | P2 | `cmd/ngm/flag_hint_guard_test.go:86-94` | `declared` 只认 5 种 flag 方法，缺 `Int64`/`Uint`，而同类网 `flagRegistrationRe` 覆盖它们 → 会误报/漏报 | 复用 `flagsetBlocksOf` 派生 |
| T-04 | P2 | `cmd/ngm/wasm_engine_acceptance_test.go:94-105` | 测试名与注释说"透传模块退出码 7，CI 据此判断"，断言却锁定 exit 1 且明说不是 7 | 统一名字、注释与断言 |
| T-05 | P2 | `cmd/ngm/sandbox_acceptance_test.go:88-90` | 注释承诺"常规输出完全一致"，断言只 `Contains(plainOut[:40])`，40 字节后的逐条结论被改动不红 | 改 `HasPrefix` 并单独校验追加段 |
| T-06 | P2 | `cmd/ngm/v137_coverage_visibility_test.go:45-47` | `mustContain` 已强制存在 `::error::internal/core/verify coverage`，故 `Count(...)==0` 恒 false；新增逐包 floor 不红 | 统计 `::error::<path> coverage` 的唯一路径集合 |
| T-07 | P3 | `cmd/ngm/v095_https_fetch_e2e_test.go:39-42` vs `:74-87` | 文件头宣称"不关证书校验"，内联注释说明 schannel 下只能用 `GIT_SSL_NO_VERIFY`，且 `:87` 确实设置它 | 改写头部承诺，与内联说明一致 |
| T-08 | P3 | `cmd/ngm/v108_osv_gate_test.go:65-70` | 在 `OSVConfig{` 后 24 行窗口内找 `CheckNet:` 子串，注释写 `// CheckNet: ...` 即可蒙混 | AST/词法解析复合字面量的字段名 |

### Git / 解析 / 缓存

| ID | 级别 | 位置 | 事实与影响 | 建议 |
|---|---|---|---|---|
| C-03 | P2 | `internal/core/git/lsremote.go:47-69`；`internal/core/git/tree.go:443-453` | 解析硬编码 40-hex，畸形行静默 `continue`；同仓已支持 64-hex SHA-256 → SHA-256 仓库全部 ref 被丢弃，误报 ref not found | fail-closed 返回 error，接受 40/64-hex |
| C-04 | P2 | `internal/core/resolve/subpath_test.go:63-69`；`internal/core/resolve/addspec.go:132-134` | 注释称"三处入口"，实际只比较 `ValidateSubPath` 与其别名 `validateDepSubPath`（`return ValidateSubPath(p)`），恒等；config 入口未纳入 | 改为断言规范化结果一致并纳入 config 入口 |
| C-05 | P2 | `internal/core/git/exec.go:97-99`；`internal/core/git/runallow.go:28` | 通用 `Run` 的 stdout/stderr 用无界 `bytes.Buffer`；WASM/subprocess 已有 `boundbuf`，大树的 `ls-tree -r -z` 输出整体驻留内存 | 改 `boundbuf` 或流式读取 + 截断检测 |
| V-01 | P2 | `internal/core/vendor/module_cache.go:97-100`；doc `:62-65` | 文档承诺"最终仍会校验 commit 可解析"，但首次 clone 后直接返回，未调 `HasCommit`；fetch 分支才校验 | clone 成功后同样校验，补对等判据 |
| V-02 | P2 | `internal/core/vendor/module_cache.go:109-112`；`internal/core/git/clone.go:141` | net 门禁用调用方传入的 `remoteURL`，实际执行 `git fetch ... origin`；Mirror.Ensure 专门读真实 origin 做 fail-closed，ModuleCache 没有这层 | 复用 Mirror 的"读真实 origin + 失败回退更严 URL"逻辑 |
| V-03 | P2 | `internal/core/vendor/module_cache.go:97-99`；对照 `mirror.go:151-169` | 首次 clone 直落最终 `path`，无落地目录 + 原子 rename，clone 中途失败无清理；Mirror 在 v0.101 已修同型残骸 | 落地目录 + rename + 清理失败残骸 |

### 确定性 / 错误分类 / 配置共享

| ID | 级别 | 位置 | 事实与影响 | 建议 |
|---|---|---|---|---|
| K-03 | P2 | `internal/core/verify/verify.go:705-711` | `moduleVersionAt` 对 JSON 解析失败 `return "", nil`，`checkModuleProof` 把空串当版本比对 → 上游 ngm.json 语法损坏时报 critical(exit 2) 而非 operational(exit 4) | 解析错误向上返回 |
| K-04 | P2 | `internal/core/digest/manifest.go:98-103`；`internal/core/digest/validate.go:66-73` | 排序假设 "Path 唯一"（git tree 成立），但模块 scope 输入是可改写 store 清单，`ValidateRecord` 不查唯一性，`sort.Slice` 不稳定 → 相同记录集合不同初始顺序得到不同 digest | 加唯一性校验或用 `(Path,Mode,BlobSHA256)` 复合键 |
| K-05 | P2 | `internal/core/lock/io.go:37-51` | `Parse` 只 `Decode` 第一个 JSON 值，不检查尾部多余内容；lock 是信任根且"同一份字节既解析又哈希"，尾部追加对象后解析结构与被哈希字节不是同一份文件 | `Decode` 后再 `Decode` 一次要求 `io.EOF` |
| P-02 | P2 | `internal/observability/tree.go:139-147`；`internal/observability/why.go:283-287` | `why` 过滤自环 `parent == n.Key`，`tree` 不过滤 → 同一张含自环的图，`why` 给 0 条路径、`tree` 多一个 cycle 节点 | `BuildTree` 复用 `childrenOf` 的自环过滤 |
| P-03 | P2 | `internal/config/config.go:456-478,100-108`；`internal/config/v066_merge_test.go:24-33` | `mergeProject` 只深拷贝 `Vendor`，`SupplyChain`/`Engines` 指针与 `Scripts` map 仍共享；v0.66 判据只断言 `Runtime` 与 `Vendor.LinkMode`，绿灯证明不了注释承诺 | 逐字段深拷贝并补断言 |

### 测试判据与工具函数

| ID | 级别 | 位置 | 事实与影响 | 建议 |
|---|---|---|---|---|
| I-01 | P2 | `internal/integrations/atomic_test.go:38,69`；`internal/integrations/scaffold.go:264-266` | glob `*.ngm-tmp` 匹配以 `.ngm-tmp` 结尾的文件，实际命名是 `.ngm-tmp-<随机>` → "临时文件已清理"断言永久空转 | glob 改 `*.ngm-tmp-*` |
| I-02 | P2 | `internal/adopt/adopt_test.go:138-143` | `os.Lstat(t.TempDir()+"/probe-symlink")` 查的是新建空目录里不存在的路径，恒不触发；fixture 真实链接是 `linked-workspace` | 改为 Lstat fixture 中的真实链接 |
| I-03 | P2 | `internal/testutils/testutils_test.go:189-192` | `git hash-object` 输出是 40 位 hex，不可能含 `CRLF`，断言恒假；有效校验在其后 `:194-197` | 删除该条或改校验 blob 字节 |
| I-04 | P2 | `internal/adopt/adopt.go:583-597` | 入口接受 `git:` 前缀，剥离只处理 `git+` → `git:ssh://...#<sha>` 的 `Provenance.URL` 残留 `git:` | 按 `git+` / `git://` / `git:` 分别处理 |
| I-05 | P2 | `internal/version/parse.go:37-64` | `DottedParts` / `atoi` 的 `cur = cur*10 + d` 无上界，超长数字串回绕为负 → tag major/minor/patch 被解析成负数，outdated 比较可误判 | 逐段检查 `n > (MaxInt-d)/10` |

---

## 4. 剔除项与判定说明

### 4.1 唯一误报：`audit` 的 `exitCode` 不同步

候选称 `cmd/ngm/audit.go:144-145` 写报告后才跑 hook，`audit_hook.go:117-123` 失败时只返回
`errs.ExitCode(err)`，导致报告里的 `exitCode:0` 与进程码 5/6 矛盾。复核后**剔除**：
`internal/supplychain/report.go:42-43` 明确规定 "`ExitCode` 只在本包裁定 `0`/`1`（策略判定）；
`3`、`4` 作为错误返回而不进报告"。hook 失败属于附加层错误，按契约本就不写回报告字段。
若认为该契约对消费者不够清晰，正确动作是文档化而非改代码。

### 4.2 对 A-01 的修正

审查者原话是"stdout 为 0 字节"；复核 `emitWriteReport`（`install.go:801-815`）后确认 stdout 仍会收到
一份 `writeReport`。缺陷成立，但准确表述是**错误信封错投 stderr、stdout 只有成功形状的报告**，
而不是"没有输出"。本报告按核实后的事实记录。

### 4.3 未计为发现的情况

- 2026-11-20 评估已修复项（P0-1 net 门禁、P1-1~7、M 系列、v0.136 各批）本轮确认已落地，不重复计数。
- 已有 ADR 明确决定的项（ADR-014 不自签、ADR-027 未签名 attestation、ADR-044 定位边界、
  ADR-050 默认 P1D、ADR-051 verify 默认级别）按决定执行，不作为缺陷。
- 项目刻意保留的本机环境失败组（V79/V86/V87/V88/V90/V91/V95/V102 等）不属于代码缺陷。

---

## 5. 建议优先级

1. **先补 P1 的契约与边界**：A-01/A-02/A-05（错误信封通道）、A-03（假通过）、A-04/C-01/C-02
   （校验与 URL 形状遗漏）、K-01/K-02（退出码语义）、P-01（权限放大）。每条都先写一个会红的判据再修。
2. **再清判据空转**：T-02/T-06/T-08/I-01/I-02/I-03/C-04 属于"绿灯不代表守住了承诺"，
   修它们比新增功能更能提高可信度；用变异验证确认判据真的会红。
3. **确定性/信任根**：K-04/K-05 涉及 lock 与 digest 字节口径，改前先固定回归语料。
4. **P3 文档漂移**：T-07 顺手改，避免下一个人据错误头部理解测试覆盖。

> **不要做的事**：不要把 P2 的"可改进"包装成 P0 去触发破坏性重构；A-01/A-02 这类 writer 修复
> 必须保住 v0.16 规则④（不接受 `--json` 的命令 stdout 为空）与"单个 JSON 文档"契约。

---

## 6. 相关文档

- [2026-11-20 全量代码评估](./ngm-full-assessment-2026-11-20.md)（实现文件按包审查）
- [竞品全维度差距评估](./ngm-competitive-gap-full-2026-11-21.md)（14 维度 + 竞品一手证据）
- [全量代码评估后续任务](../specs/assessment-2026-11-20-followup/tasks.md)
- [ADR-044：独立生态定位](../adr/adr-044-independent-ecosystem.md)
- [ADR-045：JSON 输出契约](../adr/adr-045-json-output-contract.md)
- [退出码契约](../../contracts/exit-codes.json)
