# 依赖解析

## 解析流程

```
ngm.json（声明）
    │
    ▼
┌─────────────────────────┐
│ 1. Git URL 归一化        │  https/ssh/git/shorthand → canonical
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│ 2. 广度优先遍历          │  每个节点：refType → commit
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│ 3. 传递依赖展开 + 冲突检测│  读依赖根 ngm.json；环去重
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│ 4. 内容清单生成          │  从 mirror 的 commit tree 展开
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│ 5. archiveDigest 计算    │  sha256(canonical manifest)
└─────────────────────────┘
    │
    ▼
┌─────────────────────────┐
│ 6. vendor 4 层落地       │  mirror → content → links
└─────────────────────────┘
    │
    ▼
ngm.lock（锁定）+ ngm.mappings.json
```

---

## 1. Git URL 归一化

### 4 种协议

| 输入形式 | canonical 形式 |
|---------|---------------|
| `https://github.com/org/repo.git` | `github.com/org/repo` |
| `git@github.com:org/repo.git` | `github.com/org/repo` |
| `git://github.com/org/repo.git` | `github.com/org/repo` |
| `github:org/repo` | `github.com/org/repo` |

### 认证处理

- SSH：`~/.ssh/config` + agent
- HTTPS token：从 `~/.git-credentials` / env / keychain 读取
- 私有 GitLab：group/subgroup/repo 路径归一化

### monorepo 子路径

```
ngm add github:org/monorepo#path=packages/utils@v1.0.0 --ref-type tag
```

锁定后记录子路径，vendor 里只放该子目录。

---

## 2. refType 解析

### tag → commit

```bash
git ls-remote --tags https://github.com/org/repo.git
# refs/tags/v1.2.3 → abc123def
```

### branch → commit

```bash
git ls-remote --heads https://github.com/org/repo.git
# refs/heads/main → def456abc
```

### commit → commit（passthrough）

直接验证 hash 是否存在。

---

## 3. commit 锁定

锁定文件中记录：

```json
{
  "name": "github:org/utils",
  "ref": "v1.2.3",
  "refType": "tag",
  "commit": "abc123def4567890abcdef1234567890abcdef12",
  "resolvedAt": "2026-09-29T10:00:00Z"
}
```

---

## 4. 内容清单生成

### 生成方式

从本地 mirror（完整裸仓）直接读取该 commit 的 tree，递归展开为规范化内容清单（canonical manifest）：**不下载、不打包、不依赖 Git host archive API**。

| 数据来源 | 产物 |
|---------|------|
| `~/.ngm/mirror/<host>/<org>/<repo>.git` 的 commit tree | 规范化内容清单（path / mode / blob-sha256） |

### 规范化规则（关键）

archiveDigest 要可重放，清单规则必须固定：

- 记录按 path 的 UTF-8 字节序排序
- mode 仅取 Git 可表示的 `100644` / `100755` / `120000`
- **不做换行符转换**（直接取 blob 原始字节）
- 不含 mtime / uid / gid（Git 对象模型天然排除）
- `.git` 与空目录天然不在 tree 中

完整定义与测试向量要求见 [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)。

---

## 5. archiveDigest 计算

```go
// 伪代码：清单 → digest
func buildManifest(commit Tree) []byte {
    // 头部固定 "ngm-archive-digest/v1\0"
    // 记录按 path 字节序排序，NUL 分隔：
    //   "<path>\0<mode>\0<blob-sha256>\0"
}

func computeDigest(manifest []byte) string {
    return "sha256:" + sha256(manifest)
}
```

digest 写入 lock；内容树落地到 content store（层 2），供 vendor 层链接。

---

## 6. vendor 落地

详见 [vendor 4 层模型](./vendor-layers.md)。

---

## 传递性依赖

### 递归规则（v0.1）

- **唯一来源**：依赖仓库根目录的 `ngm.json`；缺失即视为无传递依赖
- **不自动递归 package.json**：上游 `package.json` 中的 Git / registry 依赖不纳入 ngm（registry 生态归 pnpm）；若检测到 `package.json` 含 Git 依赖，`ngm install` 输出提示，建议上游迁移到 `ngm.json`
- **refType 强制**：上游声明缺少 `refType` → 报错（exit 3），提示指向该上游仓库
- **循环引用**：按"名称 + ref"合并去重，已访问节点不重复展开
- **依赖分类**：v0.1 只有 `dependencies` 一种，无 dev / optional

```
project
├── ngm.json → github:org/A@v1.0.0
│   └── A/ngm.json → github:org/B@v2.0.0
│       └── B/ngm.json → github:org/C@v3.0.0
└── ngm.lock（包含所有层级）
```

**传递性依赖也进入 lock、verify 和 OSV 范围，并同样受 allowlist 与 archiveDigest 约束**。

---

## 冲突检测

同一依赖的不同版本：

```
project
├── ngm.json → github:org/A@v1.0.0
└── ngm.json → github:org/B@v2.0.0
    └── B → github:org/A@v2.0.0  ← 冲突！
```

处理策略：

- 同 refType 同 ref：合并
- **根声明优先**：根 `ngm.json` 对该依赖有显式声明时，以根声明为准（root wins），不报错——这是解决冲突的出口
- 仅传递之间冲突（根未声明）：报错（exit 3），提示"在根 ngm.json 显式声明以覆盖"
- 不同 refType：同上处理

---

## 已知限制（v0.1）

| 限制 | 行为 |
|------|------|
| Git LFS | 不支持：检测到 LFS 指针报错（见 [ADR-008](../adr/adr-008-archive-digest.md)） |
| submodule | 不支持：tree 中的 gitlink 条目报错 |
| 浅克隆 / partial clone | 不支持：mirror 保留完整历史（大仓库成本列入后续探索） |
| registry 传递依赖 | 不管理：交给 pnpm / npm |

---

## 相关文档

- [信任模型](./trust-model.md)
- [锁定机制](./locking.md)
- [vendor 4 层模型](./vendor-layers.md)
- [ADR-004：为什么声明用 refType 锁定用 commit](../adr/adr-004-reftype.md)
- [ADR-008：archiveDigest 的定义](../adr/adr-008-archive-digest.md)
