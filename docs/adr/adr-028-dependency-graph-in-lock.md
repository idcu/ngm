# ADR-028：把**直接依赖边**写进 ngm.lock

- **状态**：已定（2026-10-09）
- **范围**：`ngm.lock` 的格式（新增可选字段）；`ngm why` 的数据来源；SBOM 的依赖关系段
- **上游**：[ADR-004](./adr-004-reftype.md)（声明用 refType、锁定用 commit）·
  [locking.md](../architecture/locking.md)（lockfileVersion 演进规则）·
  [评估报告 2026-10-09](../internals/assessment-2026-10-09.md)（判断前提：每 3 版至少 1 版用户可见增量）

> **结论**：v0.73 起，lock 的每个依赖条目新增 `"requiredBy"` 字段，记录**直接引用者**
> （排序、去重的身份列表；项目的直接依赖记为根标记 `(root)`，且**字段恒在**）。
>
> **v0.81 更正**：这里的"空数组 `[]`"是裁决时的写法，实现时已被下文
> **§2.1b 修订**推翻（根声明必须写成 `(root)`，否则 `why` 会少列"根 → 它"那条路径）。
> 顶部结论是大多数人唯一会读的一段，而它与实现不一致——
> **结论与修订冲突时，被改的必须是结论**。
> `ngm why` 优先从 lock 回答（**0 次 git 子进程、离线可用**）；SBOM 的 CycloneDX
> `dependencies` 段与 SPDX `RELATIONSHIPS` 由同一份边导出。
>
> **代价**：lock 每依赖多一行；依赖关系变化时 diff 变大（但那正是该被审的变化）；
> 实现方必须把"确定性"再守一遍（排序 + 去重 + 身份记法）。
>
> **本 ADR 只裁决，不实现**——实现是 v0.73 的交付（用户可见增量），本版只把形状定死，
> 让实现不再需要临场发明。

---

## 1. 背景：三个事实

1. **`ngm why` 今天要跑完整图解析**（`cmd/ngm/why.go` 在读了 lock 之后仍然调用
   `resolve.ResolveGraph`）——为了回答"它从哪来"，要付出 EnsureMirror / ls-remote
   等一串 git 子进程，且离线时直接不可用。而"它从哪来"的答案**就躺在解析结果里**，
   只是从来没被存下来。
2. **SBOM 缺依赖关系段**（v0.69 复盘 §2 未达成项）：CycloneDX 的 `dependencies` 与
   SPDX 的 `RELATIONSHIPS` 才是"依赖图形"的标准表达；扁平清单在合规读者眼里
   是"物料列表"，不是"依赖树"。
3. **图在解析时是现成的**：`resolve.ResolveGraph` 的 `Node.RequiredBy` 就是
   直接引用者集合（v0.68 的菱形测试钉过它：两个直接引用者都要在）。写入是免费的，
   不写才是刻意的浪费。

同时，评估报告的**判断前提**（每 3 版至少 1 版用户可见增量）与
**停止清单**（停止新建契约网 / 停止补发历史版本）意味着：剩下的能力增量必须
来自**产品本身**，而这是清单上最后一个大项。

## 2. 决策

### 2.1 形状：每依赖一个 `requiredBy`，记录**直接**引用者

```json
{
  "name": "github:b/util",
  "ref": "v1.0.0",
  "refType": "tag",
  "commit": "abc123…",
  "archiveDigest": "sha256:…",
  "resolvedAt": "2026-10-09T00:00:00Z",
  "vendorPath": "github.com/b/util",
  "requiredBy": ["github:a/app"]
}
```

逐条裁决：

| 问题 | 决定 | 理由 |
|------|------|------|
| 存全路径还是直接边 | **只存直接边** | 全路径是指数的（ADR-024 的教训：41 个节点 → 100 万条路径）；直接边可重构全部路径，而 SBOM 与 `why` 要的都只是直接边 |
| 字段位置 | 依赖条目内（与 `subPath` 同级） | lock 的哲学是扁平清单、条目自足；顶层 `graph: [{from,to}]` 会把"一个依赖的来源"拆到文件另一头，diff 与手读都变差 |
| 直接依赖怎么表示 | 引用者里带**根标记** `(root)`（见下文修订），且**字段恒在**（不用 `omitempty`） | 实现时才发现：一个依赖可以同时是"根声明"与"被上游引用"（root wins 的情形，见 `mergeIntoExisting`）。若只存依赖引用者、把根边丢掉，`RootDeclared` 就丢了——`why` 会少列"根 → 它"那条路径。**空数组/`[]`/缺失一律表示"来源未知"** |
| 引用者的身份记法 | 与 lock 条目同构：`name`，带子路径时 `name#subPath` | 与 purl 的 subpath 段同一套记法（v0.69 已在 SBOM/attestation 里用过），不发明新语法 |
| 顺序 | 按 (slug, subPath) 字节序排序、去重 | lock 的一切序列化都必须确定（同一解析 ⇒ 同一字节） |
| lockfileVersion | **不动**（仍 `1.0.0`） | locking.md 的规则："同 MAJOR 内只允许新增可选字段"；lock 解码器**忽略未知字段**（`io.go` 的契约注释），旧版 ngm 读新 lock 不受影响——`subPath` 字段已有同款先例 |
| Validate 规则 | `requiredBy` 引用的身份必须**存在于本 lock 的依赖集** | 幽灵边（手改 lock 指向不存在的条目）要让 `ngm config validate` 之前的每一次 lock 读取就红——lock 是唯一事实源，事实源里不许有悬空引用 |

### 2.1b 修订（v0.73 实现时）

**根要一起存进去。** 裁决时写的是"直接依赖 = `[]`"，实现时这条站不住：
一个依赖可以同时是**根声明**与**被上游引用**（root wins 的情形，
`mergeIntoExisting` 正是这么记的）。只存依赖引用者会把 `RootDeclared` 弄丢，
`observability.FindPaths` 就不再从它出发——`why` 会少列"根 → 它"那条路径。
因此：

- `requiredBy` 记**解析层给的直接引用者全集**，根声明处就是 `(root)`
  （与 `resolve.RootMarker` 同值；lock 里另立 `RootRequirer` 并有判据钉住同值）；
- `RootDeclared` 由"requiredBy 含根标记"派生，**不单列字段**；
- `[]` / 缺失 / `null` **一律读作"来源未知"**（解码后都是"没有引用者"），
  消费端据此降级——于是"恒在"仍然是有意义的：它保证新写的 lock 永远说得清来源。

### 2.2 消费端：谁读它

| 消费者 | 行为 |
|--------|------|
| `ngm why` | **lock 里有图 ⇒ 0 次 git 子进程、离线可用**（路径由直接边重构，沿用 ADR-024 的 64 条上界）；**没有图（旧 lock）⇒ 保持现行为**（重解析），并在输出里注明来源是"重新解析"还是"lock" |
| `ngm sbom` | CycloneDX 增加 `dependencies` 段（根组件 = `ngm.json` 的 name，边来自空/非空 `requiredBy`）；SPDX 增加 `RELATIONSHIPS`（`DEPENDS_ON`）。旧 lock 无图 ⇒ 两段省略，不假装 |
| `ngm attest` | **不变**。语句是"内容 ↔ commit ↔ 输入"的绑定；多层 material 留给图形字段真正落地后再评估，不预支 |
| `ngm verify` | **不变**。verify 管内容一致性；图是解析层的事，混进来只会让 verify 的语义变糊 |
| `ngm tree` | **不变**。漂移标记必须读上游（这是 tree 的本职），图的基线形状对它没有增量 |

### 2.3 写入端：谁写它

`ngm install` 与 `ngm update`——它们在解析完成的那一刻手里就有
`Node.RequiredBy`（直接引用者集合），序列化即可。**`ngm add` 不单独写**：
它内部走同一条安装路径。

## 3. 代价（诚实记）

1. **lock 变大**：每依赖一行。一个 20 依赖的项目多 20 行——换来的是
   `why` 离线化与合规图段，账算得过来。
2. **diff 噪声**：依赖关系变化时，lock 的 diff 不再只有"改了哪个 commit"，
   还有"谁引用了它"。**这不是噪声，这是该被 code review 看见的变化**——
   把它挡在 diff 外面才是错的。
3. **确定性要多守一处**：排序、去重、身份记法——三处都是"同一解析 ⇒ 同一字节"的
   新增义务，实现时必须有 golden 钉住（v0.73 的验收项）。
4. **两套数据源并存的过渡期**：旧 lock（无图）与新 lock（有图）会共存到所有人
   重新 `install` 为止。所有消费端必须**显式**处理"图未知"分支并说出自己用了
   哪个来源——静默回退是本项目最忌讳的失败形态。

## 4. 停止面（本 ADR 同时决定不做什么）

- **不存全路径 / 历史路径**：指数膨胀（ADR-024）；`why` 的路径由直接边重构。
- **不存版本约束 / semver**：ngm 没有 semver 概念（refType + commit 就是全部）。
- **不让 verify 消费图**：内容一致性与解析图是两件事，混在一起会让两边的
  失败都更难解释。
- **不做 lock 的"图迁移工具"**：重新 `ngm install` 即得新格式；写迁移器
  是在给一个一次性动作造永久代码。

## 5. 实现条件（v0.73 的验收预告）

1. lock 字段 + `Validate` 的幽灵边检查 + golden 更新（`TestMarshal_GoldenFormat`
   会红一次，属预期，按流程在提交说明里写明）。
2. install / update 写入（含确定性 golden）。
3. `why` 离线化 + "来源：lock / 来源：重新解析"的输出区分 + 旧 lock 回归。
4. SBOM 的 `dependencies` / `RELATIONSHIPS` + 旧 lock 省略分支。
5. 判据：`why --offline` 在有图 lock 上 **0 次 git spawn**（计数器，不是秒表）；
   SBOM 输出被一个真实 fixture 的 golden 钉住。

任一条不成立时，回本 ADR 修订，而不是在代码里临时发明。

---

## 相关文档

- [ADR-004 声明用 refType、锁定用 commit](./adr-004-reftype.md)
- [ADR-024 解释输出必须有界](./adr-024-bounded-explanations.md)（"不存全路径"的依据）
- [locking.md](../architecture/locking.md)（lockfileVersion 演进规则与 resolvedAt 的语义）
- [v0.69 复盘](../development/v0.69-retrospective.md)（SBOM `dependencies` 段的未达成项）
- [评估报告 2026-10-09](../internals/assessment-2026-10-09.md)（判断前提与停止清单）
