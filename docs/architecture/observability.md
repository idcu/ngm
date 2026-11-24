# 可观测性

> **结论**：所有机器可读的"结果"都来自同一份判定——退出码是它的标量，`--json` 是它的结构，
> 人类文本只是它的渲染。判定先算出来，再决定怎么送出去；送不出去本身是一类失败，不是成功。
> 契约出处只有一处：下面的[退出码约定](#退出码约定全局)；各命令的用法文本从它取上界。

---

## 数据流

```
lock + ngm.json + 本地 mirror/content store
        │
        ▼
  判定层（ref 漂移 / binding / digest 重放 / 落地 / OSV / 策略）
        │  → 一份结果：driftKind + checks[] + summary.exitCode
        ├──────────────► 退出码 0..6（进程状态，CI 唯一该读的东西）
        ├──────────────► --json / --format=sarif|github（机器通道，同一份判定）
        └──────────────► 人类文本（→ 行动行照失败的性质给建议）
```

**状态**：判定是纯函数式的（给同样输入给同样结论）；`--offline` 只读本地镜像与缓存，不触网；
"没问到"与"问到了、没问题"是两种状态，前者记为 `operational: true`，绝不冒充通过。

**边界**：渲染不新增判定。`sarif` / `github` 是别人的 schema，`--json` 是 ngm 自己的 schema；
两者同时给出是配置错误（exit 3）。没有产出报告时 stdout 为空，错误走 stderr。

---

## 退出码约定（全局）

所有命令共用一套退出码（与 `internal/core/errs/errs.go` 的 `Code*` 一一对应）：

| 退出码 | 语义 | 典型来源 |
|--------|------|---------|
| `0` | 成功（verify 的"仅预期更新"也归此） | 所有命令判定通过；branch 快进、签名未发现等 |
| `1` | 策略失败 | **策略漂移**：verify 非预期漂移（tag 重打 / 分支历史改写 / 声明的 ref 消失；`why` / `outdated` 走同一出口）；**引擎运行失败**：adapter 引擎跑起来了但自己失败（引擎原始退出码保留在消息与 `--json`，不透传）；**漏洞超阈值**：audit 发现超阈值漏洞、`tree --osv` 查到漏洞；**审计钩子否决或超时**（`--hook`）。五类的登记由 `TestV57ExitCodeSourcesAreRegistered` 从源码派生双向核对，每个站点都要落在这里 |
| `2` | 完整性失败 | digest 重放不匹配；模块 `subtreeOid` 绑定不符；`--require-signed` 下依赖不是由你信任的密钥签名；`--sandbox` 自检失败或超时 |
| `3` | 配置/策略/lock 错误 | schema 非法、lock 损坏、frozen 与声明不一致、权限未授予、引擎未声明、缺少 Deno 之外的配置 |
| `4` | Git/网络失败 | fetch / ls-remote 失败；`--offline` 下资源缺失；OSV 查询失败且无缓存 |
| `5` | 引擎不可用 | adapter 找不到外部引擎；已声明但引擎没装或干不了该能力 |
| `6` | 内部失败 | **内部失败**：panic、"结论有了却送不出去"（写 stdout / JSON 编码 / 读 stdin 失败）、错误没走 ngm 错误模型而落到兜底。与策略失败分开：策略漂移要人看，写不出去重跑可能就好（见 ADR 汇总表 ADR-026） |

**合并语义**：两层判据同时给码时取"最严重者"，不是后写者赢——优先级
`2 > 3 > 5 > 4 > 6 > 1 > 0`。完整性失败（2）压过环境类失败，CI 才不会把"依赖被篡改"读成"机器上没装工具"。

**失败语义的几条硬规矩**：

- 码 2 禁止构建，`--allow-drift` 对它无效；码 1 默认阻断，可用 `--allow-drift` 降级为 0。
- `0` 不等于"一切正常"：branch 前进（预期更新）也是 0，要严格拦下需 `--strict`。
- `ngm run` 是唯一例外：项目脚本自己的退出码原样透传（信号终止为 `128 + signal`），不映射到 0–6。
- `ngm doctor` 是诊断型命令：结论写在报告里，命令本身恒退 0（用法错误退 3）。

**错误码枚举**（与 `internal/core/errs/errs.go` 名字和顺序一致；`iota + 1` 从 1 起）：

```go
const (
    ErrRefDrift ErrorCode = iota + 1 // 1 策略失败
    ErrDigestMismatch                // 2 完整性失败
    ErrConfigInvalid                 // 3 配置/策略/lock 错误
    ErrGitFetch                      // 4 Git/网络失败
    ErrEngineNotFound                // 5 引擎不可用
    ErrInternal                      // 6 内部失败（panic / 结论有了却送不出去）
)
```

---

## ngm verify

**用途**：检查 ref 是否仍指向锁定 commit、digest 是否可本地重放一致、落地内容是否完好。

**检查层**（每一项都记录，"没做"记为 `status: fail` + `operational: true`，不让数组变短）：

| 检查 | 内容 | 默认 |
|------|------|------|
| ref → commit | 在线读远端 ref 广播（`ls-remote`）；`--offline` 用本地 mirror 快照并标 stale | 是 |
| binding | 模块条目的 `commit:subPath` tree OID 对比 `subtreeOid` | 是（本地可离线） |
| digest 重放 | 根条目重放 `archiveDigest`；模块条目重放 `moduleDigest` | 是（本地可离线） |
| 落地完整性 | vendor 与 content store 的存在性/链接校验 | 是 |
| 逐文件哈希 | `--content`（旧别名 `--deep`）追加全量字节比对 | 否 |
| 签名 | `--signatures` 追加报告锁定 commit 的 Git 签名状态；`--require-signed` 强制 | 否 |

**"预期更新" vs "非预期漂移"**：两者外部现象相同（ref 指向别的 commit），区别在新旧 commit 的祖先关系，
用 `git merge-base --is-ancestor` 判定。快进 → `expected`（退 0，`--strict` 时退 1）；非快进 / tag 重打 /
ref 消失 → `unexpected`（退 1）；digest 重放不匹配 → `critical`（退 2，`--allow-drift` 不降级）；
问不到（网络失败 / 权限拒绝 / mirror 缺失）→ 不是结论，`operational: true`，退 4。

**边界**：无法证明快进时按 `unexpected` 处理——保守方向，不是遗漏；"没问到"不冒充"漂移"（见 ADR 汇总表 ADR-039、ADR-053）。
在线路径只在 ref 已变或 lock 的 commit 不在本地时才 fetch，"什么都没变"的常见路径不传输对象（见 ADR 汇总表 ADR-010）。

**输出**：

```
$ ngm verify

✓ github:org/A@v2.0.0 (tag) → fedcba9 — match
⚠ github:org/logger@main (branch) → def456a (now 789xyz0) — expected update
  → driftKind: expected; expected update, not blocking; run `ngm update github:org/logger`
✗ github:org/legacy@v0.9.0 (tag) → 123abc4 — integrity failure

verified 4 dependency(ies): 2 ok, 1 expected, 1 critical (exit 2)
```

每一类失败都有一行以 `→` 开头的建议，且建议承认失败的性质：漂移指向 `ngm update`、
字节被改指向 `ngm install`、漏洞指向 `ngm audit`；"检查未能完成"没有可分类的漂移时只写建议，
并原样透传最贴近成因的那一层（权限层知道该加哪个 `net:` host，报告不替它猜）。
CI 只需读 `driftKind` 与 `summary.exitCode`，不解析人类文本。

**机器出口**：`--json` 是 ngm 自己的 schema；`--format=sarif|github` 走外部 schema，渲染不引入新判定
（级别跟着阻断语义走、不编造行号、无时间戳与随机 id，两次渲染逐字节相同）。

---

## ngm why

**用途**：回答"我项目里为什么有这个依赖"。同一依赖被多个父节点引入时列出全部路径。
依赖图写在 lock 里（见 ADR 汇总表 ADR-028），`why` 读 lock 即可回答、不启动 git 子进程；
旧 lock 没有图时退回重新解析，并明说"重新解析上游清单"。

**输出**：

```
github:snap/leaf@v1 (tag) → <sha>

直接依赖：否
传递依赖：1 条路径
  ngm.json → github:snap/parent@v1 → github:snap/leaf@v1

锁定：<sha> (<time>)
archiveDigest: sha256:7f69fa5497bee3a6342b807b9b65014422143ea7ecc65e69526f6fa5f0c96f24

来源：ngm.lock（未重新解析：0 次 git 子进程）
```

**上界**：默认最多枚举 64 条路径；达到上界时明写"已列出 64 条…用 `--all` 展开"，
`--json` 对应 `pathsTruncated: true` / `pathsLimit: 64`。路径数在"宽"图里是指数的，而图的形状来自上游清单
（见 ADR 汇总表 ADR-024）。

---

## ngm tree

**用途**：依赖树可视化。`⚠` 标记漂移，`↺` 标记环（`cycle, not expanded`），`…` 标记展开预算用尽。
`✗`（已知漏洞）需显式 `--osv`——"打印一棵树"不该顺手触网；不带它时报告明写"未查询漏洞数据"。

**输出**：

```
github.com:my-org/app
github:snap/parent@v1 (tag) → <sha>
│   github:snap/leaf@v1 (tag) → <sha>

source: ngm.lock (requiredBy graph; no re-resolution)
vulnerability data not consulted; run `ngm audit` (or `ngm tree --osv`)
```

**上界**：默认最多展开 4096 个条目；达到时末尾明写"树不完整；用 `--all` 展开"，
`--json` 对应 `entriesTruncated` / `entriesLimit`。树按路径展开，条目数与 why 的路径数同量级（见 ADR 汇总表 ADR-024）。

---

## ngm outdated

**用途**：检查哪些依赖有新版本。

**硬纪律**：查不到就报 `unknown`，绝不写"已是最新"。"不知道"与"查过，确实没有"是两种结论；
`--offline` 下的 branch 依赖是 `unknown` 而不是 `no`，且离线不退化成一次 clone。

**实现**：tag 依赖只读本地 mirror 的 tag 列表（不依赖 Git host API，见 ADR 汇总表 ADR-002）；
模块条目只在该模块的 tag 前缀里选最新；branch 需要 fetch 才知道最新 tip；commit 无更新。

---

## ngm audit

**用途**：按 lock 中的 commit 查询 OSV.dev 的已知漏洞（不是按版本——"版本没变、commit 变了"正是投毒常见形态）。

**失败语义**：发现超阈值漏洞 → exit 1；配置/策略错误 → exit 3；OSV 网络失败且无缓存 → exit 4。
每条发现项都必须有一行"接下来做什么"：公告有修复版本就写 `Fixed in: <版本>`；没有就给两条真实的路
（读公告，或**明确**把该严重度记进 `supplyChain.osvIgnoreSeverities`——是"记录下来"，仍计入 ignored 条数）。
"审计通过"只表示库里没有关于这个 commit 的记录，不等于安全。详见[供应链防护](./supply-chain.md)。

---

## ngm doctor

**诊断型命令**：只回答"这台机器、这个目录现在能不能正常干活"。发现写进报告，命令恒退 0（用法错误退 3）。

| 检查 | 读数 | 什么时候值得看 |
|------|------|----------------|
| `git` | 可执行文件与版本 | 版本过低或不在 PATH，后续失败都会像网络问题 |
| `project` | `ngm.json` 的 name / version | 读不出来 ⇒ 其余每项都不可信 |
| `lock` | 依赖条数 | 缺 lock 是 warn：还没装过，不是坏了 |
| `layout` / `store` | `~/.ngm` 是否存在 / 三层仓库数 | install 会创建；条数与 `ngm store usage` 同口径 |
| `vendor` | `present` / `ignored-by-git` / `absent` | 见下 |
| `modules` | 嵌套模块条数 | 集合仓库 `modules validate` 前的快速体检 |

**vendor 读数只是读数**：随仓库分发（必须提交）与由 `.gitignore` 排除（CI 现场装）都合法；ngm 不替用户选，
也不执行任何 Git 写操作（见 ADR 汇总表 ADR-038）。错的只有"打算分发、却被 Git 忽略"——它的表现极具误导性
（冷克隆 CI 在 `install --frozen-lockfile --offline` 处 exit 4，报告只说"缺内容"），所以 doctor 用 `warn` 说出来。

---

## 诚实说明

1. `outdated` 的 branch 需要 fetch；离线时报 `unknown`，不是"已是最新"
2. `audit` 依赖 OSV.dev：零日不在库中；多数公告按 semver 而非 commit 记录
3. 签名检查里**未签名不是失败**（多数依赖没签名，全报红会让真警报失效）；判定完全交给用户自己的 Git 密钥配置，
   ngm 不管理密钥。签名证明"谁说的"，不证明"说的是真的"（见 ADR 汇总表 ADR-014）
4. verify 在线只读一次远端 ref 广播，对象按需才取；`--offline` 用本地 mirror 快照（缺失 exit 4），digest 重放完全本地
5. `ngm run` 透传脚本退出码，不参与 0–6 与 `retryable` 判定

## 相关文档

- [架构总览](./overview.md)
- [锁定机制](./locking.md)
- [供应链防护](./supply-chain.md)
- [ADR 汇总表](../adr/README.md)
