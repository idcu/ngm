# 开发总览

> **当前阶段：v0.1 ~ v0.5 均已交付并发布。** `v0.1.0` ~ `v0.4.0` 都已打 tag
> （后三版于 2026-10-01 补发），GitHub 上各有 7 个附件；首个提交 `a47b0ad` 已推送 Gitee 主仓
> 并镜像到 GitHub，CI（GitHub Actions）三平台全绿。
> **v0.5 已于 2026-10-02 交付**（[计划](./v0.5-plan.md) / [复盘](./v0.5-retrospective.md)）：
> 在线 verify 的成本与方差、ADR-013 翻案条件的判定、权限施加点的机械核对、三版补发布、挂账项收尾。
> **当前唯一的残项是 Gitee 侧的 21 个附件待人工上传**——在传完之前，`v0.2` ~ `v0.4`
> 对国内用户仍取不到（清单见文末发布清单）。
>
> 本文回答"先做什么、怎么验收"。设计与规范（做什么、为什么）的唯一事实源是：
> [architecture/](../architecture/)、[adr/](../adr/)、[modules/](../modules/)、[guides/](../guides/)。
> 实施计划只引用它们，不复制规范内容（见 [README 文档约定](../README.md)）。

---

## 目标与范围

- **v0.1 目标**：`ngm install && ngm verify` 能在真实项目上证明 Git 依赖的可追溯性（范围与退出标准见 [internals/roadmap.md](../internals/roadmap.md)）
- **非目标**（禁止在 v0.1 实现）：自研引擎、test/docs/LSP/Dev Server、registry 包管理、插件生态（能力归属见 [capability-matrix](../internals/capability-matrix.md)）
- 所有新增行为必须能追溯到文档条目；**实现与 ADR 冲突时，先修订 ADR 再改代码**（见 [adr/README.md](../adr/README.md)）

---

## 开发环境

| 工具 | 版本要求 | 用途 |
|------|---------|------|
| Go | >= 1.22 | 编译 ngm（运行时不依赖 Go） |
| Git | >= 2.30 | 依赖拉取、测试 fixture |
| Node.js | >= 22（可选） | 端到端演示（v0.1 起） |
| Deno | >= 2.0（可选） | 端到端演示；`deno bundle` 需 2.4+（v0.2 起） |

平台：macOS / Linux / Windows 三平台均需通过测试（hardlink 相关用例在非 NTFS 文件系统按 linkMode 降级路径验证）。
数据竞争由 ubuntu job 的 `go test -race` 覆盖——Windows 上 `-race` 需要 cgo，本地一般跑不了，这是环境限制而非遗漏。

---

## 代码仓库结构

代码仓 `ngm` 与文档**同仓**：`docs/` 就是本目录。这样"ADR 修订"与"实现改动"能在同一个 PR 里评审——
分两个仓会让文档与代码各自漂移，而本项目的纪律是**实现偏离文档时先改文档**。

```
ngm/
├── cmd/ngm/                 # CLI 入口（文件清单见 modules/p0-core.md）
├── internal/                # 包结构以 modules/p0-core.md 为唯一事实源（包级）
├── testdata/
│   ├── help.golden          # --help 输出快照
│   └── vectors/             # digest 与 lock 的测试向量（golden）
├── docs/                    # 本目录：设计文档、ADR、实施计划与复盘
└── .github/workflows/       # CI：三平台矩阵、行尾/格式检查、真实引擎集成、tag 触发发布
```

> **fixture 仓库不在 `testdata/` 里**：它们由 `internal/testutils` 在 `t.TempDir()` 现场创建
> （见下方"测试策略"），因此测试既不依赖公网、也不残留状态。

约束：

- 依赖最小化：只用 Go 标准库 + `golang.org/x/...` 按需；引入第三方库需在 PR 说明理由。
  至今只有一项例外：**`wazero`**（v0.3 的 wasm 运行时，[ADR-011](../adr/adr-011-wasm-runtime.md)），
  `go.mod` 里除它之外没有直接依赖
- CLI 框架：v0.1 用标准库 `flag`（[p0-core 决策](../modules/p0-core.md)）；若子命令树复杂度超出承受度，走 ADR 修订评估 cobra

---

## 工作流

1. **分支与提交**：`main` 保护 + feature 分支；提交信息用 Conventional Commits（feat / fix / test / docs / chore）
2. **每个 PR 的完成定义（DoD）**：
   - `go build ./... && go vet ./... && go test -count=1 ./...` 全绿（**`-count=1` 不能省**，否则会命中测试缓存）
   - `gofmt -l ./cmd ./internal` 无输出（CRLF 会让 gofmt 判为未格式化，行尾纪律见仓库根的 `.gitattributes`）
   - 涉及输出格式（lock / digest / mappings）的变更必须更新并说明 golden 差异
   - 行为偏离文档时：先提文档/ADR PR，再提代码 PR
3. **CI**（GitHub Actions，三平台 matrix，共 **15 个 job**——`main` 分支 push 与 PR 均触发）：
   - build / vet / test（ubuntu / macos / windows）+ `go test -race`（仅 ubuntu）
   - `gofmt` 与**行尾**检查（CRLF 会让 golden 与 archiveDigest 永久失配）
   - 各里程碑验收 `TestM1Acceptance` … `TestM7Acceptance`（三平台）
   - lock 字节可复现（独立 job，三平台）
   - 真实 esbuild 集成（三平台）
   - 文档链接检查：**仅当 `docs/` 存在时才执行**——本目录并入 `docs/` 后自动生效

---

## 测试策略（核心纪律）

| # | 策略 | 说明 |
|---|------|------|
| 1 | **Fixture 仓库** | `testutils` 用 `t.TempDir()` 现场创建本地 Git 仓库（tag / annotated tag / branch / force push / LFS 指针 / gitlink / monorepo 子路径）；**禁止测试依赖公网** |
| 2 | **Golden 文件** | lock、digest 向量、mappings 输出全部 golden 化；支持 `-update` 重生成；golden 变更必须在 PR 说明 |
| 3 | **表驱动** | URL 归一化、退出码、schema 校验、漂移分类全部表驱动 |
| 4 | **规格即测试** | ADR-008 清单规则、verify 退出码表、漂移分类表逐条对应测试用例，测试名引用条目 |
| 5 | **确定性优先** | 禁止 `map` 迭代参与序列化；时间字段排除在可复现断言外；三平台断言同一字节 |
| 6 | **性能非阻塞** | [metrics](../internals/metrics.md) 目标值作为 benchmark 基线记录，不作为合并门槛 |

---

## 本地怎么跑测试与验收

```bash
# 全量（单元 + 集成）。-count=1 别省——否则会命中测试缓存，"刚改完还是绿的"
go test -count=1 ./...

# 单个包
go test -count=1 ./internal/vendor

# 里程碑验收：每个里程碑都有一条可执行验收
go test -count=1 -run 'TestM1Acceptance' -v ./cmd/ngm
go test -count=1 -run 'TestM[1-7]Acceptance' -v ./cmd/ngm

# 只跑某个验收子场景。Go 会把子测试名里的空格规范化为下划线，
# 所以 t.Run("lock is reproducible") 的选择器写作 lock_is_reproducible
go test -count=1 -run 'TestM3Acceptance/lock_is_reproducible' -v ./cmd/ngm

# 性能基线（默认跳过，需显式开启）
NGM_BENCH=1 go test -count=1 -run TestBaseline -v ./cmd/ngm
```

> 需要真实外部工具（如 esbuild）的用例，在机器上没有该工具时**自动跳过**，不会伪装成通过
> （例如 `TestM7Quickstart_RealToolchain`）。这条"缺工具 ≠ 已验证"的纪律是刻意保留的。

---

## 阶段地图

### v0.1（详细计划：[v0.1-plan.md](./v0.1-plan.md)）

| 阶段 | 名称 | 前置 | 关键交付 |
|------|------|------|---------|
| M0 | 工程骨架与配置层 | — | CLI 骨架 / 错误码 / 配置三级合并 / `init` / CI |
| M1 | Git 层 | M0 | 4 协议归一化 / refType 解析 / mirror / `add` |
| M2 | 内容清单与 archiveDigest | M1 | ADR-008 实现 / digest 向量冻结 / content store |
| M3 | 依赖图 / 冲突 / lock | M2 | 传递依赖 / root wins / 确定性 lock / `install` |
| M4 | vendor 4 层与 mappings | M3 | link tree（linkMode）/ cache / `cache clean` / mappings 生成与校验 |
| M5 | verify | M4 | 三级检查 / driftKind / 退出码 / `--offline`·`--deep`·`--json` |
| M6 | adapter 与构建命令 | M1 | esbuild adapter / `build`·`typecheck`·`css` / `engines` |
| M7 | 端到端验收与发布 | M5+M6 | 三平台 CI / 四问实测 / 发布产物 |

### v0.2（计划：[v0.2-plan.md](./v0.2-plan.md)）

供应链防护完整：OSV + audit、策略引擎（minimumReleaseAge / 白名单 / postInstallPolicy）、why·tree·outdated、tsc·deno·postcss adapter、install 的 `--frozen-lockfile` / `--offline` CI 模式。

### v0.3（计划：[v0.3-plan.md](./v0.3-plan.md)）

引擎生态与协议：wasm adapter（`remote` 经 [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布）、
集成脚手架（Vite / esbuild / Deno / Webpack + tsconfig paths）、Deno sandbox、权限与凭证、mappings 子路径扩展。

### v0.4（计划：[v0.4-plan.md](./v0.4-plan.md)）

验证与收敛：把上一版留下来的开放问题收敛掉，并把"人工纪律"换成机器检查——
`ngm typedecl` 给 `typeDecl` 一个入口（真 tsc 验收）、`verify --signatures` / `--require-signed`
（[ADR-014](../adr/adr-014-self-report-signatures.md)）、配置字段接线的机械检查。

### v0.5（复核：[v0.5-review.md](./v0.5-review.md) / 计划：[v0.5-plan.md](./v0.5-plan.md) / **复盘：[v0.5-retrospective.md](./v0.5-retrospective.md)**）

**已交付**（唯一的残项见下）。**范围**：把三件"已经量出来但仍挂着"的事推到结论——
在线 verify 的成本与方差（含两条需先立 ADR 的改动）、ADR-013 翻案条件的**判定**、
权限施加点的机械核对；外加一项代码之外的交付：**v0.2 ~ v0.4 补发布**。

**三条与预期相反或需特别注意的结论**（详读复盘）：

1. **在线 verify 的 3s 目标线在今天的机器上不可判别**——受控对照（未优化）最大 2.433s 也过线，
   而跨机器状态的方差（≥0.3s）大于本轮收益（0.45s）。因此纪律改为**次数优先于秒数**。
2. **跨机器可复现性已实测**（3 平台 × 7 形态字节一致），但**翻案条件成立不等于要发布
   `remote` adapter**——那是一个需要另立 ADR 的新决策。
3. **`read:` / `write:` 裁决为"不在 ngm 自身路径上施加"**——这是裁决，不是遗漏。

**残项**：Gitee 侧 21 个附件待人工上传（在此之前那三版对国内用户仍取不到，
见[发布清单](./README.md#补发记录2026-10-01)）。

开工前复核已完成，并**继续做掉了它自己列出的未做项**：

- 配置字段接线核对：新查出 4 项确认未接线 + 3 项弱接线并逐项处置，首次运行又多点出 3 项"已接线、没断言"
- 性能回归复测：**在线 verify 贴着 3s 目标线**（3 次采样里 1 次超过），"单次读数达标"的写法已删除
- 文档与实现一致性：逐页核对，修掉 4 类失效陈述 + 2 处引用了不存在/已失效东西的用户可见提示
- 安全模型：沙箱在**真实 Deno 1.45.2 与 2.4.0** 上重测，修掉 4 处**假失败夹具**与 1 处弱断言
  （Deno 2 把权限错误的类名从 `PermissionDenied` 改成 `NotCapable`，而 CI 当时钉在 1.x）
- 挂账两项：内部并行哈希**已做**（两个可复现基准）；deno **`typeCheck` 已覆盖**、`bundle` 仍等上游
- `ngm transform`：补上最后一处"有能力、无入口"，并修掉一处会**静默丢弃** `--format` / `--minify` /
  `--sourcemap` 的协议缺陷

### v0.6（计划：[v0.6-plan.md](./v0.6-plan.md) / **复盘：[v0.6-retrospective.md](./v0.6-retrospective.md)**）

**已交付**（两处未完成都不是工程：缺凭据、读不到 CI）。

v0.5 把挂着的事推到了结论，但其中三处是"**测了，但判不了或数据不够**"：3s 目标在噪声里
不可判别、可复现性只证到合成夹具、GC 缺磁盘数据。**v0.6 补的是可判别性**：

| 组 | 内容 | 状态 |
|----|------|------|
| A | 真实项目形态的可复现证据（[ADR-017](../adr/adr-017-remote-adapter-release-decision.md) 门槛 a） | ⚠️ **本机已证**（10 形态 × 20 轮全稳定）；**跨机器未取得**（读不到 CI） |
| B | **spawn 预算**：把"次数优先于秒数"变成 CI 门禁 | ✅ **已交付**（开门第一天抓到一次**记账错误**：v0.5 C 组的重复记账让数字虚高 1/依赖） |
| C | store / mirror 的磁盘增长数据（只测不做，为 GC 排期提供数据） | ✅ **已交付**（**160.2 KiB/commit**，是源码真实增量的 **20×**；结论：做 GC，但下一步是 ADR 而不是代码） |
| D | Gitee 附件补传（把手工步骤降到一条命令） | ⚠️ **工具已就绪**（一条命令 + `-DryRun` 预检；已验无 token/坏 tag 明确失败）；**实际传入仍需 `GITEE_TOKEN`** |
| E | 挂账：`deno bundle` 等上游 | ✅ 已核查（条件未变；**间接证据**，见[复盘](./v0.6-retrospective.md) §1.5） |

> 本版**先行完成**的一项：[ADR-017](../adr/adr-017-remote-adapter-release-decision.md)
> —— remote adapter 的发布决策（结论仍不发布，但把剩下的问题写成可判定的门槛）。

---

## 验收机制

- 每个阶段的"验收"必须是**可执行验证**（命令 + 期望输出），写入对应测试或手测脚本
- v0.1 总验收 = [roadmap 退出标准](../internals/roadmap.md) 4 条 + [README 四问](../README.md)（1/2/3 实测记录，4 由 ADR-008 定义）：
  - 真实项目跑通 `ngm install` ✅
  - lock 跨平台字节级一致（排除 `resolvedAt`）✅
  - verify 能检测 tag 重打与 branch 前进 ✅
  - digest 重放不匹配能阻断构建（exit 2）✅
- 进入 v0.2 前，先完成 v0.1 复盘：用实测结果复核性能目标与设计假设，必要时先修 ADR
  - ✅ 已完成：[v0.1 复盘](./v0.1-retrospective.md)（四条退出标准达成；性能 5 条中 2 条未达标，
    根因与 v0.2 行动项已记录）
  - ✅ 该复核有实质产出：发现 `resolve.Node.RequiredBy` 必须存**节点 Key**，
    否则来源链在 monorepo 下会指错节点
- 进入 v0.3 前，先完成 v0.2 复盘：逐条对验收标准给证据、登记设计偏离与未达标项
  - ✅ 已完成：[v0.2 复盘](./v0.2-retrospective.md)（六组全部实现；在线 `verify` 差 1.1×；
    6 条设计偏离、6 处已修缺陷；其中 4 处是靠"做完之后再验证一次"发现的）
- 进入 v0.4 前，先完成 v0.3 复盘：同上（逐条给证据、登记设计偏离与**未结项**）
- 进入 v0.5 前，先完成 v0.4 复盘：同上（逐条给证据、登记设计偏离与偏差）
- 进入 v0.5 前，先做**开工前复核**：把"我们以为的"换成"我们测到的"
  - ✅ 已完成（第 1 项实测）：[v0.5 复核](./v0.5-review.md)——配置字段接线核对，
    新查出 **4 项确认未接线**（`types` / `vendor.commit` / 死类型 `EngineRef` /
    `--concurrency` flag）+ 3 项弱接线待裁定
  - ✅ 已收口：同一份复核的[处置结果](./v0.5-review.md#处置结果本轮收口)——
    裁定"生效 = 改变行为"，7 项逐项接线或删除，并把 §7 的建议机械化
    （`internal/config/field_wiring_test.go`，用 reflect 枚举字段而非按名字形状猜）。
    该检查首次运行**多查出 3 项**（两份 `schemaVersion` 与 `Entry.version`），已一并处置
  - ✅ 已完成：[v0.3 复盘](./v0.3-retrospective.md)（五组交付项全部结项；`remote` 经
    [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布；11 条设计偏离、8 处已修缺陷——
    其中 2 处在**已发布代码**里；1 项未结项：沙箱自述文件签名检查）

---

## 发布清单（每个版本）

> **第 0 步是 v0.5 补上的，它是这份清单里最容易被跳过的一步**：
> **版本交付即打 tag**。v0.2 / v0.3 / v0.4 交付时都没打——结果是这三版在仓库里存在、
> 在任何发行源上都取不到，直到 2026-10-01 才补发。**不发 tag 等于"这版只存在于源码里"**。

0. **交付该版本时立刻打 tag**（不要等"下次一起发"）：版本的最后一次提交（本项目的惯例是
   该版的复盘提交）上打附注 tag。判据很简单：**这一版的产物能不能被用户下载到**。
1. `git tag -a v0.x.y -m "..."`，然后 `git push origin v0.x.y`
2. Gitee 镜像把 tag 推到 GitHub → `release.yml` 自动执行：**先跑全量测试**，再交叉编译六平台，
   最后 `gh release create` 发布（含 `SHA256SUMS`）
3. 核对 GitHub release 的 7 个 asset（6 个二进制 + `SHA256SUMS`）
4. **把这 7 个文件从 GitHub release 上传到 Gitee 发行版**（在 Gitee 无 API token 时，这是最短且最稳的路径）。
   上传后附件直链即为 `https://gitee.com/idcu/ngm/releases/download/<tag>/<文件名>`
5. 抽查一次：两个源的同一文件名 `sha256` 应完全相同（它们共用同一份 `SHA256SUMS`）

> **v0.6：第 3 ~ 5 步现在是一条命令**（`scripts/upload-gitee-assets.ps1`）：
>
> ```powershell
> powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.4.0 -DryRun   # 先看清单与 sha256
> $env:GITEE_TOKEN = '<Gitee 私人令牌>'
> powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.4.0            # 上传 + 逐个双向校验
> ```
>
> 它**从 GitHub 取字节**而不是本地重编——两个源必须是同一份字节，而本地重编会得到另一份
> （工具链、时间、路径都可能不同），那样"两个源一致"就成了一句没法核对的话。
> 它按 `SHA256SUMS`（**同一个文件**）逐个核对，并在上传后**再从 Gitee 下载一遍比对**；
> 同名附件已存在且一致时跳过（**幂等**），不一致时报错并让你决定，**绝不静默覆盖**。
> 缺 token 时它明确失败、不动远端——没有"降级成不做事"这种路径。
> 已在本机验证：`-DryRun` 的清单自洽（6 条清单 ↔ 6 个附件）、无 token 与坏 tag 都以 exit 1 明确失败；
> **上传路径尚无真实令牌验证过**（编写环境没有 Gitee 凭据），第一次真跑请先 `-DryRun`。

### 补发记录（2026-10-01）

v0.2 ~ v0.4 的补发，一次做完，作为第 0 步的反面证据：

| tag | 落点 | GitHub release | 附件 |
|-----|------|---------------|------|
| `v0.2.0` | `51e5e29`（加 v0.2 复盘那次提交） | 14:19:34Z，7 个 | 6 平台二进制 + `SHA256SUMS` |
| `v0.3.0` | `f78d0c0` | 14:19:56Z，7 个 | 同上 |
| `v0.4.0` | `03e6426` | 14:20:00Z，7 个 | 同上 |

推送前核对过三件事，值得沿用：①`release.yml` 与 `build-release.sh` 在三个落点上与当前**完全一致**
（否则"补发"等于用未知版本的流水线去构建）；②`GO_VERSION=1.27` 满足各落点的 `go 1.22.0`；
③三个落点都在 `main` 的历史上、且当时的 CI 是绿的。

> ⚠️ **仍未完成**：第 4 步（Gitee 附件上传）需要人工操作。在传完之前，
> Gitee 侧只有 `v0.1.0` 有附件——而[安装指南](../guides/installation.md)把 Gitee 列为国内推荐源，
> 也就是说这三版走推荐路径会 404。要传的是 **3 × 7 = 21 个文件**，
> 从 `https://github.com/idcu/ngm/releases/download/<tag>/<文件名>` 逐个下载后上传。
>
> **v0.6 把它降到了三条命令**（每个 tag 一条：`-DryRun` 看清单 → 设 token → 正式跑），
> 见上面的 `scripts/upload-gitee-assets.ps1`。**剩下要人做的只有"填 token、跑命令、看输出"**，
> 以及把结果补进本表。此前"21 个文件逐个下载再上传"的手工步骤不再需要。

> **Gitee 免费额度的容量**（已核实）：单附件 ≤ 100MB、仓库附件总量 ≤ 1GB，
> 且**仓库附件与发行版附件合并计算**。
>
> **v0.5 更正**：这条估算此前按 v0.1.0 的 25.05 MB/版写下"约可容纳 40 个版本"，**现在不成立**——
> v0.2 起二进制大了约 3.5 倍（v0.1 的网络操作全走 git 子进程，因此没有 `net/http`；
> v0.2 的 OSV 引入了它，v0.3 又加上 wazero）。按补发时从 GitHub API 读到的**实测**附件体积：
>
> | 版本 | 7 个附件合计 | 单文件区间 |
> |------|-------------|-----------|
> | `v0.1.0` | 25.05 MB | 4.06–4.35 MB |
> | `v0.2.0` | **66.73 MB** | 10.1–11.1 MB |
> | `v0.3.0` | **88.89 MB** | 13.5–14.8 MB |
> | `v0.4.0` | **89.09 MB** | 13.5–14.9 MB |
>
> 已发布四版合计约 **270 MB**，按 v0.4.0 的 ~89 MB/版计，1GB 大约还够 **8 个版本**。
> 也就是说：**还有余量，但不再是"不需要为此做任何取舍"**——这条要在体积再涨时重算，
> 而不是等它撞上配额。（`build-release.sh` 刻意不 strip，用体积换 panic 栈可诊断性；
> 真要换体积，那是第一个可以动的地方，但必须留下记录。）

---

## 相关文档

- [v0.1 实施计划](./v0.1-plan.md) / [v0.2](./v0.2-plan.md) / [v0.3](./v0.3-plan.md) / [v0.4](./v0.4-plan.md) / [v0.5](./v0.5-plan.md) / [v0.6](./v0.6-plan.md)
- [v0.1 复盘](./v0.1-retrospective.md) / [v0.2 复盘](./v0.2-retrospective.md) / [v0.3 复盘](./v0.3-retrospective.md) / [v0.4 复盘](./v0.4-retrospective.md) / [v0.5 复盘](./v0.5-retrospective.md) / [v0.6 复盘](./v0.6-retrospective.md) / [v0.5 复核](./v0.5-review.md)
- [internals/roadmap.md](../internals/roadmap.md)
- [internals/metrics.md](../internals/metrics.md)
- [modules/p0-core.md](../modules/p0-core.md)（包级结构唯一事实源）
- [guides/cli.md](../guides/cli.md)（命令形态）