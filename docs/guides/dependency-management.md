# 依赖管理

## 核心概念

ngm 只管理来自 Git 仓库的依赖。registry 包（npm/pnpm）交给 pnpm/npm/yarn。

---

## 添加依赖

### 基本语法

```bash
ngm add <git-url>@<ref> --ref-type <type>
```

### 示例

```bash
# tag 依赖
ngm add github:my-org/utils@v1.2.3 --ref-type tag

# branch 依赖
ngm add github:my-org/logger@main --ref-type branch

# commit 依赖
ngm add github:my-org/legacy@abc123def --ref-type commit

# monorepo 子路径
ngm add github:my-org/monorepo@v2.0.0 --ref-type tag --path=packages/core
```

`--ref-type` 必填。因为 `@main` 无法区分是 tag 还是 branch。

---

## refType 三种类型

| refType | 示例 | 适用场景 | 锁定行为 |
|---------|------|---------|---------|
| `commit` | `@abc123def` | 生产环境，绝对锁定 | 锁定该 commit |
| `tag` | `@v1.2.3` | 生产环境，语义化版本 | 锁定解析到的 commit |
| `branch` | `@main` | 开发环境，跟随最新 | 锁定解析到的 commit |

---

## 安装

```bash
ngm install
```

### 流程

```
1. 读取 ngm.json
2. 归一化 Git URL（4 种协议）
3. 解析 refType → commit
4. 生成规范化内容清单
5. 计算 archiveDigest
6. vendor 4 层落地
7. 生成 ngm.lock
8. 生成 ngm.mappings.json
```

> 术语（`refType` / `archiveDigest` / `vendor 4 层`）见[术语表](./glossary.md)。

### install 的 `--frozen-lockfile` / `--offline`（v0.2 已实现）

> **v0.1 的 `ngm install` 只有 `--dir` / `--digest`**：下面三条命令属 v0.2，现在照抄会因用法错误得到 `exit 3`。
> 两个 flag 约束的是两件不同的事：frozen 管"装什么"（不许重新决定 commit），
> offline 管"怎么拿"（不许联网）。只想要可复现性用前者即可（它仍可从网络取内容）；
> 合并使用才得到"完全离线且可复现"。

```bash
ngm install --frozen-lockfile            # 不解析新 ref、不改 lock；允许网络
ngm install --offline                    # 不访问网络，只用本地 content store / vendor
ngm install --frozen-lockfile --offline  # 完全离线可复现安装（CI 首选）
```

- `--frozen-lockfile`：lock 缺失或与 ngm.json 不一致 → 失败
- `--offline`：本地资源未命中 → 失败

（具体退出码见[可观测性 · 退出码](../architecture/observability.md)，此处不重复。）

---

## 更新

```bash
# 更新单个依赖
ngm update github:my-org/utils

# 更新所有依赖
ngm update --all

# 想换到别的 ref（例如升到 v1.3.0）：先改 ngm.json 里的 ref/refType，再 update
# 注意：ngm update 只接受依赖名，不接受 `<name>@<ref>` 写法，也没有 --ref-type
# （refType 是声明层的信息，只写在 ngm.json 里）
$EDITOR ngm.json
ngm update github:my-org/utils
```

更新后 verify 检查是否为"预期更新"。

---

## 删除

```bash
ngm remove github:my-org/utils
```

从 ngm.json 和 vendor/ 中移除。

---

## vendor 模式

### 全局缓存

```json
{
  "vendor": {
    "mode": "global"
  }
}
```

所有项目共享 `~/.ngm/global-vendor/`。省磁盘，但削弱隔离。

### 本地 vendor/（默认）

```json
{
  "vendor": {
    "mode": "local"
  }
}
```

每个项目独立的 `ngm.vendor/`。隔离清晰，CI 可复现。

### 提交 vendor/

```json
{
  "vendor": {
    "mode": "local",
    "commit": true
  }
}
```

把 `ngm.vendor/` 提交到 Git。适合离线交付、审计门禁、镜像场景。

**代价**：仓库体积膨胀、clone 时间变长、依赖更新 diff 巨大。

---

## 与 registry 包共存

ngm 只管 Git 依赖。registry 包交给 pnpm：

```json
// package.json（pnpm 管理）
{
  "dependencies": {
    "express": "^4.21.0"
  }
}

// ngm.json（ngm 管理）
{
  "dependencies": [
    { "name": "github:my-org/utils", "ref": "v1.2.3", "refType": "tag" }
  ]
}
```

构建时：

- pnpm 把 registry 包装到 `node_modules/`
- ngm 把 Git 依赖装到 `ngm.vendor/`
- ngm 生成 `ngm.mappings.json`，构建工具（Vite/esbuild/Deno）读取后把裸导入映射到 vendor 路径

---

## ngm.mappings.json

`ngm install` 后自动生成 `ngm.mappings.json`，把裸导入（如 `github:my-org/utils`）映射到 vendor 路径。

- schema 与生成规则：[P4 — 生态与协议](../modules/p4-ecosystem.md)
- 接入示例（Vite / esbuild / Deno）：[构建指南](./build.md)

---

## 传递性依赖

Git 依赖内部也可能声明自己的 Git 依赖。

ngm 递归解析：

```
project
├── ngm.json → github:org/A@v1.0.0
│   └── A/ngm.json → github:org/B@v2.0.0
│       └── B/ngm.json → github:org/C@v3.0.0
└── ngm.lock（包含所有层级）
```

**传递性依赖也进入 lock 与 verify 的范围**；OSV / `audit` 属 v0.2，届时同样覆盖传递依赖。

---

## 相关文档

- [配置详解](./configuration.md)
- [架构：依赖解析](../architecture/dependency-resolution.md)
- [架构：vendor 4 层](../architecture/vendor-layers.md)
- [架构：锁定机制](../architecture/locking.md)
