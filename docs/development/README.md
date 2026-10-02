# 开发总览

> **当前阶段：v0.1 ~ v0.8 均已交付**；**`v0.1.0` ~ `v0.8.0` 八个 tag 均已打**。
> 其中 `v0.1.0` ~ `v0.4.0` 已在两个源上发布（后三版于 2026-10-01 补发，
> Gitee 侧 2026-10-02 补齐），GitHub 上各有 7 个附件；
> **`v0.5.0` ~ `v0.8.0` 的 tag 于 2026-10-02 补打**（附注 tag，已在 Gitee 与 GitHub 上）；
> 但**镜像转发了 tag 却没有触发 `release.yml`**，所以那四版的 GitHub release 尚未生成
> ——补发走 Actions → Release → **Run workflow**（见[发布清单](#发布清单每个版本)第 2 步），
> **Gitee 侧附件随后待上传**。
> 首个提交 `a47b0ad` 已推送 Gitee 主仓并镜像到 GitHub，CI（GitHub Actions）三平台全绿。
> "已交付 ⇔ 已打 tag"现在由 `scripts/check-release-status.sh`（CI job `release-status`）机械守住。
>
> | 版本 | 计划 | 复盘 | 一句话 |
> |------|------|------|--------|
> | v0.5 | [计划](./v0.5-plan.md) | [复盘](./v0.5-retrospective.md) | 收敛与交付；三版补发布 |
> | v0.6 | [计划](./v0.6-plan.md) | [复盘](./v0.6-retrospective.md) | 让结论**可判别** |
> | v0.7 | [计划](./v0.7-plan.md) | [复盘](./v0.7-retrospective.md)（补写） | store 占用可见、残骸可回收 |
> | v0.8 | [计划](./v0.8-plan.md) | [复盘](./v0.8-retrospective.md) | 层 2 换布局：20.0× → 0.6× |
> | v0.9 | [计划](./v0.9-plan.md) | [复盘](./v0.9-retrospective.md) | 让 store 的读数说真话（共享/独占/孤儿）+ 锚点检查 + 10 万级规模实测 |
> | v0.10 | [计划](./v0.10-plan.md) | [复盘](./v0.10-retrospective.md) | 检查推到最外圈：根 README + store 不完整的两条承诺 |
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

**当时的残项（已办）**：Gitee 侧 21 个附件曾待人工上传——那次补传于 **2026-10-02**（v0.6 期间）完成：
21 个附件逐个从 Gitee 取回复核 sha256，两源 `SHA256SUMS` 逐字节相同，
**"国内推荐源取不到"这个问题不再存在**（见[发布清单](./README.md#补发记录2026-10-01)）。

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
| A | 真实项目形态的可复现证据（[ADR-017](../adr/adr-017-remote-adapter-release-decision.md) 门槛 a） | ✅ **已交付**：本机 20 轮全稳定 + **跨机器三平台同值**（`treeA=124f03b1de58091a`）→ **门槛 a 成立** |
| B | **spawn 预算**：把"次数优先于秒数"变成 CI 门禁 | ✅ **已交付**（开门第一天抓到一次**记账错误**：v0.5 C 组的重复记账让数字虚高 1/依赖） |
| C | store / mirror 的磁盘增长数据（只测不做，为 GC 排期提供数据） | ✅ **已交付**（**160.2 KiB/commit**，是源码真实增量的 **20×**；结论：做 GC，但下一步是 ADR 而不是代码） |
| D | Gitee 附件补传（把手工步骤降到一条命令） | ✅ **已完成**（2026-10-02：三版各 **7/7**，两源 `SHA256SUMS` 逐字节相同、抽查二进制与清单一致） |
| E | 挂账：`deno bundle` 等上游 | ✅ 已核查（条件未变；**间接证据**，见[复盘](./v0.6-retrospective.md) §1.5） |

> 本版**先行完成**的一项：[ADR-017](../adr/adr-017-remote-adapter-release-decision.md)
> —— remote adapter 的发布决策（结论仍不发布，但把剩下的问题写成可判定的门槛）。
> 收尾时又补上 [ADR-018](../adr/adr-018-store-reclaim.md)
> ——内容寻址 store 的回收与去重（**不做**按可达性自动删除；写入侧去重是长期解法）。

### v0.10（计划：[v0.10-plan.md](./v0.10-plan.md) / 复盘：[v0.10-retrospective.md](./v0.10-retrospective.md)）— **已交付**

**把检查推到最外圈**：v0.9 让 store 的读数说了真话；这一版补两个**没人看的角落**——
最外圈的文档（根 `README.md`）与用户真会遇到的一种失败（store 不完整）。

| 组 | 内容 | 状态 |
|----|------|------|
| A | "store 里少了一个 blob"时的报错与残骸行为 | ✅ **已交付**（两条承诺钉进测试：点名条目 + 给出路；残骸必须被 `verify` 认出） |
| B | 检查边界：根 `README.md` 的链接与锚点 | ✅ **已交付**（显式根缺失 = 失败；53 个锚点；双向牙齿测试） |

### v0.9（计划：[v0.9-plan.md](./v0.9-plan.md) / 复盘：[v0.9-retrospective.md](./v0.9-retrospective.md)）— **已交付**

**让 store 的读数说真话**：v0.8 换了布局之后，这一层的数字开始需要解释
（逻辑体积 / 共享 / 独占是三个不同的东西），而"能不能回收"至今没有数据。

| 组 | 内容 | 状态 |
|----|------|------|
| A | `store usage` 把 blob 池切成**共享 / 独占 / 孤儿**，每棵树报"丢掉它能回收多少" | ✅ **已交付**（断言是不变量，已验证有牙齿：把共享算进独占立刻红） |
| B | 锚点检查（`[label](x.md#anchor)` 的锚点此前**无人校验**） | ✅ **已交付**（Go 测试，46 个锚点全验；放回死锚点即红并给出正确答案） |
| C | blob 池的规模（10 万级，**只测不做**） | ✅ **已交付**（10 万 blob：单目录 341~437 文件、`usage` 244ms） |

### v0.8（计划：[v0.8-plan.md](./v0.8-plan.md)）— **已交付**

层 2 换布局（[ADR-019](../adr/adr-019-content-addressed-blobs.md)：blob 池 + 树清单）分三阶段落地。
顺序之所以是"先解耦、再换布局"：`verify` 是安全关键路径，它的偏差都是"看起来通过"，
不能与布局改动同时发生。

| 阶段 | 内容 | 状态 |
|------|------|------|
| A | 消费方与布局解耦（`Entries` + `compareTrees` + `VerifyVendorAgainstEntries`） | ✅ 等价重构，既有测试一字未改全部通过 |
| B | `Put` 写 v2（blobs + manifest），读路径 v2 优先、回落 v1 | ✅ 含双布局验收测试 |
| C | 收敛与观测（v1 被重写时回收；`usage` 分别报告两种布局） | ✅ |

**结果**：同一把尺子（12 个 commit、每个只改 1 个文件）**20.0× → 0.6×**
（占用 1.88 MiB → **59.9 KiB**）；那把尺子的断言已翻转为"不得接近 N 棵树"，
于是它同时成了去重的**回归门禁**。

**三处实施时才发现、ADR 没写到的点**（都记进 [ADR-019 §修订](../adr/adr-019-content-addressed-blobs.md#修订v08-实现时发现并改掉的五处)）：

1. **可执行位无法只靠"内容寻址"承载**——落地层用 hardlink，而 hardlink 共享 inode，
   于是 blob 的身份改为 `H(mode, 内容)`（代价：同内容两种 mode 存两份）。
   少了这一步，`scripts/*.sh` 落地后会**丢可执行位**，而且是静默的。
2. **`symlink` 落地模式在 v2 下没有整棵树可链**——退化为逐条目 hardlink 并如实报告实际 mode。
   这是本版**唯一一处用户可见的行为变化**。
3. **收敛不是自动的**——`install` 在 `Has` 为真时短路（那是"无网络也能安装"的承诺），
   而健康的 v1 条目 `Has` 正是真，所以**旧条目不会被自动迁移**。处置是把出路交给用户
   （`usage` 打印 `rm -rf <root>/sha256 && ngm install`），而不是为了迁移去动那条短路。

### v0.7（计划：[v0.7-plan.md](./v0.7-plan.md)）— **已交付**

范围 = [ADR-018](../adr/adr-018-store-reclaim.md) 决策 2 的落地：让 content store 的
**占用可见**（`ngm store usage`，只读）与**残骸可回收**（`ngm store prune`，只清解包残骸）。
两组均已交付 ✅。

**剩下的候选**（按证据强度排序，详见 [v0.6 复盘 §7](./v0.6-retrospective.md#7-v07-候选按证据强度排序非路线图承诺)）：

| # | 候选 | 前置 |
|---|------|------|
| 1 | ~~层 2 **写入侧去重**（blob 池 + 树清单）~~ | ✅ **v0.8 已交付**（[ADR-019](../adr/adr-019-content-addressed-blobs.md)）：20.0× → 0.6×。**它改的是斜率，不是终点**——回收仍受 ADR-018 那两个条件约束 |
| 2 | `remote` adapter 门槛 b：抽样构建的**成本数字** | **门槛 a 已满足** → 这是"要不要发布"**唯一**剩下的条件 |
| 3 | `deno bundle` 真引擎覆盖 | 上游把 bundle 标为稳定（截至 2026-10-02 未变） |
| 4 | 3s 目标的口径重定义 | 等次数门禁稳定运行几个版本 |

---

## 验收机制

- **能与代码同源检查的，一律做成机械检查**（CI 里红，而不是文档里写）。目前这些是：
  | 检查 | 位置 | 守什么 |
  |------|------|--------|
  | `go test ./...` + 验收组 | `ci.yml` | 行为与不变量（含权限网、spawn 预算、store 布局） |
  | `gofmt -l` + 行尾 | `ci.yml` | 格式与 LF（golden 按字节比对，CRLF 会让它永久失配） |
  | `scripts/check-docs-links.sh` | `ci.yml` job `docs-links` | 文档**相对链接的目标文件是否存在**（**不校验锚点**——中文标题的 GitHub 锚点算法不复刻，见脚本头部） |
  | `scripts/check-release-status.sh` | `ci.yml` job `release-status` | **有复盘 ⇔ 有 tag**（"已交付"与"已发布"不许脱节） |
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
- **复盘不能跳**：v0.6 之后这一条被跳过了一次——v0.7 交付时没有写复盘，直到 v0.8 收尾才发现
  （[v0.7 复盘](./v0.7-retrospective.md) §5.2 记了这件事与它的代价）。复盘是本项目**唯一**
  系统记录"计划 vs 实际"的地方，跳过它，偏离就只剩代码注释与 commit message 可查。
  - ✅ 已补：[v0.7 复盘](./v0.7-retrospective.md)（补写，合并登记 v0.8 对它三处交付面的改动）
  - ✅ 已完成：[v0.8 复盘](./v0.8-retrospective.md)

---

## 发布清单（每个版本）

> **第 0 步是 v0.5 补上的，它是这份清单里最容易被跳过的一步**：
> **版本交付即打 tag**。v0.2 / v0.3 / v0.4 交付时都没打——结果是这三版在仓库里存在、
> 在任何发行源上都取不到，直到 2026-10-01 才补发。**不发 tag 等于"这版只存在于源码里"**。
>
> ⚠️ **同一个错误又犯了四次**：`v0.5.0` ~ `v0.8.0` 交付时也都没打 tag——与 v0.2~v0.4
> 当年一模一样。这说明"写进清单"不足以让这一步发生：它是**唯一一步不与代码同源、
> 且过去没有任何自动提醒**的交付动作（代码提交会顺手做，打 tag 不会）。
>
> ✅ **已处置（2026-10-02）**：四个 tag 已补（`v0.5.0` → `3c3640f`、`v0.6.0` → `7ce4566`、
> `v0.7.0` → `bf6ba5e`、`v0.8.0` → `9af7f7f`，都是附注 tag），已推 `origin`；
> **并且把"已交付 ⇔ 已打 tag"变成了一条机械检查**（`scripts/check-release-status.sh`，
> CI job `release-status`）——判据是**复盘文件**：有 `docs/development/vX.Y-retrospective.md`
> 就必须有 `vX.Y.*` 的 tag，反之亦然。
> 之所以不用计划里那行"状态：已交付"：那一行是散文，格式历来不统一（v0.1 写"本计划已全部完成"，
> v0.2~v0.4 干脆没有），**用一个会漏的判据做门禁等于没做**。而复盘与 tag 的位置在本文里
> 是同一处（"版本的最后一次提交是该版的复盘提交"），所以两条可以互推。
> 反例已验证（缺 tag / 缺复盘 / 轻量 tag / **根本看不到 tag** 四种都会红——最后一种顺带
> 修掉了脚本里一个"静默退出、一个字都不打印"的 bug，见脚本头部）。

0. **交付该版本时立刻打 tag**（不要等"下次一起发"）：版本的最后一次提交（本项目的惯例是
   该版的复盘提交）上打附注 tag。判据很简单：**这一版的产物能不能被用户下载到**。
1. `git tag -a v0.x.y -m "..."`，然后 `git push origin v0.x.y`
2. Gitee 镜像把 tag 推到 GitHub → `release.yml` 执行：**先跑全量测试**，再交叉编译六平台，
   最后 `gh release create` 发布（含 `SHA256SUMS`）

   ⚠️ **别假定这一步发生了**（2026-10-02 实测）：同一批补打的 tag 里，
   `v0.5.0` ~ `v0.8.0` 四个**没有**触发 `release.yml`（`git ls-remote` 可见、附注对象完整，
   而 `/actions/workflows/370079274/runs` 里只有 v0.1.0 ~ v0.4.0 四次），
   而稍后补打的 `v0.9.0` **触发了**，release 也正常发布（2026-10-02 14:50，7 个产物 + 2 个源码包）。
   同样的路径、同样的手法，结果不一致——**原因不在本仓库可观测的范围内**。
   （这也顺带证明 `release.yml` 在加过 `workflow_dispatch` 之后，**tag 推送那条路仍然工作**。）

   所以规则不是"它会不会触发"，而是"**打完必须验**"：

   ```bash
   bash scripts/check-release-status.sh --published
   ```

   它逐个 tag 问 GitHub：release 在不在、资产是不是 7 个。缺的走**不依赖镜像**的入口：
   Actions → Release → **Run workflow**（填那个 tag；`workflow_dispatch` 是 v0.8 收尾时加的，
   与 tag 推送跑同一份脚本与同一组断言）。**别用"删掉 tag 再推一次"**——
   那是拿同一条已被证明不稳定的路径再赌一次。

   > 本地跑受**匿名 API 限额**（60 次/小时，每个 tag 一次请求）约束；有 `GITHUB_TOKEN` /
   > `GH_TOKEN` 时带上（5000 次/小时）。**撞限额后它不再逐个去撞**：报一次原因、其余标成
   > `NOT CHECKED`。查不动时以 `exit 3` 报告"没查成"——**不报成功**
   > （与脚本里其它空跑保护同源）；CI 里带 token，所以那条告警路径不受影响。
3. 核对 GitHub release 的 7 个 asset（6 个二进制 + `SHA256SUMS`）——**一条命令**：

   ```bash
   bash scripts/check-release-status.sh --published
   ```

   它对每个 tag 问一次 GitHub：有没有 release、资产是不是 7 个、`SHA256SUMS` 在不在，
   并给缺失项打印可直接照做的出路（`Actions → Release → Run workflow`）。
   退出码 `0` = 全部就绪，`1` = 有缺，`3` = 查不动（网络/限额）——**查不动不报成功**。

   > 它同时是"**tag 存在 ≠ 产物能被下载**"这条的机械读数：2026-10-02 就是四个 tag 全在、
   > release 一个没有，而当时没有任何检查会红。CI 里它作为 `release status` job 的
   > 第二个步骤跑（**告警不阻塞**：那四个缺失只有维护者能清，一条推送清不掉的红 job
   > 会训练人忽略红色；补齐后改成真门禁）。

   > **CI 结果本身也可以自己读**：`https://api.github.com/repos/idcu/ngm/actions/runs?per_page=5`
   > （公开仓库的 Actions 结果是**公开 REST API**，不需要 token；某个 run 的失败 job 见
   > `.../runs/<id>/jobs`）。v0.8 收尾时正是因为一直假定"读不到 CI"，让 `main` 连红了四次
   > 才发现——见 [v0.8 复盘 §5.7](./v0.8-retrospective.md) 与
   > [v0.6 复盘 §5.4](./v0.6-retrospective.md)。
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
> **已用真实令牌端到端跑通**（2026-10-02，v0.2.0/v0.3.0/v0.4.0 各 7/7）；
> GitHub 连不上时可加 `-GitHubProxy`（清单仍优先直连取得，见下）。

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

> ✅ **已完成（2026-10-02）**：三个版本的 21 个附件（各 6 个二进制 + `SHA256SUMS`）已上传到 Gitee，
> 每个附件都在上传后**从 Gitee 取回重新核对** sha256 —— 与 GitHub 的 `SHA256SUMS` 一致。
> 此前"21 个文件逐个下载再上传"的手工步骤不再需要；现在每个 tag 是一条命令。

### 补发记录（2026-10-02，Gitee 侧）

| tag | Gitee release id | 附件 | 上传后复核 |
|-----|-----------------|------|-----------|
| `v0.2.0` | 1179281 | 6 个二进制 + `SHA256SUMS` = **7** | 7/7 sha256 一致 ✓ |
| `v0.3.0` | 1179275 | 同上 | 7/7 ✓ |
| `v0.4.0` | 1179268 | 同上 | 7/7 ✓ |

**两源直接抽查（D4，不依赖上传脚本的内部状态）**：

| 项 | 结果 |
|----|------|
| 三个 tag 的 `SHA256SUMS`：Gitee vs GitHub | **逐字节相同**（`ca17049e…` / `f8512161…` / `08cfa80e…`） |
| 抽查 `v0.4.0` 的 `ngm-linux-amd64`（**从 Gitee** 下载） | `2b2dd4f422fbb8a8…` —— 与清单里的值**完全一致** ✓ |

> **Gitee 的 API 有三个坑，都已写进脚本**（下次改它的人不必再踩一遍）：
>
> 1. **`target_commitish` 必须是分支名**（`main`）。写成 tag 名时 Gitee **不报错**，
>    而是回一个没有 `id` 的 JSON——正是"静默失败"的形态；脚本靠"没有 id 就停"接住了它。
> 2. **"release 不存在"返回的是 JSON `null`（HTTP 200）**，而 PowerShell 5.1 的
>    `Invoke-RestMethod` 把它变成字符串 `"null"`——既 truthy 又没有 `.id`，于是"查不到"
>    判不出来。现在统一由一个 `Get-GiteeRelease` 折成 `$null`。
> 3. **`Invoke-RestMethod` 的字符串 body 按 ISO-8859-1 编码**，中文会写成 `???`；
>    要按 UTF-8 **字节**发（否则 release 说明是乱码）。
>
> 另有一条环境事实：**本机到 github.com 的连通性会抖**（实测 `curl: (28)` 连接超时，
> 重试 4 次仍失败）。因此脚本支持 `-GitHubProxy https://ghproxy.net`，
> 并且**清单优先直连取得**——清单是校验的信任锚，只有直连失败时才退到代理且明确警告。
>
> 还有一处与直觉不同的计数：Gitee 会给每个 release **自动附上两个源码包**
> （`<tag>.zip` / `<tag>.tar.gz`），所以 API 里会看到 7 + 2 = 9 个 asset。
> "7 个附件"指的是**我们自己传的那 7 个**。

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

- [v0.1 实施计划](./v0.1-plan.md) / [v0.2](./v0.2-plan.md) / [v0.3](./v0.3-plan.md) / [v0.4](./v0.4-plan.md) / [v0.5](./v0.5-plan.md) / [v0.6](./v0.6-plan.md) / [v0.7](./v0.7-plan.md) / [v0.8](./v0.8-plan.md) / [v0.9](./v0.9-plan.md) / [v0.10](./v0.10-plan.md)
- [v0.1 复盘](./v0.1-retrospective.md) / [v0.2 复盘](./v0.2-retrospective.md) / [v0.3 复盘](./v0.3-retrospective.md) / [v0.4 复盘](./v0.4-retrospective.md) / [v0.5 复盘](./v0.5-retrospective.md) / [v0.6 复盘](./v0.6-retrospective.md) / **[v0.7 复盘](./v0.7-retrospective.md)**（补写）/ **[v0.8 复盘](./v0.8-retrospective.md)** / [v0.9 复盘](./v0.9-retrospective.md) / [v0.10 复盘](./v0.10-retrospective.md) / [v0.5 复核](./v0.5-review.md)
- [internals/roadmap.md](../internals/roadmap.md)
- [internals/metrics.md](../internals/metrics.md)
- [modules/p0-core.md](../modules/p0-core.md)（包级结构唯一事实源）
- [guides/cli.md](../guides/cli.md)（命令形态）