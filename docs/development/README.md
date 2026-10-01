# 开发总览

> **当前阶段：v0.1 已完成并已发布**——首个提交 `a47b0ad` 已推送 Gitee 主仓并镜像到 GitHub，
> CI（GitHub Actions）三平台 15 个 job 全绿；**`v0.1.0` 已发布**（六平台二进制 + `SHA256SUMS`，共 25.05 MB）。
> **v0.2 尚未开始**。
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

- 依赖最小化：v0.1 只用 Go 标准库 + `golang.org/x/...` 按需；引入第三方库需在 PR 说明理由
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
  - ✅ 已完成：[v0.3 复盘](./v0.3-retrospective.md)（五组交付项全部结项；`remote` 经
    [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布；11 条设计偏离、8 处已修缺陷——
    其中 2 处在**已发布代码**里；1 项未结项：沙箱自述文件签名检查）

---

## 发布清单（每个版本）

发布由**打 tag 触发**，但 Gitee 侧的附件是**手动**的——别漏掉第 4 步：

1. `git tag -a v0.x.y -m "..."`，然后 `git push origin v0.x.y`
2. Gitee 镜像把 tag 推到 GitHub → `release.yml` 自动执行：**先跑全量测试**，再交叉编译六平台，
   最后 `gh release create` 发布（含 `SHA256SUMS`）
3. 核对 GitHub release 的 7 个 asset（6 个二进制 + `SHA256SUMS`）
4. **把这 7 个文件从 GitHub release 上传到 Gitee 发行版**（在 Gitee 无 API token 时，这是最短且最稳的路径）。
   上传后附件直链即为 `https://gitee.com/idcu/ngm/releases/download/<tag>/<文件名>`
5. 抽查一次：两个源的同一文件名 `sha256` 应完全相同（它们共用同一份 `SHA256SUMS`）

> **Gitee 免费额度的容量**（已核实）：单附件 ≤ 100MB、仓库附件总量 ≤ 1GB，
> 且**仓库附件与发行版附件合并计算**。v0.1.0 实测 7 个产物共 **25.05 MB**
> （单文件 4.06–4.35MB；`build-release.sh` 刻意不 strip，用体积换 panic 栈可诊断性），
> 因此约可容纳 40 个版本——短期不需要为此做任何取舍。

---

## 相关文档

- [v0.1 实施计划](./v0.1-plan.md) / [v0.2](./v0.2-plan.md) / [v0.3](./v0.3-plan.md)
- [v0.1 复盘](./v0.1-retrospective.md) / [v0.2 复盘](./v0.2-retrospective.md) / [v0.3 复盘](./v0.3-retrospective.md)
- [internals/roadmap.md](../internals/roadmap.md)
- [internals/metrics.md](../internals/metrics.md)
- [modules/p0-core.md](../modules/p0-core.md)（包级结构唯一事实源）
- [guides/cli.md](../guides/cli.md)（命令形态）