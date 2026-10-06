# 开发总览

> **当前阶段：v0.1 ~ v0.43 均已交付**；**`v0.1.0` ~ `v0.43.0` 四十三个 tag 均已打**。
> v0.12 的主题是**把读数修准**（含文档自己的读数）：见[复盘](./v0.12-retrospective.md)；
> v0.13 把这件事推到下一步——**把形状固化成网**（产品代码 0 行改动）：
> 见[复盘](./v0.13-retrospective.md)；
> v0.14 换了方向——**把"文档承诺"与实测行为对账**（同样 0 行产品代码改动）：
> 见[复盘](./v0.14-retrospective.md)；
> v0.15 把两条**对外契约**钉成网（文档示例必须成形 + 用法错误可判别），
> 顺带修掉一个真缺陷（解析错误绕过注入的 writer）：见[复盘](./v0.15-retrospective.md)。
> 发布是另一回事，且**2026-10-04 已直接问过两个源的 API**：
> GitHub 有 42 个里的 **38 个**，Gitee 只有 **3 个**
> （`v0.2.0`/`v0.3.0`/`v0.4.0`，各 7 个附件）——**`v0.1.0` 在 Gitee 上没有发行版**，
> 而本文件此前把它算成了"两个源都可取到"。逐版状态见[发布状态](../README.md#发布状态)；
> Gitee 侧随时可用 `bash scripts/check-gitee-release-status.sh` 复读（发布清单第 6 步；
> Windows 用 `scripts/check-gitee-release-status.ps1`，它转调同一个实现）。
>
> **历史版本补发：暂缓**（2026-10-04，项目所有者决定）
>
> | 待补 | 缺什么 | 恢复时怎么做 |
> |------|--------|-------------|
> | `v0.5.0` ~ `v0.8.0` 的 GitHub release | **4 个**——镜像转发了 tag 却没触发 `release.yml`，**原因不明** | Actions → Release → **Run workflow**，填 tag（[发布清单](#发布清单每个版本)第 2 步） |
> | Gitee 发行版 | **15 个**：`v0.1.0` 及 `v0.5.0` ~ `v0.18.0` | `scripts/upload-gitee-assets.ps1 -Tag vX.Y.0`（需 `GITEE_TOKEN`） |
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
>
> | 版本 | 计划 | 复盘 | 一句话 |
> |------|------|------|--------|
> | v0.5 | [计划](./v0.5-plan.md) | [复盘](./v0.5-retrospective.md) | 收敛与交付；三版补发布 |
> | v0.6 | [计划](./v0.6-plan.md) | [复盘](./v0.6-retrospective.md) | 让结论**可判别** |
> | v0.7 | [计划](./v0.7-plan.md) | [复盘](./v0.7-retrospective.md)（补写） | store 占用可见、残骸可回收 |
> | v0.8 | [计划](./v0.8-plan.md) | [复盘](./v0.8-retrospective.md) | 层 2 换布局：20.0× → 0.6× |
> | v0.9 | [计划](./v0.9-plan.md) | [复盘](./v0.9-retrospective.md) | 让 store 的读数说真话（共享/独占/孤儿）+ 锚点检查 + 10 万级规模实测 |
> | v0.10 | [计划](./v0.10-plan.md) | [复盘](./v0.10-retrospective.md) | 检查推到最外圈：根 README + store 不完整的两条承诺 |
> | v0.11 | [计划](./v0.11-plan.md) | [复盘](./v0.11-retrospective.md) | 四项决策结项（ADR-020~023）+ **第一次真回收**（`store prune --orphans`）+ workflow YAML 门禁 + `WaitDelay` 修复 + 端到端复核 |
> | v0.12 | [计划](./v0.12-plan.md) | [复盘](./v0.12-retrospective.md) | 可信读数：文档三层纠偏（9 处）+ **MIT 许可证** + 子命令帮助真实现（并修掉一个死代码级缺陷）+ **六处**静默失效缺陷 + 6 条机械网 |
> | v0.13 | [计划](./v0.13-plan.md) | [复盘](./v0.13-retrospective.md) | **把"缺陷形状"固化成网**：位置参数必须有界（探针验证）+ `--offline` 零 spawn（带对照），并记录一张**故意不做**的网；副产品是全仓过期读数扫描（修 6 处 + 2 处 ADR 补录） |
> | v0.14 | [计划](./v0.14-plan.md) | [复盘](./v0.14-retrospective.md) | **把"文档承诺"与实测行为对账**：修 4 处（"esbuild 能做 css"在三个位置各错一遍）+ **3 张新网**（dry-run 不写盘 / 引擎默认选择 / flag 绑定必须被解引用）。**非测试代码 0 行改动** |
> | v0.15 | [计划](./v0.15-plan.md) | [复盘](./v0.15-retrospective.md) | **两条对外契约钉成网**：文档示例命令必须成形（306 条 / 27 份活文档）+ 用法错误的退出码与流向（21 个命令 × 4 条契约）。顺带修掉一个真缺陷：解析错误绕过注入的 writer 直写进程 stderr |
> | v0.16 | [计划](./v0.16-plan.md) | [复盘](./v0.16-retrospective.md) | **机器可读输出不能说谎**：`--json` 契约网（20 个状态 × 4 条规矩）+ 把机器接口的形状写进 [CLI 参考](../guides/cli.md)。产品代码 **0 行改动**；20 个状态上**没有产品缺陷** |
> | v0.17 | [计划](./v0.17-plan.md) · [ADR-024](../adr/adr-024-bounded-explanations.md) | [复盘](./v0.17-retrospective.md) | **解释输出必须有界**：`why` 默认 64 条路径、`tree` 默认 4096 条目（可见标记 + `--all` 解除）——实测 41 个节点就能产出 104 万条路径 / 626 MB，而图形状来自上游清单；另修 `observability.md` 两段与渲染器不符的输出示例，并加网钉住 |
> | v0.18 | [计划](./v0.18-plan.md) | [复盘](./v0.18-retrospective.md) | **第一次真实联网复核**：新增可选复核（`NGM_REAL_UPSTREAM=1`，默认跳过；联网部分只记录、不可达即"未证明"）+ 一次真实运行记录。**产品代码 0 行改动**——一条"网络等待没有上界"的假设被测量否掉（失败由 git 自己的界终止：RST ~20s / TCP ~21s）。**完整首次上手路径未在本环境验证**（网络），如实记为未完成 |
> | v0.19 | [计划](./v0.19-plan.md) | [复盘](./v0.19-retrospective.md) | **用户看到的文本也是接口**：每个命令的用法文本都写了自己的 `EXIT CODES`（8 个新增 + 修正 `install` 漏掉的 2 / 5）+ 修掉 `ngm config` 的三处缺陷 + 清掉用户可见文本里的内部阶段标签（`M\d`） |
> | v0.20 | [计划](./v0.20-plan.md) | [复盘](./v0.20-retrospective.md) | **先测量，结果把两个候选都证伪了**：① 缺引擎时报错**已经列了候选**；② 三类配置错误下 **21 个命令已经一致**（exit 3 且点名路径）。**产品代码 0 行改动** |
> | v0.21 | [计划](./v0.21-plan.md) | [复盘](./v0.21-retrospective.md) | **声明 ↔ 实测 对账**：70 对声明，44 条离线实测、26 条登记为已知缺口；缺口名单自带过期绊线。顺带修掉一张网当场抓出的产品缺陷：`ngm tree --offline` 不穿 `--offline`（退 3 而非承诺的 4） |
> | v0.22 | [计划](./v0.22-plan.md) | [复盘](./v0.22-retrospective.md) | **把缺口压下去**：缺口 26 → **3**（实测 44 → **67 / 70**）。用假引擎覆盖五个 kind 的成功与失败，用漂移/篡改/冷 mirror/缺 Deno 夹具覆盖 1/2/4/5。剩下的 3 条 `update` 缺口**每条都带实测记录**说明为什么测不到 |
> | v0.23 | [计划](./v0.23-plan.md) | [复盘](./v0.23-retrospective.md) | **名字必须说发生了什么**：把"显示名"从"数值"里拆出来（`NgmError.Label`）——退出码 1 是**四种含义共用一个数字**，`ngm typecheck` 失败时不再说 `RefDrift:` 而说 `EngineFailed:`。数值、`--json`、`errors.Is/As` 一字未动；顺带在**规范**里抓到一处同类漏写 |
> | v0.24 | [计划](./v0.24-plan.md) | [复盘](./v0.24-retrospective.md) | **退出码的四方事实源必须对得上**：实现 / 规范表 / 规格常量块 / 用户侧汇总，首次被一条判据串起来（**`iota` 顺序错了会让所有码静默平移**）。开工第一次跑就抓到 `observability.md` 另一处漏写（码 1 没提 audit 钩子）；顺带补上 v0.23 遗留的"改了但没有网看着" |
> | v0.25 | [计划](./v0.25-plan.md) | [复盘](./v0.25-retrospective.md) | **文档里手工写下的字段清单不许与实现脱节**：`--json` 形状表（9 命令 / 48 键）+ `configuration.md` 三张字段表（19 键）首次对账。**产品代码 0 行改动**；最贵的一课是判据先量再写（朴素版误报 27/62） |
> | v0.26 | [计划](./v0.26-plan.md) | [复盘](./v0.26-retrospective.md) | **反向判据**（补上 v0.25 记下的限制）：报告的每个顶层字段都必须被文档点名（**67 个字段**）；正向网跟着增到 **71 键**。顺带发现反向判据有一个**前提**——两处形状是匿名结构体，**没有名字就无法对账**，提成具名类型 + 第三张网钉住"每行都要有映射"。另修两处文档缺口（数组元素形状 11 键 / `WhyReport.subPath`） |
> | v0.27 | [计划](./v0.27-plan.md) | [复盘](./v0.27-retrospective.md) | **散在句子里的字段名也是机器接口**：把对账推进到最散的那一份——只扫"提到 `--json` 的行"（**52 行 / 21 个字段名**，误报从 27/62 降到 0），并给 `configuration.md` 补反向（**14 个字段**）。**产品代码 0 行改动**；最有价值的一刻是**牙齿验证没有红**——它测出判据漏了"键 + 取值"这一整类写法 |
> | v0.28 | [计划](./v0.28-plan.md) | [复盘](./v0.28-retrospective.md) | **可行动的建议不许在包装时丢掉**：普查"错误有没有下一步"（全仓库 300 个构造点 / 129 个空 hint；用户层 47 / 11）时抓到**用户可见的真缺陷**——`hint:` 会在 `Wrap` 时消失（实测只剩 `cause:` 一行）。修在**渲染处**（沿 `Cause` 链取第一条非空 hint）：一次覆盖全部 300 个构造点、且不改错误数据；另加**棘轮**（用户层空 hint 不许变多）。顺带把 `update:1` 的缺口用"前—中—后"逐步骤量清（竞态，代码路径存在但无法确定性触发） |
> | v0.29 | [计划](./v0.29-plan.md) | [复盘](./v0.29-retrospective.md) | **第一次上手的错误必须有下一步**：把 v0.28 定下的棘轮值 11 **逐条看过**——判断翻案（"有些错误没法更好"不成立，11 处全都能写真实的下一步），棘轮收紧成**硬门禁 0/47**；加一张**端到端**的网证明建议真的到得了用户眼前（让 `init` 真撞一次建不出来的路径）。另修 v0.28 登记文本里"把 `--json` 与 `errors.As` 写在同一行"的措辞 |
> | v0.30 | [计划](./v0.30-plan.md) | [复盘](./v0.30-retrospective.md) | **错误必须说话，且说得出下一步**：复用现成的 **64 个退出码用例**在**运行时**取证据——43 个非零里 **15 处错误文本全部带建议、0 处静默**，28 个报告式失败不套该契约但**要报出来**；判据自带可达性守卫（两类样本缺一即红）。另加**全仓库棘轮**（空 hint 118，用户层已是硬门禁 0）。**产品代码 0 行改动** |
> | v0.31 | [计划](./v0.31-plan.md) | [复盘](./v0.31-retrospective.md) | **错误面按失败路径看，而不是按声明看**：v0.30 的配置类错误 21 条用例**全是同一个场景**（未知 flag）；这一版按真实入口重排成 **14 条路径**——12 处错误文本全部带建议、2 条走报告、0 静默，错误**身份 2 种**（`ConfigInvalid` 11 · `GitFetch` 1）。一条判断被测量当众否掉（我以为"删掉 vendor 后 verify 退 0"是缺陷——真相是我删的路径**不存在**，项目里是 `ngm.vendor`）。**产品代码 0 行改动** |
> | v0.32 | [计划](./v0.32-plan.md) | [复盘](./v0.32-retrospective.md) | **报告式失败也要说话**：每一条失败项（`✗`）都要配一行"接下来做什么"（`→ …` / `Fixed in: …`）。新网**第一次跑就抓出两处真实缺口**：`audit` 在公告没记录修复版本时一句话不说、`tree --osv` 查到漏洞时不指向 `ngm audit`（而它**没查**时反而有提示）。**本轮系列第一次由新网直接抓出产品缺陷** |
> | v0.33 | [计划](./v0.33-plan.md) | [复盘](./v0.33-retrospective.md) | **建议要指名道姓**：仅"有一行下一步"不够——那行必须点名一个**真能执行**的东西（`ngm <cmd>` 须在运行时命令表里 · 配置键须逐段在 schema 源码里 · `Fixed in: <版本>`）。**一个指错地方的指引，比没有指引更糟**。判据的边界写在明处：判得出"有东西"，判不出"东西对不对"。**产品代码 0 行改动** |
> | v0.34 | [计划](./v0.34-plan.md) | [复盘](./v0.34-retrospective.md) | **建议要承认失败的形状**：每类失败登记它的**解法**，报告里每一条行动行都必须命中该类的解法——漂移 ⇒ `ngm update`、字节被改 ⇒ `ngm install`、漏洞 ⇒ `ngm audit`，**而不是随便一个真实存在的命令**。立判据时抓出真实缺陷：`verify` 的"检查未能完成"那一支**算好了建议又被渲染门丢掉**（与 v0.28 同形）。牙齿：**v0.33 放行、v0.34 抓住** |
> | v0.35 | [计划](./v0.35-plan.md) | [复盘](./v0.35-retrospective.md) | **建议要承认成因**：`res.Err` 非空是**一支**却覆盖成因不同的失败（mirror 不在 vs 网络被策略拒），v0.34 给它们同一句话——**对后者是错的**。修法从"让猜的人拿到更多线索"改成"**让知道的人直接说**"（`errs.Hint` + `DepResult.ErrHint` 优先）。**"算好了又丢掉"的第三次，三个不同的丢法** |
> | v0.36 | [计划](./v0.36-plan.md) | [复盘](./v0.36-retrospective.md) | **扫描，而不是挑选**：报告契约从手工挑的 9 条扩到**扫全部 57 个非零用例**——当场抓到盲区里的 `ngm engines validate`（7 个问题、退 5、一行下一步都没有）。判据自己也造过一处**假阳性**（用法文本在解释 `✗` 记号本身）；另**证伪了一个候选**：词法版死赋值检查全仓库 0 真命中 |
> | v0.37 | [计划](./v0.37-plan.md) | [复盘](./v0.37-retrospective.md) | **枚举，而不是列举**：由**21 个命令 × 3 种配置错误 = 63 次运行**给出全称，四条通道各守契约——错误文本带 `hint:`（55/55）· 报告带行动行 · **用法文本不许出现** · 静默红 · **"其它"桶也红**（新通道是有人有意识做的决定）。**0.86 秒、零夹具**。产品代码 **0 行改动**；倒是我自己的两处错被它逼出来（参数表不完整走不到配置层 · 判据里一个**永远触发不了的分支**，被"其它"桶兜住） |
> | v0.38 | [计划](./v0.38-plan.md) | [复盘](./v0.38-retrospective.md) | **形状再枚举 + "退 0" 必须登记**：形状 **3 → 8**（缺 `name` · 未知 runtime · 依赖缺 `name` · `dependencies` 类型错 · **锁损坏**），矩阵 **63 → 168** 次运行（**1.17 秒**）；**146 处错误文本全部带建议、0 违规**。新增的判据在另一头：**退 0 的格子不受任何通道契约约束**，是这张网唯一能悄悄攒起来的地方——逐条登记"为什么它是 0"并**双向对账**（22 格）。量到三处**合法但反直觉**的行为（锁损坏时 `verify`/`install` 拒绝、`add` 不读锁、`update` 重写锁）。**产品代码 0 行改动** |
> | v0.39 | [计划](./v0.39-plan.md) | [复盘](./v0.39-retrospective.md) | **参数维度**：形状维度之后补上 `no-args`（不给位置参数）与 `junk-arg`（多一个没人认识的参数）——**42 次运行全部非零**（27 用法 · 15 错误文本全部带建议 · **0 退 0 · 0 静默 · 0 其它**）。核心不变量：**多余参数必须被看见，绝不静默忽略**（v0.19 只修了一个命令，这一版变成全命令不变式）。两处前提值得记：`junk-arg` 必须用**合法项目**（坏目录会让配置错误掩盖参数处理）· 判据经实测**收窄**一次（`FLAGS:` 不是普遍存在的，`cache`/`integrations`/`store` 本来就没有 flag）。**产品代码 0 行改动** |
> | v0.40 | [计划](./v0.40-plan.md) | [复盘](./v0.40-retrospective.md) | **参数怎么写**：顺序 · 重复 · `--` 三族各对应产品**自己写下的一句话**（19 命令 × 3 族 = 59 次运行，1.5 秒）。**抓到一处真实产品缺陷**：`normalizeArgs` 把 `--` 之后的 token 正确归为 positional，**但重排时把 `--` 丢了**——它们又回到 flag 位置被 `flag.Parse` 当成 flag，实测 **9 个命令**受影响、与函数自己开头那句承诺相反。修法是把 `--` 还回去。另有一处 `root_test.go` 的**旧期望就是这个缺陷本身**（已改并写明为什么）。核心不变量：**多余参数必须被看见，绝不静默忽略** |
> | v0.41 | [计划](./v0.41-plan.md) | [复盘](./v0.41-retrospective.md) | **`--dir` 的四种拼法 · 空值必须被拒绝**：19 命令 × 2 族（1.5 秒）——四种拼法（`--dir=X`/`-dir=X`/`--dir X`/`-dir X`）**输出逐字节相同** ✅；**`--dir=`（显式空值）必须被拒绝且 CWD 原封不动** ✅。**抓到一处危险的产品缺陷**：Go 的 flag 把 `--dir=` 解析成空串，而空串与"没给这个 flag"**无法区分**，于是命令**静默把空值当成 CWD**——`ngm add … --dir=` 改的是当前目录的 `ngm.json`、`ngm init … --dir=` 在当前目录建出项目（最可能的来源是 `--dir="$PROJ"` 而未展开）。**判据只看副作用**（CWD 换成空目录后必须还是空的）——比读文本可靠（v0.40 的回显教训） |
> | v0.42 | [计划](./v0.42-plan.md) | [复盘](./v0.42-retrospective.md) | **取值 flag 的空值**（v0.41 的教训**推广到全部取值 flag**）：15 个 (命令, flag) 组合（0.8 秒）——**14 个明确拒绝**、**1 个与"不给"等价**（`--path=`）、**没有一处再碰 CWD** ✅。判据是两条不变量：① **显式空值要么与"没给"完全等价、要么失败——绝不能"两个都成功、结果却不同"**（后者正是"静默改变行为"的定义，也正是 v0.41 那个缺陷的形状）；② **任何一次运行都不许在 CWD 里留下东西**。判据刻意**不要求**"必须拒绝"——**拒绝**与**等价于不给**都合格，不合格的只有"静默走了另一条路"。**产品代码 0 行改动** |
> | v0.43 | [计划](./v0.43-plan.md) | [复盘](./v0.43-retrospective.md) | **布尔 flag 的三条语义**：**41 对 (命令, flag) 从源码派生**（`fs.Bool("name"` in `<cmd>.go`）→ **38 对跑满 4 条断言**、3 对公开跳过（属于某个子命令的 flagset），1.8 秒。四条断言：① `--flag=true` ≡ `--flag` ② `--flag=false` ≡ **不给** ③ `--flag=x` **必须失败** ④ `--flag ZZ-JUNK` **必须失败**（布尔 flag **从不消费下一个 token**——`normalizeArgs` 的注释专门写过这条）。**产品代码 0 行改动**；红的两次都是**我自己的东西**：手抄的表里有不存在的组合（`remove --force` / `typecheck --json` / `css --sourcemap`）→ 改成从源码派生；守卫阈值是拍的（写 45、实测 41）→ 改成**结构性**守卫（派生表总数 == 跑完 + 跳过）。牙齿：让 `--all` 之后的 positional 被无声忽略 ⇒ ④ 在两处命中 |
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

### v0.11（计划：[v0.11-plan.md](./v0.11-plan.md) / 复盘：[v0.11-retrospective.md](./v0.11-retrospective.md)）— **已交付**

**把挂着的决策结掉，并第一次真正回收**：v0.10 之后剩下的事几乎全是"等决策"——
数据都已就位，缺的是拍板。这一版把四件拍完（三件写 ADR、一件写 ADR **并实现**），
再补一处"改坏了会静默失效"的门禁，最后**当一次用户**走完整条路。

| 组 | 内容 | 状态 |
|----|------|------|
| A | 四项决策结项：[ADR-020](../adr/adr-020-remote-adapter-shelved.md)（`remote` 搁置——它在代码里**不存在**）· [ADR-021](../adr/adr-021-symlink-link-mode.md)（symlink 保留降级）· [ADR-022](../adr/adr-022-verify-performance-target.md)（3s 降级为观测值）· [ADR-023](../adr/adr-023-orphan-reclaim.md)（**回收孤儿，含实现**） | ✅ **已交付** |
| A4 | `ngm store prune --orphans [--older-than] [--dry-run]`：层 2 第一次真回收。判据单点（`scanManifests` 被 `usage` 与回收**共用**）+ 年龄门槛 + **有清单读不出来时拒绝删除** | ✅ **已交付**（实测：回收 21 B 后 `verify` 仍 `1 ok`） |
| B | workflow 的 **YAML 有效性门禁**（CI job `workflow-lint`，判据用真实解析器 js-yaml） | ✅ **已交付** |
| C | 端到端可用性复核（干净环境 + 真实 git + 真实 esbuild） | ✅ **已交付**——6 步闭环全通，**抓到 6 个问题**（C 组 3 个：1 已知限制 / 2 已修；全量测试 3 个：`WaitDelay` 三处同缺、不 hermetic 的测试、CI 超时把"慢"读成"坏"） |

> **本版最值得记住的两条**：
> ① **ADR-023 的洞察可迁移**——当一条设计卡在"缺项目注册表"时，先问有没有
> **更弱但由构造可判定**的版本（"无人引用"不需要注册表，而它覆盖了大部分实际收益）；
> ② **C 组抓到的三个问题，在锚点/链接/单测/门禁全绿时依然存在**——
> 内部一致性检查证明不了"对外真的能用"（详见[复盘 §5.1](./v0.11-retrospective.md)）。

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
  | `scripts/check-gitee-release-status.sh`（`.ps1` 转调它） | **读数（v0.12 追加，v0.13 后改为平台无关的单实现）** | Gitee 侧"到底有没有这个发行版、7 个附件齐不齐"——此前只能靠人去点。**故意不进 CI**（长期黄的步骤 + 匿名限额噪音，触发条件见发布清单第 6 步），因此它是发布清单第 6 步；**判据本身用本地替身验证过**（全就位 / 有缺 / 查不动三分支 + 计数只算我们传的 7 个） |
  | `TestV12EveryRegisteredFlagIsDocumented` | `cmd/ngm`（**v0.12 新增**） | 扫源码：注册到 flagset 上的 flag 必须在该文件里被写下过（命令行 flag 此前**没有任何机械网**，配置结构体字段有 `field_wiring_test.go`） |
  | `TestV12SubcommandHelpPrintsItsOwnUsage` | `cmd/ngm`（**v0.12 新增**） | 21 个命令的 `--help` **逐字等于**它自己的用法常量，且该常量以自己的名字开头 |
  | `TestV13PositionalArgsAreBounded` | `cmd/ngm`（**v0.13 新增**） | 扫源码：读 `fs.Arg` 的文件必须校验 `fs.NArg()`——**"多给的输入被静默丢掉"这个形状**（v0.12 D3）。牙齿已用探针验证 |
  | `TestV02ObservabilityAcceptance` 的 `--offline` 子用例 | `cmd/ngm`（**v0.13 加固为行为网**） | 冷 mirror + `--offline` ⇒ **0 次 git 子进程**，并配**暖 mirror 必须起 git** 的对照（v0.12 D1 的形状）。用 spawn 次数而不是输出文本 |
  | `TestV14DryRunAndReadOnlyCommandsWriteNothing` | `cmd/ngm`（**v0.14 新增**） | 8 条 `--dry-run` + 4 条只读命令：项目目录与 ngm home **快照不变**（cache 层除外——按契约它可随时整层删除）。三条陪衬：走到了 dry-run 分支、残骸**先造后清**、对照（真 `add` 必须让快照变） |
  | `TestV14EngineKindsWithoutBuiltinDefaultNeedDeclaration` | `cmd/ngm`（**v0.14 新增**） | 内置**默认选择**只覆盖 `bundle`/`transform`；`typeCheck`/`typeDecl`/`css` 未声明 = `exit 3`；esbuild 不是 css 引擎；**声明为 typeCheck 条目后**才是 `exit 5`（能力先于可用性）。同时钉住 4 处文档更正 |
  | `TestV14EveryFlagBindingIsDereferenced` | `cmd/ngm`（**v0.14 新增**） | 扫源码：flag 绑定不许在注册处被丢弃（`_ = fs.String(...)`），且必须被解引用过（覆盖 76 处）。两条规则各用探针验证过会红；**会漏报**的边界写在注释里 |
  | `TestV15DocExamplesAreRealInvocations` | `cmd/ngm`（**v0.15 新增**） | **活文档**里的示例命令必须成形：子命令在运行时命令表里、flag 属于该命令（判据取自各命令的 usage 文本）。只扫代码块与行内跨度（整行扫描会误报）；`docs/development`、`docs/adr` 是快照，不扫；负例用**会自己过期**的名单登记 |
  | `TestV15UsageErrorsPrintOwnUsage` | `cmd/ngm`（**v0.15 新增**） | CLI 的用法错误契约（21 个命令）：`exit 3`、自己的用法进 **stderr**、**stdout 为空**、**没有任何字节绕过注入的 writer**（把进程 `os.Stderr` 收进管道断言为空）。对照：`--help` ⇒ exit 0 + 用法进 stdout。牙齿：摘掉 `SetOutput` ⇒ 报 800 字节泄漏 |
  | `TestV16JSONReportsTellTheTruth` | `cmd/ngm`（**v0.16 新增**） | `--json` 契约（**20 个状态**、9 种命令形态）：stdout 是空或**恰好一份**合法 JSON、**`--json` 不改变退出码**、报告里的 `exitCode` 等于进程退出码、输入错误时 stdout 为空（失败态**不是**输入错误）。`audit` 用本地 OSV 替身保持离线。牙齿：把报告里的 `exitCode` 改成常量 ⇒ 三条状态断言红 |
  | `TestV17WhyPathsAreBoundedAndSaySo` · `TestV17TreeExpansionIsBounded` | `internal/observability`（**v0.17 新增**） | 上界本身正确（ADR-024）：输出有界 · **工作量有界**（有界枚举 544 µs vs 不限量 0.778s/626 MB）· 截断可见 · **恰好等于上界时不许说"还有更多"**。格状图夹具（`RequiredBy` 是**父节点**，写反了会得 0 条路径） |
  | `TestV17ExplanationIsBoundedEndToEnd` | `cmd/ngm`（**v0.17 新增**） | 上界的**接线**（15 个真实仓库的格状图，128 条路径）：默认 64 条且两种输出都写明、`--all` 给全部、`tree` 在预算内不被标记。牙齿：摘掉默认上界 ⇒ `paths=128 want 64` |
  | `TestV17DocOutputExamplesMatchTheGoldens` | `cmd/ngm`（**v0.17 新增**） | **输出示例**与快照同形（v0.15 钉的是**命令**示例）：从 `observability.md` 取出两段示例，归一化后与 `testdata/*.golden` 逐行比较；缩进/标签/标记不许漂。牙齿：改回旧的 `├──` 形状 ⇒ 点名 `tree.golden` |
  | `TestV18RealUpstreamFirstRun` | `cmd/ngm`（**v0.18 新增**，**默认跳过**） | **可选联网复核**：只在本项目能控制的事上断言（真实 ref 解析、无 `ngm.json` 的真仓库、`archiveDigest` 跨独立项目逐字相同、坏仓库必须点名自己）；联网部分**只记录**，远端不可达（exit 4）判为"未证明"并 skip。跑法：`NGM_REAL_UPSTREAM=1 go test -count=1 -timeout 1200s -run TestV18RealUpstreamFirstRun -v ./cmd/ngm` |
  | `TestV19EveryCommandDocumentsItsExitCodes` | `cmd/ngm`（**v0.19 新增**） | 每个命令的用法文本必须有 `EXIT CODES` 段，且段内至少一行 `<0-5> <说明>`。补上 8 个命令缺失的段，并修正 `install` 漏掉的 2 / 5（它有代码路径与既有测试支撑）。**它不证明"列全了"**（那需要数据流分析） |
  | `TestV19NoInternalStageLabelsInUserFacingText` | `cmd/ngm` + `internal/**`（**v0.19 新增**，用 `go/scanner`） | 用户可见的**字符串字面量**里不许出现内部阶段标签 `M\d`（`M0…M7` 是内部里程碑编号）。**注释里可以出现**（那是设计记录），**字符串里不行**（那会打印给人看）。大小写敏感——否则 `sha256.Sum256` 里的 `m2` 会被误判 |
  | `TestV19ConfigValidatesTheDirectoryItIsToldTo` | `cmd/ngm`（**v0.19 新增**） | `config validate` 的三条契约：① 校验 **`--dir` 指向的项目**（此前只认进程 CWD，多余的 `--dir=x` 被当成位置参数静默丢掉）② 目标目录没有 `ngm.json` → **exit 3**（此前打印 `ngm.json OK`）③ 多余位置参数 → **exit 3 + 用法**。对照：`config show` 是查看，不设门禁（exit 0，但写明项目层缺席） |
  | `TestV21UsageExitCodeSectionsAreWellFormed` | `cmd/ngm`（**v0.21 新增**） | 21 个命令的用法文本都**写了** `EXIT CODES` 段，且格式正确（至少一个码 / 码 ∈ 0..5 / 不重复 / 每条带说明）。**第一版解析器曾把段里的续行误判成段结束**（21 个只解出 6 个）——判据自己错是最危险的失败 |
  | `TestV22DeclaredCodesAreMeasuredOrKnownGaps` | `cmd/ngm`（**v0.21 新增为 `TestV21…`，v0.22 扩到 67/70**） | **声明 ↔ 实测 对账**：每个命令声明的每个码，要么有一条**能在本地复现**它的实测、要么由专属测试测量（`exitCodeElsewhere`）、要么在 `exitCodeGaps` 里登记。**缺口名单自带过期绊线**（条目必须仍对应真实声明）。当前 70 对 → **64 条主网实测 + 3 条专属测试 + 3 条已知缺口**。夹具覆盖：假引擎（`FAKE_EXIT` 控制成功/失败）· 漂移 · lock 篡改 · 冷 mirror · OSV 本地替身 · 缺 Deno。牙齿：改一条期望码 ⇒ 红，并点名"现在没人盯着"的那一格 |
  | `TestV22DenoIsMissing` | `cmd/ngm`（**v0.22 新增**） | **"缺 Deno"那 3 条**（`verify:5` / `install:5` / `audit:5`）必须单独成测：清空 `PATH` 是全局且不可逆的，而夹具要用 git 建——所以每个子测试先建夹具、再清 PATH。它同时是**"不降级"这条安全边界的可执行证据**：宁可退 5，也不在沙箱外执行依赖作者的代码 |
  | `TestLabeledOnlyChangesTheDisplayName` | `internal/errs`（**v0.23 新增**） | `Labeled` **只**改显示名：码、退出码、`errors.Is/As`（返回的是浅副本）、原错误都不受影响；空标签退回码的默认名。它守的是 v0.16 那条机器契约——**机器读到的东西只有数字** |
  | `TestV23EngineFailureNamesItself` | `cmd/ngm`（**v0.23 新增**） | **端到端**：`ngm typecheck` 因引擎失败退出时，stderr 必须**含 `EngineFailed`、不含 `RefDrift`**，且**仍然退 1**（数值契约不许跟着名字变）。对照：新标签**不许泄漏**到 `verify` 的报告里。牙齿：摘掉 `runner.go` 的 `Labeled` ⇒ 红，并打印出旧消息 `RefDrift: typeCheck: … failed` |
  | `TestV24ExitCodeContractAgreesAcrossSources` | `cmd/ngm`（**v0.24 新增**） | **退出码的四方对账**：实现（`internal/errs`）· 规范表（`observability.md`）· 规格常量块（`p0-core.md`）· 用户侧汇总（`cli.md`）必须一致。**重点是规格的 `iota + 1` 顺序**——重排一行所有码静默平移，而规格是给人抄的。另有**会自己过期**的关键词表守着"码 1 的四种来源都写全了"。牙齿：换位 `ErrConfigInvalid`/`ErrDigestMismatch` ⇒ 红并点名数值错位 |
  | `TestV24AuditHookVerdictNamesItself` | `cmd/ngm`（**v0.24 新增**） | 补 v0.23 明确记下的欠账（"改了但没有网看着"）：把钩子判断抽成纯函数 `auditHookVerdict(*security.Result)` 后用合成结果测——**是谁否决的**（`AuditHook` 而非 `RefDrift`）· **数值仍是 1** · **两种失败说的话必须不同**（"它说自己不过关" ≠ "它没能给出结论"） |
  | `TestV25JSONShapeTableMatchesTheImplementation` | `cmd/ngm`（**v0.25 新增**） | `cli.md`《`--json` 的形状》表里点名的每个键（**9 个命令 / 48 个键**），必须在**该命令的报告实现**里作为 json tag 存在（映射按命令给，其中 `integrations`/`engines` 指向 **CLI 层自己**）。豁免名单自带过期绊线。**它只证明"表里写了的都真的存在"，不证明"该写的都写了"**。牙齿：把 `allowDrift` 改成 `allowDrifting` ⇒ 红并点名 |
  | `TestV25ConfigFieldTablesMatchTheSchema` | `cmd/ngm`（**v0.25 新增**） | `configuration.md` 三张「字段」表（`ngm.json` / `dependencies` / `supplyChain`，共 **19 个键**）必须在 schema 的 json tag 里存在。注意 `supplyChain` 指向 `internal/config`（`SupplyChainConfig` 的 tag），而 `internal/supplychain` 是**策略解析**（方法形态）——**这类网唯一会骗人的地方就是映射**。牙齿：把 `verifyOnLock` 改成 `verifyOnLocked` ⇒ 红 |
  | `TestV26EveryDocumentedShapeIsANamedType` | `cmd/ngm`（**v0.26 新增**） | **形状表里的每一行都必须映射到一个具名类型**（反向也钉：映射不许有孤儿）。它挡的是"反向判据静默少看几行"——而**"少看几行"与"全都对得上"在测试输出里长得一模一样**。前提是产品代码那两处匿名结构体已提成具名类型 |
  | `TestV26EveryFieldOfTheReportIsDocumented` | `cmd/ngm`（**v0.26 新增**） | 反向判据：每个报告**顶层** json 字段都必须在形状表里被点名（**67 个字段**）。正向那张挡"写了一个不存在的键"，这张挡"有了字段而文档没说"——**用户看得见、文档没有名字的字段是被藏起来一半的契约**。牙齿：从 why 那行删掉 `rootDeclared` ⇒ 红并点名 |
  | `TestV27ProseJSONFieldNamesExist` | `cmd/ngm`（**v0.27 新增**） | **散文里点名的 json 字段**必须在实现里存在。判据只扫"**提到 `--json` 的行**"（52 行 / 21 个字段名）——这是"锚点型判据"：全量扫描误报 27/62，绑到锚点上误报降到 0。允许 `` `x: 取值` `` 形态（第一版只认纯标识符，**牙齿验证没红**才发现漏了一整类写法）。牙齿：两处同时改名 ⇒ 红并点名 |
  | `TestV27EveryConfigFieldIsDocumented` | `cmd/ngm`（**v0.27 新增**） | `ProjectFile` / `Dependency` 的每个 json 字段都必须在 `configuration.md` 里被点名过（**14 个字段**，含只在正文里提到的 `schemaVersion`）。判据比形状表松一档（允许在文件别处点名），因为配置文档是"表 + 说明"混排。`vendor` / 全局配置**明确不做**（它们用散文描述，机械判据只会误报） |
  | `TestV28HintSurvivesWrapping` | `internal/errs`（**v0.28 新增**） | **建议不许在包装时丢掉**：内层 hint 要熬过 1 层与 2 层 `Wrap` · 自己的建议优先（**只显示一条**，最近的那条）· 整条链都没建议时**不许凭空造一句** · 标签仍来自最外层（v0.23 契约）· `Error()` 仍不含 hint。牙齿：摘掉继承 ⇒ 红并原样打印出缺陷输出 |
  | `TestV28UserFacingErrorsCarryAHint` | `cmd/ngm`（**v0.28 新增**，**v0.29 收紧为硬门禁**） | 用户层构造的每条错误都要给出下一步：空 hint 站点 **0 / 47**。v0.28 立它时定的是棘轮（11），理由是"有些错误确实没有更好的下一步"；v0.29 把那 11 处**逐条看过**，发现每一处都能写——判断翻案，注释里留着翻案的理由。用 `go/ast` 扫（hint 是"最后一个字符串实参"，只有解析器认得准）。牙齿：加一个空 hint 站点 ⇒ 红并**列出全部站点** |
  | `TestV29TheFirstRunErrorTellsYouWhatToDo` | `cmd/ngm`（**v0.29 新增**） | 上一条的**端到端对照**：让 `ngm init` 真撞一次建不出来的路径（父路径是**文件**，`MkdirAll` 必失败）——断言 exit 3 · 输出里有 `hint:` · 提示指向可归因的线索（可写性）· **没有谎报 `created`**（入口都没建出来）。静态网只能证明"源码里写了 hint"，这张证明**它到得了用户眼前** |
  | `TestV30ErrorsThatReachTheUserAreNotSilentAndCarryAHint` | `cmd/ngm`（**v0.30 新增**） | **运行时**普查（复用退出码网的 64 个用例）：① 非零**不许静默** ② 输出带错误前缀就**必须带 `hint:`** ③ **两类样本缺一即红**（防"所有失败都变成报告"之后判据静默失效）。读数：43 非零 → 15 错误文本（**全部带建议**）+ 28 报告式 + **0 静默**。牙齿：让 `FormatHuman` 不打印建议 ⇒ 红，并原样打印用户看到的那段 |
  | `TestV30EmptyHintCeilingIsARepositoryWideRatchet` | `cmd/ngm`（**v0.30 新增**） | **全仓库**棘轮：空 hint 构造点不许变多（**118**，用户层已是硬门禁 0），并把按目录的分布打进测试输出。它挡的是用户层门禁**看不到**的那一半：新加在 `internal/` 里、且下面无可继承建议的错误。牙齿：上限改成 117 ⇒ 红 |
  | `TestV31EveryReachableFailurePathExplainsItself` | `cmd/ngm`（**v0.31 新增**） | **按失败路径**组织的错误面网（v0.30 那张是按**声明**组织的，于是配置类错误的 21 条用例全是同一个场景）：**14 条真实入口** + 2 条预期为 0（对照组 + **实测边界**）。读数：12 处错误文本全部带建议 · 2 条报告式 · 0 静默 · 错误身份 **2 种**（`ConfigInvalid` 11 · `GitFetch` 1）。守卫：钉住条数（防缩水）+ 身份不少于 2 种 + 必须有报告式。牙齿：条数 +1 ⇒ 红；**让夹具不再造出它自称的状态** ⇒ 红（正是本版踩过的坑） |
  | `TestV32EveryFailureReportSaysWhatToDoNext` | `cmd/ngm`（**v0.32 新增**） | **报告式失败也要给下一步**：7 条用例（`verify` × 3 · `audit` × 2 · `tree --osv` · `install`）断言 **行动行数 ≥ 失败项数**（失败项 = 文本里的 `✗` 记号）。两种约定都算：`→ …`（verify/audit/tree）与 `Fixed in: <版本>`（audit）。**这张网当场抓出两处真实缺口**：`audit` 在公告没记录修复版本时一句话不说、`tree --osv` 查到漏洞时不说哪条公告也不指向 `ngm audit`（而它**没查**时反而有提示）。牙齿：去掉两处 `→` 记号 ⇒ 两条用例各报 `1 failing item(s) but only 0 next-step line(s)` |
  | `TestV33ActionLinesNameSomethingExecutable` | `cmd/ngm`（**v0.33 新增**） | **建议要指名道姓**：仅"有一行下一步"不够——那一行必须点名一个**真能执行**的东西：`` `ngm <cmd>` ``（子命令必须在**运行时命令表**里）· 配置键（**每一段**都要在 **schema 源码**里找得到）· `Fixed in: <版本>`。读数：8 行 → 命令 ×6 · 配置键 ×1 · 版本 ×1；核对源为 21 个命令 + 48 个配置键。**一个指错地方的指引比没有指引更糟**。牙齿：`ngm audits` ⇒ 红（不是命令）· 键名写错一个字母 ⇒ 锚点消失 ⇒ 红 · 整条换成纯空话 ⇒ 红。**边界**：判据点名了"有东西"，判不出"东西对不对" |
  | `TestV34AdviceMatchesTheFailureShape` | `cmd/ngm`（**v0.34 新增**） | **建议要承认失败的形状**：每一类失败在用例上登记它的**解法**（`reportCases[].remedies`），断言报告里**每一条**行动行都命中该类的解法之一——因漂移失败指向 `ngm update`、因字节被改失败指向 `ngm install`、因漏洞失败指向 `ngm audit`，**而不是随便一个真实存在的命令**。读数：9 条用例 · 9 行动行 · **6 种不同解法**。牙齿（这张网存在的理由）：把 critical 的建议改指向 `ngm audit`（**真实但不合适**的命令）⇒ **v0.33 放行、v0.34 红**。**边界**：判得出"方向对不对"，判不出"措辞清不清楚、有没有漏掉更重要的下一步" |
  | `TestV35OperationalFailuresAdviseTheirOwnCause` | `cmd/ngm`（**v0.35 新增**） | **建议要承认成因**：`res.Err != ""` 是**一支**却覆盖成因完全不同的失败——mirror 不在（⇒ 那句建议点名 `ngm install`）与网络被**策略**拒（⇒ 点名 `permissions.allow`）。判据三条：每条行动行命中**自己成因**的标志 · 两句话**不相同** · 两条用例都真的失败。**依据与 v0.28 同源**：建议沿包装链找第一句非空的——最贴近成因的那一层最知道该怎么办，`remediationFor` 猜不出来。牙齿：回退"优先用链上的建议"⇒ 两条断言**同时**开火（`does not acknowledge **this** cause` + `both operational causes were given the same advice`） |
  | `TestV36EveryFailingReportCarriesANextStep` | `cmd/ngm`（**v0.36 新增**） | **扫描，而不是挑选**：把**全部非零用例**（退出码表 43 + 错误面表 14 = **57**）跑一遍，凡 `reportBody` 里出现 `✗` 的都必须至少有一行行动行。读数：**57 → 9 个报告 → 9/9 带下一步**。与 v0.32 的分工：**广度**（覆盖我没想到的地方）vs **深度**（逐条对齐条数）。**当场抓到盲区**：`ngm engines validate` 报 7 个问题、退 5、一行下一步都没有。`reportBody` 会**截掉用法文本**——那段 `MARKERS:` 在解释记号本身，不截会把 `tree` 的用法错误误报成"有失败项却没下一步"（**判据自己造的假缺陷**）。牙齿：回退 `engines` 的下一步 ⇒ 红；关掉截断 ⇒ 红（证明那一步是承重的） |
  | `TestV37EveryCommandUnderEveryConfigErrorUsesItsChannel` | `cmd/ngm`（**v0.37 新增**） | **枚举，而不是列举**：**21 个命令 × 8 种配置错误 = 168 次运行**（v0.38 扩形状），四条通道各守契约——错误文本必须带 `hint:`（**146/146**）· 报告必须带行动行 · **用法文本不许出现**（配置形状下打用法 = 矩阵没走到配置层，或某个命令悄悄变成了全局命令）· 静默红 · **"其它"桶也红**。参数表 `matrixArgs` **每个命令都必须有条目**（`len == len(commands)`，少一个即红）；**"退 0" 必须登记**（`exitZeroByDesign`，双向对账）。读数：168 次 → 146 错误文本 + 22 退 0 + **0 用法 + 0 静默 + 0 其它**，**1.17 秒零夹具**。牙齿：清空 `update` 的参数 ⇒ 红；表里删一个命令 ⇒ 红；登记一个没退 0 过的命令 ⇒ 红；撤掉一条登记 ⇒ 红（两个方向） |
  | `TestV39ArgumentDimensionHoldsItsContracts` | `cmd/ngm`（**v0.39 新增**） | **参数维度**：**21 个命令 × 2 形状**（`no-args` = `ngm <cmd>`；`junk-arg` = 正常参数 + `ZZ-JUNK-ARG`）= **42 次运行**。契约：`no-args` ⇒ 良构用法（首行必须点名该命令）或带建议的错误；`junk-arg` ⇒ **多余参数必须被看见**（拒绝或用掉），**静默忽略是这一族最坏的结果**（v0.19 只修了一个命令，这里是全命令不变式）。读数：**27 用法 + 15 错误文本（全带建议）+ 0 退 0 + 0 静默 + 0 其它**。两处前提：`junk-arg` 必须用**合法项目**（坏目录会让配置错误掩盖参数处理）；判据经实测**收窄**（`FLAGS:` 非普遍，`cache`/`integrations`/`store` 没有 flag）。牙齿：去掉 `install` 的多余参数检查 ⇒ 红（`an argument nobody understands was ignored`）；让 `tree` 打别的用法 ⇒ 红 |
  | `TestV40ArgumentSpellingHoldsItsContracts` | `cmd/ngm`（**v0.40 新增**） | **参数怎么写**：三族各对应产品**自己写下的一句话**——**顺序**（`normalizeArgs` 注释：重排是为了两种顺序等价）⇒ 两种写法**输出逐字节相同**；**重复**（Go flag 语义：最后一个胜出）⇒ 与单 flag 那次运行逐字节比对；**`--`**（`normalizeArgs` 注释：`--` 之后一律视为 positional）⇒ 坏目录绝不生效。读数：**19 命令** × 三族（3 个命令在重复族**公开跳过**：好/坏目录对它们给出同一份结果 ⇒ 没有牙齿）。**这张网抓到一处真实产品缺陷**：`normalizeArgs` 分类正确但**重排时丢了 `--`**，于是 `--` 之后的 flag 形状 token 又被当成 flag（**9 个命令**受影响）。判别式刻意**不用**"输出里出现过坏目录"（命令会**回显 token**），而用两种不会说谎的证据：文件系统痕迹、或"目录+分隔符"。牙齿：**忠实**退回修复 ⇒ 9 个命令精确命中；把那些 token 丢掉（第三种行为）⇒ 另一条断言命中 |
  | `TestV41DirFlagSpellingsAndTheEmptyValue` | `cmd/ngm`（**v0.41 新增**） | **`--dir` 的四种拼法 · 空值必须被拒绝**：19 命令 × 2 族（1.5 秒）。① 四种拼法（`--dir=X`/`-dir=X`/`--dir X`/`-dir X`）**输出逐字节相同**；② `--dir=` / `-dir=` / `--dir ""` **必须被拒绝**，且**CWD 原封不动**。**这张网抓到一处危险缺陷**：Go 的 flag 把 `--dir=` 解析成空串，而空串与"没给这个 flag"**无法区分**，于是命令**静默把空值当成 CWD**——`add` 改的是当前目录的 `ngm.json`、`init` 在当前目录建出项目（来源多是 `--dir="$PROJ"` 而未展开）。**判据只看副作用**：把 CWD 换成一个空目录，跑完必须还是空的——比读文本可靠。牙齿：回退修复 ⇒ 4 个失败（**只有会写东西的命令**：`--dir=` 在 CWD 里留下 `[ngm.json src]`），只读命令本来就非零、网正确放行 |
  | `TestV42EmptyValueNeverSilentlyChangesBehaviour` | `cmd/ngm`（**v0.42 新增**） | **取值 flag 的空值**（v0.41 的推广）：15 个 (命令, flag) 组合（`--engine`/`--outfile`/`--outdir`/`--hook`/`--loader`/`--target`/`--format`/`--path`/`--ref-type`/`--runtime`），0.8 秒。两条不变量：① **显式空值要么与"没给这个 flag"完全等价、要么失败——绝不能"两个都成功、结果却不同"**（后者正是"静默改变行为"的定义）；② **任何一次运行都不许在 CWD 里留下东西**。对照运行由 `withoutFlag` 从同一张参数表去掉那个 flag 得到（不手抄第二份）。读数：**14 明确拒绝 · 1 与"不给"等价 · 0 碰 CWD**。判据刻意不要求"必须拒绝"——**拒绝**与**等价于不给**都合格。牙齿：让 `--path=` 走另一条路 ⇒ 红（`都成功、结果却不同——这正是「静默改变行为」的定义`） |
  | `TestV43BooleanFlagsHoldGoSemantics` | `cmd/ngm`（**v0.43 新增**） | **布尔 flag 的三条语义**（三条来自 Go 标准库，第四条来自产品自己的注释）：表**从源码派生**（`fs.Bool("name"` in `cmd/ngm/<cmd>.go`）→ **41 对**，其中 **38 对跑满 4 条断言**、3 对**公开跳过**（属于某个子命令的 flagset），1.8 秒。断言：① `--flag=true` ≡ `--flag` ② `--flag=false` ≡ **不给** ③ `--flag=x` **必须失败** ④ `--flag ZZ-JUNK` **必须失败**——布尔 flag **从不消费下一个 token**，而 `normalizeArgs` 的注释专门写过「`--all` 绝不能吞掉紧随其后的 positional」，吞掉就是**静默丢参数**。守卫分两层：**结构性**（派生表总数 == 跑完 + 跳过）与**规模下限**（实测值之下）。牙齿：让 `--all` 之后的 positional 被无声忽略 ⇒ ④ 在两处命中 |
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

   > **2026-10-04：本步对 `v0.5.0` ~ `v0.8.0` 暂缓**（项目所有者决定；2026-10-03 曾暂缓一次又取消，
   > 这次是重新拍板）。成因是**凭据**（无 token 时 `POST /actions/workflows/…/dispatches` 返回 401），
   > 而理由是"现在没有人在等这些二进制"——**有意延后，不是遗漏**，恢复信号见本文开头。
   > 暂缓的是"补"，不是"看"：下面的读数与告警步骤**一律保留**。

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
   **（2026-10-04：本步对历史版本暂缓——缺 39 个；新版本发布时仍然照做，恢复信号见本文开头。）**
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

   > ⚠️ **暂缓期间它报"缺 39 个"是预期的**（见本文开头）：这个读数的用途不是催办，
   > 而是让"两个源到底有什么"永远有一个不需要信任谁的答案。

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

- [v0.1 实施计划](./v0.1-plan.md) / [v0.2](./v0.2-plan.md) / [v0.3](./v0.3-plan.md) / [v0.4](./v0.4-plan.md) / [v0.5](./v0.5-plan.md) / [v0.6](./v0.6-plan.md) / [v0.7](./v0.7-plan.md) / [v0.8](./v0.8-plan.md) / [v0.9](./v0.9-plan.md) / [v0.10](./v0.10-plan.md) / **[v0.11](./v0.11-plan.md)**
- [v0.1 复盘](./v0.1-retrospective.md) / [v0.2 复盘](./v0.2-retrospective.md) / [v0.3 复盘](./v0.3-retrospective.md) / [v0.4 复盘](./v0.4-retrospective.md) / [v0.5 复盘](./v0.5-retrospective.md) / [v0.6 复盘](./v0.6-retrospective.md) / **[v0.7 复盘](./v0.7-retrospective.md)**（补写）/ **[v0.8 复盘](./v0.8-retrospective.md)** / [v0.9 复盘](./v0.9-retrospective.md) / [v0.10 复盘](./v0.10-retrospective.md) / **[v0.11 复盘](./v0.11-retrospective.md)** / [v0.5 复核](./v0.5-review.md)
- [internals/roadmap.md](../internals/roadmap.md)
- [internals/metrics.md](../internals/metrics.md)
- [modules/p0-core.md](../modules/p0-core.md)（包级结构唯一事实源）
- [guides/cli.md](../guides/cli.md)（命令形态）