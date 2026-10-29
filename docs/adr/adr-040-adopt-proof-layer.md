# ADR-040：从"包管理器"退回"证明层"——`ngm adopt`

## 结论

ngm **不再要求**用户换掉安装器。新增 `ngm adopt`：读已有的 `node_modules`，
按 ngm 自己的清单格式（[ADR-008](./adr-008-archive-digest.md)）算内容摘要，
并按 `package-lock.json` 标注来源，产出 `ngm.proof.json`。

它**只读**：不安装、不写 `node_modules`、不改任何 lock，也不判断代码是否安全。

## 背景

技术上这个项目已经交付了一百个版本，而外部使用者是 **0**。上一轮全量审计给出的
判断是：问题不在能力，在**进入成本**——ngm 只对自己装出来的树说话，而"先换安装器"
对一个已有几十个 npm 依赖的项目是不可接受的交换。

同一轮审计还确认了 ngm 的不可替代资产只有三件：`refType` 必填、规范化内容清单哈希、
漂移三态。这三件都**不需要**接管安装才能交付。

## 决定

1. **新增 `ngm adopt`**，把"证明"与"安装"解耦：
   - 摘要与 `ngm verify` 的 `archiveDigest` **逐字节同构**（同一个函数算出来的），
     因此 adopt 的产物与 ngm 自己装出来的条目可以直接比较，而不是又一套哈希。
   - 来源优先用 npm 的 `package-lock.json`（v2/v3 的 `packages` 段）：
     `git+…@<40hex>` → `commit`；Git 但 fragment 不是 40 位 hash → `ref`
     （**不可追溯，因此不写进 commit 字段**）；其余 → registry `integrity`。
   - 递归展开嵌套 `node_modules`：只看第一层会让"漏掉的包"与"不存在的包"长得一样。
   - 符号链接**不跟随**：workspace 链接指向项目自己的源码，跟随会把"不在任何 lock 里"
     的内容算成一份依赖。
2. **读不了就说出读不了**：`pnpm-lock.yaml` / `yarn.lock` 存在但未解析时，
   输出 `notes` 明说没有 YAML 解析器（ADR-003 不引入依赖），并把每个包标成
   `provenance=unknown`。**绝不**退化成"看起来全都追溯得到"。

   > **这一条不是"待补的缺口"，是明确的非目标**（2026-10-29 收敛）。补它要吃掉
   > 一个 YAML 解析器（或每次多起一个 `pnpm` 子进程），换来的只是 **registry 条目**的
   > 来源信息——而那份信息不是 ngm 的轴：`resolution.integrity` 是 npm 自己的完整性
   > 机制，ngm 重算一遍不增加任何保证。ngm 只对**能绑到 commit 的东西**负责。
   > 一个 pnpm 项目要用 ngm，走的是**共存**（见"后果"第一条），不是"被 adopt 解析"。
3. **覆盖率是一等字段**：`coverage.unknown` 与 `withCommit` 同时出现。
   一份 42 个包里只有 3 个可追溯的证明，与 42 个全可追溯的证明，
   在只有 `packages` 数组的形态下**完全一样**——那是本项目一直在杀的那类谎。
4. `--format=sarif|github` 同步加在 `verify` 上（[ADR-039](./adr-039-exit-code-precedence.md) 同批）：
   证据要能被别人的流水线消费，否则"证明"只停在终端里。

## 后果

- **不再把 registry 互通列为待办**：ngm 不与 pnpm 争夺安装，只对结果说话。
  这条改写了过去"先窄后宽"里那个未答的问题——"宽"不是补 registry，是**不接管**。

  **共存的具体形状**（这才是 pnpm 项目用 ngm 的路径，不需要 adopt 去解析它的 lock）：

  | | pnpm 管 | ngm 管 |
  |---|---|---|
  | 落地目录 | `node_modules/` | `ngm.vendor/` |
  | 锁定文件 | `pnpm-lock.yaml` | `ngm.lock` |
  | 对象 | registry 包 | **仅 Git 依赖** |
  | 证明 | `integrity`（tgz 哈希） | `archiveDigest`（规范化清单 + commit 绑定） |

  两套目录、两套 lock，互不覆盖，也互不解析对方的格式。真实项目里的 Git 依赖是个位数
  （私有 fork、必须追到 commit 的上游），所以"接入"的成本就是把那几行从 `package.json`
  挪进 `ngm.json`——adopt 只服务于**已经用 npm 且不想改任何东西**的那一档。
- `ngm.proof.json` 与 `ngm.lock` 是**两种东西**：前者描述磁盘现状（可以被任何人装出来），
  后者钉住 Git 来源（只有 ngm 能维护）。文件名不同是为了让 `install` 不会读到它。
  也因此 **adopt 是入口，不是主干**：它出证据，但 ngm 的判定能力全部建立在 `ngm.lock` 上。
- **快照可以被复核，但复核的仍是快照**（v0.109 补 `--check`）：写下的证明若没人再读，
  它和一次从未发生的审计等价；于是有一条只读的比较路径，按**路径**（而非包名）对比
  内容摘要，有差异即 exit 2。它不需要 lock、不需要 `ngm install`、也不需要网络，
  但它回答的是"字节变了没有"，**不**回答"这些字节绑在哪个 commit 上"——
  后者只有 `ngm.lock` 能给。这条边界写在这里，是因为"证明"这个词很容易被读大。
- 摘要覆盖面仍是**内容**：一个被攻破的上游、一个恶意 commit，adopt 会如实证明它存在。
  它与 Socket 这类"检测恶意"的工具是互补，不是替代。
- **不为 registry 条目做来源解析**（含 pnpm/yarn lock）。理由见决定 2 的注：
  那既不是 ngm 的轴，又要换来一个永久的解析器负担。已从路线图移除。

## 相关文档

- [CLI 参考 · `ngm adopt`](../guides/cli.md)
- [竞品对比](../COMPETITIVE-ANALYSIS.md)
- [观察：证据如何被消费](../architecture/observability.md)
