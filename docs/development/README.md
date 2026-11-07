# 开发总览

> **当前阶段：v0.1 ~ v0.101 均已交付**；**`v0.1.0` ~ `v0.101.0` 共 101 个 tag 均已打**。
>
> **工作线编号不是版本号**（读代码前先看这条）：v0.101 那一版的改动分散在若干条并行的
> 工作线上，源码注释与测试名里用了 `v0.105`–`v0.113` 作为**线内编号**
> （`TestV108EveryOSVQueryCarriesTheGate`、`v107_test.go`、`internal/boundbuf` 等）。
> 真实版本只到 **v0.101**——目录里没有、也不会有 v0.105 的复盘。
>
> 为什么不现在改名：这些编号出现在测试名里，而退出码来源登记表（`V57`）按**名字**
> 引用若干用例，批量改名要同时动 78 个文件与几张登记表；风险大于收益。
> 等某条线真的走到那个版本号时顺手改即可。在那之前，读到 `TestV108…` 应当理解为
> "v0.101 的第八条工作线"，而不是"v0.108 版本"。

> v0.12 的主题是**把读数修准**（含文档自己的读数）：见[复盘](./v0.12-retrospective.md)；
> v0.13 把这件事推到下一步——**把形状固化成网**（产品代码 0 行改动）：
> 见[复盘](./v0.13-retrospective.md)；
> v0.14 换了方向——**把"文档承诺"与实测行为对账**（同样 0 行产品代码改动）：
> 见[复盘](./v0.14-retrospective.md)；
> v0.15 把两条**对外契约**钉成网（文档示例必须成形 + 用法错误可判别），
> 顺带修掉一个真缺陷（解析错误绕过注入的 writer）：见[复盘](./v0.15-retrospective.md)。
> 发布是另一回事，且**2026-11-03 已复核过两个源的 API**：
> GitHub 有 101 个里的 **63 个**，Gitee 只有 **3 个**
> （`v0.2.0`/`v0.3.0`/`v0.4.0`，各 7 个附件）——**`v0.1.0` 在 Gitee 上没有发行版**，
> 而本文件此前把它算成了"两个源都可取到"。逐版状态见[发布状态](../milestones-b.md#发布状态)；
> Gitee 侧随时可用 `bash scripts/check-gitee-release-status.sh` 复读（发布清单第 6 步；
> Windows 用 `scripts/check-gitee-release-status.ps1`，它转调同一个实现）。
>
> **历史版本补发：暂缓**（2026-10-04，项目所有者决定）
>
> | 待补 | 缺什么 | 恢复时怎么做 |
> |------|--------|-------------|
> | `v0.5.0` ~ `v0.8.0` 的 GitHub release | **4 个**——镜像转发了 tag 却没触发 `release.yml`，**原因不明** | Actions → Release → **Run workflow**，填 tag（[发布清单](#发布清单每个版本)第 2 步） |
> | Gitee 发行版 | **15 个**：`v0.1.0` 及 `v0.5.0` ~ `v0.18.0` | `bash scripts/gitee-release.sh --tag vX.Y.0 --assets dist`（需 `GITEE_TOKEN`） |
>
> **这是有意延后，不是遗漏。** 判据是"有没有人要这些二进制"：
> 两个缺口涉及的版本**功能全都在源码里**（`v0.5` ~ `v0.12` 的每一处能力都能按
> [安装指南](../guides/installation.md)「方式二」编译得到），而最新的 `v0.12.0`
> 已经可以在 GitHub 上直接下载。补发的收益是"历史版本可下载"，成本是一个凭据加几分钟——
> **因此它随时可以恢复，只是现在不值得占用注意力。**
>
> **值得恢复的信号**（任一出现就值得做）：① 有人要用 Gitee 作为分发源、且要 `v0.4.0` 之后的版本；
> ② 需要为审计/追溯把某个历史版本的二进制固定下来；③ token 已经在手上——那时它退化成纯机械动作。
>
> ⚠️ **暂缓期间有两处黄灯是预期的，不是回归信号**：
> CI job `release-status` 里的 `--published` 步骤会继续报那 4 个缺失（它按设计**只告警不阻塞**，
> 理由正是"只有维护者能清"），`scripts/check-gitee-release-status.ps1` 也会报 9 个缺。
> **两条读数都要保留**：暂缓的是"补"，不是"看"——一旦有人误删了已发布的附件，它们仍然会说话。
>
> 那条老纪律仍然有效：`release.yml` 会不会被触发**不能假定**（同一批推四个没触发、
> 单独推 `v0.9.0`/`v0.10.0`/`v0.11.0`/`v0.12.0` 四次都触发），所以规则是"**打完必须验**"。
>
> 首个提交 `a47b0ad` 已推送 Gitee 主仓并镜像到 GitHub，CI（GitHub Actions）三平台全绿
> （最近 10 次 run 全部 success）。
> "已交付 ⇔ 已打 tag"由 `scripts/check-release-status.sh`（CI job `release-status`）机械守住；
> "release 是否真的存在"由它的 `--published` 读取，**查不动时拒绝报成功**。
> 例外只有**预发布 tag**（`vX.Y.Z-<suffix>`，如 `v0.102.0-dev.1`）：它不是一个交付版本，
> 不要求复盘，但仍须是附注 tag、且仍须真的发布（`--published` 连同它一起查）。
> 发布流水线与回填范围见[发布流水线](../guides/release-pipeline.md)。
>
> 逐版账本（每版的计划 / 复盘 / 一句话结论）单独成文：[ledger.md](./ledger.md) ——
> 这张表 87KB，挪走后本文件只留"怎么工作、怎么验收"。
>

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

> 单独成文：[stages.md](stages.md) —— 它是一张 160 行的逐版表，占原文件 100KB；索引要先回答"怎么工作、怎么验收"。
> `TestV63MilestoneTablesListEveryVersionExactlyOnce` 要求每个版本恰好一行、顺序正确（起点 v0.5）。

## 验收机制

> 单独成文：[acceptance.md](./acceptance.md) —— 什么算做完、谁来核、核什么（39KB，含各类判据的口径）。

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

   > **同一个问题的另一个方向：被下游 pin 住的版本，也必须有产物。**
   > 上面第 0 步问的是"我这一版发出去了吗"，这一问是"**别人钉住的那一版**还存在吗"——
   > 下游把 `.ngm-version` 钉在某个版本上，而那个版本没有可下载产物时，每个下游都只能
   > 复制一段"如何从源码构建正确版本"的知识，而那段知识没有判据守着。
   > 判据已经在 `scripts/check-release-status.sh` 里：`--published` 会读 `ngm-repo` 的
   > `.ngm-version`，并对该版本逐个核对 release 与附件，读数里 `pin` 那一行就是结论
   > （读不到时写 `COULD NOT CHECK`，**不会**写成通过）。
   > 这条与"每个版本都有复盘"是**两条不同的判据**，别把它们混成一条。
2. Gitee 镜像把 tag 推到 GitHub → `release.yml` 执行：**先跑全量测试**，再交叉编译六平台，
   发布前 `sha256sum -c SHA256SUMS` + 版本注入检查，最后 `gh release create`
   上传既有名字与版本化别名；发布说明由 `CHANGELOG.md` 的对应段落提供
   （`scripts/release-notes.sh`，缺失即失败）。完整规则见[发布流水线](../guides/release-pipeline.md)。

   ⚠️ **别假定这一步发生了**（2026-10-02 实测）：同一批补打的 tag 里，
   `v0.5.0` ~ `v0.8.0` 四个**没有**触发 `release.yml`（`git ls-remote` 可见、附注对象完整，
   而 `/actions/workflows/370079274/runs` 里只有 v0.1.0 ~ v0.4.0 四次），
   而稍后补打的 `v0.9.0` **触发了**，release 也正常发布（2026-10-02 14:50，7 个产物 + 2 个源码包）。
   同样的路径、同样的手法，结果不一致——**原因不在本仓库可观测的范围内**。
   （这也顺带证明 `release.yml` 在加过 `workflow_dispatch` 之后，**tag 推送那条路仍然工作**。）

   > **2026-10-04：本步对 `v0.5.0` ~ `v0.8.0` 暂缓**（项目所有者决定；2026-10-03 曾暂缓一次又取消，
   > 这次是重新拍板）。成因是**凭据**（无 token 时 `POST /actions/workflows/…/dispatches` 返回 401），
   > 而理由是"现在没有人在等这些二进制"——**有意延后，不是遗漏**，恢复信号见本文开头。
   > 暂缓的是"补"，不是"看"：下面的读数与告警步骤**一律保留**。
   >
   > **2026-11-03 重新划定回填范围**：只补最近 3 个正式版（`v0.99.0` / `v0.100.0` /
   > `v0.101.0`）与 `ngm-repo` 钉住的 `v0.102.0-dev.1`；其余历史 tag **明确不回填**。
   > 执行入口是 `.github/workflows/backfill.yml`（`workflow_dispatch`），
   > 需要 GitHub 写权限；Gitee 附件还需要 `GITEE_TOKEN`。详见
   > [发布流水线](../guides/release-pipeline.md) §2。

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
   >
   > ⚠️ **2026-10-04 起那四个缺失是"暂缓"，因此这条黄灯会长期存在**——**它是预期的，
   > 不是回归信号**（见本文开头的恢复信号）。**但读数不能因为"知道结果"就撤掉**：
   > 读数的用途是让"谁也不知道"变成"谁都知道"，而不是省一次查询。

   > **CI 结果本身也可以自己读**：`https://api.github.com/repos/idcu/ngm/actions/runs?per_page=5`
   > （公开仓库的 Actions 结果是**公开 REST API**，不需要 token；某个 run 的失败 job 见
   > `.../runs/<id>/jobs`）。v0.8 收尾时正是因为一直假定"读不到 CI"，让 `main` 连红了四次
   > 才发现——见 [v0.8 复盘 §5.7](./v0.8-retrospective.md) 与
   > [v0.6 复盘 §5.4](./v0.6-retrospective.md)。
4. **把这 7 个文件从 GitHub release 上传到 Gitee 发行版**（在 Gitee 无 API token 时，这是最短且最稳的路径）。
   上传后附件直链即为 `https://gitee.com/idcu/ngm/releases/download/<tag>/<文件名>`
   **（2026-10-04：本步对历史版本暂缓——缺 98 个；新版本发布时仍然照做，恢复信号见本文开头。）**
5. 抽查一次：两个源的同一文件名 `sha256` 应完全相同（它们共用同一份 `SHA256SUMS`）
6. **读一次 Gitee 侧的状态**（**v0.12 追加**）——它此前**没有读数**，而"两个源都可取到"
   那句错话正是从这条空白里长出来的：

   ```bash
   bash scripts/check-gitee-release-status.sh          # 唯一实现（任何有 bash 的环境）
   powershell -File scripts/check-gitee-release-status.ps1   # Windows 入口，转调上面那个
   ```

   它对每个 `v*` tag 报"有没有发行版、我们自己传的 **7 个**附件齐不齐"，并对缺的那几个
   打印可直接照做的补发命令。判据与 GitHub 侧对齐：**查不动不报成功**（exit 3），
   有缺 exit 1、全齐 exit 0。它**只读公开 API、不需要 token**，因此随时可跑。
   Gitee 自动附的 `<tag>.zip` / `<tag>.tar.gz` **单独计数**（按名字排除，不按数量相减）。

   > **它是单实现**：`.sh` 是唯一实现，`.ps1` 只转调（与 `check-docs-links` 同一形状——
   > 要检查的规则只允许有一种写法；那份脚本曾因两份实现漂移而让本地一直报 OK、CI 连红四次）。
   > 因此这条读数在 Windows / macOS / Linux 上是**同一段代码**。

   > **它故意不进 CI**（v0.13 的候选 #1 因此结项为"不做"）：暂缓期间它会是一条**长期黄**的
   > 步骤，而项目自己的纪律是"一条推送清不掉的红/黄 job 会训练人忽略颜色"；
   > 另外 Gitee 的匿名 API **有限额**（实测连跑多次会被限流 → 全部 `NOT CHECKED` + exit 3），
   > 那会让 CI 多出一类与代码无关的噪音。**触发条件**：补发恢复后，
   > 或它在 CI 上被证明稳定（例如接一个只读的 Gitee token）。

   > ⚠️ **暂缓期间它报"缺 98 个"是预期的**（见本文开头）：这个读数的用途不是催办，
   > 而是让"两个源到底有什么"永远有一个不需要信任谁的答案。

> **v0.6：第 3 ~ 5 步现在是一条命令**（`scripts/gitee-release.sh`，Windows 入口 `.ps1`）：
>
> ```bash
> gh release download v0.99.0 --dir dist          # 先取 GitHub 上已发布的字节
> bash scripts/gitee-release.sh --tag v0.99.0 --assets dist --dry-run
> GITEE_TOKEN='<Gitee 私人令牌>' bash scripts/gitee-release.sh --tag v0.99.0 --assets dist
> ```
>
> 它**只上传已有产物**，不在 Gitee 侧重编——两个源必须是同一份字节。
> 它按 `SHA256SUMS`（**同一个文件**）逐个核对，并在上传后**再从 Gitee 下载一遍比对**；
> 同名附件默认跳过（**幂等**），`--force` 才先删后传。缺 token 时明确 exit 2、
> 不动远端；dry-run **零网络请求**。
> **本轮没有 `GITEE_TOKEN`**：真实上传**未执行**；dry-run 与假 API 判据
> （创建 / 更新 / 跳过 / `--force` / 零请求）已跑通，见 `TestV102GiteeRelease*`。

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

## 纪律自评（十问看板）

```bash
node scripts/check-discipline.mjs
```

它读的是 `docs/practice/engineering-discipline.md` §8 的十问：判据有没有、在不在 CI、
本地与 CI 是否同一条、契约、生成物、依赖锁定、文档分层、删除记录、牙齿。
**它不替代 `go test`**——它只回答"这个仓库现在几档、缺哪一条"。

当前：**10/10 → L4**。

## 相关文档

- [v0.1 实施计划](./v0.1-plan.md) / [v0.2](./v0.2-plan.md) / [v0.3](./v0.3-plan.md) / [v0.4](./v0.4-plan.md) / [v0.5](./v0.5-plan.md) / [v0.6](./v0.6-plan.md) / [v0.7](./v0.7-plan.md) / [v0.8](./v0.8-plan.md) / [v0.9](./v0.9-plan.md) / [v0.10](./v0.10-plan.md) / **[v0.11](./v0.11-plan.md)**
- [v0.1 复盘](./v0.1-retrospective.md) / [v0.2 复盘](./v0.2-retrospective.md) / [v0.3 复盘](./v0.3-retrospective.md) / [v0.4 复盘](./v0.4-retrospective.md) / [v0.5 复盘](./v0.5-retrospective.md) / [v0.6 复盘](./v0.6-retrospective.md) / **[v0.7 复盘](./v0.7-retrospective.md)**（补写）/ **[v0.8 复盘](./v0.8-retrospective.md)** / [v0.9 复盘](./v0.9-retrospective.md) / [v0.10 复盘](./v0.10-retrospective.md) / **[v0.11 复盘](./v0.11-retrospective.md)** / [v0.5 复核](./v0.5-review.md)
- [internals/roadmap.md](../internals/roadmap.md)
- [internals/metrics.md](../internals/metrics.md)
- [modules/p0-core.md](../modules/p0-core.md)（包级结构唯一事实源）
- [guides/cli.md](../guides/cli.md)（命令形态）
