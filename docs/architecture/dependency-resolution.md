# 依赖解析

> **结论**：解析的输入是声明（refType + ref），输出是锁定（commit + 内容 digest）。
> 解析只读上游 `ngm.json` 声明的 Git 依赖，不递归 `package.json`，不碰 registry。

---

## 数据流

```
ngm.json（声明）
   │ 1. Git URL 归一化（https/ssh/git/shorthand/local → canonical）
   ▼
2. refType → commit（ls-remote 读远端 ref 广播；commit 型 0 次 git 子进程）
   ▼
3. 广度优先展开传递依赖 + 冲突检测（读模块自己的 ngm.json）
   ▼
4. 从本地 mirror 的 commit tree 生成规范化清单（path / mode / blob-sha256）
   ▼
5. sha256(canonical manifest) → archiveDigest（根）/ moduleDigest + subtreeOid（模块）
   ▼
6. vendor 4 层落地 + 写入 ngm.lock
```

**状态**：每个节点解析后是不可变锚点（commit 或 local-dir 的 `dirDigest`）；`resolvedAt` 只记"何时信了它"，
不参与门禁。条目身份是 `name + subPath`，同仓两个子路径是两个独立条目。

**失败语义**：URL 形状可疑、上游缺 refType、传递依赖冲突、lock 损坏 → exit 3；资源取不到 → exit 4；
引擎/工具缺失 → exit 5（与解析本身分开，见[可观测性](./observability.md)）。

**边界**：mirror 保留完整历史（不做浅克隆/partial clone 的通用依赖），但 lock 钉住的模块可用
`blob:none` 的部分克隆缓存，filter 不支持时明确回退完整 mirror（见 ADR 汇总表 ADR-034）。

---

## 1. Git URL 归一化

| 输入形式 | canonical 形式 |
|---------|---------------|
| `https://github.com/org/repo.git` | `github.com/org/repo` |
| `git@github.com:org/repo.git` | `github.com/org/repo` |
| `git://github.com/org/repo.git` | `github.com/org/repo` |
| `github:org/repo` | `github.com/org/repo` |
| `git://localhost/org/repo` | `localhost/org/repo`（回环地址按字面收下，见 ADR 汇总表 ADR-029） |

认证交给 Git：SSH 用 `~/.ssh/config` + agent，HTTPS 用 credential helper / env / keychain；
ngm 不读取、不缓存、不落盘凭证。私有 GitLab 的 group/subgroup/repo 路径按普通路径归一化。

**子路径**：`github:org/monorepo#path=packages/utils@v1.0.0` 形式的声明在锁定后记录 `subPath`，
vendor 里只放该子目录。清单不能注入命令：远端地址在构造子进程前做形状校验（拒绝 `-` 开头、
拒绝 `helper::<address>`），详见[安全模型](./security-model.md)。

---

## 2. refType 解析

| refType | 解析方式 | 边界 |
|---------|---------|------|
| `tag` | 本地 mirror / 远端广播的 tag 列表 → commit | tag 语义不可变，重打即漂移 |
| `branch` | 读分支头 → commit | 快进是预期更新，非快进是漂移 |
| `commit` | 直接验证 hash 存在 | 窄默认：完整 40 位 hex 可省略 refType；0 次 git 子进程 |
| `local-git` | 本地 git 仓的 commit | 复用 commit 证明 |
| `local-dir` | 目录清单摘要 `dirDigest` | 不产生 commit；见 ADR 汇总表 ADR-052 |

`--infer-ref-type` 只在 `add` 缺 `--ref-type` 且非完整 commit 时，按远端证据推断 tag/branch；
同名或缺失即失败，不猜（见 ADR 汇总表 ADR-035、ADR-036）。

---

## 3. commit 锁定

```json
{
  "name": "github:org/utils",
  "ref": "v1.2.3",
  "refType": "tag",
  "commit": "abc123def4567890abcdef1234567890abcdef12",
  "archiveDigest": "sha256:9f86d081...",
  "resolvedAt": "2026-09-29T10:00:00Z"
}
```

字段完整语义见[锁定机制](./locking.md)。

---

## 4. 内容清单生成

从本地 mirror（完整裸仓）直接读该 commit 的 tree 并递归展开，**不下载、不打包、不依赖 Git host archive API**。
规范化规则固定，digest 才可本地重放：

- 记录按 path 的 UTF-8 字节序排序
- mode 只取 Git 可表示的 `100644` / `100755` / `120000`
- 不做换行符转换（直接取 blob 原始字节）；不含 mtime / uid / gid
- `.git` 与空目录天然不在 tree 中；LFS 指针与 gitlink（submodule）明确报错

完整定义与测试向量要求见 ADR 汇总表 ADR-008。

---

## 5. archiveDigest 计算

```
头部固定 "ngm-archive-digest/v1\0"
记录按 path 字节序排序，NUL 分隔："<path>\0<mode>\0<blob-sha256>\0"
digest = "sha256:" + sha256(manifest)
```

模块条目改用 `ngm-module-digest/v1` 作用域清单，并额外绑定 `subtreeOid = commit:subPath` 的 tree OID，
把"字节错"与"位置错"分开报（见 ADR 汇总表 ADR-032）。digest 写入 lock；内容树落地 content store。

---

## 6. vendor 落地

mirror → content store（blob 池 + 树清单，按文件内容寻址）→ link tree（默认 hardlink；`symlink` 模式保留降级）。
同一 commit 的整棵树不再重复复制；`symlink` 按需物化一棵 hardlink 树不做（见 ADR 汇总表 ADR-019、ADR-021）。
content store 的回收是**按当前项目 lock 的显式回收**（`ngm gc`，默认 dry-run），不做自动/跨项目 GC（见 ADR 汇总表 ADR-018、ADR-041）。

---

## 传递性依赖

- **模块清单优先**：先读 `<subPath>/ngm.json`，存在即权威；缺位置时回退仓库根清单并 warning
- **不自动递归 package.json**：上游的 Git / registry 依赖不纳入 ngm；检测到时给提示，建议上游迁移
- **refType 强制**：上游声明缺 refType → exit 3，报错点名该上游模块（`name#subPath`）
- **环去重**：按"名称 + ref"合并，已访问节点不重复展开
- **依赖分类**：只有 `dependencies` 一种，没有 dev / optional

传递依赖同样进 lock、verify 与 OSV 范围，同样受 allowlist 与 digest 约束。

---

## 冲突检测

同一依赖的不同版本：

- 同 refType 同 ref：合并
- **根声明优先**：根 `ngm.json` 对它有显式声明时以根为准（root wins），这是解决冲突的出口
- 仅传递之间冲突（根未声明）→ exit 3，提示"在根 ngm.json 显式声明以覆盖"

---

## 已知限制

| 限制 | 行为 |
|------|------|
| Git LFS | 不支持，检测到指针报错 |
| submodule | 不支持，tree 中的 gitlink 条目报错 |
| registry 传递依赖 | 不管理，交给 pnpm / npm |
| workspace | 不做共享 lock 的 workspace（见 ADR 汇总表 ADR-031） |

## 相关文档

- [锁定机制](./locking.md) · [安全模型](./security-model.md) · [可观测性](./observability.md)
- [ADR 汇总表](../adr/README.md)
