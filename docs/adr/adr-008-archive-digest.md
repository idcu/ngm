# ADR-008：archiveDigest 的定义

- **状态**：已定
- **日期**：2026-09-29
- **范围**：依赖内容摘要的生成、归一化与验证
- **关联**：ADR-004 要求"archiveDigest 必须有定义，否则可证明无定义"；本 ADR 给出定义

---

## 背景

ADR-004 确立"只有 archiveDigest + commit 能回答字节一样不一样"，但把 origin / normalization / algorithm 留成了待定问题。若按 v1 方案（对 `git archive` 或 Git host API 生成的 tar.gz 字节流做哈希）实现，存在三个不可控因素：

1. **host 不可控**：GitHub / GitLab / 自建 Git 的归档字节随各自实现与压缩库版本变化，同一 commit 在不同 host 产出不同 digest
2. **打包管线不可控**：tar 格式（ustar/pax）、目录项排序、gzip 头里的 mtime/OS 字节都由生成器决定，不是规范保证的
3. **origin 语义自相矛盾**：若归档由本地 `git archive` 生成，再问"从哪个 host 取的归档"没有意义

---

## 决策

**archiveDigest 不对归档文件（tar/zip）的字节流计算，而对从 Git 对象直接生成的"规范化内容清单"（canonical manifest）计算。**

**清单由 ngm 本地生成，不依赖 Git host archive API。**

---

## 清单格式 `ngm-archive-digest/v1`

对 commit 的完整 tree 递归展开，生成如下字节流：

```
"ngm-archive-digest/v1" \0
<path> \0 <mode> \0 <blob-sha256> \0
<path> \0 <mode> \0 <blob-sha256> \0
...
```

> **字节流约定（2026-09-29 澄清）**：字段之间**仅以单个 NUL（`0x00`）分隔**，
> 不存在空格、换行或其他分隔字节。上例中 `\0` 两侧的空格、以及头部字符串两侧的
> 引号，均为排版可读性，**不属于字节流**。等价写法：
>
> ```
> "ngm-archive-digest/v1" 0x00 <path> 0x00 <mode> 0x00 <blob-sha256> 0x00 ...
> ```
>
> 之所以选 NUL 作为唯一分隔符：它不可能出现在路径或十六进制摘要中，
> 因此无需转义（与 `git ls-tree -z`、`find -print0` 的取舍一致）。
> 该约定已由冻结向量（`testdata/vectors/archive-kitchen-sink.manifest`）锁定。

| 规则 | 定义 |
|---|---|
| 头部 | 固定字符串 `ngm-archive-digest/v1` + NUL |
| 排序 | 全部记录按 `path` 的 UTF-8 字节序升序 |
| path | 仓库根相对路径，无前导 `./`；目录不单独成条 |
| mode | Git 模式串：`100644` 普通 / `100755` 可执行 / `120000` 符号链接 |
| blob-sha256 | 文件内容原始字节的 sha256（小写 hex）；symlink 为链接目标字符串的 sha256 |
| 换行符 | **不做任何转换**：直接取 commit 中 blob 的原始字节 |
| mtime / uid / gid / 权限位 | 不进入清单（Git 对象模型天然排除） |
| 空目录 | Git tree 不表达空目录，天然排除 |

**archiveDigest = `sha256:` + sha256(清单字节流)**

---

## 关键说明

1. **origin 的含义**：归档由 ngm 从本地 mirror（完整裸仓）生成；对外记录的是"规范仓库坐标 + commit + digest 规范版本"，而不是"某个 host 的某个归档 API"
2. **压缩与 digest 无关**：传输/离线交付可以用任意确定性打包格式，digest 永远在清单字节流上计算
3. **不做 CRLF → LF 转换**：ngm 证明的是"仓库里的字节"，转换会改变证据对象本身
4. **不应用 `.gitattributes` 的 export-ignore / export-subst**：清单直接来自 commit tree，不经过 `git archive` 的过滤
5. **规范版本**：清单格式随 `lockfileVersion` 绑定（1.x 固定使用 v1 清单）；规则变更必须递增规范版本号并伴随 lock 迁移，旧 digest 不得与新规则混用

---

## 验证方式

| 环节 | 行为 |
|---|---|
| `ngm install` | 从 mirror 读取 commit tree → 生成清单 → 计算 digest → 写入 lock → 落地 content store |
| `ngm verify` | 从 mirror 的同一 commit **重建清单并重算 digest**，与 lock 比对（本地重放，可离线） |
| 测试向量 | 实现必须内置固定 fixture（普通文件 / 可执行 / symlink / 空文件 / 嵌套目录 / Unicode 与空格路径 / 大文件）及期望 digest；规则变更必须同步更新向量并递增规范版本 |

> 上游篡改由 commit hash（内容寻址）与 verify 的 ref 检查负责；digest 负责本地重放与字节一致，两者职责互补。

---

## 已知限制（v0.1）

- **Git LFS 不支持**：清单哈希的是 LFS 指针文件内容，不是实体文件；实现检测到 LFS 指针必须报错，不得静默哈希
- **submodule 不支持**：tree 中的 gitlink（`160000`）条目直接报错
- **算法**：v1 固定 sha256；更强/更快的算法列入后续探索

---

## 后果

- 免 host API 限流与 token，digest 生成完全离线可用
- cache 层可随时清空，不影响可证明性（事实源是 mirror + content + lock）
- mirror 成为 digest 生成的前提：mirror 缺失且离线时，verify 报告 stale（exit 4）

---

## 相关文档

- [ADR-004：为什么声明用 refType 锁定用 commit](./adr-004-reftype.md)
- [信任模型](../architecture/trust-model.md)
- [锁定机制](../architecture/locking.md)
- [vendor 4 层](../architecture/vendor-layers.md)