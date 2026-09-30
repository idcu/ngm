# 可观测性

## 核心命令

| 命令 | 作用 | 成熟度 | 退出码 |
|------|------|--------|--------|
| `ngm verify` | ref 漂移 + digest 重放检查 | **done (v0.1)** | 0/1/2/3/4 |
| `ngm why <dep>` | 为什么装了这个依赖 | planned (v0.2) | 当前 `exit 3` |
| `ngm tree` | 依赖树可视化 | planned (v0.2) | 当前 `exit 3` |
| `ngm outdated` | 哪些依赖有新版本 | planned (v0.2) | 当前 `exit 3` |
| `ngm audit` | 已知漏洞扫描（OSV.dev） | planned (v0.2) | 当前 `exit 3`；规划 0/1/3/4 |

> **标 `planned` 的四条命令尚未实现**：调用时会明确返回 `exit 3` 与可读提示，不会静默成功。
> 至此「ngm verify」（v0.1）与「ngm audit / why / tree / outdated」（v0.2）各节
> 描述的都是**已实现行为**，不再是规划稿。

---

## 退出码约定（全局）

所有命令共用一套退出码（与 P0 的 `ErrorCode` 对齐）：

| 退出码 | 语义 | 典型来源 |
|--------|------|---------|
| `0` | 成功（verify 的"仅预期更新"也归此） | 所有命令 |
| `1` | 策略失败 | verify 非预期漂移；adapter 引擎运行失败（引擎自己的退出码保留在消息与 `--json` 里，不透传）；audit 存在超阈值漏洞 |
| `2` | 完整性失败 | verify digest 重放不匹配 |
| `3` | 配置/策略/lock 错误 | schema 非法、lock 损坏、frozen 与声明不一致 |
| `4` | Git/网络失败 | fetch / ls-remote 失败；`--offline` 下资源缺失 |
| `5` | 引擎不可用 | adapter 找不到外部引擎 |

`ngm verify --json` 输出的 `driftKind`（`expected` / `unexpected` / `critical`）用于区分"预期更新"与"非预期漂移"。

---

## ngm why

> **done (v0.2)**。下面的输出即当前真实行为。同一依赖被多个父节点引入时会列出
> **全部**路径——"为什么装了它"常常不止一个答案。

### 用途

回答："我项目里为什么有这个依赖？"

### 输出

```
$ ngm why github:org/utils

github:org/utils@v1.2.3 (tag)
├── 直接依赖：ngm.json
└── 传递依赖：
    └── github:org/A@v2.0.0 → ngm.json
        └── github:org/utils@v1.2.3 (tag)

锁定：abc123def (2026-09-20)
archiveDigest: sha256:9f86d081...
```

---

## ngm tree

> **done (v0.2)**。`⚠`（漂移）默认就有；`✗`（已知漏洞）需要显式 `--osv`。
>
> 这条是对原设计的**刻意偏离**：让"打印一棵树"顺手发起网络查询不合适。
> 不带 `--osv` 时报告会明写"未查询漏洞数据"——把"没查"说成"查过没事"
> 是这类工具最容易骗人的地方。

### 输出

```
my-app
├── github:org/A@v2.0.0 (tag) → fedcba987
│   ├── github:org/B@v1.5.0 (tag) → 123456789
│   └── github:org/C@v3.1.0 (branch) → abcdef012
├── github:org/utils@v1.2.3 (tag) → abc123def
└── github:org/logger@main (branch) → def456abc ⚠ 漂移
```

`⚠` 标记有漂移的 ref。

---

## ngm outdated

> **done (v0.2)**。一条硬纪律：**查不到就报 `unknown`，绝不写"已是最新"**。
> 因此 `--offline` 下的 branch 依赖是 `unknown` 而不是 `no`——"不知道"
> 与"查过，确实没有"是两种结论，报告刻意把它们分开。

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
```

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
| `2` | digest 重放不匹配（严重事件） |
| `3` | 配置/策略错误 |
| `4` | Git 网络/操作失败（含 `--offline` 资源缺失） |

### 检查层次与开关

| 检查 | 内容 | 默认 |
|------|------|------|
| ref → commit | 重新解析 refType，对比 lock | ✓（在线读**远端 ref 广播**；`--offline` 用本地 mirror 快照并标 stale） |
| digest 重放 | 从 mirror 重建清单、重算 digest 对比 lock | ✓（本地、可离线） |
| 落地完整性 | vendor 与 content store 的存在性/链接校验 | ✓ |
| 逐文件哈希 | `--deep` 追加：全量校验 vendor 文件字节 | ✗ |

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
3. **verify 的网络依赖（在线）**：ref 判定只读一次**远端 ref 广播**（`ls-remote`，不传输对象）；
   **对象按需才取**——只有 ref 已变（判性质需要祖先关系）或 lock 的 commit 不在本地时才 fetch。
   因此"什么都没变"的常见路径**完全不 fetch**（[ADR-010](../adr/adr-010-online-verify-fetch-policy.md)）。
   `--offline` 用本地 mirror 快照（缺失时 exit 4），digest 重放本身完全本地。

---

## 相关文档

- [供应链防护](./supply-chain.md)
- [信任模型](./trust-model.md)
- [锁定机制](./locking.md)
