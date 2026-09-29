# 信任模型

## 核心问题

> 当依赖直接来自 Git 仓库时，你怎么证明"我装的代码"就是"我审过的代码"？

ngm 的信任模型围绕一个四元组展开。

---

## 四元组：声明与证据的分离

```json
{
  "name": "github:org/utils",
  "ref": "v1.2.3",
  "refType": "tag",
  "commit": "abc123def4567890abcdef1234567890abcdef12",
  "archiveDigest": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "resolvedAt": "2026-09-29T10:00:00Z"
}
```

### 语义分层

| 字段 | 语义层 | 含义 | 可漂移？ |
|------|-------|------|---------|
| `ref` | 声明层 | 人类意图："我要 v1.2.3 / main / 某 commit" | 可变 |
| `refType` | 声明层 | 意图类型：tag / branch / commit | 不变 |
| `commit` | 解析层 | Git 对象里的不可变指针 | 不可变（除非历史改写） |
| `archiveDigest` | 证据层 | 规范化内容清单的哈希 | 不可变 |
| `resolvedAt` | 审计层 | 你什么时候信了它 | 不可变 |

### 关键洞察

```
ref 会漂移（tag 重打、branch 移动）
  ↓
但 commit 是锚点（除非 force push）
  ↓
archiveDigest 是终极证据（字节级可重放）
```

> tag 会重打，branch 会移动，commit 会重组历史；
> **只有 archiveDigest + commit 能回答"字节一样不一样"**。

---

## archiveDigest 的定义（已定）

**ADR-004 要求"archiveDigest 必须有定义，否则'可证明'无定义"——完整定义见 [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)。**

要点：

1. **对象**：不对 tar/zip 归档字节流计算，而对从 Git 对象生成的**规范化内容清单**（canonical manifest）计算
2. **origin**：由 ngm 从本地 mirror 生成，不依赖 Git host archive API；记录的是"规范仓库坐标 + commit + digest 规范版本"
3. **normalization**：清单规则固定——path 按 UTF-8 字节序排序、mode 取 Git 三态（`100644` / `100755` / `120000`）、不含 mtime/uid/gid、不做换行符转换
4. **algorithm**：`sha256:` + sha256(清单字节流)；清单规范版本随 `lockfileVersion` 绑定
5. **验证**：`ngm verify` 从 mirror 的同一 commit 重建清单并重算 digest（本地重放，可离线）

> 上游篡改由 commit hash（内容寻址）与 ref 检查负责；digest 负责本地重放与字节一致。

---

## verify：检查内容与退出码

```bash
ngm verify                 # ref 对比 + digest 本地重放 + 落地校验
ngm verify --offline       # 不访问网络，用本地 mirror 快照
ngm verify --deep          # 追加 vendor 逐文件内容哈希校验
ngm verify --json          # 机器可读输出（含 driftKind）
```

### 检查层次

| 检查 | 内容 | 默认 |
|------|------|------|
| ref → commit | 重新解析 refType，对比 lock 中的 commit | ✓ |
| digest 重放 | 从 mirror 同一 commit 重建清单、重算 digest，对比 lock（本地，可离线） | ✓ |
| 落地完整性 | vendor 与 content store 的存在性/链接校验 | ✓ |
| 逐文件哈希 | `--deep` 追加：全量校验 vendor 文件字节 | ✗ |

### 退出码

完整定义唯一维护在[可观测性 · 退出码约定](./observability.md)。与"信任"直接相关的只有三条：

- `2`（digest 重放不匹配）：**不可降级**，`--allow-drift` 对它无效
- `1`（非预期漂移：tag 重打 / commit 改写）：默认阻断，`--allow-drift` 可降级为 0
- `0`：**包含** branch 前进这类"预期更新"；要连它一起拦下需 `--strict`（升级为 1）

### verify 输出示例

```
✓ github/org/utils@v1.2.3 (tag) → abc123def (匹配)
⚠ github/org/logger@main (branch) → def456abc (预期更新；当前 main 指向 789xyz)
  → driftKind: expected（不阻断；建议 ngm update github/org/logger 并提交 lock）
✗ github/org/legacy@v0.9.0 (tag) → 123abc456 (digest 重放不匹配！)
  → driftKind: critical（阻断：本地内容被篡改/损坏，或清单规范版本变化）
```

---

## "预期更新" vs "非预期漂移"

verify 必须区分，且**区分必须机器可读**（仅靠文本输出不够）：

| 场景 | 性质 | driftKind | verify 行为 |
|------|------|-----------|------------|
| branch 前进了 3 个 commit | 预期更新 | `expected` | 警告输出；exit 0（`--strict` 时 exit 1）；提示可 `ngm update` |
| tag 被重打 | 非预期漂移 | `unexpected` | 严重告警；exit 1；需人工确认 |
| commit 被改写（force push） | 非预期漂移 | `unexpected` | 严重告警；exit 1；需人工确认 |
| digest 重放不匹配 | 严重事件 | `critical` | 阻断；exit 2；禁止构建 |

预期更新默认不阻断 CI（branch 依赖的本质就是跟随上游）；要求 lock 与上游强一致的团队使用 `--strict`，或消费 `--json` 的 `driftKind` 做精细判断。

---

## 供应链门禁最小字段

> **planned (v0.2)**：这些字段在 v0.1 **只被解析与校验，不参与任何门禁**。
> 现在写下 `minimumReleaseAge: "P30D"` 不会拦住任何依赖——它是一份"将要生效"的声明，
> 不是当前已有的保护。失效期与生效条件见[供应链防护 · 诚实说明](./supply-chain.md)。

```json
{
  "supplyChain": {
    "allowedGitHosts": ["github.com", "gitee.com"],
    "allowlistRepos": ["github.com/my-org/*"],
    "minimumReleaseAge": "P3D",
    "osvIgnoreSeverities": ["LOW"],
    "postInstallPolicy": "deny",
    "verifyOnLock": true
  }
}
```

| 字段 | 作用 | 备注 |
|------|------|------|
| `allowedGitHosts` | 限制可接受的 Git 托管来源 | 防 typosquatting |
| `allowlistRepos` | 白名单指定仓库 | 精确到 org/repo |
| `minimumReleaseAge` | 推迟新 commit 进入项目（基于 commit 的 committer date） | pnpm 已有同类概念；时间源见[供应链防护](./supply-chain.md) |
| `osvIgnoreSeverities` | OSV.dev 告警阈值 | LOW/MEDIUM/HIGH/CRITICAL |
| `postInstallPolicy` | 控制 build script 是否运行 | deny / prompt / allow |
| `verifyOnLock` | 更新锁定时是否自动检查漂移 | **无默认值**（未配置即不触发）；生效属 v0.2 |

---

## 相关文档

- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)
- [ADR-004：为什么声明用 refType 锁定用 commit](../adr/adr-004-reftype.md)
- [供应链防护](./supply-chain.md)
- [锁定机制](./locking.md)
- [安全模型](./security-model.md)
