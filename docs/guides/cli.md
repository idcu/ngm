# CLI 参考

> 成熟度列标注该命令自哪个版本可用；v0.1 ~ v0.4 的命令**均已实现**，v0.1 的由
> [端到端验收](../development/v0.1-plan.md)守护，其后的由各版验收测试守护。
> 未实现的命令不会静默成功——它们明确返回 `exit 3` 与可读提示。
>
> **全局 flag 的作用域（v0.12 修 · v0.55 收紧）**：`ngm --help` / `ngm --version` 输出根帮助与版本，
> **`ngm <command> --help` 输出该命令自己的用法**——而且**就是**它参数错误时打印的那份文本
> （各命令的 `xxxUsage` 常量），不是另写的第二份帮助。判据是**位置**：`--help` 出现在子命令
> **之前**才算根级，因此 `ngm verify --help` 给你 verify 的用法，不是根帮助。
> v0.12 之前这条是坏的：根级扫描跨越**整条**参数，子命令帮助那一分支永远走不到——
> 它打印的 "help not yet implemented" 没有任何人见过。
>
> **根级只认 `--help` / `-h` / `--version` 这三个**（v0.55 起）。子命令**之前**的其他 token
> 一律拒绝（`exit 3`，并提示"命令的 flag 写在命令**之后**"），不再静默丢弃。
>
> 为什么收紧：从前它们**进得去、出不来**——实测在一个有 `lock` 的项目上：
>
> ```text
> $ ngm --json verify         # 用户要的是 JSON
> exit 0 · stdout 是**人读文本** · stderr 0 字节
> ```
>
> 这是最坏的一种：**走错通道、还一声不响**——脚本拿到散文、退出码还是 0，
> 连"出事了"都不知道。现在它 `exit 3`，而且因为用户提了 `--json`，
> 机器那侧还会收到一份**错误信封**（v0.54）：
>
> ```json
> {"version":1,"error":{"code":"Usage","exitCode":3,"message":"unknown root flag: --json",
>   "hint":"root level accepts only --help/--version; a command's flags go after the command (e.g. `ngm verify --json`)"}}
> ```
>
> 校验与动手是**两个循环**：先全部核对、再执行——否则 `--version --bogus` 与
> `--bogus --version` 会给不同结果，而"顺序敏感"不是一条能记住的规则。

---

## 命令一览

| 命令 | 作用 | 成熟度 | 主要来源文档 |
|------|------|--------|-------------|
| `ngm init <name> [--runtime=node\|deno] [--dir=<path>] [--force]` | 初始化项目（同时生成 `src/index.ts`，不覆盖已存在的） | v0.1 | [快速上手](./quickstart.md) |
| `ngm add <git-url>[@<ref>] --ref-type <type> [--path=<子路径>] [--dry-run]` | 添加依赖（refType 必填） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm install [--digest]` | 解析并安装依赖（有 lock 则尊重 lock） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm update [<dep>] [--all] [--offline] [--digest] [--store]` | 更新 ref 与锁定 | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm remove <dep>` | 移除依赖（只改声明） | v0.1 | [依赖管理](./dependency-management.md) |
| `ngm verify [<dep>...] [--offline] [--deep] [--json] [--strict] [--allow-drift] [--sandbox] [--signatures] [--require-signed]` | ref 漂移 + digest 重放检查；`--sandbox` 追加在 Deno 沙箱里执行依赖自带的 `verify.js`（缺 Deno 且确有脚本 → exit 5）；`--signatures` 追加报告 Git 签名状态（**未签名不是失败**），`--require-signed` 把它变成门槛（未签名/无法用你的密钥验证 → exit 2） | v0.1 / **v0.3 增 `--sandbox`** / **v0.4 增 `--signatures`、`--require-signed`** | [信任模型](../architecture/trust-model.md) · [ADR-012](../adr/adr-012-sandbox.md) · [ADR-014](../adr/adr-014-self-report-signatures.md) |
| `ngm build [<entry>] [--engine=<name>] [--outfile=<path>] [--production] [--dry-run]` | 构建（adapter） | v0.1 | [构建](./build.md) |
| `ngm typecheck [<entry>] [--engine=<name>] [--tsconfig=<path>] [--dry-run]` | 类型检查（adapter） | v0.1 命令 / **v0.2 有引擎**（内置清单里是 `typescript` = tsc）。**需要声明**：`engines.typeCheck` 或 `--engine=typescript`——未声明是 `exit 3`，声明了但 `tsc` 未装才是 `exit 5` | [构建](./build.md) |
| `ngm typedecl [<entry>] --outdir=<dir> [--engine=<name>] [--dry-run]` | 生成 `.d.ts` 声明（adapter）；**`--outdir` 必填**，并报告**实际出现**的文件 | **v0.4 已实现**（此前该能力只有 adapter 与单测，没有命令驱动它） | [构建](./build.md) |
| `ngm transform [<file>] [--engine=<name>] [--outfile=<path>] [--loader=<name>] [--target=<es20xx>] [--format=<fmt>] [--minify] [--sourcemap] [--dry-run]` | **单文件**转换（adapter）；输入默认走 **stdin**，产物默认走 stdout。**不解析导入**——那是 `ngm build`。loader 按 `--loader` → 文件扩展名（推断会说出来）→ `engines.transform.options.loader` 的顺序取；都不适用时**不猜**，"必须有 loader" 由引擎自己判（esbuild 会） | **v0.5 已实现** | [构建](./build.md) |
| `ngm css <input.css> [--engine=<name>] [--outfile=<path>] [--minify] [--dry-run]` | CSS 编译（adapter）；**只接受一个输入文件**，多给的位置参数会**明确报错**（v0.12 之前是静默只编第一个，而 `ngm css dist/*.css` 这种 glob 展开很容易中招） | v0.2 起适配 `postcss`。**本行此前写"v0.1（`esbuild`）"是错的**：实测 `ngm css --engine=esbuild` 报 **no css engine named esbuild**（可用：postcss、self）——esbuild 覆盖的是 bundle / transform。**需要声明**（`engines.css` 或 `--engine=postcss`），未声明 `exit 3`、未装 `exit 5`；它无内建压缩，`--minify` 会被明确告知忽略 | [构建](./build.md) |
| `ngm mappings validate` | 校验 mappings 与 lock / vendor 一致性 | v0.1 | [P4 — 生态与协议](../modules/p4-ecosystem.md) |
| `ngm cache clean` | 清空缓存层（不影响可证明性） | v0.1 | [vendor 4 层](../architecture/vendor-layers.md) |
| `ngm store usage` | 报告 content store 的占用：**只读**，按布局分组——blob 池（去重后的真实内容，并切成**共享 / 独占 / 孤儿**）、v2 树清单、v1 遗留树，各自来自哪个 `repo@commit`，以及解包残骸 | **v0.7 已实现**；v0.8 输出按布局分组；**v0.9 加共享/独占/孤儿**（见下） | [ADR-018](../adr/adr-018-store-reclaim.md) · [ADR-019](../adr/adr-019-content-addressed-blobs.md) |
| `ngm store prune [--dry-run] [--orphans] [--older-than=<dur>]` | 清掉**中断留下的解包残骸**（`.unpack-*`）。**不碰任何内容树**，并报告"留下了 N 份没动"。加 `--orphans` 时**额外**删掉"没有任何清单引用、且比门槛（默认 24h）更旧"的 blob | **v0.7 已实现**；`--orphans` **v0.11 已实现** | [ADR-018](../adr/adr-018-store-reclaim.md) · [ADR-023](../adr/adr-023-orphan-reclaim.md) |
| `ngm config validate\|show [--dir=<dir>]` | 配置校验与查看。**`validate` 校验 `--dir` 指向的项目**（v0.19 修：此前只认进程 CWD，且**没有 ngm.json 时照样打印 `ngm.json OK`**——CI 在错的目录里会拿到绿色假通过）；`show` 是查看，不设门禁 | v0.1（v0.19 修三处） | [配置详解](./configuration.md) |
| `ngm engines list\|info\|validate [--json]` | 引擎管理 | v0.1 | [配置详解](./configuration.md) |
| `ngm audit [<dep>...] [--json] [--offline] [--no-cache] [--hook=<script.js>]` | OSV 漏洞扫描（按 **commit** 查询 + 24h 缓存）；`--hook` 在沙箱里跑团队自己的策略（报告从 stdin 进入，否决 → exit 1）。`--json --hook` 时 hook 的横幅与它自己的 stdout **走 stderr**，stdout 保持是单个 JSON 文档（v0.12 修） | **v0.2 已实现**；`--hook` **v0.3** | [供应链防护](../architecture/supply-chain.md) · [ADR-012](../adr/adr-012-sandbox.md) |
| `ngm why <dep> [--json] [--all]` | 该依赖的来源路径（有多个父节点时列出多条）。**默认最多枚举 64 条**并写明"还有更多"；`--all` 解除（[ADR-024](../adr/adr-024-bounded-explanations.md)：路径数是指数的，而图形状来自上游清单） | **v0.2 已实现**（上界 **v0.17**） | [可观测性](../architecture/observability.md) |
| `ngm tree [--osv] [--offline] [--json] [--all]` | 依赖树 + 漂移（`⚠`）；漏洞（`✗`）需 `--osv`。**默认最多展开 4096 个条目**并写明"树不完整"；`--all` 解除 | **v0.2 已实现**（上界 **v0.17**） | [可观测性](../architecture/observability.md) |
| `ngm outdated [--offline] [--json]` | 有哪些新版本；查不到报 `unknown` 而非"最新"，**并在行下打印原因**（v0.12：原因此前只存在于 `--json` 的 `note` 字段）；`--offline` **不触网**——冷 mirror 时报"没有本地镜像"而不是去 clone（v0.12 修） | **v0.2 已实现** | [可观测性](../architecture/observability.md) |
| `ngm install [--frozen-lockfile] [--offline]` | CI 模式：frozen 禁止解析新 ref / 改写 lock（不一致 exit 3）；offline 禁止联网（资源缺失 exit 4） | **v0.2 已实现** | [锁定机制](../architecture/locking.md) |
| `ngm integrations add <tool> [--dry-run] [--json]` | 生成 `vite` / `esbuild` / `deno` / `webpack` 集成配置（**不覆盖已有文件**，冲突 exit 3） | **v0.3 已实现** | [P5 — 外部工具集成](../modules/p5-integrations.md) |
**content store 的回收**（[ADR-018](../adr/adr-018-store-reclaim.md) / [ADR-023](../adr/adr-023-orphan-reclaim.md)）：
占用可见（`ngm store usage`，只读，**v0.7**）、残骸可回收（`ngm store prune`，**v0.7**）。
**v0.11 起**多了一件：`prune --orphans` 可以删掉**没有任何清单引用**的 blob（带年龄门槛）。

两件事**仍然不做**，而且是有意的：

- **没有按可达性删除**（"这个 digest 还有没有人要"）：那需要一个 ngm 没有的项目注册表，
  误删会让别的项目的 `ngm verify` 在某天突然验不过；
- **没有自动回收**：回收是**显式的一步**，默认什么都不删——不带 `--orphans` 时
  `prune` 的行为与 v0.7 完全一致。

`--orphans` 的安全边界（写进实现的，不是承诺口吻）：

| 约束 | 为什么 |
|------|--------|
| 只删"没有任何清单引用"的字节 | 由构造可判定，不需要注册表（与上一条的区别在这里） |
| 年龄门槛（默认 24h） | `Put` 先写 blob、后发布清单，刚写下的东西必须不动 |
| **有清单读不出来时拒绝删除** | 那时"无人引用"只是下界，删了可能破坏那份清单 |
| `--dry-run` 同样拒绝 | 一份在那种状态下"将会删 X"的报告是误导 |
| 输出必须报"留下多少"与"多少被引用的没动" | 删除类命令的安全声明该在的地方 |

**三个数字，三个问题**（**v0.9**）：`store usage` 把 blob 池切开之后，这一层第一次能
分开回答"去重省了多少"和"我还能回收多少"：

| 数 | 回答的问题 | 口径 |
|----|-----------|------|
| **共享**（shared by 2+ trees） | 去重省了多少 | 被 2 棵及以上内容树引用的 blob 字节和（若不去重，这一块要乘以引用数） |
| **独占**（exclusive） | 丢掉某一棵树能回收多少 | 只被**那棵**树引用的 blob；每棵树一行，**只算 blob**——所以 `exclusive <= logical`，且各树独占之和**恰好**等于池里那一块 |
| **孤儿**（orphaned） | 有多少空间没有任何人需要 | 没有任何清单引用的 blob。**不是"垃圾"的同义词**（也可能来自被删掉的项目）。`prune` 不带 `--orphans` 时**不会**动它；带上才清，且只清比年龄门槛更旧的 |

> 每行那两列也要分开读：`logical` 是**逻辑体积**（共享 blob 会被重复计算，把所有行加起来
> 会比 store 还大），`exclusive` 才是"丢掉它能回收多少"。
> 这三个数也是 ADR-018 将来判"要不要做 GC"所需的数据——在那之前，它们只是把
> "没有人回收"从一句印象变成一个数字。

真正能改变增长曲线的是**层 2 的写入侧去重**（**v0.8 已落地**，[ADR-019](../adr/adr-019-content-addressed-blobs.md)）：
布局改为 blob 池 + 树清单，实测把同一场景的放大比从 **20×** 降到 **0.6×**
（[metrics](../internals/metrics.md#磁盘增长内容寻址-storev06)）。
**但这改的是斜率，不是终点**：blob 的**新增**仍然只增不减，**按可达性**的回收仍受
[ADR-018](../adr/adr-018-store-reclaim.md) 那两个条件约束（缺项目注册表）。
**v0.11 起多了一条**：[ADR-023](../adr/adr-023-orphan-reclaim.md) 的 `prune --orphans`
能回收**无人引用**的 blob——它改的是**残骸**，不是被引用的增长。

**升级已有 store 时**：新装的依赖立刻用新布局，**老条目不会自动迁移**（`install` 见到
`Has` 为真就短路——那是"无网络也能安装"的承诺）。`ngm store usage` 在存在老条目时会把
出路打印出来：删掉整个 `sha256/` 再 `ngm install`（层 2 是派生物，可重建）。

**内置引擎清单（v0.4 时点）**：`esbuild`（`bundle` + `transform`）、`typescript`（`typeCheck` + `typeDecl`，
自带 `tsc --emitDeclarationOnly`，`optional`）、`postcss`（`css`，`optional`）、`self` stub；
wasm adapter 自 v0.3 起可用（模块路径写在清单里，缺失是**可用性**问题 → `exit 5`）。
`remote` **按 [ADR-013](../adr/adr-013-remote-adapter.md) 决定不发布**，声明它会得到"按决定排除"而不是"待实现"。
自行声明 subprocess 引擎的方法见 [配置详解](./configuration.md) 与 [P4 协议](../modules/p4-ecosystem.md)。

**`transform` 自 v0.5 起有命令入口**（`ngm transform`，见下表）。在此之前它与 v0.4 之前的
`typeDecl` 同型：能力在 adapter 层已实现，却没有任何命令驱动它——于是 `engines.transform`
与 `engines.defaultTransform` 两个配置键对用户没有任何可观察的效果。补上入口时顺带修掉一处
协议缺陷：`genericInvocation` 对 transform **只转发 loader 与 target**，
`--format` / `--minify` / `--sourcemap` 会被静默丢弃（自定义引擎收不到它们）。

---

## 全局约定

- **退出码**：0 成功 / 1 策略失败（含引擎运行失败）/ 2 完整性失败 / 3 配置错误 / 4 Git 网络失败 / 5 引擎不可用；完整定义见[可观测性](../architecture/observability.md)
- **配置文件**：`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json` / `~/.ngm/config.json`，见[配置详解](./configuration.md)
- **`--offline`**：禁止网络访问，只用本地 mirror / content store；资源未命中即失败（exit 4）。
  适用于 `verify` / `update` / `install` / `audit` / `outdated`。
- **`--json`**：机器可读输出走 stdout，人类文本与诊断走 stderr；CI **不应**解析人类文本。
  支持它的有 **7 个命令**：`verify` / `audit` / `tree` / `why` / `outdated` / `engines` / `integrations`。
  各命令的**顶层形状**见下一节。

---

## `--json` 的形状（机器接口）

这些形状**是接口**，不是实现细节。下表是**实测**的顶层形状（在 v0.16 之前，
它们只存在于代码里——用户侧一个字都没有，写脚本的人只能靠试）：

| 命令 | 顶层形状 | 带退出码字段 |
|------|---------|------------|
| `ngm verify --json` | 对象：`version` · `strict` · `deep` · `offline` · `allowDrift` · `dependencies` · `summary` | ✅ `summary.exitCode` |
| `ngm audit --json` | 对象：`generatedAt` · `coverageNote` · `dependencies` · `findings` · `vulnerabilities` · `ignoredByPolicy` · `exitCode`（有发现时另有 `bySeverity`） | ✅ `exitCode` |
| `ngm integrations add <tool> --json` | 对象：`tool` · `dryRun` · `artifacts` · `warnings` · `exitCode` | ✅ `exitCode` |
| `ngm tree --json` | 对象：`project` · `entries` · `drifted` · `dependencies` · `osvChecked`（截断时另有 `entriesTruncated` · `entriesLimit`，停止处条目带 `truncated`；查到漏洞时另有 `remediation`） | ✗ |
| `ngm why <dep> --json` | 对象：`name` · `ref` · `refType` · `commit` · `subPath`（monorepo 子路径，无则省略） · `locked` · `paths` · `rootDeclared`（截断时另有 `pathsTruncated` · `pathsLimit`） | ✗ |
| `ngm outdated --json` | 对象：`offline` · `entries` · `dependencies` · `updates` · `stale` | ✗ |
| `ngm engines list --json` | **数组**：每个元素是 `name` · `kind` · `adapter` · `command` · `declaredVersion` · `version` · `available` · `stub` · `builtin` · `supportedInput` · `defaultOptions` | ✗ |
| `ngm engines info <name> --json` | **数组**：同一个元素形状（`name` · `kind` · `adapter` · `command` · `declaredVersion` · `version` · `available` · `stub` · `builtin` · `supportedInput` · `defaultOptions`）；同名不同 kind 会各占一行（`esbuild` 同时是 bundler 与 transformer） | ✗ |
| `ngm engines validate --json` | 对象：`version` · `ok` · `issues` | ✗ |

> 那两处**数组**不是笔误：`engines` 的两条命令返回的是"行"，而一个名字可以对应多种 kind。
> 其余命令返回的是"一份报告"，所以是对象。本页只如实记录两者的差异——
> **统一形状是破坏性变更**，要另立决定，不在文档里顺手改。

**"接下来做什么"在机器那一侧也有名字**（v0.50）：人读的报告里，
每一条失败项都配了一行 `→ …` 或 `Fixed in: …`（v0.32 起的契约）。
从 v0.50 起，**同一个句子**在 JSON 里也有落处，于是读 `--json` 的脚本
不必去解析人读文本：

| 报告 | 字段 | 何时出现 |
|------|------|---------|
| `verify` | `dependencies[].remediation` | 该依赖有可分类的漂移或操作性失败时 |
| `audit` | `findings[].vulnerabilities[].remediation` | **该公告没有记录修复版本**时（有修复版本时是 `fixedIn`） |
| `tree` | `remediation`（顶层） | 查到漏洞时 |
| `engines validate` | `issues[].remediation` | 每一条问题都带 |

两条纪律：① **同一条句子、两个通道同源**——`NoFixAdvice` / `VulnRemediation` /
`issueAdvice` 各只有一处，人读与 JSON 都从它取；② 这条契约由
`TestV50MachineReadableReportsCarryTheNextStepToo` 机械守住，而它查的是**内容**
（人读那句话必须逐字出现在 JSON 里），不是"有没有某个字段"。

四条**每次调用都成立**的规矩（由 `TestV16JSONReportsTellTheTruth` 在 **20 个状态**上机械守住：
17 次"带 `--json` 与不带"的对照 + 3 条输入错误路径）：

1. stdout 要么为空、要么是**恰好一份**合法 JSON 文档——**不会出现半份**；
2. **`--json` 不改变退出码**：同一状态、同一夹具下，带与不带它退同一个码；
3. 报告里若带 `exitCode` / `summary.exitCode`，它**等于进程退出码**——机器可读输出不许说谎；
4. 失败时 stdout 是**报告**或**错误信封**（**v0.51 起**）：
   - 有报告可给（如 `verify` 的漂移/完整性失败）⇒ 一份**完整报告**（exit 1/2 时 stdout 里有整份 JSON）；
   - 没有报告可给（依赖不在图里、引擎不在目录里、工具名不合法）⇒ 一份**完整的错误信封**：

```json
{
  "version": 1,
  "error": {
    "code": "ConfigInvalid",
    "exitCode": 3,
    "message": "no ngm.lock found at /path/to/ngm.lock",
    "hint": "run `ngm install` to resolve dependencies and create the lock"
  }
}
```

   · `code` 是错误码的**显示名**（与人读那侧同一处产出：`NgmError.DisplayName`）；
   · `exitCode` **等于进程退出码**（规矩 3 的同一件事）；
   · `hint` 与人读那侧的 `hint:` 行**逐字相同**——两个通道说同一句话；
   · **命令不接受 `--json` 时 stdout 仍为空**：出错时不假装有机器可读输出。
   · **解析阶段的失败也给信封**（v0.54）：`ngm verify --json --bogus` 这类失败发生在
     "命令还不知道 `--json`"之前——从**原始参数**里问一次，是用户提的就给一份信封
     （`code` 是 `Usage`、`exitCode` 3、`message` 说清是哪个 flag 不认识）。
     人读那侧不受影响：usage 照旧在 stderr，而且**只说一次**。

   > 规矩 4 改过两次，两次都是**实测逼出来的**：
   >
   > · **v0.51**：原先要求"输入错误时 stdout **必须为空**"，后果是脚本拿到
   >   "退出码 + 空 stdout + 一段人读文本"——**知道出了事，却读不出是什么事**。
   >   精神没变（"我没有报告可给"不能用半份 JSON 表达），改的是：
   >   **错误信封不是报告**，它说的正是"我失败了、原因与下一步在这里"。
   > · **v0.54**：v0.51 的信封写在 `runErr` 里，而它只在**命令内部**被调用——
   >   解析失败与"命令不存在"都发生在它之前。实测：`ngm verify --json --bogus`
   >   → exit 3 · **stdout 0 字节** · stderr 3198 字节 usage。

---

## 常用组合

CI 门禁（v0.1 可用）：

```bash
ngm install            # 有 ngm.lock → 尊重 lock，不重新解析 ref
ngm verify --json      # 读 driftKind 与退出码：0 通过 / 1 非预期漂移 / 2 完整性失败
ngm build --outfile=dist/app.js
```

完全离线可复现（v0.2 已实现）：

```bash
ngm install --frozen-lockfile --offline
ngm verify --json
```

开发循环：

```bash
ngm add github:my-org/utils@main --ref-type branch
ngm install
ngm verify
ngm build --dry-run    # 先看 ngm 会怎么调引擎
```

---

## 相关文档

- [快速上手](./quickstart.md)
- [依赖管理](./dependency-management.md)
- [构建](./build.md)
- [配置详解](./configuration.md)