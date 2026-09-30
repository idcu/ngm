# 配置详解

ngm 的配置文件全部采用 **JSON**——有利于机器生成、schema 校验、CI diff，也能避免不同包管理器配置语法的历史差异。

| 文件 | 作用 | 位置 |
|------|------|------|
| `ngm.json` | 依赖声明（refType 必填）+ 引擎选择 + 供应链策略 + 运行时 | 项目根目录 |
| `ngm.lock` | 锁定文件：commit + archiveDigest + resolvedAt | 项目根目录，**必须提交** |
| `ngm.mappings.json` | 构建工具映射（供外部构建工具读取裸导入） | 项目根目录 |
| `ngm.engines.json` | 引擎清单（可选，覆盖内置清单） | 项目根目录或全局 `~/.ngm/` |
| `~/.ngm/config.json` | 全局配置（Git 凭证、默认引擎、权限） | 用户主目录 |

> **成熟度**：本页描述的全部字段都**已被 ngm 解析并由 `ngm config validate` 校验**（写错会被拒绝，不会被静默忽略）。
> 区别在于它们**是否生效**：
>
> - `name` / `version` / `runtime` / `dependencies` / `engines` / `vendor` —— **v0.1 已生效**
> - `supplyChain` —— **全部字段均已生效**：`allowedGitHosts` / `allowlistRepos` / `minimumReleaseAge`
>   构成真实门禁（解析阶段判定，命中即 `exit 3` + 来源链）；`osvIgnoreSeverities` 参与 `ngm audit`
>   过滤；`verifyOnLock` 在 install / update 后触发复查；`postInstallPolicy` 自 v0.3 起
>   有受控执行入口（沙箱内、仅 JS 钩子，见 [ADR-009](../adr/adr-009-supply-chain-policy.md)）。
>
> 执行依赖代码这件事的边界见[供应链防护 · postInstallPolicy](../architecture/supply-chain.md)——
> **`allow` 也只在沙箱里跑，且钩子没有写权限**。

---

## ngm.json

### 完整 schema

```json
{
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "types": "./src/index.d.ts",

  "dependencies": [
    {
      "name": "github:my-org/utils",
      "ref": "v1.2.3",
      "refType": "tag"
    },
    {
      "name": "github:my-org/logger",
      "ref": "main",
      "refType": "branch"
    },
    {
      "name": "github:my-org/legacy",
      "ref": "abc123def456",
      "refType": "commit"
    },
    {
      "name": "github:org/monorepo",
      "ref": "v2.0.0",
      "refType": "tag",
      "path": "packages/core"
    }
  ],

  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild",
    "typeCheck": "typescript",
    "css": "postcss"
  },

  "vendor": {
    "mode": "local",
    "commit": false
  },

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

### 字段说明

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 项目标识（Git URL 形式） |
| `version` | string | 是 | 项目版本 |
| `runtime` | `"node" \| "deno"` | 是 | 宿主运行时 |
| `main` | string | 否 | 入口文件（本项目作为被依赖方时，供 mappings 推断；见下"入口推断"） |
| `types` | string | 否 | 类型声明入口（同上） |
| `dependencies` | array | 是 | 依赖列表 |
| `engines` | object | 否 | 引擎选择 |
| `vendor` | object | 否 | vendor 模式配置 |
| `supplyChain` | object | 否 | 供应链策略 |

### 入口推断（作为被依赖方）

当本项目被其他仓库依赖时，mappings 的 `main` / `types` 按以下顺序推断：

1. 本项目 `ngm.json` 的 `main` / `types`
2. `package.json` 的 `exports["."]` → `main` / `types`
3. `./index.ts` / `./index.js`（类型：`./index.d.ts`）

全部缺失时 `ngm install` 输出警告，映射仍生成但不带入口字段。

---

## dependencies

### 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 依赖标识，格式 `git-host:org/repo` |
| `ref` | string | 是 | refType 必填 |
| `refType` | `"commit" \| "tag" \| "branch"` | 是 | **必填**，不允许省略 |
| `path` | string | 否 | monorepo 子路径 |

### refType 为什么必填

`@main` 无法区分是 tag 叫 main 还是 branch 叫 main。refType 让声明自描述，不依赖推断。

**推断规则**（仅在 `ngm add` 交互时辅助）：

- 匹配 `v*.*.*` 或 `*.*.*` → tag
- 匹配 7 位以上 hex → commit
- 其他 → branch

推断可能出错（branch 叫 `v1.2.3`、tag 叫 `main`），所以写入 ngm.json 时**必须显式带上 refType**。

---

## engines

### 简单写法

```json
{
  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild",
    "typeCheck": "typescript",
    "typeDecl": "typescript",
    "css": "postcss"
  }
}
```

简写 `"transform": "esbuild"` 等价于 `{"primary": "esbuild", "fallbacks": []}`——不做自动回退。

### 完整写法

```json
{
  "engines": {
    "transform": {
      "primary": "esbuild",
      "options": { "target": "es2022" }
    },
    "bundle": {
      "primary": "esbuild"
    },
    "typeCheck": {
      "primary": "typescript",
      "fallbacks": ["deno"]
    },
    "typeDecl": {
      "primary": "typescript"
    },
    "css": {
      "primary": "postcss"
    }
  }
}
```

按 `primary` → `fallbacks` 顺序尝试，失败则回退；省略 `fallbacks` 即不自动回退。

> `self` 引擎仅用于 `--dry-run` 与离线 stub，**不要**放入生产 fallbacks——引擎缺失时应显式报错，而不是静默降级为无效产物。

---

## vendor

### 三种模式

| 模式 | 配置 | 适用场景 | 代价 |
|------|------|---------|------|
| 全局缓存 | `"mode": "global"` | 节省磁盘，多项目共享 | 削弱隔离收益 |
| 本地 vendor/ | `"mode": "local"`（默认） | 隔离清晰，CI 可复现 | 跨项目重复存储 |
| 提交 vendor/ | `"mode": "local", "commit": true` | 离线交付、审计门禁、镜像 | 仓库体积膨胀 |

### linkMode（落地方式）

| 值 | 行为 |
|------|------|
| `auto`（默认） | hardlink 优先，跨卷/不支持时自动降级复制 |
| `hardlink` | 强制 hardlink，失败即报错 |
| `copy` | 普通复制（网络文件系统、提交 vendor 的保守选择） |
| `symlink` | 依赖目录整体 symlink 到 content store（Windows 需开发者模式） |

> **`auto` / `hardlink` 的就地写入风险**：这两种模式落地的是指向 content store 的硬链接，
> 因此**直接编辑 `ngm.vendor/` 里的文件会同时改写 content store**，且后续 `ngm install`
> 不会察觉（digest 目录已存在，写入被跳过）。需要就地修改依赖代码时请选择 `copy`。
> 检测路径是 `ngm verify --deep`。详见 [vendor 4 层模型 §诚实说明](../architecture/vendor-layers.md)。

```json
{
  "vendor": {
    "mode": "local",
    "linkMode": "auto"
  }
}
```

### 示例

```json
{
  "vendor": {
    "mode": "global",
    "globalPath": "~/.ngm/global-vendor"
  }
}
```

```json
{
  "vendor": {
    "mode": "local",
    "commit": true
  }
}
```

**没有普遍最优解**。构建镜像、离线交付和审计门禁才需要提交 vendor/。

---

## supplyChain

### 最小字段集

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `allowedGitHosts` | string[] | 限制可接受的 Git host | `["github.com", "gitee.com"]` |
| `allowlistRepos` | string[] | 白名单仓库（glob） | `["github.com/my-org/*"]` |
| `minimumReleaseAge` | string | 最小晾晒期：基于 commit 的 committer date（ISO 8601 duration） | `"P3D"` |
| `osvIgnoreSeverities` | string[] | OSV 忽略的严重级别 | `["LOW"]` |
| `postInstallPolicy` | `"deny" \| "prompt" \| "allow"` | postinstall 策略：`allow` 时才在**沙箱内**执行 `postinstall.js`（`prompt` 在非交互工具里等于不执行） | `"deny"` |
| `verifyOnLock` | boolean | 更新 lock 时自动 verify | `true` |

### 完整示例

```json
{
  "supplyChain": {
    "allowedGitHosts": ["github.com", "gitee.com"],
    "allowlistRepos": ["github.com/my-org/*"],
    "minimumReleaseAge": "P7D",
    "osvIgnoreSeverities": ["LOW", "MEDIUM"],
    "postInstallPolicy": "prompt",
    "verifyOnLock": true
  }
}
```

---

## 全局配置 ~/.ngm/config.json

> **成熟度**：`git` 与 `engines` 段**已生效**；`permissions` 段自 **v0.3 起已实际施加**
> （施加点与默认档位见[安全模型](../architecture/security-model.md)）。
>
> **升级注意**：`net:` 与 `run:` 的默认档位是"需配置"。升级后第一次访问远端会 `exit 3`，
> 提示里给出可以直接粘贴的那一行；把 `net:<host>`（以及要用到的 `run:<engine>`）加进
> `allow` 即可。本地 mirror 与 `file://` 路径不是网络访问，不受影响。

```json
{
  "git": {
    "defaultProtocol": "ssh",
    "tokenEnvVars": {
      "github.com": "GITHUB_TOKEN",
      "gitee.com": "GITEE_TOKEN"
    }
  },
  "engines": {
    "defaultTransform": "esbuild",
    "defaultBundle": "esbuild",
    "defaultTypeCheck": "typescript"
  },
  "permissions": {
    "allow": ["run:git", "run:esbuild", "run:tsc", "run:deno", "run:postcss", "net:github.com"],
    "deny": ["run:npm", "run:yarn", "run:pnpm", "env:GITHUB_TOKEN"]
  }
}
```

权限命名空间（`read:` / `write:` / `net:` / `run:` / `env:`）与默认值定义见[安全模型](../architecture/security-model.md)。

---

## schema 版本策略

所有配置与协议文件（`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json`）都带版本标识，遵循同一演进规则（与 [lockfileVersion](../architecture/locking.md) 一致）：

- 同一大版本内只新增可选字段，旧工具应忽略未知字段继续工作
- 破坏性变更递增大版本，并提供迁移命令（`ngm lock migrate` 等，v0.2+ 规划）
- `archiveDigest` 的清单规范版本与之绑定（见 [ADR-008](../adr/adr-008-archive-digest.md)）

---

## 校验与查询

```bash
# 校验配置是否符合 schema
ngm config validate

# 查看当前生效的配置（合并内置/全局/项目三级）
ngm config show

# 查看所有可用引擎
ngm engines list

# 查看某个引擎的元信息
ngm engines info esbuild

# 校验引擎是否可用
ngm engines validate
```

---

## 相关文档

- [依赖管理](./dependency-management.md)
- [构建](./build.md)
- [架构：信任模型](../architecture/trust-model.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [ADR-005：为什么引擎统一接口动态选择](../adr/adr-005-engine-interface.md)
