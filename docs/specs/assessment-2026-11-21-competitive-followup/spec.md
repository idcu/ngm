# Spec：2026-11-21 竞品差距评估之后（全维度推进）

依据：[ngm 与市面主流竞品的全维度差距评估（2026-11-21）](../../analysis/ngm-competitive-gap-full-2026-11-21.md)。
基线：`dedf89b`（评估报告 + 过期事实纠偏已合入并推送；P0 纠偏已完成）。
本 spec 定义"**不怕风险**"授权下的下一轮工作；逐项清单与判据见 [tasks.md](./tasks.md)。

> **实施补记（2026-11-21，全部 10 项已完成）**：两份初稿假设在实施时被源码/既有决定纠正，
> 以 [tasks.md](./tasks.md) 的"实施时的两处安全改判"为准：(1) `invocationId` 本就是 lock 哈希，
> 不是空串（A2 只补时间字段）；(2) `P0D` 被 `ParseISODuration` 有意拒绝，B1 的逃生口改为
> "不配 supplyChain 段 + `--allow-fresh`"；(3) D4 的 38 个缺口是 `release-pipeline.md` 明确
> **不回填**的区间，不复活为待办。spec 正文作为决策快照保留，不改写历史。

> **"不怕风险"的准确含义**：解除上一轮因"收益/风险不匹配"而暂缓的批次（默认冷却、结构债重构），
> **不解除产品定位边界**（ADR-044）。定位边界不是"风险"，是"是不是同类东西"——
> registry/semver/peer deps/node_modules/publish 即使不怕风险也不做，理由见 §5。
> 每一项仍必须有触发门槛、契约决定与变异检验；"不怕风险"不等于"不做判断直接堆改动"。

## 0. 结论：按 A → B → C → D 四批推进，前一批合入才开下一批

评估报告判定"真实落后"只有三处（均在标准/采用侧，不在主轴），"正交/明确不做"占多数。
本轮据此分四批，按"能否独立验收 + 是否触碰核心不变量"排序：

1. **A 合规产物（低风险，先做）**：SBOM 升 CycloneDX 1.7（对手 pnpm 已 1.7，ngm 仍 1.5）；
   `attest` 谓词补齐 SLSA v1 的 `runDetails.metadata`（当前只有 `invocationId`，缺标准的时间字段——
   但补时间会破坏"同 lock 同字节"的确定性契约，需先拍板，见 §3.1）。
2. **B 默认安全姿态（中风险，需写 ADR 再改代码）**：把 `minimumReleaseAge` 从"可选"改为
   **默认 P1D**（对齐 pnpm/Deno 的 2026 默认）+ 逃生口；`verify` 默认级别从 structure 改 content
   或只在 action 里改（契约决定 §3.2）。这是"不怕风险"新解锁的一批——上一轮 §6.2 因"更像 npm"
   把它排除，本轮重新评估后认为它有**独立的安全理由**（晾晒期防新版本投毒，不是兼容 npm）。
3. **C 签名与第三方背书（中高风险，跨组织）**：把 `ngm attest` 的"未签名语句 + 用户现场 cosign"
   路径**固化成可复现的 CI 配方 + 判据**，并推动一次外部安全审计/渗透测试立项（非工程项单列）。
   注意：ADR-014/027 的"不自造签名"仍然有效，本批只是把既有决定**落成证据**，不翻转它。
4. **D 结构性债（高风险，"不怕风险"才解锁）**：`cmd/ngm` 拆包（ADR-047）、usage 单源（ADR-048）、
   契约网复核（ADR-049）。三项 ADR 当前都是"暂不"，本轮**只做触发条件已明确达成"用户明确要求
   不怕风险地推进"这一条的**——但每项仍须过 §3.3 的"真实触发 + 数字"门槛才动代码，避免为拆而拆。

**非工程项单列**：发布资产补齐（GitHub 63 / Gitee 3 / 缺口 38）需 `GITEE_TOKEN` 与 workflow dispatch
权限，不占工程排期——见 tasks D4 与 §5。

## 1. 不变量（任何批次都不得破坏）

- **定位不变量（ADR-044）**：不新增 registry 能力、不解析 semver/peer deps、不接管 `node_modules`、
  不做 publish；npm/pnpm 只是迁移来源，不是兼容目标。这是"不怕风险"也不解除的边界。
- **签名不变量（ADR-014/027）**：ngm 不自造签名格式、不分发密钥、不管理信任；只产被签对象，
  签名交给用户的标准工具。B/C 批不得引入签名密钥管理代码。
- **确定性不变量**：`attest`/`sbom`/lock 的输出"同 lock 同字节"——字段顺序 + 固定缩进/LF/末尾换行；
  任何时间字段的引入必须先过 §3.1 契约决定，否则破坏 `TestV70Deterministic` / `TestV69OutputIsDeterministic`。
- **退出码 0–6 单码语义**（ADR-039）：完整性失败(2) 不被资源缺失(4) 或工具缺失(5) 覆盖；
  `run` 透传族不变。B 批改默认级别时不得改变"结构级 vs 内容级"的退出码映射。
- **闸门不合并**：`run:git` / `net:<host>` / `run:<engine>` / `run:script` 各自独立；触网前必过 `CheckNetAccess`。
- **JSON 契约**：`--json` stdout 恰好一份 JSON；新增 flag 必须同步 `docs/guides/cli.md` 形状表（`TestV26` 守）。
- **判据纪律（ADR-049）**：新增源码扫描判据必须指着一个**已发生**的产品缺陷；
  改了源码的每一项带变异检验（撤掉即红、装上即绿）。
- **文档同源**：读数与 `release-pipeline.md`（63/3/38/101,`TestV126`）、ADR 索引（`TestV71`）同源；
  新增 ADR 必须同步索引。

## 2. 批次与耦合

| 批次 | 内容 | 前置 | 提交粒度 | 风险 |
|---|---|---|---|---|
| A | A1 SBOM→CDX 1.7 · A2 attest 谓词 SLSA 字段 | — | 每提交 1 项 | 低–中（A2 需先定 §3.1） |
| B | B1 minimumReleaseAge 默认 P1D + 逃生口 · B2 verify 默认级别 | A 合入后 | 每提交 1 项 | 中（破坏性变更，需 ADR + changelog） |
| C | C1 cosign 签名 CI 配方 + 判据 · C2 外部审计立项 | B 合入后 | C1 提交 1 项；C2 文档 | 中高（C2 跨组织） |
| D | D1 拆包 · D2 usage 单源 · D3 契约网复核 · D4 发布回填 | 各自独立 | 每提交 1 项 | 高（D1/D2 重写 golden） |

## 3. 必须先做的契约决定

**3.1 attest 的时间字段 vs 确定性（A2）**
现状 `internal/attest/attest.go` 的 `metadata` 只有 `InvocationID`（无 `startedOn`/`finishedOn`），
`runDetails.builder` 有 `id`/`version`。SLSA v1 谓词的可选字段含 `metadata.startedOn/finishedOn`
与 `byproducts`。选项：
- (a) **不补时间字段**，只在 `invocationId` 语义上补强（如写入 lock 的字节哈希而非空串）；
  保持"同 lock 同字节"，代价是与 npm provenance 的"哪次 CI"语义仍不对齐。
- (b) **补时间字段但默认省略**，加 `--with-timestamps` 显式开启（开启即放弃该产物的字节确定性，
  文档标明"这种语句不可做 golden 比较"）；代价是新增开关与两种输出形态。
默认建议 **(b)**：它同时守住确定性默认与标准完整性，且 `attest` 的产物本就常被现场签名（时间戳
由签名时刻提供更有意义）。**先定再改代码**；无论选哪个，`TestV70Deterministic` 必须在默认路径保持绿。

**3.2 verify 默认级别与 action 默认值（B2）**
现状：`cmd/ngm/verify.go` 默认 structure（`--content` 显式开启），`action.yml` 的 `verify-level`
默认也是 `structure`。选项：
- (a) **CLI 默认改 content**：最强的默认承诺，但 CI per-push 成本上升（100 依赖 2s→逐文件哈希），
  且改变既有用户行为——破坏性变更，需 ADR + changelog + 明确逃生口 `--level=structure`。
- (b) **只把 action 默认改 content**，CLI 默认不动：GitHub/Gitee CI 是供应链关键路径，
  本地开发保持轻；代价是 CLI 与 action 默认值不同（需在 help/文档写明）。
- (c) **都不改**，只在文档推荐 CI 显式 `--content`。
默认建议 **(b)**：它把安全默认落在最关键的自动化路径，又不对所有 CLI 用户做破坏性变更。
需写 ADR 说明"CLI 与 action 默认值不同的理由"，并在 `verify --help` 与 `action.yml` 双向可见。

**3.3 结构债的真实触发门槛（D1/D2/D3）**
三条旧 ADR 都写了"暂不 + 触发条件"。用户说"不怕风险"是否等同于触发条件达成？逐项判定：
- **D1 拆包（ADR-047）**：触发条件是"同类跨文件耦合缺陷 ≥2 次"或"约定失守造成用户可见缺陷"或
  "编译/测试时间可观测退化"。**"不怕风险"不是这三条之一**——本轮不因授权就拆；
  但可以先做**拆包的可行性验证**（把"报告渲染"这一最小职责抽包试跑，验证 golden 影响面），
  产出"若拆，成本是多少"的数字，达到触发条件时直接可用。本轮 D1 产出**可行性报告 + 数字**，不改产品结构。
- **D2 usage 单源（ADR-048）**：触发条件是"golden 漏过一次真实漂移"或"命令数使维护成本显著上升"。
  同理不直接改；改为**审计**:统计当前命令数与 AST 守卫的维护点，给出"合并收益"的量化结论。
- **D3 契约网复核（ADR-049）**：触发条件是"某网连续 3 版因自身红却从未抓到缺陷"或"删除能明确减少
  维护成本（给数字）"。本轮可**执行复核**（只读 + 统计），但删除任何网都要过门槛。
> 结论：D 批是"测量 + 证据"，不是"无脑重构"——这就是"不怕风险"与"负责任推进"的交集。

## 4. 验收总闸（每批提交前都要过）

```bash
go build ./... && go vet ./...
go test -count=1 ./internal/...                       # 单元与核心库
go test -count=1 ./cmd/ngm -run '<本批判据>'           # 定向判据
bash scripts/check-docs-links.sh docs README.md
node scripts/check-discipline.mjs                      # 必须仍是 13/13
git diff --check && gofmt -l <changed .go>             # 必须为空
```

- A/B 批改了 JSON/输出形状的项：同步 `docs/guides/cli.md` 形状表（`TestV26` 反向守护）。
- 新增 ADR 必须同步 `docs/adr/README.md` 索引（`TestV71` 守护）；新增时间字段必须过 §3.1 且保持默认确定性。
- 每项判定必须附**变异检验**：撤掉修复即红、装上即绿（纯文档/CI annotation 项除外）。
- 破坏性变更（B1/B2）必须有 ADR + CHANGELOG `Unreleased` 段 + 逃生口，缺一不可。

## 5. 明确不做（定位边界，"不怕风险"也不解除）

| 项 | 为什么不做 | 归属 |
|---|---|---|
| registry 包管理 / semver 求解 / peer deps / `node_modules` / publish | ADR-044：六座大山 60–120 人日且永远追不上；做了就摧毁"Git-first 证明层"定位 | 不是风险问题，是同类问题 |
| 自研 bundler / HMR / test runner / LSP / dev server | capability-matrix：交给 esbuild/Vite/Vitest/tsc；自研追赶无意义 | 正交，非落后 |
| 自造签名格式 / 密钥分发 | ADR-014/027：ngm 只产被签对象，签名交标准工具 | 定位 + 安全边界 |
| 默认强制版本冷却的"更像 npm"论据 | 本轮改为独立安全理由才做（§3+B1），若找不到独立理由则不做 | ADR-044 准入规则 |
| 删 ADR（49 个）与逐版本归档（211 个） | 17+ 测试引用它们 | 回归档案 |
| 无证据的新建契约网 | ADR-049：新判据必须指向已发生的产品缺陷 | 停止清单 |

**非工程项（不占排期）**：外部审计需预算与外部机构；发布回填需 `GITEE_TOKEN` /
workflow dispatch 权限；两者脚本已就绪（`gitee-release.sh`、`check-gitee-release-status.sh`），属人工动作。

## 6. 相关文档

- [全维度竞品差距评估（2026-11-21）](../../analysis/ngm-competitive-gap-full-2026-11-21.md) —— 本轮依据
- [能力矩阵](../../internals/capability-matrix.md) · [路线图（停止清单）](../../internals/roadmap.md)
- [ADR-044 独立生态](../../adr/adr-044-independent-ecosystem.md) · [ADR-014 签名](../../adr/adr-014-self-report-signatures.md)
- [ADR-027 attestation](../../adr/adr-027-attestation.md) · [ADR-047/048/049 结构债触发条件](../../adr/)
