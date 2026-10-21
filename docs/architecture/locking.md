# 锁定机制

## 核心原则

**声明层用 refType（commit/tag/branch），锁定层统一记录实际解析到的 commit + 内容 digest + resolvedAt。根条目用 `archiveDigest`；模块条目用 `moduleDigest + subtreeOid`（ADR-032）。**

---

## ngm.lock schema

```json
{
  "version": 1,
  "lockfileVersion": "2.0.0",
  "dependencies": [
    {
      "name": "github:org/utils",
      "ref": "v1.2.3",
      "refType": "tag",
      "commit": "abc123def4567890abcdef1234567890abcdef12",
      "archiveDigest": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "resolvedAt": "2026-09-29T10:00:00Z",
      "vendorPath": "github.com/org/utils"
    },
    {
      "name": "github:org/logger",
      "ref": "packages/logger/v2.0.0",
      "refType": "tag",
      "commit": "def456abc7890123def456abc7890123def456ab",
      "moduleDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "subtreeOid": "sha1:0123456789abcdef0123456789abcdef01234567",
      "version": "2.0.0",
      "resolvedAt": "2026-09-29T10:00:00Z",
      "vendorPath": "github.com/org/logger",
      "subPath": "packages/logger"
    }
  ]
}
```

---

## 字段语义

| 字段 | 语义 | 来源 |
|------|------|------|
| `version` | lock schema 版本 | 常量 |
| `lockfileVersion` | 语义化版本，控制兼容策略 | 常量 |
| `name` | 依赖标识 | ngm.json |
| `ref` | 人类声明 | ngm.json |
| `refType` | 声明意图 | ngm.json |
| `commit` | 解析结果（不可变锚点） | `git ls-remote` |
| `archiveDigest` | **根条目**的规范化内容清单哈希（`sha256:<hex>`，ADR-008） | 本地生成 |
| `moduleDigest` | **模块条目**的模块作用域内容清单哈希（`ngm-module-digest/v1`，ADR-032） | 本地生成 |
| `subtreeOid` | **模块条目**的 Git tree 绑定锚（`sha1:`/`sha256:`） | `git rev-parse commit:subPath` |
| `version` | 模块版本标签（可选；与模块 `ngm.json` 的 version 一致） | 模块清单 |
| `resolvedAt` | 解析时间（仅审计，不参与门禁计算） | 当前时间 |
| `vendorPath` | vendor 落地路径 | 归一化规则 |
| `subPath` | monorepo 子路径（可选，带 `omitempty`；空表示整仓） | ngm.json 的 `path` |

**字段纪律**：digest 只表达一次——根条目写 `archiveDigest`，模块条目写 `moduleDigest + subtreeOid`，两者互斥；`resolvedAt` 只存在于依赖条目内，lock 顶层不含时间戳。

> **条目身份是 `name` + `subPath`，不是 `name`**：同一仓库的两个 monorepo 子路径是**两个独立条目**，
> 排序与查找都以二者为键（内部形式 `name#subPath`）。少了 `subPath`，同仓多子路径的依赖会互相覆盖。
>
> **没有 v1 迁移**：项目尚无用户，v2 直接废止“模块条目同时写 archiveDigest”的过渡态；
> 旧 lock 读到 v2 会明确报 `lockfileVersion` 不兼容，重新 `ngm install` 即可。

---

## commit vs 内容 digest

| | commit | archiveDigest（根） | moduleDigest + subtreeOid（模块） |
|---|--------|---------------------|-----------------------------------|
| 锚定对象 | Git tree 对象 | 整仓规范化清单 | 模块子树清单 + Git tree 绑定 |
| 可变性 | 不可变（除非 force push） | 不可变（规则由规范版本固定） | 不可变；位置由 subtreeOid 绑定 |
| 能回答 | "哪个 Git 版本" | "整仓字节是否一致" | "这个模块的字节与位置是否一致" |
| 局限 | 不保证 working tree 一致 | 需要整仓 blob | 需要 `commit:subPath` 的 tree + 模块 blob |

**它们互补**：

- commit 变了 → ref 漂移（tag 重打 / branch 移动）
- commit 没变但 digest 重放不匹配 → 本地内容被篡改/损坏，或清单规范发生版本变化 → 阻断（exit 2）
- 模块条目还要校验 `subtreeOid`：位置错与字节错分开报

---

## 可复现性的定义

"lock 可复现"指**确定性字段集**的字节级一致，而不是整个文件：

- 确定性字段：结构、字段顺序、`commit`、内容 digest（`archiveDigest` 或 `moduleDigest`/`subtreeOid`）、`vendorPath` 等
- 非确定性字段：`resolvedAt`（解析时刻）
- 序列化规范：UTF-8、2 空格缩进、字段顺序固定、末尾换行
- 判定方式：同一 ngm.json + 同一上游 ref 状态 → 除 `resolvedAt` 外字段字节一致，即视为可复现

vendor 落地的可复现定义：相同 lock + 相同 content store → 相同 vendor 内容树（文件字节一致）。

---

## lockfileVersion 演进

- 格式为 `MAJOR.MINOR`；当前为 `2.0.0`（ADR-032，模块级证明）
- 同 MAJOR 内：只允许新增可选字段，旧版本工具应忽略未知字段继续工作
- MAJOR 变更：破坏性字段调整（含 digest 清单规范版本升级）。项目尚无用户，v1 → v2
  **不提供迁移命令**；旧 lock 会被明确拒绝，重新 `ngm install` 即可
- 根条目的 `archiveDigest` 与 `ngm-archive-digest/v1` 绑定；模块条目的 `moduleDigest`
  与 `ngm-module-digest/v1` 绑定，见 [ADR-008](../adr/adr-008-archive-digest.md) /
  [ADR-032](../adr/adr-032-scoped-content-proof.md)

---

## lock file 的提交策略

**ngm.lock 必须提交到 Git。**

理由：

- 确保 `ngm install` 在不同机器/时间得到相同结果
- 可审计：PR review 能看到依赖变化
- CI 可复现：配合 content store 或 vendor，环境无网络也能安装与构建

**`install --frozen-lockfile`（CI 模式，v0.2 已实现）**：禁止解析新 ref、禁止修改 lock file；lock 缺失或与 ngm.json 不一致立即失败（exit 3）。它**不禁止网络**——content store 未命中时仍需下载依赖。

**`install --offline`（v0.2 已实现）**：禁止一切网络访问；content store 与 mirror 都缺失时失败（exit 4）。注意它**不等于**"装出来的东西差不多"——离线装不上就是装不上。

**`--frozen-lockfile --offline`（CI 首选）**：完全离线、完全由已提交的 lock 决定。前提是把 `ngm.lock` 提交进仓库，并让 content store 可用（CI 上可缓存 `~/.ngm/content`）。

> 为什么 frozen 与 offline 要分成两个 flag：它们约束的是**两件不同的事**——
> frozen 约束"装什么"（不许重新决定 commit），offline 约束"怎么拿"（不许联网）。
> 只想要可复现性时用前者即可（它仍可从网络取内容）；合并使用才得到"完全离线且可复现"。
> v0.1 的 `ngm install` 只有 `--dir` / `--digest`，`--offline` 只适用于 `ngm update` 与 `ngm verify`。

---

## resolvedAt 的语义

`resolvedAt` 记录"你什么时候信了它"，用于：

- 审计：哪个 commit 在什么时候被引入
- 协作：团队成员何时更新了锁定

**`resolvedAt` 不参与 `minimumReleaseAge`**：后者的时间源是 commit 的 committer date（见[供应链防护](./supply-chain.md)）。

**`resolvedAt` ≠ commit 的 author date，也不是"上游发布时间"**。

---

## 更新策略

### `ngm install`（尊重 lock）

- 有 lock：按 lock 解析，不更新 ref
- 无 lock：按 ngm.json 解析，生成 lock

### `ngm update`（更新 ref）

- `ngm update github:org/utils`：重新解析 refType → 新 commit → 新 digest
- `ngm update --all`：更新所有依赖
- 更新后 verify 检查是否为"预期更新"

### `ngm verify`（检查漂移）

- 重新解析所有 refType → 对比 lock 中的 commit
- 从 mirror 同一 commit 重建清单、重算 digest → 对比 lock（本地重放，可离线）
- 非预期漂移 → exit 1；digest 重放不匹配 → exit 2；"预期更新"（branch 前进）→ exit 0 并输出 `driftKind: expected`
- 完整语义见[信任模型](./trust-model.md)

---

## 相关文档

- [信任模型](./trust-model.md)
- [依赖解析](./dependency-resolution.md)
- [配置详解](../guides/configuration.md)
- [ADR-004：为什么声明用 refType 锁定用 commit](../adr/adr-004-reftype.md)
- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)
