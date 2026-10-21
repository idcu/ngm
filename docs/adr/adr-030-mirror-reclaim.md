# ADR-030：层 1（mirror）的**占用、残骸与整理**

- 状态：已定**并已实施**（2026-10-15，v0.100）
- 相关：[ADR-002 直接拉 Git 仓库](./adr-002-git-direct.md)（mirror 为什么存在）·
  [ADR-008 archiveDigest](./adr-008-archive-digest.md)（"离线可重放"是它守的承诺）·
  [ADR-010 在线 verify 的抓取策略](./adr-010-online-verify-fetch-policy.md)（按需取物）·
  [ADR-018 store 的回收与去重](./adr-018-store-reclaim.md)（**不做**按可达性删除）·
  [ADR-023 孤儿 blob 回收](./adr-023-orphan-reclaim.md)（"无人引用"是可判定的那一类）

> **结论**：层 1 接入与层 2 同形的一套——`store usage` 报它的占用（并点名"一个 ref 都没有"的目录）、
> `store prune` 收它的残骸（`objects/pack/tmp_*`，年龄门槛 **1 小时**）、
> 新增 `store gc` 跑 `git gc` 整理历史本体。
> **同时明确不做的**：`--single-branch` 与部分克隆（见"决定"第 4 条）。
>
> **代价**：mirror 的**形状**一点没变（全历史 + 全部 refs 的裸仓库，ADR-002 的延续），
> 所以本 ADR **不会**让一个 18 MB 的 mirror 变成 3 MB——它削的是**残骸与碎片**这两头。
> `store prune` 因此从"层 2 的残骸"变成"两层各自的残骸"（一个命令两个语义）；
> `store gc` 要读全局配置才能判定 `run:git`，于是**坏掉的全局配置会让它退 3**，
> 而同一个坏配置下 `store usage` 照旧退 0。

---

## 背景

### 触发它的是一个很自然的问题

"如果我只用得到最新版本的代码，一个有很多提交的仓库会不会白占我的磁盘？"

这个问题在 ngm 上格外值得回答，因为 ngm 的依赖**就是 Git 仓库本身**（[ADR-002](./adr-002-git-direct.md)），
而它的本地根基是一个 `git clone --mirror` 的裸仓库（`internal/git/clone.go`）——
也就是说，**用户为整个历史付磁盘**这件事是设计的结果，不是意外。

### 实测（本机，2026-10-15）

先量再判。三个读数：

| 量 | 值 | 说明 |
|----|----|------|
| `express` 的 mirror（`--mirror`，全部 refs） | **18.41 MiB**（in-pack 54155 个对象 / 3982 个 ref / 默认分支 6181 个提交） | 真实 JS 库的完整历史 |
| 它最新一版的源码（HEAD 树） | **694.33 KiB** | "只用最新版"真正需要的东西 |
| 若每个提交各存一份完整快照 | 6181 × 694 KiB ≈ **4.1 GiB** | 朴素实现的开销 |
| 实际 ÷ 朴素 | **约 228 倍压缩** | 内容寻址 + delta 压缩的功劳 |

⇒ **"提交多"几乎不是成本来源**：18.41 MiB ÷ 694 KiB ≈ 26 倍是"全历史 vs 最新版"的真实倍率，
而它由两块构成——其中**约 40% 的对象（54155 − 32545 = 21610 个）不在默认分支的历史上**，
只属于那 3981 个非 HEAD ref。对"只用最新版"来说那部分是纯浪费。
（另一个方向的读数：`lodash` 的 `--depth=1` 克隆总共 5.36 MiB，其中 `.git` 只占 **0.82 MiB**，
工作区 4.54 MiB —— 历史开销比代码本身还小。）

### 真正的浪费，是两台"看不见"的账

实测里最刺眼的不是 18 MB，而是两件事：

**① 一次被中断的克隆会留下一个没有任何人认领的包。**

```
lodash-lodash-full.git   66.82 MiB / in-pack: 0 / packed-refs 不存在
```

那个目录是 `git clone --mirror` 被中断后的形状：**有体积、没有一个可用对象、也没有一个 ref**。
它看起来像个 mirror（`HEAD`、`objects/`、`refs/`、`core.bare=true` 都齐），
但解析不出任何 commit。在此之前，ngm 里**没有任何代码看得见它**：
`Mirror.Exists` 只用 `IsBareMirror` 判形状，而那个形状恰好是齐的。

**② git 自己不会及时收走它。**

`git gc` 确实会删 `objects/pack/tmp_*`，但**只删超过两周的**（实测：13 天存活、15 天被清，
阈值即 `gc.pruneExpire` 的默认值 `2.weeks`）。也就是说，中断一次克隆之后，
那几十 MB 要**两周**才会被 git 自己拿回去。

### 而文档一直把出路写在用户那一边

`docs/internals/metrics.md` 的"两层的分工"表写着：

| 层 | 谁来回收 | 现状 |
|----|---------|------|
| 1 mirror（Git 裸仓） | `git gc` —— git 的既有能力，用户随时可跑 | 有出路 |
| 2 content store | `store prune --orphans`（ADR-023） | 有第一条回收路径 |

"用户随时可跑"在当时是对的。但 v0.7 起 `store usage` 已经把层 2 的体积摆了出来，
于是"层 1 的体积"成了一个**看得见却点不动**的位置：用户拿到"这个 mirror 占了 18 MB"
之后，下一步只能自己去猜 `~/.ngm/mirror` 在哪、以及该跑什么。

## 决定

1. **占用可见**：`ngm store usage` 增报层 1——逐仓库的体积、其中多少是 **packfile**、
   多少是**残骸**，并点名**一个 ref 都没有**的仓库（`Refless`）。
2. **残骸可回收**：`ngm store prune` 收 `objects/pack/tmp_*`，年龄门槛 `MirrorResidueGrace` = **1 小时**。
   它是**同一个命令的第二段**（既有那几行的顺序与措辞一字不动，新段追加在最后）。
3. **历史可整理**：新增 `ngm store gc`（`--dry-run` 可用），对每个可用的 mirror 跑 `git gc`。
4. **不做**改克隆形状：**拒绝** `--single-branch`，**拒绝**部分克隆（`--filter=blob:none` / `tree:0`）。

## 理由

### 为什么第 4 条是"拒绝"而不是"排期"

这两条路都能真的减小 mirror，代价是把 ngm 的核心承诺换成"用到的时候再说"：

- **`--single-branch` 会打断依赖声明的完整性**。ngm 的 `refType` 允许 **tag / branch / commit**
  三种（[ADR-004](./adr-004-reftype.md)），也就是用户完全可以写 `#v1.2.3` 或 `#branch=next`。
  只留默认分支的 mirror 会在解析这些 ref 时**在本地找不到东西**，而它是"离线也能解析"的根基。
- **部分克隆会把"本地可证明"变成"需要时下载"**。`archiveDigest` 的重放要读**整棵树**的
  blob（[ADR-008](./adr-008-archive-digest.md)），而 `verify` 的卖点正是"digest 重放完全在本地进行"
  （`internal/vendor/mirror.go` 的那句注释）。按需取 blob 会把每一次离线 verify
  变成一次网络访问——那恰好是 [ADR-010](./adr-010-online-verify-fetch-policy.md) 与
  [ADR-018](./adr-018-store-reclaim.md) 一路小心避开的方向。

**结论：成本只能从"整理"与"残骸"两头削，不能从"要什么才拿什么"那头削。**
而这恰好够用——因为实测里的 66.82 MiB 一个字节都不是历史，是残骸。

### 为什么残骸的门槛是 1 小时，而 git 自己的是两周

`tmp_*` 只由**正在进行中**的 clone/fetch 写入。mirror 层**没有跨进程锁**
（`internal/vendor/mirror.go` 的 `Mirror` 文档如实写着这一点），
所以删除它的唯一风险是"砍掉另一个进程正在写的临时包"。

- 1 小时对真实的并发足够宽松：临时包的生命周期就是那次 `git clone --mirror` / `git fetch` 本身，
  而 JS 依赖的仓库在这个量级上远短于一小时；
- 1 小时比 git 自己的两周短得多，而那正是这条路径存在的理由：
  **中断一次克隆，用户不该等两周才拿回磁盘。**

## 代价与边界

| 边界 | 为什么 |
|------|--------|
| mirror 仍是**全量**（全历史 + 全部 refs） | 第 4 条的拒绝；两头的整理不改变形状，所以本 ADR 不会让一个 18 MB 的 mirror 变成 3 MB |
| `store gc` **不**传 `--prune=now` | 默认的"不可达对象保留两周"是 git 的安全窗口（上游删掉的分支/回退的提交仍可恢复）；ngm 没有理由为"数字好看"缩短它 |
| `store gc` **跳过**没有 ref 的目录，并且**不删**它们 | 它不是"需要整理的历史"，而是"一个没写完的仓库"；对它跑 `git gc` 没有意义，它的失败还会伪装成"整理失败了"。ngm 不删不属于自己的东西——**报出来 + 给出路**（`usage` 的 note 里写着 `rm -rf` + 重跑 `install`） |
| `store gc` 跳过不是裸仓库的路径 | 同上：mirror 根下不该有这种东西，但那是用户的磁盘 |
| `store gc` 的部分失败**不伪装成成功** | 某个 mirror 被 git 拒绝时，继续整理其余的，然后**原样返回失败**（退出码 4），而"已经整理好的那些"照样打印出来——用户要不要重跑取决于它 |
| `store prune` 的层 1 段没有新 flag | 门槛是一个**常量**（1 小时）而不是旋钮：`--older-than` 的既有语义（只对 `--orphans` 有效）一个字节没动，因为它同样是既有契约 |
| 跨进程并发**仍然没有锁** | 这是既有的、已知的缺口（v0.66 写在 `Mirror` 的文档里）。年龄门槛是**缓解**，不是防护——本 ADR 不假装它变成了防护 |
| `objects/<shard>/tmp_obj_*` 不在口径内 | 范围只认 `objects/pack/tmp_*`（实测到的那一类）。单对象的临时文件是 KB 级的；**口径写下来**是为了让"没清干净"永远有一个可核对的边界 |

## 后果

- 判据（层 1）：`internal/vendor/mirror_maintain_test.go` ——
  小计与明细逐项对账、残骸的**归属**（不能算到干净仓库头上）、门槛的
  **两个方向**（旧的走 / 年轻的留）、0 门槛被拒、`dry-run` **一个 git 子进程都不起**
  （`git.SpawnCount` 仪器断言，不是掐表）、以及**安全边界**：对象库里除残骸以外的每一个文件都必须活下来。
- 判据（端到端）：`cmd/ngm/store_gc_acceptance_test.go` —— 三件事的闭环；
  码 **4** 的测量（夹具是一个**真实存在**的破损形状：refs 指向不存在的对象），
  并登记进 `exitCodeElsewhere`（`TestV100StoreGCReportsGitFailureInsteadOfPretending/store gc`）。
- `store gc --dry-run` 纳入 V14 的 dry-run/只读网（快照比对：一个字节都不写）。
- `store usage` 的一处**既有缺陷随本 ADR 一并修掉**：它此前在"content store 不存在"时
  **提前返回**并说"没有可报告的东西"——于是只有层 1 的机器（clone 过、还没 install）
  会被这句错话挡住。现在两层都读，只有两层都不存在才说那句话。
- 文档同步：`docs/internals/metrics.md` 的"两层的分工"表（层 1 那一行不再是"用户自己跑"）、
  `docs/architecture/vendor-layers.md`（层 1 的读数口径）、`docs/guides/cli.md`（store 一节）。
