# ADR-004：为什么声明用 refType，锁定用 commit

- **状态**：已定
- **日期**：2026-09-29
- **范围**：依赖锁定粒度与 ref 语义

> **结论**：声明层 `refType` 必填（commit / tag / branch），锁定层统一记录实际解析到的 commit。
> 让声明自描述，不靠推断——`@main` 无法区分 tag 叫 main 还是 branch 叫 main。
> **代价**：每个依赖多一个必填字段（写起来更啰嗦）；只能锁定**已存在**的 commit，
> 上游 force push 后旧 commit 可能从远端消失，此时只能靠本地 mirror / vendor 兜底。

---

## 背景

依赖锁定存在三选一的张力：

| ref 类型 | 便利性 | 安全性 |
|---------|--------|--------|
| commit | 差（hash 难记） | 绝对确定 |
| tag | 好（v1.2.3 直观） | 可能漂移（tag 重打） |
| branch | 最好（不用等发布） | 最频繁漂移 |

如果只写 `@main`，无法区分是 tag 叫 main 还是 branch 叫 main——仓库里恰好两者同名时冲突。

---

## 决策

**声明层 refType 必填（commit / tag / branch），锁定层统一记录实际解析到的 commit。**

---

## 四元组语义

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

| 字段 | 含义 | 可漂移？ |
|------|------|---------|
| `ref` | 人类声明："我要 v1.2.3 / main / 某 commit" | 可变 |
| `refType` | 声明意图：tag / branch / commit | 不变 |
| `commit` | 解析结果：Git 对象里的不可变指针 | 不可变（除非历史改写） |
| `archiveDigest` | 可重放证据：规范化内容清单的哈希 | 不可变 |
| `resolvedAt` | 审计时间：你什么时候信了它 | 不可变 |

---

## 关键洞察：archiveDigest 必须有定义

> tag 会重打，branch 会移动，commit 会重组历史；
> **只有 archiveDigest + commit 能回答"字节一样不一样"**。

但 archiveDigest 不是免费的——必须定义 origin、normalization、algorithm，否则"可证明"无定义。

**定义见 [ADR-008：archiveDigest 的定义](./adr-008-archive-digest.md)**：基于本地从 Git 对象生成的规范化内容清单（不依赖 tar.gz / host archive API）。详见 [信任模型](../architecture/trust-model.md)。

---

## 推断规则（辅助，不写入声明）

`ngm add` 交互时辅助推断 refType：

- 匹配 `v*.*.*` 或 `*.*.*` → tag
- 匹配 7 位以上 hex → commit
- 其他 → branch

推断可能出错（branch 叫 `v1.2.3`、tag 叫 `main`），所以**写入 ngm.json 时必须显式带上 refType**。

---

## 对比参考

| 工具 | ref 锁定方式 | ngm 的差异 |
|------|-------------|-----------|
| npm | 记录 Git commit | 不强制 refType |
| pnpm | Git dependencies | 无 archive 级策略 |
| Yarn | approvedGitRepositories + checksum | 不强制 refType |
| Deno | Git import + integrity | 无 refType 语义 |

---

## 后果

- `ngm add` 推断 refType，写入 ngm.json 时带上 refType
- `ngm install` 解析 refType → commit → archiveDigest
- `ngm verify` 检查 ref 是否仍指向锁定 commit，漂移即告警
- 供应链防护（minimumReleaseAge / OSV / 白名单）全部基于锁定 commit，不基于 tag/branch

---

## 相关文档

- [信任模型](../architecture/trust-model.md)
- [锁定机制](../architecture/locking.md)
- [配置详解](../guides/configuration.md)
