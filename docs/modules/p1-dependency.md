# P1 — 依赖管理

> 依赖解析、锁定、vendor 4 层、mappings 生成

---

## 职责

P1 是 ngm 的核心能力层，负责把"Git URL + refType"变成"可证明的 vendor 落地"。

---

## 模块结构

```
internal/
├── config/              # 配置加载
├── git/                 # Git 操作
├── resolve/             # 依赖解析
├── lock/                # 锁定机制
├── vendor/              # vendor 4 层
├── digest/              # archiveDigest
└── mappings/            # mappings 生成
```

（本模块只列相关包；完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## 依赖解析流程

流程、传递依赖规则与冲突策略的唯一维护位置：[架构：依赖解析](../architecture/dependency-resolution.md)（6 个阶段），本页不再复制。

---

## Git URL 归一化

### 输入形式

| 输入 | canonical |
|------|-----------|
| `https://github.com/org/repo.git` | `github.com/org/repo` |
| `git@github.com:org/repo.git` | `github.com/org/repo` |
| `git://github.com/org/repo.git` | `github.com/org/repo` |
| `github:org/repo` | `github.com/org/repo` |
| `gitee:org/repo` | `gitee.com/org/repo` |
| `gitlab:group/subgroup/repo` | `gitlab.com/group/subgroup/repo` |

### 实现

```go
func NormalizeURL(raw string) (string, error) {
    // 1. 识别协议前缀
    // 2. 提取 host/org/repo
    // 3. 去除 .git 后缀
    // 4. 返回 canonical 形式
}
```

---

## refType 解析

### tag → commit

```go
func ResolveTag(repo, tag string) (string, error) {
    out, err := exec.Command("git", "ls-remote", "--tags", repo, tag).Output()
    // refs/tags/v1.2.3 -> abc123def
}
```

### branch → commit

```go
func ResolveBranch(repo, branch string) (string, error) {
    out, err := exec.Command("git", "ls-remote", "--heads", repo, branch).Output()
    // refs/heads/main -> def456abc
}
```

### commit → commit

验证 hash 是否存在（通过 `git ls-remote` 或本地 mirror）。

---

## 依赖图

```go
type Dependency struct {
    Name     string
    Ref      string
    RefType  RefType
    Commit   string
    Digest   string
    Children []*Dependency
}

type Graph struct {
    Root *Dependency
    Nodes map[string]*Dependency
}
```

### 广度优先遍历

```go
func (g *Graph) ResolveAll() error {
    queue := []*Dependency{g.Root}
    for len(queue) > 0 {
        dep := queue[0]
        queue = queue[1:]

        // 解析 refType → commit
        commit, err := ResolveRef(dep)
        if err != nil { return err }

        // 读取传递性依赖（dep 的 ngm.json / package.json）
        children, err := ParseTransitiveDeps(dep)
        for _, child := range children {
            g.Nodes[child.Name] = child
            dep.Children = append(dep.Children, child)
            queue = append(queue, child)
        }
    }
}
```

---

## 冲突检测

### 同 refType 同 ref

```
github:org/A@v1.0.0 (tag) + github:org/A@v1.0.0 (tag) → 合并
```

### 同 refType 不同 ref

```
github:org/A@v1.0.0 (tag) + github:org/A@v2.0.0 (tag) → 报错
```

### 不同 refType

```
github:org/A@v1.0.0 (tag) + github:org/A@abc123 (commit) → 报错
```

### 传递性冲突

```
project → A@v1.0.0 → B@v1.0.0
project → C@v2.0.0 → B@v2.0.0  ← 冲突！
```

---

## archiveDigest

基于**规范化内容清单**计算（定义见 [ADR-008](../adr/adr-008-archive-digest.md)），不基于 tar/zip 归档字节流。

### 清单规则

| 规则 | 处理 |
|------|------|
| 排序 | 按 path 的 UTF-8 字节序 |
| path | 仓库根相对路径，目录不单独成条 |
| mode | 仅 `100644` / `100755` / `120000` |
| blob 哈希 | 文件原始字节 sha256；symlink 为目标字符串 sha256 |
| 换行符 | 不做转换 |
| mtime / uid / gid | 不进入清单 |

### 计算

```go
func BuildManifest(tree Tree) []byte { /* NUL 分隔记录，见 ADR-008 */ }

func ComputeDigest(manifest []byte) string {
    h := sha256.Sum256(manifest)
    return "sha256:" + hex.EncodeToString(h[:])
}
```

---

## vendor 4 层

详见 [架构：vendor 4 层](../architecture/vendor-layers.md)。

| 层 | 实现要点 |
|---|---------|
| mirror | `git clone --mirror`，后续 `git fetch`；为 digest 重放提供对象库 |
| content store | 按 digest 存解包内容树（`tree/` + `meta.json`） |
| link tree | 逐文件 hardlink（目录不能 hardlink）；失败按 `linkMode` 降级复制或整目录 symlink |
| cache | metadata + OSV 响应 + tmp；可随时清空 |

---

## mappings 生成

```go
type Mapping struct {
    From  string `json:"from"`
    To    string `json:"to"`
    Main  string `json:"main,omitempty"`
    Types string `json:"types,omitempty"`
}

func GenerateMappings(graph *Graph) ([]Mapping, error) {
    var mappings []Mapping
    for _, dep := range graph.Nodes {
        mappings = append(mappings, Mapping{
            From:  dep.Name,
            To:    "./ngm.vendor/" + dep.VendorPath,
            Main:  detectEntryPoint(dep),
            Types: detectTypesFile(dep),
        })
    }
    return mappings, nil
}
```

入口推断顺序（ngm.json → package.json → index 约定）见[配置详解](../guides/configuration.md)。

---

## 成熟度

| 子模块 | 成熟度 | 备注 |
|--------|--------|------|
| URL 归一化 | done (v0.1) | 4 种协议 |
| refType 解析 | done (v0.1) | tag / branch / commit |
| 依赖图 | done (v0.1) | 传递性 + 广度优先 |
| 冲突检测 | done (v0.1) | 同 refType 不同 ref |
| 内容清单生成 | done (v0.1) | commit tree → canonical manifest |
| digest 计算 | done (v0.1) | sha256(manifest)，见 ADR-008 |
| vendor 4 层 | done (v0.1) | mirror / content / link tree / cache |
| mappings | done (v0.1) | 供外部工具读取 |

> 成熟度口径与唯一事实源[能力矩阵](../internals/capability-matrix.md)一致。

---

## 相关文档

- [P0 — ngm core](./p0-core.md)
- [P2 — 构建引擎](./p2-build.md)
- [架构：依赖解析](../architecture/dependency-resolution.md)
- [架构：锁定机制](../architecture/locking.md)
- [架构：vendor 4 层](../architecture/vendor-layers.md)
