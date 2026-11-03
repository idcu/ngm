# 工程纪律方法论 · 附录

> **这份文件是什么**：`engineering-discipline.md` 的 §9 模板与两份附。
> 模板是**抄写用**的（契约文件、单项检查、CI、删除记录），附是**证据**（四个样本的成熟度对照、
> 与 ngm 的关系）——它们与正文的读者节奏不同：正文要一口气读完，附录是"真要动手时翻一眼"。
> **节号沿用正文**（§9 / 附）。正文见 [engineering-discipline.md](engineering-discipline.md)。

## 9. 模板

### 9.1 契约文件骨架

```json
{
  "schemaVersion": 1,
  "为什么": "一句话说清违反的代价",
  "规则": {
    "写者": { "allowed": ["modules/x/repo.js"], "baseline": [], "due_date": "2026-12-31" }
  },
  "ignore_files": ["test/fixtures/**"]
}
```

**三条纪律**：`why` 必填（半年后你自己也需要它）；`baseline` 只降不升，
到期日到了判据必须红；契约里不留"待定"。

### 9.2 单项检查骨架

```js
// check-<one-thing>.js —— 只回答一个问题，输出一行读数
const violations = scan();           // 返回 [{file, detail}]
console.log(`${readings.checked} checked, ${violations.length} violation(s)`);
if (readings.checked === 0) fail('扫到 0 个对象——判据空跑比漏报更糟');
if (violations.length) { violations.forEach(v => console.error(' - ' + v.file + ': ' + v.detail)); process.exit(1); }
```

### 9.3 CI 骨架

```yaml
jobs:
  check:      # 单项/总闸门：与本地同一条命令
    steps: [npm ci || true, npm run verify]
  deps:       # 依赖可复现：离线 + frozen
    steps: [npm run deps:install -- --frozen-lockfile --offline, npm run deps:verify]
  # 跑不了的 job 也要存在：用 notice 说明为什么跳过，而不是删掉它
```

### 9.4 结构性删除记录模板

```text
删除：<路径/符号>
日期：YYYY-MM-DD   批准：<人>
为什么：<一句话>
影响：<哪些引用已改；哪条判据的假设被打破，如何处理>
验证：<删除后跑过哪条命令，结论是什么>
```

---

## 附：四个样本的成熟度对照

四个真实仓库，**刻意处于不同阶段**——照抄最高那行是错的，先找到与自己规模最像的那行。

| 项 | 枢衡（企业运营平台） | PlotPin（离线标注 PWA） | 淬题（题库 / 间隔重复） | ngm-repo（模块集合） |
|---|---|---|---|---|
| 级别 | **L4** | L3 | L3 | L3 |
| 依赖 | 9 个 Git 模块，全部按 tag 锁定 | 10 个，`linkMode: copy` | 6 个 | 自身是包仓库 |
| 契约 | `contracts/` 9 份（表所有权、域边界、FK、字段单一源、查询预算、品牌、兼容计划、读写白名单） | `contracts/conventions.json`（版本号 / i18n 键 / 文件头 / 枚举同源） | `js/data/contracts.js`（运行时断言）+ `ngm.json` + DEVELOPMENT.md §0 的版本读数表 | `ngm.json` + `ngm.collection.json` |
| 模块化装配 | **L4**：`module.json` × 14（id/requires/entry/tables/routePrefixes/permissions/ui 插槽）+ `config/modules.json` 装配清单；拓扑排序、路由冲突即失败、热启停只反注册、失败回滚 | **L2**：`contracts/plugin-api.json` 守 `PlotPin` 的 13 个键（双向比对：泄漏与落空都报） | **L3**：81 个 `<script>` 的层顺序（入口最后 / 配置最先 / 无孤儿） | —（包仓库只做清单与 tag 校验） |
| 单项判据 | 23 个 `check-*.js` + 1 个 `.mjs`（文档、契约巡检、所有权、字段、预算、兼容…） | `check.mjs`（语法 / 资源双向 / 读数回写）+ `check-contracts.mjs` + `check-plugin-api.mjs` | 25 个测试套件 + 3 个检查脚本 + `docs-links.mjs` + `check-modules.mjs` | `check-collection.mjs`（7 项）+ `ngm-validate.sh` |
| 总闸门 | `npm run verify` | `npm run ci`（= `check.mjs`） | `npm run verify`（本地与 CI 同一条） | 两个脚本并行（`check-collection` + `ngm-validate`） |
| 判据的牙齿 | 每条 `check-*` 都配过负例 | `contracts:selftest` 造一棵必然违约的临时树 | `docs-check` 的 live/static 双模式互为对照 | 三条新判据落地时各红过一次（见下） |
| CI | 4 job，含"跑不了就明说跳过" | 3 job + 可选的 `verify --deep` | 5 job（比本地全） | 3 job + 发布声明"CI 不发版" |
| 文档分层 | 入口 / 权威 / 过程三分，过程文档不入库 | 入口 README + 权威 `docs/engineering.md`（含"故意不设门禁"清单） | `docs/` 18 篇，分「权威层」与「过程快照」两节索引 | README + 每包 README |
| 落地时抓到的真实漂移 | 一条判据"存在但从不跑"（收口时没跑它）；`compat-plan.json` 写了两层到期日，却**没有任何东西会在过期时变红** | `io.js`/`ui-panels.js` 各两行互相矛盾的文件头；枚举提取漏掉数组最后一项 | 权威文档写"去重键 v2"而实现是 v3；一处 `../js/sw.js` 死链 | README 写的 `.ngm-version` 落后一个小版本；`xlsx-write` 的依赖声明与清单冲突 |
| 一条可带走的教训 | **判据的价值 = 写了没有 × 什么时候跑** | **读数回写**（把断言数写回 README）让文档不再人手同步 | **本地与 CI 不同源，绿也说明不了什么** | **硬编码的计数是"下一个新增"的地雷** |

**读法**：枢衡那一列不是"标准答案"，它的成本只有在"9 域 + 真实工资数据 + 单人快节奏"这个组合下才划算；
PlotPin 那一列证明**小项目也能有纪律**，代价是一份契约 JSON 加两个脚本（约 300 行）；
淬题那一列是"纪律不均衡"的样本，它的修法是**把本地与 CI 收成同一条命令**，而不是再加十条判据；
ngm-repo 那一列是"生产者端"的样本——它守的不是运行时，而是**发布出去的东西能不能被消费**。

**四列的共同点**：新判据落地时**都当场抓到过真实漂移**（最后一列第二行）。
这不是巧合：一条判据写得出来、却一次都没红过，多半是它检查的东西根本不会漂，
或者它查得太浅。**先让它红一次，再谈它有用。**

---

## 附：与 ngm 的关系（依赖字节层的同一件事）

本方法论解决"**代码与结构**的可核对"，ngm 解决"**依赖字节**的可核对"。两者是同一件事在不同层的实例化：

| 方法论条目 | 通用形态 | ngm 的对应能力 |
|---|---|---|
| §3.1 契约先行 | 机器可读的边界文件 + `check-*` | `ngm.json` 声明 `(name, ref, refType)`，非法即 exit 3 |
| §3.2 单一来源 | spec → 生成物 → 校验 | `ngm.lock` 是唯一事实源，必须提交 |
| §3.2 生成物可校验 | 比对"源生成的"与"仓库里存的" | `ngm verify` 重放 digest；`--content` 逐字节比对 |
| §3.4 门禁分层 | 单项 → 总闸门 → CI job | `verify --json` / `--format=github\|sarif` 供 CI 消费 |
| §3.6 可回滚 | 备份 → 重建 → 等价性判据 | `install --frozen-lockfile --offline` 只重放已钉住的内容 |
| §3.8 留痕 | 事故记录 + 兼容计划到期 | `ngm sbom` / `ngm attest` 从 lock 导出可审计产物 |
| §3.3 生成物策略 | 判据："删掉它能否重建" | `vendor.mode=local` 时 `ngm.vendor/` 随仓库分发，提交与否由 Git 侧决定 |
| §3.3 层下游 | 二次分发后哪些保证仍成立 | [vendor 4 层 · 层的下游](../architecture/vendor-layers.md#层的下游二次分发与证明边界) |

**一条来自本仓库的读数**：`ngm doctor` 会报告 vendor 树的 Git 可见性
（`present` / `ignored-by-git` / `absent`）。它**只报告、不判定**——因为"vendor 是否提交"
是部署形态的选择，不是对错；错的只有"打算分发、却被 Git 忽略"这一种**意图与状态错配**，
而它会让冷克隆的 CI 在 `install --offline` 处以 exit 4 失败，错误信息却指向"缺内容"。
判据的软硬是语境的属性：这里选**可见**，不选**失败**。
