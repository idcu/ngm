# 锁定机制

> **结论**：声明层用 refType，锁定层记录锚点——git/local-git 来源用 commit + 内容 digest + resolvedAt，
> local-dir 用 `dirDigest`。根条目写 `archiveDigest`，模块条目写 `moduleDigest + subtreeOid`，两者互斥。

---

## 状态：ngm.lock

```json
{
  "lockfileVersion": "3.0.0",
  "dependencies": [
    {
      "name": "github:org/utils", "ref": "v1.2.3", "refType": "tag",
      "commit": "abc123def4567890abcdef1234567890abcdef12",
      "archiveDigest": "sha256:9f86d081...", "resolvedAt": "2026-09-29T10:00:00Z",
      "vendorPath": "github.com/org/utils"
    },
    {
      "name": "github:org/logger", "ref": "packages/logger/v2.0.0", "refType": "tag",
      "commit": "def456abc7890123def456abc7890123def456ab",
      "moduleDigest": "sha256:01234567...", "subtreeOid": "sha1:01234567...",
      "version": "2.0.0", "subPath": "packages/logger",
      "resolvedAt": "2026-09-29T10:00:00Z"
    }
  ]
}
```

| 字段 | 语义 | 来源 |
|------|------|------|
| `lockfileVersion` | 语义化版本，控制兼容策略（当前 3.0.0） | 常量 |
| `name` / `ref` / `refType` | 依赖标识与人类声明 | ngm.json |
| `commit` | git / local-git 来源的不可变锚点 | 解析结果 |
| `dirDigest` | local-dir 来源的目录内容锚点（`ngm-local-digest/v1`） | 本地生成 |
| `sourcePath` / `localRepo` | 本地来源的相对目录 / 本地 git 仓根 | ngm.json |
| `archiveDigest` | 根条目的规范化内容清单哈希（`sha256:<hex>`） | 本地生成 |
| `moduleDigest` / `subtreeOid` | 模块条目的模块作用域清单哈希 / Git tree 绑定 | 本地生成 / `git rev-parse` |
| `version` | 模块版本标签（可选，与模块清单一致） | 模块清单 |
| `resolvedAt` | 解析时刻，**仅审计，不参与门禁** | 当前时间 |
| `vendorPath` / `subPath` | vendor 落地路径 / monorepo 子路径（空表示整仓） | 归一化 / ngm.json |

**条目身份是 `name + subPath`**，不是单独的 `name`：同仓两个子路径是两个独立条目，排序与查找都以二者为键。
`vendorPath` / `subPath` 必须是 vendor 根内的安全相对路径（无 `.` / `..`、盘符、反斜杠、NUL、URL 形状），
且在拼接后再判一次，越界条目不参与任何 `Stat` / 哈希 / 脚本执行。

---

## 数据流与互补性

```
声明(refType+ref) → 解析(commit) → 重放清单(digest, 可离线)
    ↑ verify 重新解析 ref 对比 commit     ↑ verify 从同一 commit 重建清单对比 digest
```

|  | commit | archiveDigest（根） | moduleDigest + subtreeOid（模块） |
|---|--------|---------------------|-----------------------------------|
| 锚定对象 | Git tree 对象 | 整仓规范化清单 | 模块子树清单 + Git tree 绑定 |
| 能回答 | "哪个 Git 版本" | "整仓字节是否一致" | "字节与位置是否一致" |
| 局限 | 不保证 working tree 一致 | 需要整仓 blob | 需要 `commit:subPath` 的 tree + 模块 blob |

**互补**：commit 变了 → 漂移（tag 重打 / branch 移动，exit 1）；commit 没变但 digest 重放不匹配 →
本地内容被改或清单规范变了（exit 2）；模块条目还要校验 `subtreeOid`，位置错与字节错分开报。

---

## 可复现性的定义

"lock 可复现"指**确定性字段集**的字节级一致，不是整个文件：

- 确定性字段：结构、字段顺序、commit、内容 digest、`vendorPath`
- 非确定性字段：`resolvedAt`（解析时刻）
- 序列化：UTF-8、固定缩进与字段顺序、末尾换行
- 判定：同一声明 + 同一上游 ref 状态 → 除 `resolvedAt` 外字节一致

vendor 可复现：相同 lock + 相同 content store → 相同 vendor 内容树（文件字节一致）。

**版本兼容**：格式为 `MAJOR.MINOR`。同 MAJOR 内只新增可选字段，旧工具忽略未知字段；
MAJOR 变更是破坏性调整（含清单规范升级），旧 lock 明确报不兼容，删除后重新生成，不提供迁移命令
（见 ADR 汇总表 ADR-052）。`resolvedAt` 不参与 `minimumReleaseAge`——后者的时间源是 commit 的 committer date。

---

## 失败语义

| 操作 | 行为 | 失败 |
|------|------|------|
| `ngm install`（有 lock） | 按 lock 解析，不更新 ref | 资源缺失 exit 4；frozen 与声明不一致 exit 3 |
| `ngm install`（无 lock） | 按声明解析，生成 lock | 配置错误 exit 3 |
| `ngm update` | 重新解析 refType → 新 commit → 新 digest | 网络失败 exit 4 |
| `ngm verify` | 重新解析 ref 对比 commit；重放 digest；查落地 | 预期更新 exit 0；非预期漂移 exit 1；digest 不匹配 exit 2；问不到 exit 4 |

**两个 flag 约束两件事**（CI 通常合并使用）：

- `--frozen-lockfile`：禁止解析新 ref、禁止改 lock；**不禁止网络**（content store 未命中仍需下载）
- `--offline`：禁止一切网络访问，本地未命中 exit 4；**不等于**"装出来的东西差不多"
- 两者组合才是"完全离线且可复现"：前提是 `ngm.lock` 已提交、content store 或 vendor 已就绪

`ngm.lock` **必须提交**：跨机器同结果、PR 可审计、CI 可离线复现。

## 相关文档

- [依赖解析](./dependency-resolution.md) · [可观测性](./observability.md) · [供应链防护](./supply-chain.md)
- [配置详解](../guides/configuration.md) · [ADR 汇总表](../adr/README.md)
