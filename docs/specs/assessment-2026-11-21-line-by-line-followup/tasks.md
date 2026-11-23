# Tasks：2026-11-21 全量逐行代码审计之后

来源：[spec.md](./spec.md) · 依据 [全量逐文件代码审计](../../analysis/ngm-line-by-line-audit-2026-11-21.md)。
基线：`b6cfad8`。状态：**用户已授权全量推进（2026-11-21）**；6 项契约决定已拍板，35 项发现执行中。
每项完成标准 = 源码/测试改动 + 会红的判据 + 变异检验结果；只写"测试通过"不算完成。

## 契约决定（用户授权后拍板，2026-11-21；spec §3 正文保留为当时的建议快照）

- [x] **C1 §3.1**：取 (b)——`--json` 失败时 stdout 恰好一份错误信封，**不写 `writeReport`**；
      成功路径继续写报告。实施时核对 `TestV51`/`TestV136` 是否依赖旧形状，有则按新契约改写并记理由。
- [x] **C2 §3.2**：取 (b)——"接受 `--json` 的命令，任何参数级失败都给信封"写成 ADR-045 补记 +
      行为级枚举判据（按命令表覆盖，不做源码扫描网）。
- [x] **C3 §3.3**：取 (a)——`LSRemote` 入口做 `ValidateRemoteURL`；C-03 同时接受 40/64-hex、
      畸形行 fail-closed 返回 error。
- [x] **C4 §3.4**：取 (a)——net/env 含逗号 target 一律拒绝，ADR-012 补记 + CHANGELOG 破坏性变更说明。
- [x] **C5 §3.5**：取 (a)——`ValidateRecord` 失败=critical(2)，文件读取/JSON 语法失败=operational(4)，
      SARIF/GitHub 级别由**最终退出码**派生（含 strict/allow-drift 调整）。ADR-039/045 补记。
- [x] **C6 §3.6**：lock `Parse` 追加一次 `Decode` 要求 `io.EOF`；模块清单先查 Path 唯一性，
      再按 `(Path,Mode,BlobSHA256)` 复合键排序。

> 实施时若源码事实推翻上述决定，以各批次的"实施改判"记录为准，不重写本节决定快照。
>
> **编号说明（2026-11-23）**：下文 V159–V169 是当时的**计划编号**，源码中从未存在，
> 不要按它们去 `go test -run` 找判据。2026-11-23 全量复扫新增的判据从 **V200** 起编号
> （V200–V208），以免与历史计划撞号；旧计划项的真实判据以各批"实施改判"记录为准。

---

## Batch A · 判据复健（12 项，无产品代码变更）

> ✅ **已落地（2026-11-21）**：12 项全部改完，定向集（V25/V27/V40/hint/V03×2/V137/V108 +
> V82/V12/V105/testutils）全绿；gofmt / diff-check 绿。
> **两处实施改判**（spec 初稿按审计报告描述，实际源码细节不同）：
> ① T-02 实际命令数是 26（其中 6 个公开跳过），不是注释里的 19；改为从命令表派生 eligible，
> 并要求"每方向至少 3/4 通过、跳过不超过 1/4"。
> ② T-05 的沙箱段落**插在逐条结论与总结之间**，不是整体追加在末尾；改为归一化短 SHA 后
> 摘除固定段落块再逐字比对（只做 HasPrefix 会假红）。
> 变异验证：T-01 把 supplyChain 映射到错误类型 → V25 红；T-06 在 ci.yml 注入逐包 floor → 红；
> C-04 把包装函数体改成分叉实现 → 红；其余纯文案/阈值型项目以"注入对应错误形态"为判据形状，
> 单独标注为结构性修复（不新增网）。

- [x] **T-01 `supplyChain` 字段表双向守护**（`cmd/ngm/doc_field_tables_test.go:58-62`、
      `cmd/ngm/doc_prose_fields_test.go:144-150`）
      - 动作：正向映射到具名类型 `SupplyChainConfig`，反向 `configReverseSections` 补登该类型。
      - 判据 V143（占位）：文档漏写一个 supplyChain 字段 → 红；写错一个字段名 → 红；还原 → 绿。
      - 注：只强化现有 V25/V27 网，不新建源码扫描网（ADR-049）。

- [x] **T-02 重复 `--dir` 判据去恒真**（`cmd/ngm/argument_spelling_test.go:177-181`）
      - 动作：给 `dupSkipped` 设上限、给 `dupGoodLast`/`dupBadLast` 设下限。
      - 判据 V144：把"所有命令的 good/bad 结果趋同"注入夹具 → 红；还原 → 绿。

- [x] **T-03 flag hint 判据补齐声明种类**（`cmd/ngm/flag_hint_guard_test.go:86-94`）
      - 动作：`declared` 捕获 `Int64`/`Uint`/`Uint64`，或复用 `flagsetBlocksOf` 派生。
      - 判据 V144（与上项共用或独立）：构造 `fs.Int64` 声明的 hint → 不再误报；去掉捕获 → 红。

- [x] **T-04 WASM 退出码测试名与断言统一**（`cmd/ngm/wasm_engine_acceptance_test.go:94-105`）
      - 动作：改名为"failing engine is exit 1"并改注释，或真正断言模块码透传（二选一，与行为契约一致）。
      - 判据 V145：名称/注释/断言三者一致性检查；把断言改成相反值 → 红。

- [x] **T-05 沙箱输出比较去 40 字节子串**（`cmd/ngm/sandbox_acceptance_test.go:88-90`）
      - 动作：改 `strings.HasPrefix(sandboxOut, plainOut)`，另校验追加段存在。
      - 判据 V145：把 `plainOut` 中部一个结论改成通过 → 红；还原 → 绿。

- [x] **T-06 覆盖率判据去恒假**（`cmd/ngm/v137_coverage_visibility_test.go:45-47`）
      - 动作：统计 `::error::<path> coverage` 的唯一路径集合，断言仅 `internal/core/verify`。
      - 判据 V146：在 ci.yml 注入一个逐包 floor → 红；还原 → 绿。

- [x] **T-07 HTTPS e2e 头部承诺纠偏**（`cmd/ngm/v095_https_fetch_e2e_test.go:39-42` vs `:74-87`）
      - 动作：改写头部"不关校验证"表述，与内联的 schannel 边界和 `GIT_SSL_NO_VERIFY=true` 一致。
      - 判据：docs/prose 型检查（不新增网）；复核注释与代码设置同向即可。
      - 风险：低；纯文档一致性，避免下一个人高估覆盖范围。

- [x] **T-08 OSV CheckNet 扫描改 AST**（`cmd/ngm/v108_osv_gate_test.go:65-70`）
      - 动作：替换 24 行子串窗口为复合字面量字段名解析。
      - 判据 V146：把 `CheckNet:` 字段注释掉（保留注释中的同名字样）→ 红；还原 → 绿。
      - 注：是**替换**过宽判据，不是新增网（ADR-049）。

- [x] **C-04 子路径判据去同义反复**（`internal/core/resolve/subpath_test.go:63-69`、
      `internal/core/resolve/addspec.go:132-134`）
      - 动作：不再比较函数与其别名；断言规范化结果一致，并纳入 config 侧入口。
      - 判据 V144：让 config 判定与 resolve 分叉 → 红；还原 → 绿。

- [x] **I-01 临时文件 glob 修正**（`internal/integrations/atomic_test.go:38,69`；
      `internal/integrations/scaffold.go:264-266`）
      - 动作：`*.ngm-tmp` → `*.ngm-tmp-*`。
      - 判据 V148：保留一个 `.ngm-tmp-<随机>` 残骸 → 红；清理 → 绿。

- [x] **I-02 adopt symlink 断言指向 fixture**（`internal/adopt/adopt_test.go:138-143`）
      - 动作：Lstat 改为 fixture 中真实创建的 `linked-workspace`。
      - 判据 V148：让 symlink 被静默忽略且 skipped 不增 → 红；还原 → 绿。

- [x] **I-03 行尾断言去掉恒假检查**（`internal/testutils/testutils_test.go:189-192`）
      - 动作：删除 hash 输出查 `CRLF` 的断言，或改校验 `git cat-file -p` 的 blob 字节。
      - 判据 V148：把 `git show` 检查失效 → 红；还原 → 绿。

---

## Batch B · 错误通道与 JSON 契约（5 项，需 C1/C2 先拍板）

> ✅ **已落地（2026-11-21）**：5 项全部改完；新判据 `TestV143`~`TestV146`（cmd/ngm/
> v143_json_contracts_test.go）+ `TestV136` + `TestV51` + `TestV66` 全绿。
> **实施改判**：C1 的"失败不写报告"不能按退出码抑制——`verifyOnLock` 的漂移 exit 1 是结论；
> 改用 `jsonErrorSink` 记录"信封已写"来抑制。ADR-045 补记 + CHANGELOG 已同步。
> 变异：抑制条件改回 `!jsonOut` → V143 红；modules 一处改回直接 return → V144 红；
> dry-run 的 `--json` 分支改 `if false` → V145 红；config OK 提前打印 → V146 红。

- [x] **A-01 `install --json` 错误信封 writer**（`cmd/ngm/install.go:138-141,148,156,356-381`；
      `cmd/ngm/main.go:83-95`；`cmd/ngm/install.go:801-815`）
      - 动作：给 `installFresh`/`installFromLock` 独立 `errOut`（真正 stdout）；进度文本仍走 `humanOut`；
        按 C1 决定失败时不写 `writeReport`。同型检查 `update.go:310`。
      - 判据 V149：`--json` 下策略拒绝/ResolveGraph 失败/晾晒期门禁，stdout 恰好一份错误信封且无报告；
        非 `--json` 路径行为不变。变异：把 errOut 改回 humanOut → 红。
      - 同步：ADR-045 补记 + CHANGELOG；README 追加前查 `wc -l`。

- [x] **A-02 `modules` 7 处位置参数失败给信封**（`cmd/ngm/modules.go:208-211,223-226,262-265,315-318,442-445,487-490,588-591`）
      - 动作：全部改走 `c.rejectExtraArgs`，顶层两处同样处理。
      - 判据 V150（行为级枚举，按命令表覆盖所有 JSON-aware 命令的 extra-arg 失败）：删掉一处调用 → 红。
      - 同步：`TestV136` case 表补 `modules`。

- [x] **A-05 `integrations` 位置参数失败给信封**（`cmd/ngm/integrations.go:55-58`）
      - 动作：同 A-02 走 `c.rejectExtraArgs`。
      - 判据 V150：覆盖到 `integrations --json`；改回直接 return 3 → 红。

- [x] **A-06 `modules index --dry-run --json` 报告形状统一**（`cmd/ngm/modules.go:662-667,676-685,735-739`）
      - 动作：dry-run 也写 `modulesIndexReport{Version:1,...,DryRun:true,Index:idx}`；裸索引只在非 `--json` dry-run 输出。
      - 判据 V151：dry-run 与普通 `--json` 报告有相同顶层字段形状，`dryRun=true`；变异：写裸 idx → 红。

- [x] **A-03 `config validate` 假通过**（`cmd/ngm/config.go:109-117`）
      - 动作：把 `ngm.json OK` 打印移到权限策略与 Conflicts 检查之后。
      - 判据 V152：全局配置写 `permissions.allow=["bogus:foo"]` → stdout 无 OK、exit 3；还原 → 绿。

---

## Batch C · 安全边界与校验一致性（8 项，需 C3/C4 先拍板）

> ✅ **已落地（2026-11-21）**：8 项全部改完（V-03 虽列在 Batch E，实施时与 V-01/V-02
> 同在 module_cache.go 一起完成）。`go test ./internal/core/git ./internal/core/vendor
> ./internal/security` 全绿，cmd/ngm integrations 定向全绿，vet 绿。
> 新判据：`TestLSRemoteRejectsHelperURL`、`TestParseLSRemoteOutput_AcceptsSHA256`、
> `TestParseLSRemoteOutput_RejectsMalformed`、`TestPlanSandboxRejectsCommaTargets`、
> `TestModuleCacheEnsureValidatesCommitAfterFirstClone`、`TestV147GitRunCapturesAreBounded`。
> ADR-012 补记 + CHANGELOG 已同步（逗号权限为破坏性变更）。
> 变异：去掉 URL 校验 / 恢复静默跳过 / 去掉逗号拒绝 / 去掉 commit 校验 / 恢复无界 buffer → 对应判据全红。

- [x] **A-04 `integrations add` 逃逸校验跳过**（`cmd/ngm/integrations.go:92-99`；对照 `cmd/ngm/mappings.go:105-114`）
      - 动作：读 manifest 失败时回退默认 vendor 根继续校验，或显式 fail-closed（二选一，与 mappings validate 同口径）。
      - 判据 V153：manifest 不可读 + 越界 `to` → exit 3；变异：改回静默跳过 → 红。

- [x] **C-01 批量模块材料漏路径校验**（`internal/core/git/tree.go:592-598`，对照 `:255-266`、`:648-663`）
      - 动作：`BuildModuleMaterials` 对每条 `e.Path` 调 `digest.ValidatePathAndMode`，或复用 `materialFromEntries`。
      - 判据 V154：子树含反斜杠/换行文件名 → install 与模块证明路径同判拒绝；变异：删校验 → 红。

- [x] **C-02 `LSRemote` URL 形状校验**（`internal/core/git/lsremote.go:31-40`；调用方 `resolve/infer.go:21-27`、
      `resolve/reftype.go:203-213`）
      - 动作：入口先 `ValidateRemoteURL(url)`，与 clone 同构造点口径。
      - 判据 V155：`ext::sh -c ...` 形状的 URL 在 ls-remote 路径被拒；变异：删校验 → 红。

- [x] **C-03 ls-remote 解析 40/64-hex + fail-closed**（`internal/core/git/lsremote.go:47-69`；
      `internal/core/git/tree.go:443-453`）
      - 动作：接受 40/64-hex；畸形行返回 error 而不是 `continue`。
      - 判据 V156：64-hex 响应解析出 ref；畸形行返回错误；变异：改回静默跳过 → 红。
      - 前置：跑全量 `cmd/ngm` 确认无 fixture 依赖旧行为。

- [x] **P-01 含逗号权限放大**（`internal/security/sandbox.go:97-108,130-131,157-158`；
      `internal/security/permissions.go:75-96`）
      - 动作：net/env target 复用 ReadDirs 的逗号 fail-closed。
      - 判据 V157：`allow: ["net:a.example,b.example"]` 被拒；放行时 argv 只含单一 target。变异：删拒绝 → 红。
      - 同步：ADR-012 补记 + CHANGELOG 破坏性变更说明。

- [x] **V-01 首次 clone 不校验 commit**（`internal/core/vendor/module_cache.go:97-100`，doc `:62-65`）
      - 动作：clone 成功后同样 `HasCommit` / 必要时 `FetchPartialCommit`。
      - 判据 V158：预置不含目标 commit 的远端，空缓存下 Ensure 不得报 `Created=true`；变异：删校验 → 红。

- [x] **V-02 fetch 门禁用错 URL**（`internal/core/vendor/module_cache.go:109-112`；`internal/core/git/clone.go:141`）
      - 动作：复用 Mirror 的"读真实 origin + 失败回退更严 URL"逻辑。
      - 判据 V159：缓存 origin 为受限地址、调用方传已授权 URL → 门禁仍拒绝；变异：改回用调用方 URL → 红。

- [x] **C-05 git 通用 Run 输出无界**（`internal/core/git/exec.go:97-99`；`internal/core/git/runallow.go:28`）
      - 动作：`boundbuf` 或流式读取 + 截断传播；保留 Result.Stdout 现有语义。
      - 判据 V160：超大输出触发可检测的截断而非无限增长；变异：改回无界 buffer → 红。

---

## Batch D · 退出码与语义分类（5 项，需 C5 先拍板）

> ✅ **已落地（2026-11-21）**：5 项全改；`go test ./internal/core/verify ./internal/observability
> ./internal/config` 全绿。新判据 `TestV148RenderLevelsFollowExitSemantics`、
> `TestV149ForgedManifestIsTypedAsIntegrityFailure`、`TestV150TreeAndWhyAgreeOnSelfLoop`、
> `TestV151MergeProjectCopiesAllReferenceFields`。变异四项全红。
> ADR-039 / ADR-045 补记 + CHANGELOG 已同步。

- [x] **K-01 SARIF/GitHub 级别与退出码同源**（`internal/core/verify/sarif.go:199-216`、
      `internal/core/verify/github.go:49-58`）
      - 动作：把 `rep.Strict`/`rep.AllowDrift` 传入级别映射，或按最终退出码派生 level。
      - 判据 V161：`--allow-drift` 下 unexpected 的 SARIF level 不为 error；`--strict` 下 expected 不为 warning/notice。
        变异：改回只看 DriftKind → 红。

- [x] **K-02 伪造模块清单归 critical**（`internal/core/verify/verify.go:878-895,1000-1028`）
      - 动作：`ValidateRecord` 类错误单独归 critical(2)；文件读取失败仍归 operational(4)。
      - 判据 V162：模块清单塞 NUL 路径 → exit 2；缺文件 → exit 4。变异：改回全归 operational → 红。

- [x] **K-03 版本 JSON 解析失败不再吞成空值**（`internal/core/verify/verify.go:705-711`）
      - 动作：解析错误向上返回 operational(4)，不再把 `""` 当版本比对成 critical。
      - 判据 V162：上游 ngm.json 语法损坏 + lock 带 version → exit 4 而非 2；变异：改回吞错误 → 红。

- [x] **P-02 tree/why 自环分歧**（`internal/observability/tree.go:139-147`；`internal/observability/why.go:283-287`）
      - 动作：`BuildTree` 复用 `childrenOf` 的自环过滤。
      - 判据 V163：构造 `RequiredBy` 含自身 Key 的 lock → tree 与 why 对同一图给出一致结论；变异：删过滤 → 红。

- [x] **P-03 `mergeProject` 浅拷贝**（`internal/config/config.go:456-478,100-108`；
      `internal/config/v066_merge_test.go:24-33`）
      - 动作：SupplyChain/Engines 指针与 Scripts map 逐字段深拷贝。
      - 判据 V164：改 Effective 的每个引用字段都不回写 Project；变异：恢复浅拷贝 → 红。

---

## Batch E · 确定性与信任根（5 项，需 C6 先拍板）

> ✅ **已落地（2026-11-21）**：4 项（V-03 已在 Batch C 同文件完成）全改；
> `go test ./internal/core/digest ./internal/core/lock ./internal/core/verify
> ./internal/adopt ./internal/version` 全绿。新判据 `TestV152ModuleManifestSortIsOrderIndependent`、
> `TestV153ParseRejectsTrailingContent`、`TestV154GitColonPrefixIsStripped`、
> `TestV155DottedPartsDoesNotOverflowToNegative`。变异四项全红。

- [x] **K-04 模块清单排序键**（`internal/core/digest/manifest.go:98-103`；`internal/core/digest/validate.go:66-73`）
      - 动作：先查 Path 唯一性（重复即拒），再按 `(Path, Mode, BlobSHA256)` 复合键排序。
      - 判据 V165：同一记录集合不同输入顺序得到相同清单字节与 digest；重复 Path 被拒。变异：改回单键 sort → 红。

- [x] **K-05 lock 尾部垃圾**（`internal/core/lock/io.go:37-51`）
      - 动作：`Decode` 后再 `Decode` 一次要求 `io.EOF`；对象内未知字段兼容不变。
      - 判据 V166：合法 lock 后追加第二个 JSON 对象 → Parse 报错；未知字段仍忽略。变异：删第二次 Decode → 红。

- [x] **V-03 首次 clone 非原子落地**（`internal/core/vendor/module_cache.go:97-99`；对照 `mirror.go:151-169`）
      - 动作：落地目录 + 原子 rename；clone 失败清理残骸（对齐 Mirror v0.101 修复）。
      - 判据 V167：clone 中途失败不留下最终 path 半成品；并发首次 clone 不互报 destination exists。
        变异：改回直落 path → 红。

- [x] **I-04 adopt 的 `git:` 前缀残留**（`internal/adopt/adopt.go:583-597`）
      - 动作：按 `git+` / `git://` / `git:` 分别处理，只剥离非 `//` 的 `git:`。
      - 判据 V168：`git:ssh://host/repo#<sha>` 产出的 Provenance.URL 不含 `git:`；`git://` URL 保持原样。
        变异：改回只 TrimPrefix("git+") → 红。

- [x] **I-05 版本号整数溢出**（`internal/version/parse.go:37-64`）
      - 动作：`DottedParts`/`atoi` 逐段检查 `n > (MaxInt-d)/10`。
      - 判据 V169：`v99999999999999999999.0.0` 不产生负 major；正常 tag 解析不变。变异：删上界检查 → 红。

---

## 全批通用收口

- [x] 每个提交都附变异检验记录（撤掉修复即红、装上即绿），不能只写"测试通过"。
      五批的变异结果记在各批"✅ 已落地"段；V143–V155 共 13 条新判据全部验证会红。
- [x] 每批合入前跑定向集；最终跑全量 `go test ./... -count=1 -timeout 60m`
      （Windows 用进程级 `PSExecutionPolicyPreference=Bypass`）。**结果**：所有 internal 包全绿；
      cmd/ngm 的失败仅剩已知环境组（V79/V86/V87/V88/V90/V95/V102：safe-delete 注入、PS wrapper、
      git-http-backend、中文乱码），与本次改动无关。GitHub CI 该提交 **24/24 success**。
- [x] docs-links / discipline 13/13 L4 / build / vet / `git diff --check` / `TestV114`（CRLF 归一化 gofmt）全绿。
- [x] 本轮**未新增 ADR**，全部用 ADR-012/039/045 补记，故 ADR 计数不变（51）；
      README 保持 599 行；`TestV71`/`TestV142` 绿。
- [x] 审计报告 [ngm-line-by-line-audit-2026-11-21.md](../../analysis/ngm-line-by-line-audit-2026-11-21.md)
      §6 已补"已授权全量执行并落地"的索引；spec 顶部补实施改判说明，§3 保留拍板前快照。
