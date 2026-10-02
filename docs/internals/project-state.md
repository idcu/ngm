# 项目状态评估（2026-10-03）

> 这一页回答六个问题：**项目目标 / 任务进度 / 未完成任务 / 阻塞原因 / 下一步任务 / 实际怎么用**，
> 外加一节**与主流工具的对比**。
>
> 它是**评估**，不是事实源：目标以 [roadmap](./roadmap.md) 为准，能力以
> [capability-matrix](./capability-matrix.md) 为准，数字以 [metrics](./metrics.md) 为准。
> 本页只做汇总与判断，并给每条判断标出出处——**没有出处的判断不写**。
>
> 写这一页的动机：这个项目已经交付了 10 个版本，但"现在到底能用到什么程度、卡在哪、
> 下一步该做什么"分散在 200 多个文档里。**一个人（或一个 AI）接手时读不到它。**

---

## 1. 项目目标

**一句话**：`ngm` 是给 **Git 依赖**做的包管理与完整性工具——把 Git 依赖的**可达、可锁、可验、
可审**做成一条闭环，而不是把 Git URL 塞进 `package.json` 之后听天由命。

**它解决的问题**（[README · 它是什么](../README.md#它是什么)）：

| 问题 | ngm 的做法 |
|------|-----------|
| "这个 commit 到底是不是我要的那份代码？" | `refType` 必填 + `archiveDigest`（规范化内容清单 + sha256）+ `verify` 可本地重放 |
| "上游 branch 前进了，这是预期更新还是被换过？" | `driftKind`：`expected` / `unexpected` / `critical` 三态，预期更新默认不阻断 |
| "构建机断网/token 失效时还能不能构建？" | vendor 4 层：落地后构建不依赖网络；安装可 `--frozen-lockfile --offline` |
| "供应链策略能不能绑在 commit 上？" | 白名单 / `minimumReleaseAge` / OSV（**按 commit 查**）/ postinstall 只跑沙箱内 JS 钩子 |
| "审计要的 vendor 目录能不能提交？" | 强调可审计（不是只省磁盘）——这一条是它与 pnpm store 的取向差别 |

**明确的非目标**（[capability-matrix](./capability-matrix.md#明确不做的能力)）：不做 bundler /
HMR / test runner / docs generator / LSP / Dev Server / CSS 编译器 / registry 包管理 / type checker。
这些一律交给已成熟的工具，ngm 通过 **adapter** 调它们。

**差异化在哪**（[capability-matrix · 诚实版](./capability-matrix.md#诚实版能力评估)）：
不在单项能力（Git 依赖、content store、OSV、白名单、漂移检测**竞品都有**），
而在**流程闭环**与**共享状态模型**——`refType` 必填 + digest + verify 三件套绑在一起，
且策略与锁定都指向**同一个 commit**。

---

## 2. 任务进度

十版全部交付（每版都有计划 + 复盘，除 v0.7 是补写的——见 [v0.7 复盘 §5.2](../development/v0.7-retrospective.md)）。

| 版本 | 主题 | 交付 |
|------|------|------|
| v0.1 | 最小闭环 | Git 声明/锁定 + vendor 4 层 + `verify` + esbuild adapter |
| v0.2 | 供应链与可观测 | 策略字段集 + `audit`（OSV，按 commit）+ `why`/`tree`/`outdated` + tsc/deno/postcss adapter + `install` 的 CI 模式 |
| v0.3 | 生态与沙箱 | wasm adapter + `integrations add`（Vite/esbuild/Deno/Webpack）+ Deno 沙箱 + 权限与凭证 + mappings 子路径 |
| v0.4 | 签名与接线 | `typedecl` + `verify --signatures`/`--require-signed` + 配置字段接线的机械检查 |
| v0.5 | 收敛与交付 | 在线 verify 成本/方差 + ADR-013 判定 + 权限施加点核对 + 三版补发布 + `transform` |
| v0.6 | 让结论可判别 | **spawn 预算门禁** + 跨机器可复现性判定 + store 增长数据 + 发布状态门禁 |
| v0.7 | store 可见可回收 | `store usage`（只读）+ `store prune`（只清残骸） |
| v0.8 | 层 2 换布局 | blob 池 + 树清单（ADR-019）：**20.0× → 0.6×** |
| v0.9 | 读数说真话 | blob 池切成共享/独占/孤儿 + 锚点检查 + 10 万级规模实测 |
| v0.10 | 检查推到最外圈 | 根 README 的链接与锚点 + "store 不完整"的两条承诺（实现 0 行改动） |

**发布状态**（`bash scripts/check-release-status.sh --published`，2026-10-03 复核）：

| 版本 | tag | GitHub release | Gitee 附件 |
|------|-----|----------------|-----------|
| `v0.1.0` ~ `v0.4.0` | ✅ | ✅ 各 7 个 | ✅ 各 7 个（2026-10-02 补） |
| `v0.5.0` ~ `v0.8.0` | ✅ | ❌ **未生成**（**你决定暂缓**，见 §4） | ❌ 同上 |
| `v0.9.0` / `v0.10.0` | ✅ | ✅ 各 7 个（tag 推送触发，已复核） | ❌ 待上传（**暂缓**） |

**表面数字**（[metrics](./metrics.md)）：14 个测试包 + 16 个 CI job（三平台）；
`verify` 的 spawn 预算 2.00（commit）/ 3.00（tag）每依赖；层 2 磁盘增长 0.6×；
10 万 blob 下 `usage` 244 ms。

---

## 3. 未完成任务

按"谁能推动"分四类——**这个分类比"优先级"更有用，因为它直接说明下一步该找谁**。

### 3.1 需要**你**决策（数据都已就位）

| # | 任务 | 数据 | 为什么卡在决策 |
|---|------|------|---------------|
| 1 | **是否发布 `remote` adapter** | 门槛 a 已满足（3 平台 × 7 形态字节一致） | 门槛 b 是"用户是否愿意为一次远端构建而在本地抽样构建"——产品问题，[ADR-017](../adr/adr-017-remote-adapter-release-decision.md) 已把它写成可判定的门槛 |
| 2 | **`symlink` 落地模式是否恢复** | v0.8 起退化为逐条目 hardlink（语义等价、磁盘收益不变） | 要恢复得"按需物化一棵 hardlink 树"——新决策，不是回滚 |
| 3 | **blob 回收（GC）是否排期** | v0.9 已把"孤儿字节"变成可读的数；ADR-018 的两个条件未变（ngm 没有项目注册表） | 删内容树的风险需要一个明确的决策 |
| 4 | **3s 目标的口径** | v0.5 证明它在噪声里不可判别；v0.6 已把次数做成门禁 | 需要一句正式的"目标线退役/降级为观测值" |

### 3.2 需要**人工动作**（你做一次，我随后可核对）

| # | 任务 | 状态 |
|---|------|------|
| 5 | `v0.5.0` ~ `v0.8.0` 的 GitHub release | **暂缓**（2026-10-03）。恢复时：Actions → Release → Run workflow，填 tag |
| 6 | `v0.9.0` / `v0.10.0` 的 Gitee 附件 | **暂缓**。恢复时：`scripts/upload-gitee-assets.ps1 -Tag vX.Y.0`（需 `GITEE_TOKEN`） |

### 3.3 等**上游**

| # | 任务 | 条件 |
|---|------|------|
| 7 | `deno bundle` 真引擎覆盖 | Deno 把 bundle 标为稳定（截至 2026-10-03 未变） |

### 3.4 **可以自动推进**（无需决策、无外部依赖）

| # | 任务 | 说明 |
|---|------|------|
| 8 | workflow 的 **YAML 有效性门禁** | 改坏 `release.yml` 会**静默不跑**（没有 run 就没有失败可读）；判据未定——需要先确认"坏文件在 API 里长什么样" |
| 9 | 更多"没人检查的角落" | v0.9/v0.10 的方法是先审计再接线；可继续地毯式找 |
| 10 | 端到端可用性复核 | 在干净环境跑一遍 quickstart（含真实引擎），把摩擦点记下来 |

---

## 4. 阻塞原因

**逐条追因**（不是"没做"，而是"为什么没做完"）：

| 阻塞 | 根因 | 证据 |
|------|------|------|
| `remote` adapter 不发布 | **产品判断**，不是技术：需要有人回答"用户愿不愿意为一次远端构建付一次本地抽样构建" | [ADR-013](../adr/adr-013-remote-adapter.md)（翻案条件第 1 条的技术前提**已成立**）· [ADR-017](../adr/adr-017-remote-adapter-release-decision.md) |
| 四个 release 未生成 | **镜像转发 tag 不保证触发 `release.yml`**：`v0.5.0`~`v0.8.0`（一批四个）没触发，而 `v0.9.0`/`v0.10.0`（单独打）**都触发了**。原因不在本仓库可观测范围内 | [发布清单](../development/README.md#发布清单每个版本) · v0.8 复盘 §2 |
| Gitee 附件 | Gitee 无 API token 时是**人工上传**；工具已就绪（幂等、双向校验） | `scripts/upload-gitee-assets.ps1` |
| blob 回收 | ADR-018 的两个条件：**需要项目注册表**（ngm 没有）与**跨项目共享**（删一个 digest 会影响别的项目） | [ADR-018](../adr/adr-018-store-reclaim.md) |
| 3s 目标 | v0.5 实测：受控对照（未优化）最大 2.433s **也过线**，而跨机器方差 ≥0.3s > 本轮收益 0.45s | [v0.5 复盘 §2.3](../development/v0.5-retrospective.md) |
| 无 registry 生态 | **非目标是刻意的**：registry 包管理交给 pnpm/npm/yarn | capability-matrix |

> **一条结构性阻塞值得单独说**：这个项目的"进度"曾经两次卡在**与代码无关的动作**上
> （v0.2~v0.4 不打 tag；v0.5~v0.8 不打 tag）。现在有门禁守着（`release-status`），
> 但"release 是否真的生成"仍**只能靠人看**——所以那一步现在也能自己读
> （`--published`，且撞限流时**拒绝报成功**）。

---

## 5. 下一步任务（建议顺序）

| 序 | 做什么 | 为什么排这里 |
|----|--------|------------|
| 1 | **`symlink` / `remote` / GC / 3s 四件决策**（写 ADR，把选项与代价摆出来） | 它们是**唯一**还挂在"等决策"上的事；数据都已就位，拖延的成本是文档里长期挂着四条"待定" |
| 2 | **workflow YAML 门禁** | 唯一"改坏了会静默失效"的角落：`release.yml` 是发布路径，而我们刚证明它会被改（加 `workflow_dispatch`） |
| 3 | **端到端可用性复核**（干净环境 + 真实引擎） | 10 个版本都在做"内部一致性"，**没人从头当一次用户**；这一步最可能发现真问题 |
| 4 | 恢复发布（四个 release + Gitee 附件） | 你已暂缓；恢复时是两条命令的事，且有门禁与工具兜着 |
| 5 | 继续找没人检查的角落 | 边际收益递减，但对这类"证据型"项目仍是最稳的推进方式 |

---

## 6. 实际怎么用

### 6.1 装

```bash
# 方式一：预编译二进制（推荐）
#   最新可取（GitHub）：v0.10.0 —— 六个平台 + SHA256SUMS
#   Gitee 侧目前只到 v0.4.0（v0.9.0 / v0.10.0 的附件待上传，你已暂缓）
# 方式二：从源码
git clone https://gitee.com/idcu/ngm && cd ngm && go build ./cmd/ngm
```

逐平台命令、`SHA256SUMS` 校验、PATH 配置见[安装指南](../guides/installation.md)。

### 6.2 最小闭环

```bash
ngm init --runtime=node                 # 初始化项目（也可 --runtime=deno）
ngm add github:org/repo --ref-type=tag --ref=v1.2.0   # refType 必填
ngm install                             # 解析 ref → commit、算 digest、落地 vendor、写 ngm.lock
ngm verify                              # 复核：ref 是否漂移 + digest 能否本地重放
ngm build                               # 通过 adapter 调引擎（内置 esbuild）
```

### 6.3 日常会用的其余命令

| 场景 | 命令 |
|------|------|
| CI 里安装（不解析新 ref、不改 lock） | `ngm install --frozen-lockfile`（不一致 exit 3） |
| 完全离线安装 | `ngm install --frozen-lockfile --offline`（资源缺失 exit 4） |
| 只有它会把锁定的 commit 移到新位置 | `ngm update` |
| 全量字节校验 | `ngm verify --deep` |
| 漂移了但我知道是预期的 | `ngm verify --allow-drift`（或看 `driftKind`） |
| 供应链 | `ngm audit`（按 commit 查 OSV，24h 缓存；`--offline` 只读缓存） |
| 为什么这个依赖在这儿 | `ngm why <dep>` / `ngm tree --osv` / `ngm outdated` |
| 接外部构建工具 | `ngm mappings` + `ngm integrations add vite\|esbuild\|deno\|webpack` |
| 类型检查 / 声明 / CSS / 单文件转换 | `ngm typecheck` / `ngm typedecl --outdir=` / `ngm css` / `ngm transform` |
| store 占用（只读）/ 清残骸 | `ngm store usage` / `ngm store prune [--dry-run]` |

**退出码语义**（很容易误读，[CLI 参考](../guides/cli.md)）：`3` = 缺配置/配置无效，
`4` = 离线且资源缺失，`5` = **声明了引擎但引擎干不了这事**（那是"没有可用引擎"，不是"通过"）。

---

## 7. 与主流工具的对比

**逐项对比的事实源是 [竞品对比分析](../COMPETITIVE-ANALYSIS.md)**（含依赖管理 5 家 × 7 个维度、
构建链路 9 家的表，以及"Git 依赖安全控制点"矩阵）。本节只做三件事：**引用它、补上它写完之后
发生的变化、给结论**——避免同一件事有两份会漂移的表（这正是本项目反复吃的亏）。

### 7.1 那页写完之后，变了的

| 那页的判断 | 现在 | 出处 |
|-----------|------|------|
| "vendor 不必然省磁盘"：pnpm 的 content store 已很高效，ngm 的全量副本更容易重复 | **一半被修掉**：层 2 换成按文件寻址后，同场景的**放大比从 20.0× 降到 0.6×**。仍成立的那一半是**落地层**（`ngm.vendor` 仍是每项目一份，copy 模式下真实复制）与**没有回收** | [metrics · 磁盘增长](./metrics.md#磁盘增长内容寻址-storev06) · [ADR-019](../adr/adr-019-content-addressed-blobs.md) |
| "Git 依赖可证明性闭环**并非完全空白**，只能说未形成默认闭环" | **不变**——这条自我约束仍然准确：单项控制点大多不独有 | [竞品对比分析 §1](../COMPETITIVE-ANALYSIS.md) |
| "速度与一体化已商品化" | **不变**（Bun 的 install 约 12ms 量级；ngm 的成本主要在 git 子进程，已做成门禁） | 同上 §2 · [metrics · git 子进程清单](./metrics.md#git-子进程清单v05-起) |

### 7.2 结论（诚实版）

1. **它不是一个"更好的 pnpm"**，也不是通用包管理器：不碰 registry。凡"只用 npm 包"的场景，
   它没有存在理由（[docs/README 已这么写](../README.md#为什么你可能不应该用-ngm)）。
2. 它的**真实价值区间窄而硬**：私有 fork / 上游 commit 追溯 / 离线与审计交付 /
   跨团队可信组件——这些场景里，`refType` + digest + verify 的闭环确实比"把 Git URL 塞进
   `package.json`"更严谨。**但"更严谨"是否能赢过"已经够用"，没有证据。**
3. **最大的风险不是技术，是采用**：GitHub 0 star / 0 fork / 无外部用户 / 单人维护（2026-10-03）。
   技术侧的账（性能、磁盘、跨平台、门禁）算得比多数同类项目更细，但这不改变"没有人用过它"。
4. 与 pnpm 对比里**唯一曾经对 ngm 不利的硬差距**（跨 commit 不去重，20×）已在 v0.8 消除；
   剩下真正落后的是**回收**（pnpm 有 `store prune`，ngm 明确不做按可达性删除，
   只提供"可见"与"清残骸"）。

**结论（诚实版）**：

1. **它不是一个"更好的 pnpm"**，也不是通用包管理器：不碰 registry。凡"只用 npm 包"的场景，
   它没有存在理由（[docs/README 已这么写](../README.md#为什么你可能不应该用-ngm)）。
2. 它的**真实价值区间窄而硬**：私有 fork / 上游 commit 追溯 / 离线与审计交付 /
   跨团队可信组件——这些场景里，`refType` + digest + verify 的闭环确实比"把 Git URL 塞进
   `package.json`"更严谨。
3. **最大的风险不是技术，是采用**：0 star、无外部用户、单人维护。技术侧的账（性能、磁盘、
   跨平台）已经算得比多数同类项目更细，但这不改变"没有人用过它"这一事实。
4. 与 pnpm 对比里**唯一曾经对 ngm 不利的硬差距**（跨 commit 不去重，20×）已在 v0.8 消除；
   剩下真正落后的是"回收"（pnpm 有 store prune，ngm 明确不做按可达性删除）。

---

## 8. 自检：这个项目有没有在自嗨

沿用 [docs/README · 四类自嗨风险](../README.md#四类自嗨风险本项目持续自检) 的口径逐条对照：

| 风险 | 现状 |
|------|------|
| "无 registry = 安全/去中心化" | ✅ 没这么说；文档明确写了仍依赖 Git host 与凭证 |
| "vendor 一定省磁盘" | ✅ 已实测并公开 20× 的坏消息，且 v0.8 修掉了它 |
| "Go 单二进制 = 更快" | ✅ 没这么说；反而证明了成本在 **git 子进程**上，并把它做成了门禁 |
| "一体化覆盖 test/docs/LSP/Dev Server" | ✅ 明确列为非目标 |

**另外三条自己想说的**：

- 性能目标**有 2 条未达标**，且项目坚持不把它读成"接近达标"（v0.1 复盘）；
- 3s 目标被证明**在噪声里不可判别**后，纪律改成"次数优先于秒数"——**把不可判别的量降级**，
  而不是继续拿它当门禁；
- 这一轮（v0.9/v0.10）连续发现的两处问题（链接检查按 GBK 解码吞掉 ASCII 标点、
  `describe()` 报错指错文件）**都是自家工具的问题**，而它们此前一直被当成"检查通过"。

---

## 相关文档

- [路线图](./roadmap.md)（目标与版本范围的事实源）· [能力矩阵](./capability-matrix.md)（能力归属）
- [metrics](./metrics.md)（所有数字与口径）· [发布清单](../development/README.md#发布清单每个版本)
- 各版证据：[开发总览](../development/README.md)（计划与复盘索引）
- 使用：[安装](../guides/installation.md) · [CLI 参考](../guides/cli.md) · [配置](../guides/configuration.md)
- 边界与风险：[docs/README · 诚实版](../README.md#诚实版劣势与边界)
