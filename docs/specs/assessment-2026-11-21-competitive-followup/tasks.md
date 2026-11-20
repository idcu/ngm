# Tasks：2026-11-21 竞品差距评估之后（全维度推进）

来源：[spec.md](./spec.md) · 依据 [竞品全维度差距评估](../../analysis/ngm-competitive-gap-full-2026-11-21.md)。
状态：**全部 [ ] 未开始**（规划轮，未执行；用户"不怕风险"授权已体现在纳入 B/C/D 批，不改变"定位边界"）。
勾选项必须带证据：判据名 + 变异检验结果（撤掉修复即红、装上即绿）。
每提交在干净树跑 `go build ./...` + `go vet ./...` + 本批定向判据后才合并。

- [ ] **A1 `ngm sbom` 升 CycloneDX 1.7 + schema 合法性判据**
  - 位置：`internal/sbom/cyclonedx.go:95`（`SpecVersion: "1.5"` → `"1.7"`）、`internal/sbom/sbom.go:35` 注释、
    `cmd/ngm/sbom.go:19` usage。对手 pnpm 已产 1.7（`pnpm sbom`，CycloneDX 1.7 / SPDX 2.3），ngm 仍 1.5。
  - 改动：默认 specVersion 升 1.7；补 CycloneDX 1.7 JSON schema 的**合法性判据**（不需要外部依赖——
    用仓库内一份精简 schema 断言关键字段存在且类型正确：`bomFormat="CycloneDX"`、`specVersion="1.7"`、
    `metadata`/`components`/`dependencies` 结构、每个 component 有 `bom-ref`/`name`/`version`、
    hashes 的 `alg`/`content` 合法）。保持字节确定性。
  - 判据：`TestV138CycloneDX17SchemaShape`（断言 specVersion + 关键字段）；`TestV69SbomIsDeterministicAcrossRuns`
    保持绿。**注意**：1.7 的 `version` 是 4 字符字符串 `"1.7"`，不要写成数字；schema 修订 1.7.1 改的是
    modelCard 等可选字段，ngm 不用，不受影响（见 spec §3.1 的同类提醒）。
  - 变异：把 specVersion 改回 1.5 → 判据红；删掉某个必需 component 字段 → 判据红。
  - 兼容决定：不保留 1.5 兼容开关（ngm 的 SBOM 尚无外部消费者依赖 1.5 的书面承诺）；若后续有需求再按
    pnpm `--sbom-spec-version` 形状加。理由写进 tasks 防重开。
  - 注意：`TestV26` 守 cli.md 形状表——若改 usage 文本需同步。

- [ ] **A2 `attest` 谓词补 SLSA v1 的 `runDetails.metadata`（契约 §3.1 先拍板）**
  - 位置：`internal/attest/attest.go` 的 `metadata` struct（当前只有 `InvocationID`）、
    `builder` struct（`id`/`version` 已有）；`TestV70StatementShape`、`TestV70Deterministic`。
  - 现状：谓词已有 `buildDefinition.buildType` / `externalParameters` / `resolvedDependencies` 与
    `runDetails.builder`，**缺** `metadata.startedOn` / `finishedOn` 与 `byproducts`；`invocationId` 当前为空串。
  - 改动（契约取 §3.1 默认 (b)）：`metadata` 补 `StartedOn` / `FinishedOn`（`omitempty` 指针）、
    `byproducts` 字段；`invocationId` 改为** lock 字节哈希**（非空、确定性）而非空串；
    新增 `--with-timestamps`（显式开启才写时间，开启时该产物放弃字节确定性）。
  - 判据：`TestV70StatementShape` 扩展断言 `invocationId` 非空且等于 lock 哈希；
    `TestV70Deterministic` 默认路径保持绿；新增 `TestV70TimestampsAreOptIn`（带 flag 时字段出现且合法 RFC3339、
    不带时既不出现也无空对象）。**关键**：不要因为补字段就破坏 `encoding/json` 的字段顺序契约——
    结构体字段顺序即序列化顺序，测试要用完整字符串比较。
  - 变异：把 `invocationId` 改回空串 → 红；让 `--with-timestamps` 时仍不写时间 → 红；删掉 omitempty 导致默认输出多出空字段 → 红。
  - 同步：`cmd/ngm/attest.go` usage + help golden + `docs/guides/cli.md`；ADR-027 补记（不翻转"不自签"决定）。

## Batch B（默认安全姿态，破坏性变更，需先写 ADR）

- [ ] **B1 `minimumReleaseAge` 默认 P1D + 逃生口（独立安全理由，非"更像 npm"）**
  - 位置：`internal/config/config.go:179` 字段、`internal/config/duration.go`（`ParseISODuration` 已修溢出）、
    `internal/supplychain/policy.go:43-71`、`cmd/ngm/policy.go:27` `checkReleaseAges`。
  - 现状：门禁代码完整（`CheckMinimumAge` 按 commit 的 committer date 判），但配置为空时 `MinimumReleaseAge()=0`
    = **不门禁**；pnpm 11+ 默认 1 天、Deno 2.9 默认 24h 已是生态默认。
  - 契约决定（写 ADR 后实施）：默认值 **P1D**（24h，对齐两家）；逃生口 = `supplyChain.minimumReleaseAge: P0D`
    显式关闭 + `--allow-fresh` 单命令放行（需确认不与现有 flag 冲突）；离线时 committer date 仍可从 mirror 读，门禁照常；
    **对 lock 中已锁定的 commit 不追溯重判**（只在解析新 ref 时生效，避免既有项目突然红）。
  - 判据：`TestV139MinimumAgeDefaultIsOneDay`（空配置时 Policy 返回 24h）；
    `TestV139AllowFreshBypasses`（新 commit <24h 默认拒、`--allow-fresh` 放行）；
    `TestV139LockedCommitsNotRetroJudged`（旧 lock 的 commit 不受默认门禁影响）；
    溢出测试（`ParseISODuration` 极大值仍拒绝，11-20 已修，保持绿）。
  - 变异：把默认值改回 0 → 红；去掉 `--allow-fresh` → 红。
  - 破坏性变更配套：ADR（独立安全理由 + 与 ADR-044 的关系）+ CHANGELOG `Unreleased` 段 + README"已知限制/迁移"提示。
  - 注意：这是**行为变更**，会打中"归一化"类判据（11-19 教训）——改完必须跑全量 cmd/ngm 判据，不能只跑定向。

- [ ] **B2 verify 默认级别：action 改 content，CLI 保持 structure（契约 §3.2 默认 (b)）**
  - 位置：`action.yml:47-53`（`verify-level` 默认 `structure`）、`cmd/ngm/verify.go:118-120`（CLI 默认 structure）、
    `docs/guides/cli.md` 与 action 文档。
  - 改动：`action.yml` 的 `default: "content"`；CLI 不变。在 `verify --help` 与 `action.yml` 注释里
    双向写明"CLI 默认 structure（本地轻量）；CI action 默认 content（供应链关键路径字节校验）"。
  - 判据：`TestV140ActionVerifyLevelDefaultsToContent`（静态检查 action.yml 默认值 + 双向说明文字）；
    `verify --help` golden 保持不变（CLI 未改）。变异：把 action 默认值改回 structure → 红。
  - 同步：写 ADR 说明"CLI 与 action 默认值不同的理由"；CHANGELOG `Unreleased`；
    README 的 CI 段落。成本提醒：CI 里 `verify --content` 对 100 依赖约 2s→逐文件哈希，可接受。
  - 明确不做：CLI 默认改 content（选项 a）——破坏性太大且不在 CI 关键路径，理由写进 tasks 防重开。

## Batch C（签名与第三方背书，中高风险）

- [ ] **C1 把"ngm attest → 现场 cosign 签名"固化成可复现 CI 配方 + 判据**
  - 位置：ADR-027 已给出路径但未落成可执行配方；`docs/guides/cli.md`、`docs/adr/adr-027-attestation.md`、
    可能新增 `docs/guides/signing.md` 与 `scripts/` 下的配方脚本。
  - 改动：提供一份**最小可复现**的 CI 片段（GitHub Actions：`actions/attest-build-provenance` 或
    `cosign sign-blob` + `gh attestation verify`），把 `ngm attest` 产出的 in-toto statement 作为 subject；
    明确 ngm **不签名、不管密钥**（ADR-014/027 不变）。
  - 判据：`TestV141SigningRecipeIsReproducible`——静态检查配方文件存在、命令序列完整
    （`ngm attest` → `cosign`/`gh attestation` → verify）、且不含任何"ngm 持有密钥"的措辞；
    如本机有 cosign 则加一条端到端冒烟（无则登记 skip 账本，不许静默跳过）。
  - 变异：删掉配方中的 verify 步骤 → 红；在配方里加入 ngm 签名命令 → 红。
  - 边界：不引入 sigstore/cosign 的 Go 依赖（go.mod 只 2 个依赖），签名完全交给外部工具。
  - 注意：ADR-027 的"内容侧 L2"与 npm L3 是**互补**关系，文档措辞不得写成"ngm 达到 L3"。

- [ ] **C2 外部安全审计 / 渗透测试立项（非工程，文档项）**
  - 产出：一份立项备忘（目标范围 = 供应链门禁 + sandbox + verify 信任链；验收 = 第三方报告；
    预算/排期留空由人填），写清当前证据只是 `scripts/packagegate-demo.sh` 的自证断言。
  - 判据：纯文档，完成定义 = 文档入册 + docs-links / discipline 13/13 绿。不虚构审计结论。

## Batch D（结构性债与采用，高风险——"不怕风险"解锁的测量批）

- [ ] **D1 `cmd/ngm` 拆包可行性验证（→ 数字报告，不改产品结构）**
  - 位置：`cmd/ngm/` 44 非测试文件 / 约 2.2 万行；ADR-047 触发条件；
    `TestCmdContextIsTheOnlySkeleton`、`TestHintsNameFlagsTheCommandAccepts` 两道守卫。
  - 本轮产出（不直接拆）：把"报告渲染"这一最小职责做成**隔离实验**（在临时目录或独立分支验证），
    给出"抽出需要改多少文件 / 多少 golden / 是否触碰骨架判据"的具体数字；
    写进 ADR-047 补记：是否达到触发条件、若达到的拆包边界。
  - **不动** `package main` 的正式结构——"不怕风险"不等于"无脑重构"，达到 §3.3 的触发条件才排期。
  - 判据：可行性报告 + 数字表 + ADR 补记入册；全量 `cmd/ngm` 判据绿（证明实验没污染主树）。
  - 变异：纯文档/实验，无产品代码变更，变异检验 N/A；但报告里的数字必须可复现（附命令）。

- [ ] **D2 usage 单源维护成本审计（→ 量化结论，不改代码）**
  - 位置：`verifyUsage` 单条 105 行；`TestHintsNameFlagsTheCommandAccepts` AST 守卫；ADR-048。
  - 产出：统计当前命令数（28）与 AST 守卫维护点数量，给出"合并 usage/flag 双源的收益 vs 重写 golden 成本"
    的量化结论，写进 ADR-048 补记。不直接合并（触发条件未达成，§3.3）。
  - 判据：统计命令可复现 + ADR 入册 + docs-links 绿。

- [ ] **D3 契约网存量复核（只读统计，删网需过门槛）**
  - 位置：约 39 个 `test_*.go` / `vNNN_*_test.go` 源码扫描判据；ADR-049 复核节奏。
  - 产出：按 ADR-049 的两条门槛（连续 3 版因自身红却从未抓缺陷 / 删除能明确减少成本给数字）
    统计候选网清单。**本轮不删除任何网**——只产出候选与数字，符合"删除需过门槛"。
  - 判据：统计报告 + ADR-049 补记 + 全量判据绿。

- [ ] **D4 发布资产回填 38 个（非工程，凭据阻塞）**
  - 位置：`docs/guides/release-pipeline.md:11-28`（GitHub 63 / Gitee 3 / 缺口 38）；
    `scripts/gitee-release.sh`、`check-gitee-release-status.sh` 已就绪。
  - 前置：`GITEE_TOKEN` + GitHub workflow dispatch 权限。无凭据时**只产出操作清单**，不空转。
  - 判据：`TestV126` 守的 63/3/38/101 读数在回填后同步更新（与 release-pipeline.md 同源）；
    `check-gitee-release-status.sh` 绿。属人工动作，不占工程排期。

## 完成定义

所有勾选项满足：判据在册 + 变异检验（或注明 N/A 的理由）+ CI 绿 + 文档同源 + 无新增未登记 `t.Skip`。
破坏性变更（B1/B2）额外要求 ADR + CHANGELOG + 逃生口三件套齐全。
C2/D4 为非工程项，完成定义 = 文档/清单入册 + 凭据或预算到位标注，不虚构执行结果。
全部合入后，在 [竞品差距评估报告](../../analysis/ngm-competitive-gap-full-2026-11-21.md) 顶部追加
"已推进"注记（不改写历史快照正文）。
