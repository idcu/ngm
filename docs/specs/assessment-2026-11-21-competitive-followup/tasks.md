# Tasks：2026-11-21 竞品差距评估之后（全维度推进）

来源：[spec.md](./spec.md) · 依据 [竞品全维度差距评估](../../analysis/ngm-competitive-gap-full-2026-11-21.md)。
状态：**全部完成**（A1/A2 · B1/B2 · C1/C2 · D1/D2/D3/D4 共 10 项；基线 `dedf89b`）。
每项判据带变异检验（或注明 N/A 的理由）；两处**实施时安全改判**已在正文记录（A2、B1）。
勾选项证据 = 判据名 + 变异检验结果（撤掉修复即红、装上即绿）。

## 实施时的两处安全改判（与 spec 初稿不同，以本节为准）

1. **A2**：spec 初稿写"`invocationId` 当前为空串，改为 lock 哈希"——**过时**。源码 v0.70 起
   `invocationId` 已经是 lock 字节哈希（`attest.go:143` + `TestV70StatementShape:184` 守着）。
   实际工作只剩"补时间字段 + `--with-timestamps`"，不改 invocationId。
2. **B1**：spec 初稿写"逃生口 = `minimumReleaseAge: P0D`"——**不可行**。`ParseISODuration`
   有意拒绝零时长（"配了却不生效"的错觉，duration.go:112）。改判为：项目级逃生口 =
   **完全不配 `supplyChain` 段**（ADR-009 底线：没配策略不门禁）；单命令逃生口 = `--allow-fresh`。
   不修改 duration.go 的既有安全决定。

---

## Batch A（合规产物，低风险）

- [x] **A1 `ngm sbom` 升 CycloneDX 1.7 + schema 判据**
  - ✅ 已落地：`internal/sbom/cyclonedx.go:95` specVersion "1.5"→"1.7"；注释、`sbom.go:35`、
    `capability-matrix.md:27`、`README.md:178` 同步。
  - 判据：`TestV138CycloneDX17SchemaShape`（新文件 `cyclonedx17_test.go`：specVersion 是字符串
    "1.7"、文档头三件套、每组件 bom-ref/name/version/purl/hashes、dependencies 按 bom-ref 引用——
    带图 entries 避免 omitempty 假红）。**变异**：specVersion 改回 "1.5" → 红；还原 → 绿。
  - 兼容决定：不加 1.5 兼容开关（SBOM 无书面承诺依赖 1.5 的外部消费者；有需求再按 pnpm
    `--sbom-spec-version` 形状加）。理由写进本节防重开。

- [x] **A2 `attest` 谓词补 SLSA v1 时间字段（契约 §3.1 取 (b)）**
  - ✅ 已落地：`internal/attest/attest.go` 的 `metadata` 加 `StartedOn`/`FinishedOn`（*string omitempty），
    `Options` 加 `WithTimestamps`/`Timestamp`；`cmd/ngm/attest.go` 加 `--with-timestamps` flag +
    usage/help；cli.md、ADR-027 补记、CHANGELOG 同步。**invocationId 保持为 lock 哈希（本就是）**。
  - 判据：`TestV70TimestampsAreOptIn`（新文件：默认无时间字段 / 开启后 RFC3339 且等于注入时刻 /
    invocationId 不受开关影响 / 同 Timestamp 两次同字节 / flag 非空转）；默认 `TestV70Deterministic`
    保持绿。**变异**：`if opts.WithTimestamps && false` → 红（编译通过的真实红）；还原 → 绿。
  - 不改 byproducts：ngm 无 byproducts 可报，空字段违背"不填的字段一律省略"纪律。

## Batch B（默认安全姿态，破坏性变更）

- [x] **B1 `minimumReleaseAge` 默认 P1D + 逃生口（ADR-050）**
  - ✅ 已落地：`internal/supplychain/policy.go` 加 `DefaultMinimumReleaseAge = 24h`，
    `FromConfig` 在"有段无字段"时用它（`sc==nil` 仍不门禁，ADR-009 底线）；
    加 `Policy.WithoutMinimumAge()` 副本方法；install/update 加 `--allow-fresh` flag +
    usage + stderr warning；ADR-050 入册、CHANGELOG `Unreleased` Added+Changed 段。
  - 判据：`TestV139DefaultReleaseAge`（有段无字段=24h / 无段=0 / P0D 仍被拒 / 副本只关冷却且
    白名单+严重级别+postInstall 不变、不泄漏回原策略）；`TestCheckMinimumAge` 表加两行
    （默认 P1D 挡新 commit / 放过老 commit）。**变异**：默认值改回 0 → 红（见上方说明）。
  - **行为变更打中两处现有测试**（均由全量 cmd/ngm 抓到，定向测试漏过）：
    ① `TestCheckMinimumAge/no_policy_never_blocks` 原用空字符串=不门禁，新语义下=P1D → 按新语义改写；
    ② `TestV124HookFailureRollsBackNewVendor/sandbox_runner_is_broken` 的 fixture 写了
    `postInstallPolicy`（有 supplyChain 段），新默认 P1D 把它挡在 exit 3，而该测期望"沙箱坏→exit 5"
    → 加 `--allow-fresh` 隔离无关门禁；③ `TestV02VerifyOnLockAcceptance/verifyOnLock_re-checks...`
    的 fixture 写了 `verifyOnLock:true`（有段），被挡在 exit 3，该测期望 install 成功→同样加 `--allow-fresh`。
    这三处都是全量 cmd/ngm 抓到、定向测试漏过——**行为变更必须跑全量**。
    另：`TestCheckMinimumAge` 之外，`postinstall/sandbox/supplychain/exit_codes/report` 等 fixture 测试
    定向全绿（79s），确认只有这 2 个端到端用例受影响。
  - 破坏性变更三件套齐全：ADR-050 + CHANGELOG + 两处逃生口（不配段 / --allow-fresh）。

- [x] **B2 verify 默认级别：action content / CLI structure（ADR-051）**
  - ✅ 已落地：`action.yml` 的 `verify-level` 默认 structure→content，description 与 bash 注释
    写明"CLI 默认 structure"；CLI（`verify.go`）不动；ADR-051 入册、CHANGELOG 同步。
  - 判据：`TestV140ActionVerifyLevelDefaultsToContent`（新文件：action.yml 默认 content +
    说明点出 CLI 默认 structure + verify.go 的 CLI 默认仍是 structure）。**变异**：默认值改回
    structure → 红；还原 → 绿。
  - 明确不做：CLI 默认改 content（选项 a）——把 CI 强默认加给本地循环，理由写进 ADR-051 防重开。

## Batch C（签名与第三方背书）

- [x] **C1 cosign 签名 CI 配方 + 判据**
  - ✅ 已落地：新文档 `docs/guides/signing.md`（三角色表 + GitHub Actions keyless 配方：
    install → verify --content → attest --with-timestamps → cosign attest-blob → verify-blob-attestation
    + 三条说破点）；docs/README 加入口。
  - 判据：`TestV141SigningRecipeIsReproducible`（新文件：文档存在 + 命令序列完整 attest<sign<verify
    + 钉死 certificate-identity/oidc-issuer + 无 ngm 自签命令 + 声明 ngm 不签名不管密钥）。
    **变异**：删掉配方里的 verify 步骤 → 红；还原 → 绿。
  - 边界：不提供签名 smoke 脚本（签名工具在 ngm 之外，固化第二实现只会漂移）；判据守配方文档，
    不冒充跑过签名。ADR-014/027 的"不自签"未翻转。

- [x] **C2 外部审计立项备忘（非工程）**
  - ✅ 已落地：`docs/analysis/external-audit-brief-2026-11-21.md`（三块范围 + 验收标准 +
    明确不在范围 + 前置/触发条件）。不虚构审计结论；当前证据仍是自证 demo。

## Batch D（结构债测量 + 采用，高风险批只做测量）

- [x] **D1 `cmd/ngm` 拆包可行性（→ 数字报告，不改结构）**
  - ✅ 已落地：`docs/analysis/cmd-ngm-split-feasibility-2026-11-21.md`。实测：非测试 43 文件 /
    11,172 行 / 9 golden / 报告渲染跨 36 文件（writeJSONDocument 15 + emitWriteReport 7 调用点）。
    **口径纠正**：旧 ADR-047 的"44 文件/2.2 万行"已过时。
  - 结论：ADR-047 三条触发条件均未达成，本轮不拆；给出"若拆先抽报告渲染层"的边界。
  - 判据：纯报告，变异 N/A；数字附采集命令可复现。全量 cmd/ngm 判据绿（证明实验没污染主树）。

- [x] **D2 usage 双源成本审计（→ 量化结论，不改代码）**
  - ✅ 已落地：`docs/analysis/usage-dual-source-audit-2026-11-21.md`。实测 29 usage 常量 /
    28 命令 / verifyUsage **79 行**（旧 ADR-048 写 105 行，已过时）/ 1 个 AST 守卫 + 9 golden。
    结论维持双源；重启条件写明。

- [x] **D3 契约网存量复核（只读，不删网）**
  - ✅ 已落地：`docs/analysis/contract-net-review-2026-11-21.md`。严格口径 AST 类 8 个 +
    源码文件读取类 6 个；ADR-049 两条删除门槛均无候选 → 保留全部。

- [x] **D4 发布回填就绪清单（非工程，凭据阻塞）**
  - ✅ 已落地：`docs/analysis/release-backfill-readiness-2026-11-21.md`。
    **口径纠正**：spec 初稿"回填 38 个"是错的——38 个是 `release-pipeline.md` 明确**不回填**的
    区间；实际可做仅 Gitee 侧补 3+1 个（需 `GITEE_TOKEN`），GitHub 维持 63。
    读数与 release-pipeline.md 同源（`TestV126` 守），不盲目补数字。

## 完成定义

全部 10 项满足：判据在册 + 变异检验（D 批报告类注明 N/A）+ CI 绿 + 文档同源 + 无新增未登记 `t.Skip`。
破坏性变更（B1/B2）的 ADR + CHANGELOG + 逃生口三件套齐全。
C2/D4 为非工程项，完成定义 = 清单入册 + 不虚构执行结果。
已在 [竞品差距评估报告](../../analysis/ngm-competitive-gap-full-2026-11-21.md) 顶部追加"已推进"注记。
