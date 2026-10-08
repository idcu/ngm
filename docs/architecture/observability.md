# 可观测性

## 核心命令

| 命令 | 作用 | 成熟度 | 退出码 |
|------|------|--------|--------|
| `ngm verify` | ref 漂移 + digest 重放检查（`--sandbox` 追加自检、`--signatures` 追加签名报告） | **done (v0.1)**；`--sandbox` v0.3、`--signatures` v0.4 | 0/1/2/3/4（`--sandbox` 缺 Deno 时 5） |
| `ngm why <dep>` | 为什么装了这个依赖 | **done (v0.2)** | 见下节 |
| `ngm tree` | 依赖树可视化 | **done (v0.2)** | 见下节 |
| `ngm outdated` | 哪些依赖有新版本 | **done (v0.2)** | 见下节 |
| `ngm audit` | 已知漏洞扫描（OSV.dev） | **done (v0.2)**；`--hook` v0.3 | 0/1/3/4 |

> 这五条命令**都已实现**，各节描述的都是已实现行为。本表此前标着 "planned" 与
> "当前 exit 3"——那是 v0.1 时代的写法，v0.2 交付后没有跟着改。
> 「文档说没做、实际做了」与反向的错误同样有害：它会让读者绕过一条可用的命令。

---

## 退出码约定（全局）

所有命令共用一套退出码（与 P0 的 `ErrorCode` 对齐）：

| 退出码 | 语义 | 典型来源 |
|--------|------|---------|
| `0` | 成功（verify 的"仅预期更新"也归此） | 所有命令 |
| `1` | 策略失败 | **策略漂移**：verify 非预期漂移（`expected` 之外都算；`why` / `outdated` 走同一出口）；**引擎运行失败**：adapter 引擎跑起来了但失败（引擎自己的退出码保留在消息与 `--json` 里，不透传）；**漏洞超阈值**：audit 存在超阈值漏洞、`tree --osv` 查到漏洞；**审计钩子否决或超时**（`--hook`）。来源分类由 `TestV57ExitCodeSourcesAreRegistered` 从源码派生核对（v0.57 立、v0.59 扩到两个码） |
| `2` | 完整性失败 | verify digest 重放不匹配 |
| `3` | 配置/策略/lock 错误 | schema 非法、lock 损坏、frozen 与声明不一致 |
| `4` | Git/网络失败 | fetch / ls-remote 失败；`--offline` 下资源缺失 |
| `5` | 引擎不可用 | adapter 找不到外部引擎 |
| `6` | 内部失败 | **内部失败**的具体来源：`runWithRecovery` 捕获的 panic；"结论有了却送不出去"——写 stdout 失败 / JSON 编码失败 / 读 stdin 失败（原先它们与策略失败共用码 1）；以及够到 `errs.ExitCode` 兜底的、**没走错误模型**的错误。**与策略失败分开的理由**：策略漂移要人看，写不出去重跑可能就好——CI 需要用一个数字区分这两种事（[ADR-026](../adr/adr-026-exit-code-6-internal-failure.md)）。分类同样由 `TestV57ExitCodeSourcesAreRegistered` 从源码派生核对 |

`ngm verify --json` 输出的 `driftKind`（`expected` / `unexpected` / `critical`）用于区分"预期更新"与"非预期漂移"。

---

## ngm why

> **done (v0.2)**。下面的输出即当前真实行为。同一依赖被多个父节点引入时会列出
> **全部**路径——"为什么装了它"常常不止一个答案。

### 用途

回答："我项目里为什么有这个依赖？"

### 输出

```
github:snap/leaf@v1 (tag) → <sha>

直接依赖：否
传递依赖：1 条路径
  ngm.json → github:snap/parent@v1 → github:snap/leaf@v1

锁定：<sha> (<time>)
archiveDigest: sha256:<digest>
```

> **这一段就是快照**（`cmd/ngm/testdata/why.golden`）：它由 `TestV02ObservabilityGolden`
> 生成，并由 `TestV17DocOutputExamplesMatchTheGoldens` 与本页**逐行对照**。
> 改输出就要同时改这两处。本页此前画的是**渲染器从不产生的形状**（树形方块 + `├──`），
> 而本页开头又声明"输出即当前真实行为"——两张网里，是快照说了算。

**上界（v0.17，[ADR-024](../adr/adr-024-bounded-explanations.md)）**：
默认最多枚举 **64** 条路径。达到上界时那一行变成

```
传递依赖：已列出 64 条（枚举达到上限 64，还有更多未列出；用 --all 展开全部）
```

`--json` 里对应 `pathsTruncated: true` 与 `pathsLimit: 64`；`--all` 解除上界。
**为什么**：路径数在图"宽"时是指数的（实测 41 个节点 → 1 048 576 条路径 / 626 MB），
而图的形状来自**上游的 `ngm.json`**——不是本项目的输入。

---

## ngm tree

> **done (v0.2)**。`⚠`（漂移）默认就有；`✗`（已知漏洞）需要显式 `--osv`。
>
> 这条是对原设计的**刻意偏离**：让"打印一棵树"顺手发起网络查询不合适。
> 不带 `--osv` 时报告会明写"未查询漏洞数据"——把"没查"说成"查过没事"
> 是这类工具最容易骗人的地方。

### 输出

```
github.com:my-org/app
github:snap/parent@v1 (tag) → <sha>
│   github:snap/leaf@v1 (tag) → <sha>
vulnerability data not consulted; run `ngm audit` (or `ngm tree --osv`)
```

> 与 why 一样，**这一段就是快照**（`cmd/ngm/testdata/tree.golden`），
> 由 `TestV17DocOutputExamplesMatchTheGoldens` 逐行对照。
>
> `--osv` 查到漏洞时（退出码 1），树末追加一行以 `→` 开头的**下一步**：
> 树只画 `✗` 与**条数**，而"哪条公告、严重度多少、修复在哪个版本"在 `ngm audit` 的报告里——
> 不指过去，用户只被告知"有事"，不知道"做什么"（v0.32 修）。
> 层级由**缩进**体现（子节点前缀 `│   `）——本页此前画的是 `├──`/`└──` 方框，
> 那是渲染器从不产生的形状。

`⚠` 标记有漂移的 ref；`↺` 标记环（`cycle, not expanded`）；
`…` 标记展开预算在此用尽（见下）。

**上界（v0.17，[ADR-024](../adr/adr-024-bounded-explanations.md)）**：
默认最多展开 **4096** 个条目。达到时报告末尾写明

```
（展开达到上限 4096，树不完整；用 --all 展开全部）
```

停止处的条目带 `… 此处达到展开上限（还有子节点未列出）`；`--json` 里对应
`entriesTruncated` / `entriesLimit`（以及该条目上的 `truncated`）。`--all` 解除上界。
**为什么**：树是**按路径展开**的，条目数与 why 的路径数是同一个量级（同一实测），
而它还要把每条路径渲染出来。

---

## ngm outdated

> **done (v0.2)**。一条硬纪律：**查不到就报 `unknown`，绝不写"已是最新"**。
> 因此 `--offline` 下的 branch 依赖是 `unknown` 而不是 `no`——"不知道"
> 与"查过，确实没有"是两种结论，报告刻意把它们分开。
>
> **v0.12 起**，`unknown` 的行下会打印**原因**（此前原因只存在于 `--json` 的 `note` 字段里，
> 于是默认输出只告诉用户"我不知道"、不告诉他该去修什么）；同时 `--offline` **不再触网**：
> 冷 mirror 时报"没有本地镜像"，而不是退化成一次 clone（那样既违反 `--offline` 的契约，
> 又会把报错引向"mirror 不可用"这个错误的方向）。

### 用途

检查哪些依赖有新版本可用。

### 实现

- tag refType：列举**本地 mirror** 中的 tag，取最新（semver 优先，无可解析 semver 时按 tag 创建时间）。
  **不依赖 Git host API**——ngm 是 Git 直连（[ADR-002](../adr/adr-002-git-direct.md)），
  走 host API 会引入 rate limit 与平台差异，而 mirror 里的 tag 列表就是权威
- branch refType：需要 fetch 才能知道最新 tip，并与 lock 对比、给出落后提交数
- commit refType：无更新（已锁定）

### 输出

```
$ ngm outdated

Package                Current    Latest     Type   Drift
github:org/A          v2.0.0     v2.1.0     tag    yes
github:org/utils      v1.2.3     v1.3.0     tag    yes
github:org/logger     main       main       branch yes (3 commits)
github:org/legacy     abc123d    -          commit no
github:org/offline    v1.0.0     -          tag    unknown
  → --offline: no local mirror for github.com/org/offline
```

---

## ngm audit

### 用途

已知漏洞扫描（OSV.dev）。

### 输出

```
$ ngm audit

✗ github:org/utils@v1.2.3 (tag) → abc123def
  HIGH: Prototype pollution (GHSA-xxxx-xxxx)
  Fixed in: v1.2.4

⚠ github:org/logger@main (branch) → def456abc
  MEDIUM: ReDoS in parser (GHSA-yyyy-yyyy-yyyy)
  Fixed in: v2.0.1
  → no fixed version is recorded for this advisory: read https://osv.dev/vulnerability/GHSA-yyyy-yyyy-yyyy, or record the acceptance via supplyChain.osvIgnoreSeverities in ngm.json
```

**每一条发现项都要有一行"接下来做什么"**（v0.32 起）：

- 公告记录了修复版本 ⇒ `Fixed in: <版本>`
- 公告**没有**记录修复版本 ⇒ 以 `→` 开头的一行，给出两条真实的路：读公告，或**明确**把该严重度记进
  `supplyChain.osvIgnoreSeverities`（不是"忽略"，是"记录下来"——它仍会被计入报告里的 ignored 条数）

> 为什么值得单列一条：修复版本**没有**被记录在 OSV 里是常见情形，而此前这种发现项只报"有什么"、
> 不说"怎么办"——用户看到"1 个 HIGH、退 1"，却没有任何可做的事。

详见 [供应链防护](./supply-chain.md)。

---

## ngm verify

### 用途

检查 ref 是否仍指向锁定 commit，digest 是否可本地重放一致。

### 退出码

| 退出码 | 含义 |
|--------|------|
| `0` | 全部匹配，或仅有"预期更新"（branch 前进；`--strict` 时升级为 1） |
| `1` | 非预期漂移（tag 重打 / commit 改写；`--allow-drift` 可降级为 0） |
| `2` | digest 重放不匹配（严重事件）；`--sandbox` 下依赖自检脚本失败/超时；`--require-signed` 下某个依赖不是由**你信任的密钥**签名的 |
| `3` | 配置/策略错误 |
| `4` | Git 网络/操作失败（含 `--offline` 资源缺失） |
| `5` | `--sandbox` 要求跑依赖自带的脚本，而 Deno 缺失或过旧（**不降级**为非沙箱执行） |

### 检查层次与开关

| 检查 | 内容 | 默认 |
|------|------|------|
| ref → commit | 重新解析 refType，对比 lock | ✓（在线读**远端 ref 广播**；`--offline` 用本地 mirror 快照并标 stale） |
| digest 重放 | 从 mirror 重建清单、重算 digest 对比 lock | ✓（本地、可离线） |
| 落地完整性 | vendor 与 content store 的存在性/链接校验 | ✓ |
| 逐文件哈希 | `--deep` 追加：全量校验 vendor 文件字节 | ✗ |
| 签名 | `--signatures` 追加：报告锁定 commit（commit 未签时再看它来源的 tag）的 Git 签名状态。判定用**用户自己的**密钥配置，ngm 不管理密钥 | ✗（默认不查：每个依赖一到两次 git 子进程，而 verify 的主要成本就是 spawn） |

### 输出

```
$ ngm verify

✓ github:org/A@v2.0.0 (tag) → fedcba9 — match
✓ github:org/utils@v1.2.3 (tag) → abc123d — match
⚠ github:org/logger@main (branch) → def456a (now 789xyz0) — expected update
    ref: branch main advanced: def456a → 789xyz0 (fast-forward)
  → driftKind: expected; expected update, not blocking; run `ngm update github:org/logger`
✗ github:org/legacy@v0.9.0 (tag) → 123abc4 — integrity failure
    landing: content store digest mismatch: the installed tree hashes to sha256:51d4433… but ngm.lock says sha256:9f86d08…

verified 4 dependency(ies): 2 ok, 1 expected, 1 critical (exit 2)
```

细节：

- 符号语义：`✓` 通过、`⚠` 预期更新、`✗` 非预期漂移或完整性失败
- **每一类失败都要有 `→` 行**（v0.32 起，v0.34 补齐最后一支）：可分类的漂移写
  `→ driftKind: <kind>; <建议>`；**检查未能完成**（mirror 不在 / 网络被权限拒）时没有可分类的漂移，
  就只写 `→ <建议>`——这一支曾经被渲染的门整块吞掉（建议算好了却没送到用户眼前）
- **建议要承认失败的形状**（v0.34）：因漂移失败指向 `ngm update`、因字节被改失败指向
  `ngm install`、因漏洞失败指向 `ngm audit`——**而不是随便一个真实存在的命令**
- **建议要承认成因**（v0.35）："检查未能完成"这一支覆盖两种**成因完全不同**的失败——
  mirror 不在（⇒ 跑一次在线，或 `ngm install`）与网络被**策略**拒（⇒ 往 `permissions.allow`
  里加那个 host）。后者由**权限层自己**给出（它知道该改哪个键、哪个 host），
  报告原样透传：**最贴近成因的那一层最知道该怎么办**，`remediationFor` 猜不出来
- 通过的检查不展开；只有**未通过**的检查才输出其 `check: detail` 行（信息密度留给行动依据）
- `--offline` 时依赖行带 `[stale]`，并在末尾注明"refs were compared against the local mirror snapshot"
- `--json` 输出同一份判定的机器可读形式（`dependencies[].checks[]` + `summary.exitCode`），
  CI 只需读 `driftKind` 与 `summary.exitCode`，**不应**解析上述人类文本

### "预期更新" vs "非预期漂移"

两者在外部现象上完全相同（ref 指向了别的 commit），区别只在**新旧 commit 的祖先关系**，
因此 `verify` 用 `git merge-base --is-ancestor` 判定：

| 场景 | 性质 | driftKind | verify 行为 |
|------|------|-----------|------------|
| branch 快进（旧 commit 是新 commit 的祖先） | 预期更新 | `expected` | 告警输出，exit 0（`--strict` 时 exit 1），提示可 update |
| branch 非快进（历史被改写 / force push） | 非预期漂移 | `unexpected` | 告警，exit 1，需人工确认（`--allow-drift` 可降级为 0） |
| tag 被重打 | 非预期漂移 | `unexpected` | 同上（tag 的语义就是不可变） |
| 声明的 ref 在上游消失（删除 / 改名） | 非预期漂移 | `unexpected` | 同上 |
| digest 重放不匹配 | 严重事件 | `critical` | 阻断，exit 2，禁止构建（`--allow-drift` **不**降级） |
| 落地内容与 content store 不一致 | 严重事件 | `critical` | 阻断，exit 2（`--deep` 追加逐文件字节比对） |

> **无法证明快进时按 `unexpected` 处理**：祖先关系查不出来（对象缺失等）时，把结果当成快进
> 会让一次 force push 悄悄通过门禁。保守方向是明确的取舍，不是遗漏。

---

## 诚实说明

1. **ngm outdated 的 branch 需要 fetch**：tag 判定只读本地 mirror（不触网），但 branch 的最新 tip
   必须 fetch 才拿得到；离线时报 `unknown`（**不是**"已是最新"）
2. **ngm audit 依赖 OSV.dev**：零日漏洞不在数据库中；多数公告按 semver 记录而非 commit
3. **verify 的签名检查（`--signatures` / `--require-signed`）**：
   **未签名不是失败**——绝大多数依赖没有签名，把它当错误会让 verify 对所有人变红，
   而假警报会让真的警报失效。要门槛的人自己开 `--require-signed`（exit 2）。
   判定完全交给**用户自己的** Git 密钥配置（GPG keyring / `gpg.ssh.allowedSignersFile`）：
   ngm 不管理密钥、不分发密钥、不发明签名格式（[ADR-014](../adr/adr-014-self-report-signatures.md)）。
   它报告的是"这份代码由谁签名"这一事实，**不是**"这份代码安全"——签名证明"谁说的"，
   不证明"说的是真的"。
   覆盖范围：锁定 commit 的签名；commit 未签且声明是 tag 时，再看那个 tag 的签名
   （多数项目只签 tag，不签每个 commit）。轻量 tag 无法携带签名，此时只会报 commit 的结论。
4. **verify 的网络依赖（在线）**：ref 判定只读一次**远端 ref 广播**（`ls-remote`，不传输对象）；
   **对象按需才取**——只有 ref 已变（判性质需要祖先关系）或 lock 的 commit 不在本地时才 fetch。
   因此"什么都没变"的常见路径**完全不 fetch**（[ADR-010](../adr/adr-010-online-verify-fetch-policy.md)）。
   `--offline` 用本地 mirror 快照（缺失时 exit 4），digest 重放本身完全本地。

---

## 相关文档

- [供应链防护](./supply-chain.md)
- [信任模型](./trust-model.md)
- [锁定机制](./locking.md)
