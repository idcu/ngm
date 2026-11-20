# Tasks：2026-11-20 评估剩余项（第二批）

来源：[spec.md](./spec.md) · 依据 [全维度评估](../../analysis/ngm-full-assessment-2026-11-20.md)。
状态：**全部完成**（H1/H2 · I1/I2 · J1/J2/J3 共 8 项；基线 `c5f1dcc`，每项判据带变异检验，I2/J 为决策项不改代码）。
- H1 ✅ `auto` 整树复制：`canHardlink` 预探测 + 中途失败 fail-closed；判据 `link_auto_whole_tree_test.go`（两处变异检验均红）。契约取 (a) 整树 copy。
- H2 ✅ 歧义 scp 拒绝：`ValidateRemoteURL` 拒绝无 @ 无点单 token；**保持 `HostFromRemoteURL` isNet=true**（否则 net 门禁 fail-open）。判据 `TestValidateRemoteURLRejectsAmbiguousScp` + `TestHostFromRemoteURL` 新行（变异检验红）。
- I1 ✅ 覆盖率可见化：coverpkg 扩到 `./internal/...`，门槛仍只取 verify（过滤 profile，数值等价），每包一条 `::notice` 不设闸；判据 `TestV137CoverageVisibilityKeepsGateAndAddsNotices`（变异检验红）。
- I2 ✅ **决定不改**（理由见下）。
勾选项必须带证据：判据名 + 变异检验结果（撤掉修复即红、装上即绿）。
每提交在独立干净 worktree 上跑 `go build ./...` + `go vet ./...` + 该批定向判据后才合并。

---

## Batch H（精确性与安全提示，低风险）

- [ ] **H1 `auto` link 模式兑现"整树复制"（M1，先做 §3.1 契约决定）**
  - 位置：`internal/core/vendor/links.go:436-489`（`linkEntries`）、`LinkResult`/`LinkMode` 定义、
    `docs/architecture/vendor-layers.md:141,249`。
  - 现状：`auto` 从 hardlink 起步，首次 `os.Link` 失败即把 `current` 切到 `LinkCopy`，**后续**条目走 copy；
    前面已 hardlink 的文件保持硬链接 → 返回 `Mode=LinkCopy` 却留下"半 hardlink 半 copy"混合树。
    文档 :141 承诺"整树复制"，:249 警告就地编辑 hardlink 污染 content store——承诺与实现分裂。
  - 改动（默认契约 a）：落地前预探测该卷 hardlink 可行性；不可行则**整棵树全部 copy**，不产生混合树；
    让 `LinkResult.Mode` 反映实际整树模式；同步 `vendor-layers.md:141,249` 与 ADR 补记（写清"auto 下就地编辑
    vendor 不污染层 2"何时成立）。
  - 判据：构造跨卷 / hardlink 必失败的场景（Windows 可用不同驱动器，或 mock `os.Link`），断言 auto 落地后
    **不存在任何 hardlink**（逐文件读 inode/链接计数或查 `LinkResult` 与实际一致）；对照正常卷全程 hardlink。
    变异：去掉预探测、恢复逐文件降级 → 混合树重现，判据红。
  - 注意：预探测不能引入额外 `os.Link` 调用点破坏 v0.82 "单一出口"类判据；先查现有 vendor 判据列表。

- [ ] **H2 `HostFromRemoteURL` 的 scp 分支不再把 `foo:bar` 误报成 `net:foo`**
  - 位置：`internal/core/git/remoteurl.go:81-90`，测试 `internal/core/git/remoteurl_test.go:14`。
  - 现状：`foo:bar` 无 `@` 却落入 scp 分支，`head="foo"` 通过非空且无 `/\` 检查 → host `foo` → 触发
    `net:foo`（fail-closed，故非安全漏洞，但提示误导：用户没写 host）。
  - 改动：scp 形式要求 `head` 含 `@`（`git@host:path`）或看起来像合法 host（含 `.` / `localhost` / IP），
    否则返回 `("", false)`，让调用方按非法/本地 URL 处理并给出贴合的提示。
  - 判据：在 `remoteurl_test.go` 加表驱动用例 `foo:bar` → isNet=false、`git@github.com:o/r.git` → host=github.com、
    `github.com:o/r.git`（合法无 @ scp）仍 → host=github.com（**不要伤到合法形式**）。
    变异：去掉新增的形状约束 → `foo:bar` 又返回 foo，判据红。
  - 文档：若提示文案变化，同步 `docs/guides/configuration.md` 的 `net:` 说明。

## Batch I（覆盖率与判据诚实度，需先定契约）

- [ ] **I1 覆盖率读数可见化（先做 §3.2 契约决定；默认不新建门槛）**
  - 位置：`.github/workflows/ci.yml:101-176`（verify 跨包门槛 floor=78，平台名已进 annotation）。
  - 现状：只有 `internal/core/verify` 有跨包门禁；其余产品代码覆盖率不可见，下滑无人察觉。
  - 改动（默认契约）：仿照 verify 的 `::notice`，把**各顶层包**的跨包覆盖率读数发 annotation（只读、不设闸）；
    门槛仍只守 verify 且保持 78；平台名写进 annotation。若 §3.2 决定加门槛，必须按实际测量平台重新校准并写进 summary。
  - 判据：静态检查 workflow —— 读数步骤的 annotation 覆盖所有产品包；门槛步骤仍只有 verify 且 floor 与校准平台同源
    （仿 `check-discipline` 的思路，不改产品代码）。
    变异：删掉某包的读数 annotation → 判据红。
  - 注意：本项动 CI，本机无法直接验证 annotation 内容；至少跑一次 workflow 静态检查 + 确认不破坏现有 verify 门槛步骤。

- [x] **I2 v089 判据：确认"按引用记账"是否收紧 —— 决定不改（2026-11-20 授权拍板）**
  - 位置：`cmd/ngm/v089_artifacts_test.go:24-27` 与 `explainedNotAutomated` 登记表。
  - 现状：判据自承"按引用记账，一个名字写进说明文字也算被提到"，是筛子不是证明。第一批 D4 已把
    `scripts/build-release.ps1` 收敛为转调 `.sh`（原"完整第二实现"问题已消除）。
  - **决定：保持按引用，不收紧。** 理由：(1) 它守的是"脚本从没被提到不可能悄悄发生"——这个下限
    已经成立，且"真的跑过一次"由各脚本自己的判据另守，两层分工清晰；(2) `explainedNotAutomated`
    里每条理由都是人工核对过的历史决定，加"理由仍成立"的反向核对等于新增一张契约网，
    正是 roadmap 停止清单禁止的；(3) 评估报告把它列为"空转判据"，但它抓的最后一个真问题
    （build-release.ps1 双实现）已在 D4 修掉，收紧的边际收益没有证据支撑。
  - 本项不改代码、不加判据。**重启条件**：出现一个"已被引用记账放行、实际从未执行"造成的真实事故。

## Batch J（结构性技术债的触发条件，先写 ADR 不改代码）

- [x] **J1 ADR：`package main` 拆包门槛**（→ ADR-047，已入索引）
  - 现状：`cmd/ngm` 44 个非测试文件、约 2.2 万行，跨文件耦合靠约定而非接口（`TestCmdContextIsTheOnlySkeleton`
    已在源文件层守骨架）。本轮**不拆**。
  - 产出：一条 ADR（或既有架构 ADR 的补记），写清**触发条件**——例如"新增命令时反复踩同一处跨文件耦合"
    或"出现因约定失守导致的真实缺陷"才启动拆包；写明拆包的边界原则（命令 / 报告渲染 / 环境装配）。
  - 校验：新 ADR 同步 `docs/adr/README.md` 索引（`TestV71` 守护）、docs-links、`TestV126` 不受影响。

- [x] **J2 ADR：usage 常量与 flag 声明双源的容忍度**（→ ADR-048，已入索引）
  - 现状：`verifyUsage` 单条 105 行；flag 改了忘改 usage 靠 golden 兜底。本轮**不合并**。
  - 产出：触发条件（例如"golden 漏过一次真实的 usage/flag 漂移"才做单一事实源改造），写清为何当前 golden 已足够。

- [x] **J3 ADR：契约网停止清单的复核节奏**（→ ADR-049，已入索引）
  - 现状：约 39 个测试文件用 `go/ast` / 源码扫描做文档↔代码对账；roadmap 自承"最后一个产品缺陷是 v0.55"。
  - 产出：明确"本轮之后不再新建契约网，除非抓到真实产品缺陷"，以及 periodic 复核触发条件。与 spec §5 同源。

## 完成定义

所有勾选项满足：**判据在册 + 变异检验 + CI 绿 + 文档同源 + 无新增未登记 `t.Skip`**。
J 批为纯文档，完成定义为"ADR 入册 + 索引同步 + docs-links / discipline 13/13 全绿"。
全部合入后，在评估报告顶部追加第二批"已修复/已决策"注记（不改写历史快照正文）。

## 附：第一批刻意没做、本轮承接的"不改"清单

M11（OSV nil fail-open，有决定 + 判据）· M9（空值 flag 语义）· M14（modproof 描述与代码不符）·
中英混杂 · 删 ADR / 归档 · 新建契约网 · 直接重构 package main / usage 双源。
理由见 [第一批 tasks](../../specs/assessment-2026-11-20-followup/tasks.md) 与 spec §5。
